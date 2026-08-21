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
	if strings.Contains(got, "10:25") {
		t.Errorf("outgoing should not render timestamp: %q", got)
	}
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
	m := telegram.Message{
		Sender: "You", Text: "hi", Outgoing: true,
		Time: time.Now(),
	}
	const width = 20
	got := FormatMessage(m, width)
	for _, line := range strings.Split(got, "\n") {
		if displayWidth(line) != width {
			t.Errorf("outgoing line not right-aligned: visible=%d, want=%d, line=%q",
				displayWidth(line), width, line)
		}
	}
}

func TestFormatMessage_OutgoingLongLineWraps(t *testing.T) {
	m := telegram.Message{
		Sender:    "You",
		Text:      strings.Repeat("x", 50),
		Outgoing:  true,
		Time:      time.Now(),
	}
	const width = 20
	got := FormatMessage(m, width)
	lines := strings.Split(got, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected header + at least 2 wrapped lines, got %d: %q", len(lines), got)
	}
	for i, line := range lines {
		if displayWidth(line) != width {
			t.Errorf("line %d not right-aligned: visible=%d, want=%d, line=%q",
				i, displayWidth(line), width, line)
		}
	}
}

func TestFormatMessage_OutgoingWidthZeroFallback(t *testing.T) {
	m := telegram.Message{Sender: "You", Text: "hi", Outgoing: true, Time: time.Now()}
	got := FormatMessage(m, 0)
	if !strings.Contains(got, "hi") || !strings.Contains(got, "You") {
		t.Errorf("missing content in width=0 fallback: %q", got)
	}
}

// TestFormatMessage_OutgoingUserNewlineIsHardBreak: a user-supplied newline
// must start a fresh wrapped segment — wrap must NEVER cut across a '\n'.
// Regression for the bug where outgoing messages in Saved Messages showed
// garbage characters because wrapGraphemes treated '\n' as a 1-cell char and
// split mid-line across the newline.
func TestFormatMessage_OutgoingUserNewlineIsHardBreak(t *testing.T) {
	m := telegram.Message{
		Sender:   "You",
		Text:     "first line\nsecond line\nthird line",
		Outgoing: true,
		Time:     time.Now(),
	}
	const width = 80
	got := FormatMessage(m, width)
	stripped := stripANSI(got)
	for _, want := range []string{"first line", "second line", "third line"} {
		if !strings.Contains(stripped, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	// No wrapped line should be a CONTIGUOUS slice that crosses a user \n.
	// Walk every visible line and assert it does not contain the literal
	// sequence "line\nsecond" or "line\nthird" — i.e. wrap never glued two
	// user-lines into one.
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "line\nsecond") || strings.Contains(line, "line\nthird") {
			t.Errorf("user newline was wrapped across: %q", line)
		}
	}
}

func TestDisplayWidth_IgnoresEscapes(t *testing.T) {
	if got := displayWidth("\033[35mhello\033[0m"); got != 5 {
		t.Errorf("displayWidth = %d, want 5", got)
	}
}

func TestDisplayWidth_CountsWideCharsAs2(t *testing.T) {
	// CJK chars are 2 cells each. displayWidth must match what the terminal
	// renders, otherwise right-alignment and wrap are off by one cell per
	// wide char and we get garbled output for messages containing them.
	if got := displayWidth("你好"); got != 4 {
		t.Errorf("displayWidth(\"你好\") = %d, want 4 (2 cells per CJK char)", got)
	}
	if got := displayWidth("👋hi"); got != 4 { // emoji 2 + h 1 + i 1
		t.Errorf("displayWidth(\"👋hi\") = %d, want 4", got)
	}
}

func TestRightPad_NoOpWhenTooLong(t *testing.T) {
	got := rightPad("hello world", 5)
	if got != "hello world" {
		t.Errorf("rightPad should not truncate or pad when too long: %q", got)
	}
}

// TestWrapGraphemes_PreservesUTF8: regression for any byte-boundary splits.
// Iterating by grapheme cluster (not bytes) keeps emoji + combining marks
// intact across wrap boundaries.
func TestWrapGraphemes_PreservesUTF8(t *testing.T) {
	// 5 emoji, each 2 cells → 10 cells. Width 4 → wrap every ~2 emoji.
	got := wrapGraphemes("👋👋👋👋👋", 4)
	for _, line := range got {
		if displayWidth(line) > 4 {
			t.Errorf("wrapped line wider than requested: %q (width %d)", line, displayWidth(line))
		}
	}
	// Reconstructed output must equal the original (no missing graphemes).
	if strings.Join(got, "") != "👋👋👋👋👋" {
		t.Errorf("wrapGraphemes lost or duplicated graphemes: %q", got)
	}
}

// stripANSI is a thin wrapper around the exported StripANSI for in-test
// readability. They share an implementation.
func stripANSI(s string) string { return StripANSI(s) }

func TestStripANSI(t *testing.T) {
	cases := []struct{ in, want string }{
		{"\033[35mhello\033[0m", "hello"},
		{"plain text", "plain text"},
		{"\033[1;31mbold red\033[0m normal", "bold red normal"},
		{"", ""},
		{"\033[K", ""},
	}
	for _, c := range cases {
		if got := StripANSI(c.in); got != c.want {
			t.Errorf("StripANSI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}