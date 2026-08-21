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
	got := FormatMessage(m, 80)
	if !strings.Contains(got, "You") {
		t.Errorf("missing sender: %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("missing text: %q", got)
	}
	// timestamps are no longer rendered
	if strings.Contains(got, "10:25") {
		t.Errorf("outgoing should not render timestamp: %q", got)
	}
	// outgoing messages render with a leading marker
	if !strings.HasPrefix(strings.TrimSpace(stripANSI(got)), "›") {
		t.Errorf("outgoing should start with '›' marker, got %q", got)
	}
}

func TestFormatMessage_Incoming(t *testing.T) {
	m := telegram.Message{
		Sender: "Alice",
		Text:   "hey",
		Time:   time.Date(2026, 8, 21, 10, 23, 0, 0, time.UTC),
	}
	got := FormatMessage(m, 80)
	if strings.HasPrefix(strings.TrimSpace(got), "›") {
		t.Errorf("incoming should not start with '›', got %q", got)
	}
	if !strings.Contains(got, "Alice") || !strings.Contains(got, "hey") {
		t.Errorf("missing fields: %q", got)
	}
	if strings.Contains(got, "10:23") {
		t.Errorf("incoming should not render timestamp: %q", got)
	}
}

func TestFormatMessage_MultiLineText(t *testing.T) {
	m := telegram.Message{
		Sender: "Bob",
		Text:   "line1\nline2\nline3",
		Time:   time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC),
	}
	got := FormatMessage(m, 80)
	for _, line := range []string{"line1", "line2", "line3"} {
		if !strings.Contains(got, line) {
			t.Errorf("missing %q in %q", line, got)
		}
	}
}

func TestFormatMessage_EmptyText(t *testing.T) {
	m := telegram.Message{Sender: "X", Time: time.Now()}
	got := FormatMessage(m, 80)
	if got == "" {
		t.Error("empty result for empty-text message")
	}
}

func TestFormatMessage_OutgoingRightAligned(t *testing.T) {
	// Outgoing text shorter than the pane width must end at the right column
	// (i.e. the visible part of the line equals `width`).
	m := telegram.Message{
		Sender: "You", Text: "hi", Outgoing: true,
		Time: time.Now(),
	}
	const width = 20
	got := FormatMessage(m, width)
	for _, line := range strings.Split(got, "\n") {
		if visibleWidth(line) != width {
			t.Errorf("outgoing line not right-aligned: visible=%d, want=%d, line=%q",
				visibleWidth(line), width, line)
		}
	}
}

func TestFormatMessage_OutgoingLongLineWraps(t *testing.T) {
	// A long outgoing line should be wrapped, with EACH wrapped piece
	// right-aligned (so continuation lines stay flush against the right edge).
	m := telegram.Message{
		Sender: "You",
		Text:   strings.Repeat("x", 50), // > 20
		Outgoing: true,
		Time:    time.Now(),
	}
	const width = 20
	got := FormatMessage(m, width)
	lines := strings.Split(got, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected header + at least 2 wrapped lines, got %d: %q", len(lines), got)
	}
	for i, line := range lines {
		if visibleWidth(line) != width {
			t.Errorf("line %d not right-aligned: visible=%d, want=%d, line=%q",
				i, visibleWidth(line), width, line)
		}
	}
}

func TestFormatMessage_OutgoingWidthZeroFallback(t *testing.T) {
	// When width is unknown (e.g. before layout), still produce valid text —
	// no panic, no double-padding.
	m := telegram.Message{Sender: "You", Text: "hi", Outgoing: true, Time: time.Now()}
	got := FormatMessage(m, 0)
	if !strings.Contains(got, "hi") || !strings.Contains(got, "You") {
		t.Errorf("missing content in width=0 fallback: %q", got)
	}
}

func TestVisibleWidth_IgnoresEscapes(t *testing.T) {
	if got := visibleWidth("\033[35mhello\033[0m"); got != 5 {
		t.Errorf("visibleWidth = %d, want 5", got)
	}
}

func TestRightPad_NoOpWhenTooLong(t *testing.T) {
	got := rightPad("hello world", 5)
	if got != "hello world" {
		t.Errorf("rightPad should not truncate or pad when too long: %q", got)
	}
}

// stripANSI removes CSI escape sequences for substring assertions.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		if r == 0x1b {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' || r == 'K' {
				inEscape = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}