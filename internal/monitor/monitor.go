package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"fnmonitor/internal/alert"
	"fnmonitor/internal/config"
	"fnmonitor/internal/logs"
	"fnmonitor/internal/metrics"
	"fnmonitor/internal/notify"
)

type State struct {
	UpdatedAt           time.Time        `json:"updated_at"`
	Snapshot            metrics.Snapshot `json:"snapshot"`
	Logs                logs.Result      `json:"logs"`
	Alerts              []alert.Event    `json:"alerts"`
	LastNotifyResults   []notify.Result  `json:"last_notify_results,omitempty"`
	LastCollectionError string           `json:"last_collection_error,omitempty"`
}

type Monitor struct {
	cfg       config.Config
	collector *metrics.Collector
	logs      *logs.Reader
	notifier  *notify.Manager

	mu       sync.RWMutex
	state    State
	notified map[string]time.Time
	triggers map[string]int
}

func New(cfg config.Config) (*Monitor, error) {
	logReader, err := logs.New(cfg.Logs)
	if err != nil {
		return nil, err
	}
	return &Monitor{
		cfg:       cfg,
		collector: metrics.New(cfg.Collect),
		logs:      logReader,
		notifier:  notify.New(cfg.Notify),
		notified:  map[string]time.Time{},
		triggers:  map[string]int{},
	}, nil
}

func (m *Monitor) Run(ctx context.Context) {
	m.tickAndLog(ctx)

	ticker := time.NewTicker(m.cfg.Collect.Interval.Duration)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.tickAndLog(ctx)
		}
	}
}

func (m *Monitor) Tick(ctx context.Context) error {
	return m.tick(ctx, true)
}

func (m *Monitor) CollectOnce(ctx context.Context) error {
	return m.tick(ctx, false)
}

func (m *Monitor) tick(ctx context.Context, sendNotifications bool) error {
	timeout := m.cfg.Collect.Interval.Duration
	if timeout < 10*time.Second {
		timeout = 10 * time.Second
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	snapshot, err := m.collector.Snapshot(tctx)
	logResult := m.logs.Recent(tctx)
	events := alert.Evaluate(m.cfg.Thresholds, snapshot, logResult)
	var notifyResults []notify.Result
	if sendNotifications {
		notifyResults = m.notifyNewAlerts(tctx, snapshot, events)
	}

	state := State{
		UpdatedAt:         time.Now(),
		Snapshot:          snapshot,
		Logs:              logResult,
		Alerts:            events,
		LastNotifyResults: notifyResults,
	}
	if err != nil {
		state.LastCollectionError = err.Error()
	}

	m.mu.Lock()
	m.state = state
	m.mu.Unlock()
	return err
}

func (m *Monitor) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Monitor) SendTest(ctx context.Context) []notify.Result {
	msg := notify.Message{
		Title:    "fnmonitor 测试通知",
		Body:     "这是一条 fnmonitor 测试通知。收到它说明通知渠道可以正常发送。",
		Severity: "info",
		Labels:   map[string]string{"source": "manual_test"},
		Time:     time.Now(),
	}
	return m.notifier.Send(ctx, msg)
}

func (m *Monitor) SendBootNotification(ctx context.Context) []notify.Result {
	if !m.cfg.Notify.BootEnabled {
		return nil
	}
	return m.notifier.Send(ctx, lifecycleNotificationMessage("boot", hostname(), time.Now()))
}

func (m *Monitor) SendShutdownNotification(ctx context.Context) []notify.Result {
	if !m.cfg.Notify.ShutdownEnabled {
		return nil
	}
	return m.notifier.Send(ctx, lifecycleNotificationMessage("shutdown", hostname(), time.Now()))
}

func (m *Monitor) tickAndLog(ctx context.Context) {
	if err := m.Tick(ctx); err != nil {
		slog.Warn("monitor tick failed", "error", err)
	}
	state := m.State()
	slog.Info("monitor tick completed",
		"alerts", len(state.Alerts),
		"log_warnings", len(state.Logs.Warnings),
		"metric_warnings", len(state.Snapshot.Warnings),
	)
}

func (m *Monitor) notifyNewAlerts(ctx context.Context, snapshot metrics.Snapshot, events []alert.Event) []notify.Result {
	now := time.Now()
	pendingEvents := m.notificationCandidates(events, now)

	if len(pendingEvents) == 0 {
		return nil
	}
	msg := alertNotificationMessage(snapshot, pendingEvents, now)
	return m.notifier.Send(ctx, msg)
}

// notificationCandidates returns alerts which have breached their thresholds for
// the configured number of consecutive collections and are outside the cooldown.
// Counters are kept per fingerprint, so one metric recovering does not affect
// another active metric.
func (m *Monitor) notificationCandidates(events []alert.Event, now time.Time) []alert.Event {
	triggerCount := m.cfg.Thresholds.ConsecutiveTriggerCount
	if triggerCount < 1 {
		triggerCount = 1
	}
	cooldown := m.cfg.Collect.NotifyCooldown.Duration

	m.mu.Lock()
	defer m.mu.Unlock()

	active := make(map[string]struct{}, len(events))
	pendingEvents := make([]alert.Event, 0, len(events))
	for _, event := range events {
		if _, seen := active[event.Fingerprint]; seen {
			continue
		}
		active[event.Fingerprint] = struct{}{}
		m.triggers[event.Fingerprint]++
		if m.triggers[event.Fingerprint] < triggerCount {
			continue
		}
		if last, ok := m.notified[event.Fingerprint]; ok && cooldown > 0 && now.Sub(last) < cooldown {
			continue
		}
		m.notified[event.Fingerprint] = now
		pendingEvents = append(pendingEvents, event)
	}

	for fingerprint := range m.triggers {
		if _, stillActive := active[fingerprint]; !stillActive {
			delete(m.triggers, fingerprint)
		}
	}
	return pendingEvents
}

func alertNotificationMessage(snapshot metrics.Snapshot, events []alert.Event, now time.Time) notify.Message {
	if len(events) == 1 {
		event := events[0]
		return notify.Message{
			Title:    event.Title,
			Body:     alertBody(snapshot, event),
			Severity: event.Severity,
			Labels:   mergeLabels(event.Labels, map[string]string{"fingerprint": event.Fingerprint, "host": snapshot.Host.Hostname}),
			Time:     now,
		}
	}

	return notify.Message{
		Title:    fmt.Sprintf("fnmonitor 告警：%d 个异常", len(events)),
		Body:     combinedAlertBody(snapshot, events),
		Severity: combinedSeverity(events),
		Labels: map[string]string{
			"host":        snapshot.Host.Hostname,
			"alert_count": fmt.Sprint(len(events)),
		},
		Time: now,
	}
}

func alertBody(snapshot metrics.Snapshot, event alert.Event) string {
	parts := []string{
		event.Message,
		"",
		"Host: " + snapshot.Host.Hostname,
		"Severity: " + event.Severity,
		"Fingerprint: " + event.Fingerprint,
		"Time: " + event.StartedAt.Format(time.RFC3339),
	}
	if snapshot.Host.Platform != "" {
		parts = append(parts, "Platform: "+strings.TrimSpace(snapshot.Host.Platform+" "+snapshot.Host.PlatformVersion))
	}
	if len(event.Labels) > 0 {
		parts = append(parts, "Labels:")
		for k, v := range event.Labels {
			parts = append(parts, fmt.Sprintf("- %s=%s", k, v))
		}
	}
	return strings.Join(parts, "\n")
}

func combinedAlertBody(snapshot metrics.Snapshot, events []alert.Event) string {
	parts := []string{
		fmt.Sprintf("本轮采集发现 %d 个异常。", len(events)),
		"",
		"Host: " + snapshot.Host.Hostname,
	}
	if snapshot.Host.Platform != "" {
		parts = append(parts, "Platform: "+strings.TrimSpace(snapshot.Host.Platform+" "+snapshot.Host.PlatformVersion))
	}
	parts = append(parts, "")
	for i, event := range events {
		parts = append(parts,
			fmt.Sprintf("%d. [%s] %s", i+1, event.Severity, event.Title),
			"   "+event.Message,
			"   Fingerprint: "+event.Fingerprint,
			"   Time: "+event.StartedAt.Format(time.RFC3339),
		)
		if len(event.Labels) > 0 {
			var labels []string
			for k, v := range event.Labels {
				labels = append(labels, fmt.Sprintf("%s=%s", k, v))
			}
			parts = append(parts, "   Labels: "+strings.Join(labels, ", "))
		}
	}
	return strings.Join(parts, "\n")
}

func combinedSeverity(events []alert.Event) string {
	for _, event := range events {
		if event.Severity == "critical" {
			return "critical"
		}
	}
	return "warning"
}

func mergeLabels(a map[string]string, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func lifecycleNotificationMessage(event, host string, now time.Time) notify.Message {
	title := "fnmonitor 开机通知"
	body := "fnmonitor 服务已启动。"
	if event == "shutdown" {
		title = "fnmonitor 关机通知"
		body = "fnmonitor 服务正在停止。"
	}
	labels := map[string]string{"event": event}
	if host != "" {
		labels["host"] = host
		body += "\nHost: " + host
	}
	return notify.Message{
		Title:    title,
		Body:     body,
		Severity: "info",
		Labels:   labels,
		Time:     now,
	}
}

func hostname() string {
	host, err := os.Hostname()
	if err != nil {
		return ""
	}
	return host
}
