package monitor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"fnmonitor/internal/alert"
	"fnmonitor/internal/config"
	"fnmonitor/internal/metrics"
)

func TestAlertNotificationMessageCombinesMultipleEvents(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	snapshot := metrics.Snapshot{}
	snapshot.Host.Hostname = "fnos-test"

	msg := alertNotificationMessage(snapshot, []alert.Event{
		{
			Fingerprint: "metric:cpu",
			Severity:    "warning",
			Title:       "CPU 使用率异常",
			Message:     "CPU 91% 超过阈值",
			StartedAt:   now,
		},
		{
			Fingerprint: "metric:disk:/",
			Severity:    "critical",
			Title:       "磁盘空间异常",
			Message:     "/ 使用率 98% 超过阈值",
			StartedAt:   now,
		},
	}, now)

	if msg.Title != "fnmonitor 告警：2 个异常" {
		t.Fatalf("title = %q", msg.Title)
	}
	if msg.Severity != "critical" {
		t.Fatalf("severity = %q, want critical", msg.Severity)
	}
	if !strings.Contains(msg.Body, "CPU 使用率异常") || !strings.Contains(msg.Body, "磁盘空间异常") {
		t.Fatalf("combined body missing event details: %s", msg.Body)
	}
	if msg.Labels["alert_count"] != "2" {
		t.Fatalf("alert_count label = %q", msg.Labels["alert_count"])
	}
}

func TestNotificationCandidatesRequireConsecutiveTriggers(t *testing.T) {
	m := &Monitor{
		cfg: config.Config{
			Thresholds: config.ThresholdConfig{ConsecutiveTriggerCount: 3},
		},
		notified: map[string]time.Time{},
		triggers: map[string]int{},
	}
	cpu := alert.Event{Fingerprint: "metric:cpu"}
	memory := alert.Event{Fingerprint: "metric:memory"}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

	if got, _ := m.notificationCandidates([]alert.Event{cpu}, now); len(got) != 0 {
		t.Fatalf("first trigger candidates = %v, want none", got)
	}
	if got, _ := m.notificationCandidates([]alert.Event{cpu, memory}, now.Add(time.Minute)); len(got) != 0 {
		t.Fatalf("second trigger candidates = %v, want none", got)
	}
	got, _ := m.notificationCandidates([]alert.Event{cpu, memory}, now.Add(2*time.Minute))
	if len(got) != 1 || got[0].Fingerprint != cpu.Fingerprint {
		t.Fatalf("third trigger candidates = %v, want only CPU", got)
	}
	got, _ = m.notificationCandidates([]alert.Event{memory}, now.Add(3*time.Minute))
	if len(got) != 1 || got[0].Fingerprint != memory.Fingerprint {
		t.Fatalf("fourth trigger candidates = %v, want only memory", got)
	}
}

func TestNotificationCandidatesResetWhenMetricRecovers(t *testing.T) {
	m := &Monitor{
		cfg: config.Config{
			Thresholds: config.ThresholdConfig{ConsecutiveTriggerCount: 3},
		},
		notified: map[string]time.Time{},
		triggers: map[string]int{},
	}
	event := alert.Event{Fingerprint: "metric:cpu"}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

	m.notificationCandidates([]alert.Event{event}, now)
	m.notificationCandidates(nil, now.Add(time.Minute))
	for i := 0; i < 2; i++ {
		if got, _ := m.notificationCandidates([]alert.Event{event}, now.Add(time.Duration(i+2)*time.Minute)); len(got) != 0 {
			t.Fatalf("trigger %d after recovery candidates = %v, want none", i+1, got)
		}
	}
	if got, _ := m.notificationCandidates([]alert.Event{event}, now.Add(4*time.Minute)); len(got) != 1 {
		t.Fatalf("third trigger after recovery candidates = %v, want one event", got)
	}
}

func TestLifecycleNotificationMessage(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	boot := lifecycleNotificationMessage("boot", "fnos-test", now)
	if boot.Title != "fnmonitor 开机通知" || boot.Severity != "info" {
		t.Fatalf("boot notification = %#v", boot)
	}
	if boot.Labels["event"] != "boot" || boot.Labels["host"] != "fnos-test" {
		t.Fatalf("boot labels = %#v", boot.Labels)
	}
	if !strings.Contains(boot.Body, "服务已启动") {
		t.Fatalf("boot body = %q", boot.Body)
	}

	shutdown := lifecycleNotificationMessage("shutdown", "fnos-test", now)
	if shutdown.Title != "fnmonitor 关机通知" || shutdown.Labels["event"] != "shutdown" {
		t.Fatalf("shutdown notification = %#v", shutdown)
	}
	if !strings.Contains(shutdown.Body, "服务正在停止") {
		t.Fatalf("shutdown body = %q", shutdown.Body)
	}
}

func TestRecoveryTransitionsBypassCooldownAndReset(t *testing.T) {
	m := &Monitor{notified: map[string]time.Time{}, triggers: map[string]int{}}
	m.cfg.Collect.NotifyCooldown.Duration = time.Hour
	m.cfg.Thresholds.ConsecutiveTriggerCount = 2
	now := time.Now()
	cpu := alert.Event{Fingerprint: "metric:cpu", Title: "CPU 使用率异常", Message: "CPU 超过阈值"}
	memory := alert.Event{Fingerprint: "metric:memory"}
	m.notificationCandidates([]alert.Event{cpu}, now)
	pending, recovered := m.notificationCandidates(nil, now.Add(time.Second))
	if len(pending) != 0 || len(recovered) != 0 {
		t.Fatal("brief abnormality must not send recovery")
	}
	m.notificationCandidates([]alert.Event{cpu, memory}, now.Add(2*time.Second))
	pending, _ = m.notificationCandidates([]alert.Event{cpu, memory}, now.Add(3*time.Second))
	if len(pending) != 2 {
		t.Fatalf("pending = %v", pending)
	}
	pending, recovered = m.notificationCandidates([]alert.Event{memory}, now.Add(4*time.Second))
	if len(pending) != 0 || len(recovered) != 0 {
		t.Fatal("recovery sent before consecutive count")
	}
	_, recovered = m.notificationCandidates([]alert.Event{memory}, now.Add(5*time.Second))
	if len(recovered) != 1 || recovered[0].Fingerprint != cpu.Fingerprint {
		t.Fatalf("recovered = %v", recovered)
	}
	_, recovered = m.notificationCandidates([]alert.Event{memory}, now.Add(5500*time.Millisecond))
	if len(recovered) != 0 {
		t.Fatal("repeated recovery")
	}
	m.notificationCandidates([]alert.Event{cpu, memory}, now.Add(6*time.Second))
	pending, _ = m.notificationCandidates([]alert.Event{cpu, memory}, now.Add(7*time.Second))
	if len(pending) != 1 || pending[0].Fingerprint != cpu.Fingerprint {
		t.Fatalf("new incident suppressed by old cooldown: %v", pending)
	}
}

func TestRecoveryNotificationMessage(t *testing.T) {
	now := time.Now()
	snapshot := metrics.Snapshot{}
	snapshot.Host.Hostname = "fnos-test"
	msg := recoveryNotificationMessage(snapshot, []alert.Event{{Fingerprint: "metric:disk:/", Title: "磁盘空间异常", Message: "使用率 98%", Labels: map[string]string{"mountpoint": "/"}}}, now)
	if msg.Severity != "info" || msg.Labels["event"] != "recovery" || msg.Labels["fingerprint"] != "metric:disk:/" || msg.Labels["mountpoint"] != "/" {
		t.Fatalf("message = %#v", msg)
	}
	if !strings.Contains(msg.Body, "磁盘空间异常 已恢复") || !strings.Contains(msg.Body, "fnos-test") || !msg.Time.Equal(now) {
		t.Fatalf("message = %#v", msg)
	}
}

func TestRecoveryCountResetsOnAbnormalCollection(t *testing.T) {
	for _, count := range []int{1, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			m := &Monitor{notified: map[string]time.Time{}, triggers: map[string]int{}}
			m.cfg.Thresholds.ConsecutiveTriggerCount = count
			m.cfg.Collect.NotifyCooldown.Duration = time.Hour
			cpu := alert.Event{Fingerprint: "metric:cpu"}
			memory := alert.Event{Fingerprint: "metric:memory"}
			now := time.Now()
			for i := 0; i < count; i++ {
				m.notificationCandidates([]alert.Event{cpu, memory}, now)
			}
			for i := 0; i < count-1; i++ {
				_, recovered := m.notificationCandidates([]alert.Event{memory}, now)
				if len(recovered) != 0 {
					t.Fatal("early recovery")
				}
			}
			m.notificationCandidates([]alert.Event{cpu, memory}, now)
			for i := 0; i < count; i++ {
				_, recovered := m.notificationCandidates([]alert.Event{memory}, now)
				if i < count-1 && len(recovered) != 0 {
					t.Fatal("recovery counter was not reset")
				}
				if i == count-1 && (len(recovered) != 1 || recovered[0].Fingerprint != cpu.Fingerprint) {
					t.Fatalf("recovered = %v", recovered)
				}
			}
		})
	}
}
