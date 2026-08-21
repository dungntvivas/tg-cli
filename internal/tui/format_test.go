package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/user/tgchat/internal/telegram"
)

func TestFormatMessage_Outgoing(t *testing.T) {
	m := telegram.Message{
		Sender:   "You",
		Text:     "hello",
		Time:     time.Date(2026, 8, 21, 10, 25, 0, 0, time.UTC),
		Outgoing: true,
	}
	got := FormatMessage(m)
	if !strings.Contains(got, "You") {
		t.Errorf("missing sender: %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("missing text: %q", got)
	}
	if !strings.Contains(got, "10:25") {
		t.Errorf("missing time: %q", got)
	}
	// outgoing messages render with a leading marker
	if !strings.HasPrefix(strings.TrimSpace(got), "›") {
		t.Errorf("outgoing should start with '›' marker, got %q", got)
	}
}

func TestFormatMessage_Incoming(t *testing.T) {
	m := telegram.Message{
		Sender: "Alice",
		Text:   "hey",
		Time:   time.Date(2026, 8, 21, 10, 23, 0, 0, time.UTC),
	}
	got := FormatMessage(m)
	if strings.HasPrefix(strings.TrimSpace(got), "›") {
		t.Errorf("incoming should not start with '›', got %q", got)
	}
	if !strings.Contains(got, "Alice") || !strings.Contains(got, "hey") || !strings.Contains(got, "10:23") {
		t.Errorf("missing fields: %q", got)
	}
}

func TestFormatMessage_MultiLineText(t *testing.T) {
	m := telegram.Message{
		Sender: "Bob",
		Text:   "line1\nline2\nline3",
		Time:   time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC),
	}
	got := FormatMessage(m)
	for _, line := range []string{"line1", "line2", "line3"} {
		if !strings.Contains(got, line) {
			t.Errorf("missing %q in %q", line, got)
		}
	}
}

func TestFormatMessage_EmptyText(t *testing.T) {
	m := telegram.Message{Sender: "X", Time: time.Now()}
	got := FormatMessage(m)
	if got == "" {
		t.Error("empty result for empty-text message")
	}
}