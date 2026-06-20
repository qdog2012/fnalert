package logs

import (
	"testing"

	"fnmonitor/internal/config"
)

func TestEntryMatchesWarningErrorLevels(t *testing.T) {
	reader, err := New(config.LogsConfig{})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		line  string
		level string
	}{
		{name: "english warning", line: "2026-06-20 app warning disk is warm", level: "warning"},
		{name: "english error", line: "2026-06-20 app error failed to open disk", level: "error"},
		{name: "chinese warning", line: "2026-06-20 飞牛日志 等级=警告 磁盘空间偏高", level: "warning"},
		{name: "chinese error", line: "2026-06-20 飞牛日志 等级=错误 服务启动失败", level: "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := reader.entry("test", tt.line)
			if !entry.Matched {
				t.Fatalf("expected entry to be matched")
			}
			if entry.Level != tt.level {
				t.Fatalf("level = %q, want %q", entry.Level, tt.level)
			}
		})
	}
}

func TestJournalPriorityMatchesWarningAndError(t *testing.T) {
	if got := journalPriorityLevel("4"); got != "warning" {
		t.Fatalf("priority 4 = %q, want warning", got)
	}
	if got := journalPriorityLevel("3"); got != "error" {
		t.Fatalf("priority 3 = %q, want error", got)
	}
	if got := journalPriorityLevel("6"); got != "info" {
		t.Fatalf("priority 6 = %q, want info", got)
	}
}

func TestNotifyMinLevelFiltersMatches(t *testing.T) {
	tests := []struct {
		name     string
		minLevel string
		line     string
		matched  bool
	}{
		{name: "warning matches warning", minLevel: "warning", line: "warning disk high", matched: true},
		{name: "error excludes warning", minLevel: "error", line: "warning disk high", matched: false},
		{name: "error matches error", minLevel: "error", line: "error failed to open disk", matched: true},
		{name: "critical excludes error", minLevel: "critical", line: "error failed to open disk", matched: false},
		{name: "critical matches critical", minLevel: "critical", line: "critical panic", matched: true},
		{name: "none disables level and pattern", minLevel: "none", line: "error failed to open disk", matched: false},
		{name: "info matches normal info", minLevel: "info", line: "service started", matched: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, err := New(config.LogsConfig{
				NotifyMinLevel: tt.minLevel,
				ErrorPatterns:  []string{`(?i)\bfailed\b`},
			})
			if err != nil {
				t.Fatal(err)
			}
			entry := reader.entry("test", tt.line)
			if entry.Matched != tt.matched {
				t.Fatalf("matched = %v, want %v; level=%s", entry.Matched, tt.matched, entry.Level)
			}
		})
	}
}
