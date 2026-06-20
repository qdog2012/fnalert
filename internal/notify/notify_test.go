package notify

import (
	"strings"
	"testing"
	"time"
)

func TestSMTPMessageUsesFromName(t *testing.T) {
	msg := Message{
		Title: "test",
		Body:  "body",
		Time:  time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC),
	}
	raw := smtpMessage("monitor@example.com", "FN Monitor", []string{"admin@example.com"}, msg)
	if !strings.Contains(raw, "From: \"FN Monitor\" <monitor@example.com>") {
		t.Fatalf("From header missing display name:\n%s", raw)
	}
}
