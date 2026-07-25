package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConsecutiveTriggerCount(t *testing.T) {
	if got := Default().Thresholds.ConsecutiveTriggerCount; got != 1 {
		t.Fatalf("default consecutive trigger count = %d, want 1", got)
	}
}

func TestValidateRejectsNonPositiveConsecutiveTriggerCount(t *testing.T) {
	cfg := Default()
	cfg.Thresholds.ConsecutiveTriggerCount = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}

func TestExampleConfigLoads(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatalf("Load(config.example.yaml) error = %v", err)
	}
	if got := cfg.Thresholds.ConsecutiveTriggerCount; got != 1 {
		t.Fatalf("example consecutive trigger count = %d, want 1", got)
	}
}

func TestLoadKeepsDefaultConsecutiveTriggerCountWhenOmitted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("thresholds:\n  cpu_percent: 80\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Thresholds.ConsecutiveTriggerCount; got != 1 {
		t.Fatalf("consecutive trigger count = %d, want default 1", got)
	}
}
