package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/user/tgchat/internal/telegram"
)

func TestRenderHistory_OrdersAndSeparates(t *testing.T) {
	msgs := []telegram.Message{
		{Sender: "Alice", Text: "hi", Time: time.Date(2026, 8, 21, 10, 23, 0, 0, time.UTC)},
		{Sender: "You", Text: "hello", Time: time.Date(2026, 8, 21, 10, 25, 0, 0, time.UTC), Outgoing: true},
		{Sender: "Alice", Text: "coffee?", Time: time.Date(2026, 8, 21, 10, 26, 0, 0, time.UTC)},
	}
	out := RenderHistory(msgs, 80)
	idxAlice := strings.Index(out, "Alice")
	idxYou := strings.Index(out, "You")
	idxCoffee := strings.Index(out, "coffee?")
	if idxAlice < 0 || idxYou < 0 || idxCoffee < 0 {
		t.Fatalf("missing fields in %q", out)
	}
	if !(idxAlice < idxYou && idxYou < idxCoffee) {
		t.Errorf("messages out of order in %q", out)
	}
}

func TestRenderHistory_Empty(t *testing.T) {
	out := RenderHistory(nil, 80)
	if !strings.Contains(out, "(no messages)") {
		t.Errorf("empty history should show placeholder, got %q", out)
	}
}

func TestRenderHistory_UsesFormatMessage(t *testing.T) {
	msgs := []telegram.Message{
		{Sender: "Bob", Text: "yo", Time: time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)},
	}
	out := RenderHistory(msgs, 80)
	if !strings.Contains(out, "Bob") || !strings.Contains(out, "yo") {
		t.Errorf("missing fields: %q", out)
	}
	if strings.Contains(out, "09:00") {
		t.Errorf("history should not render timestamps: %q", out)
	}
}