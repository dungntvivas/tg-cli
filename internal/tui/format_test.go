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
	lines := strings.Split(got, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header + body line, got %d: %q", len(lines), got)
	}
	if w := displayWidth(stripANSI(lines[0])); w != senderColWidth {
		t.Errorf("sender column width = %d, want %d, line=%q", w, senderColWidth, lines[0])
	}
	if w := displayWidth(stripANSI(lines[1])); w != senderColWidth+len("hello") {
		t.Errorf("body line width = %d, want %d, line=%q", w, senderColWidth+len("hello"), lines[1])
	}
}

func TestFormatMessage_Incoming(t *testing.T) {
	m := telegram.Message{
		Sender: "Alice",
		Text:   "hey",
		Time:   time.Date(2026, 8, 21, 10, 23, 0, 0, time.UTC),
	}
	got := FormatMessage(m, 80)
	if !strings.Contains(got, "Alice") || !strings.Contains(got, "hey") {
		t.Errorf("missing fields: %q", got)
	}
	if strings.Contains(got, "10:23") {
		t.Errorf("incoming should not render timestamp: %q", got)
	}
	lines := strings.Split(got, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header + body line, got %d: %q", len(lines), got)
	}
	if w := displayWidth(stripANSI(lines[0])); w != senderColWidth {
		t.Errorf("sender column width = %d, want %d, line=%q", w, senderColWidth, lines[0])
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

// TestFormatMessage_OutgoingLongLineWraps: a 50-char body in a 20-cell
// pane (sender col=10, body wraps at 10) → header line + 5 body lines.
// Header line is 10 cells; body lines are blank-col + 10 cells = 20 cells
// (matches the pane width).
func TestFormatMessage_OutgoingLongLineWraps(t *testing.T) {
	m := telegram.Message{
		Sender:   "You",
		Text:     strings.Repeat("x", 50),
		Outgoing: true,
		Time:     time.Now(),
	}
	const width = 20
	got := FormatMessage(m, width)
	lines := strings.Split(got, "\n")
	if len(lines) < 6 {
		t.Fatalf("expected header + 5 wrapped lines, got %d: %q", len(lines), got)
	}
	if w := displayWidth(stripANSI(lines[0])); w != senderColWidth {
		t.Errorf("header line width = %d, want %d", w, senderColWidth)
	}
	for i, line := range lines[1:] {
		if w := displayWidth(line); w != width-senderColWidth+senderColWidth {
			// body line = blankCol (10) + body wrap (10) = 20 = width
			t.Errorf("body line %d width = %d, want %d", i+1, w, width)
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
	// Continuation lines start with the blank sender column (10 spaces).
	// No wrapped line should contain a literal "\nsecond" or "---" — i.e.
	// wrap never glued two user-lines into one.
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "line\nsecond") || strings.Contains(line, "line\nthird") {
			t.Errorf("user newline was wrapped across: %q", line)
		}
	}
}

// TestFormatMessage_SenderColumnFixed10: every message's first line must be
// exactly senderColWidth cells — keeps the body column aligned across rows.
func TestFormatMessage_SenderColumnFixed10(t *testing.T) {
	for _, name := range []string{"Al", "Alice", "Bob", "Christopher"} {
		got := FormatMessage(telegram.Message{Sender: name, Text: "x"}, 40)
		first := strings.Split(got, "\n")[0]
		if w := displayWidth(stripANSI(first)); w != senderColWidth {
			t.Errorf("sender %q: header width = %d, want %d", name, w, senderColWidth)
		}
	}
}

// TestFormatMessage_SenderTruncatesWithEllipsis: a sender name longer than
// the column width must be truncated to fit with an ellipsis. Truncation
// must respect display cells (so a CJK char isn't cut mid-glyph).
func TestFormatMessage_SenderTruncatesWithEllipsis(t *testing.T) {
	m := telegram.Message{Sender: "VeryLongNameIndeed", Text: "x"}
	got := FormatMessage(m, 40)
	first := stripANSI(strings.Split(got, "\n")[0])
	if w := displayWidth(first); w != senderColWidth {
		t.Errorf("truncated header width = %d, want %d", w, senderColWidth)
	}
	if !strings.Contains(first, "…") {
		t.Errorf("long sender not truncated with ellipsis: %q", first)
	}
	// 8 visible chars + ellipsis + trailing space = 10 cells.
	if !strings.HasSuffix(first, "… ") {
		t.Errorf("expected truncated + ellipsis + space, got %q", first)
	}
}

// TestFormatMessage_ContinuationAlignedToBody: lines after the first
// (continuations of a wrapped or user-newline-split message) must start
// with the blank sender column so the body column stays aligned.
func TestFormatMessage_ContinuationAlignedToBody(t *testing.T) {
	m := telegram.Message{Sender: "Alice", Text: "line one\nline two"}
	got := FormatMessage(m, 80)
	lines := strings.Split(got, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected header + 2 body lines, got %d: %q", len(lines), got)
	}
	blankCol := strings.Repeat(" ", senderColWidth)
	for i, line := range lines[1:] {
		if !strings.HasPrefix(line, blankCol) {
			t.Errorf("body line %d missing blank sender column: %q", i+1, line)
		}
	}
	if lines[1] != blankCol+"line one" {
		t.Errorf("body line 1 = %q", lines[1])
	}
	if lines[2] != blankCol+"line two" {
		t.Errorf("body line 2 = %q", lines[2])
	}
}

// TestFormatMessage_OutgoingUsesAccentColor: outgoing "You" must be colored
// magenta so the user's own messages stand out in the thread without
// changing the alignment. Uses tview markup — SetDynamicColors(true) doesn't
// parse raw ANSI escapes; an unparsed ESC byte leaves "[35m" as literal text.
func TestFormatMessage_OutgoingUsesAccentColor(t *testing.T) {
	got := FormatMessage(telegram.Message{Sender: "You", Text: "x", Outgoing: true}, 40)
	if !strings.Contains(got, "[magenta]") {
		t.Errorf("outgoing sender should use [magenta] markup, got %q", got)
	}
	if strings.Contains(got, "\033[35m") {
		t.Errorf("raw ANSI must not be emitted (tview would show literal [35m): %q", got)
	}
}

// TestFormatMessage_NoLiteralAnsiLeak guards the bug we just fixed: even
// though we never emit raw ANSI, defense in depth — if someone reintroduces
// it (e.g. via a stray fmt.Sprintf("\x1b[%dm")), this catches it.
func TestFormatMessage_NoLiteralAnsiLeak(t *testing.T) {
	for _, m := range []telegram.Message{
		{Sender: "Alice", Text: "hi"},
		{Sender: "You", Text: "hi", Outgoing: true},
		{Sender: "LongSender", Text: "x"},
	} {
		got := FormatMessage(m, 60)
		if strings.Contains(got, "\x1b[") {
			t.Errorf("FormatMessage emitted raw ANSI escape: %q", got)
		}
	}
}

func TestDisplayWidth_IgnoresEscapes(t *testing.T) {
	if got := displayWidth("\033[35mhello\033[0m"); got != 5 {
		t.Errorf("displayWidth = %d, want 5", got)
	}
}

// TestDisplayWidth_IgnoresTviewMarkup proves the switch from raw ANSI to
// tview markup didn't change cell counts — markup like `[cyan]` must count
// as 0 cells just like the CSI sequences it replaces.
func TestDisplayWidth_IgnoresTviewMarkup(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"[cyan]hello[-]", 5},
		{"[magenta]You[-]", 3},
		{"[#ff0000]hi[-]", 2},
		{`["sel"]hi[""]`, 2},
		{"plain", 5},
	}
	for _, c := range cases {
		if got := displayWidth(c.in); got != c.want {
			t.Errorf("displayWidth(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestDisplayWidth_LiteralBracketNotMisread: a real `[` in chat content
// (followed by non-tag chars) must count as a visible character, not be
// eaten as markup. Guards against over-eager tag detection.
func TestDisplayWidth_LiteralBracketNotMisread(t *testing.T) {
	if got := displayWidth("[abc]"); got != 5 {
		t.Errorf("displayWidth(\"[abc]\") = %d, want 5 (all visible)", got)
	}
	// Reset marker alone — exactly the format.go resetMarkup shape.
	if got := displayWidth("[-]"); got != 0 {
		t.Errorf("displayWidth(\"[-]\") = %d, want 0 (reset tag has no width)", got)
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
			t.Errorf("wrapped line wider than than: %q (width %d)", line, displayWidth(line))
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
		// Tview markup — same function now strips both so callers don't
		// have to chain cleanup calls before clipboard copy.
		{"[cyan]hello[-]", "hello"},
		{"[magenta]You[-] [cyan]Alice[-]", "You Alice"},
		{`["sel"]BCD[""]`, "BCD"},
		{"[#ff0000]hi[-] plain", "hi plain"},
	}
	for _, c := range cases {
		if got := StripANSI(c.in); got != c.want {
			t.Errorf("StripANSI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestApplySelection_SingleLine covers a single-line, ANSI-padded selection.
// The region must wrap the visible cells only — escape sequences must stay
// outside the region so the highlight aligns with what the terminal draws.
//
// Cell coordinates are 1-based and the range is half-open [from, to): from=2,
// to=5 means cells 2,3,4 are selected.
func TestApplySelection_SingleLine(t *testing.T) {
	in := "\033[35mABCDE\033[0m"
	out := ApplySelection(in, 1, 2, 1, 5)
	want := "\033[35mA[\"sel\"]BCD[\"\"]E\033[0m"
	if out != want {
		t.Errorf("ApplySelection single-line = %q, want %q", out, want)
	}
}

// TestApplySelection_MultiLine covers selection that crosses a newline. Cells
// 2,3 of line 1 ("bc") plus cell 1 of line 2 ("d").
func TestApplySelection_MultiLine(t *testing.T) {
	in := "abc\ndef"
	out := ApplySelection(in, 1, 2, 2, 2)
	want := "a[\"sel\"]bc[\"\"]\n[\"sel\"]d[\"\"]ef"
	if out != want {
		t.Errorf("ApplySelection multi-line = %q, want %q", out, want)
	}
}

// TestApplySelection_Normalized ensures reversed start/end coordinates still
// produce a valid (forward) selection.
func TestApplySelection_Normalized(t *testing.T) {
	in := "abcdef"
	// Reversed: (1,5)..(1,3) should normalize to (1,3)..(1,5) → cells 3,4 → "cd".
	out := ApplySelection(in, 1, 5, 1, 3)
	want := "ab[\"sel\"]cd[\"\"]ef"
	if out != want {
		t.Errorf("ApplySelection reversed = %q, want %q", out, want)
	}
}

// TestApplySelection_EmptyReturnsUnchanged guards against emitting empty
// region tags when the range collapses to a single cell.
func TestApplySelection_EmptyReturnsUnchanged(t *testing.T) {
	in := "abcdef"
	if got := ApplySelection(in, 1, 3, 1, 3); got != in {
		t.Errorf("empty range modified text: %q", got)
	}
}

// TestApplySelection_AcrossEscape proves escapes don't shift cell positions.
// The escape sequence should not count toward the cell offset.
func TestApplySelection_AcrossEscape(t *testing.T) {
	// Line: ESC[31m foo ESC[0m bar  → visible cells: " foo  bar" (9 cells).
	in := "\033[31m foo \033[0m bar"
	// Select cells 2..6 → "foo " (with trailing space).
	out := ApplySelection(in, 1, 2, 1, 6)
	if !strings.Contains(out, `["sel"]`) {
		t.Fatalf("no region tag: %q", out)
	}
	// Re-extract and verify what the user "selected".
	got := ExtractSelection(in, 1, 2, 1, 6)
	if StripANSI(got) != "foo " {
		t.Errorf("ExtractSelection = %q (stripped %q), want %q", got, StripANSI(got), "foo ")
	}
}

// TestExtractSelection_Plain is the symmetric counterpart of TestApplySelection.
func TestExtractSelection_Plain(t *testing.T) {
	in := "abc\ndef"
	got := ExtractSelection(in, 1, 2, 2, 2)
	want := "bc\nd"
	if got != want {
		t.Errorf("ExtractSelection = %q, want %q", got, want)
	}
}

// TestExtractSelection_SingleLine mirrors TestApplySelection_SingleLine to make
// sure the byte→cell→byte round-trip is lossless for an ANSI-padded line.
func TestExtractSelection_SingleLine(t *testing.T) {
	in := "\033[35mABCDE\033[0m"
	got := ExtractSelection(in, 1, 2, 1, 5)
	if got != "BCD" {
		t.Errorf("ExtractSelection = %q, want %q", got, "BCD")
	}
}

// TestApplySelection_WithTviewMarkup is the markup counterpart of
// TestApplySelection_SingleLine: input has [magenta]…[-] around the text
// instead of raw ANSI. The region must wrap cells 2-4 ("BCD") and the
// markup bytes must stay OUTSIDE the region so the highlight is exactly
// on the visible glyphs.
func TestApplySelection_WithTviewMarkup(t *testing.T) {
	in := "[magenta]ABCDE[-]"
	out := ApplySelection(in, 1, 2, 1, 5)
	want := "[magenta]A[\"sel\"]BCD[\"\"]E[-]"
	if out != want {
		t.Errorf("ApplySelection markup = %q, want %q", out, want)
	}
	// Round-trip: extract the selected substring and confirm it strips
	// back to the visible "BCD" with no markup/ANSI leakage.
	extracted := StripANSI(ExtractSelection(in, 1, 2, 1, 5))
	if extracted != "BCD" {
		t.Errorf("ExtractSelection stripped = %q, want %q", extracted, "BCD")
	}
}

// TestWrapGraphemes_PreservesMarkup: wrap must keep tview markup on the
// line where it started (don't break a tag across two wrapped lines) AND
// not count it toward the wrap width.
func TestWrapGraphemes_PreservesMarkup(t *testing.T) {
	// 20 visible chars, width 10 → 2 wrapped lines. Markup must not count
	// toward the wrap width (otherwise it would force an early break).
	in := "[magenta]" + strings.Repeat("a", 20) + "[-]"
	got := wrapGraphemes(in, 10)
	if len(got) < 2 {
		t.Fatalf("expected >= 2 lines, got %d: %v", len(got), got)
	}
	// Markup is preserved somewhere — exact line is undefined (depends on
	// which cluster crosses the break).
	if !strings.Contains(got[0]+ strings.Join(got[1:], ""), "[magenta]") {
		t.Errorf("color opener lost across wrap: %v", got)
	}
	if !strings.Contains(got[0]+ strings.Join(got[1:], ""), "[-]") {
		t.Errorf("reset lost across wrap: %v", got)
	}
	// Joined output must equal input — wrap must not duplicate or drop anything.
	if joined := strings.Join(got, ""); joined != in {
		t.Errorf("wrap changed content: got %q, want %q", joined, in)
	}
	// And no wrapped line exceeded the width (markup chars must not count).
	for i, line := range got {
		if w := displayWidth(line); w > 10 {
			t.Errorf("line %d width = %d, want <= 10 (markup leaked into count): %q", i, w, line)
		}
	}
}