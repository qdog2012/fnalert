package monitor

import (
	"strings"
	"testing"
	"time"

	"fnmonitor/internal/alert"
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
