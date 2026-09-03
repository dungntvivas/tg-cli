// Package tui renders the terminal UI for tgchat.
package tui

import (
	"fmt"
	"strings"

	"github.com/rivo/uniseg"

	"github.com/user/tgchat/internal/media"
	"github.com/user/tgchat/internal/telegram"
)

// senderColWidth is the fixed width of the sender-name column in display
// cells. Slack-style thread: every message's first line is exactly this
// wide; subsequent lines start with `blankCol` spaces so the body column
// stays aligned across the whole thread.
const senderColWidth = 10

// blankCol is `senderColWidth` spaces — the leading indent used for every
// body line (including continuations of the first message line).
var blankCol = strings.Repeat(" ", senderColWidth)

// Tview markup for the two colors we render. Kept here (not interpolated as
// strings) so the format code reads naturally and so test asserts can refer
// to them by name.
//
// Why markup, not raw ANSI: tview's TextView with SetDynamicColors(true)
// only parses `[color]` tags. Raw `\x1b[36m` ANSI is silently dropped —
// the ESC byte is eaten and `[36m` shows up as literal text in the chat
// pane. Using tview's own markup avoids that footgun.
const (
	cyanMarkup    = "[cyan]"
	magentaMarkup = "[magenta]"
	resetMarkup   = "[-]"
)

// FormatMessage renders one message as a Slack-style thread block:
// [sender column, senderColWidth cells] | [body, wraps to width-senderColWidth].
// Continuation lines start with a blank sender column so the body column
// stays vertically aligned. Incoming sender names are cyan; "You" for
// outgoing is magenta so the user's own messages pop in the thread.
//
// `width` is the chat pane's drawable width (rect width minus border). A
// non-positive `width` skips wrapping and padding (safe fallback before the
// view has been laid out).
func FormatMessage(msg telegram.Message, width int) string {
	if msg.Outgoing {
		return formatColored(msg, width, magentaMarkup, "You")
	}
	return formatColored(msg, width, cyanMarkup, msg.Sender)
}

// formatColored builds the block for one message with the given sender color.
// The sender name is rendered in a fixed-width column; body lines (wrapped
// or split on user newlines) are indented by `blankCol` so they share the
// same column as the first body line.
func formatColored(msg telegram.Message, width int, senderColor, senderName string) string {
	header := senderColor + senderColumn(senderName) + resetMarkup
	body := msg.Text
	if isDownloadable(msg) {
		body = downloadLine(msg)
	}
	if width <= senderColWidth {
		// No wrap before layout / on too-narrow panes. Emit header + raw body.
		return header + "\n" + body
	}
	bodyW := width - senderColWidth
	var lines []string
	for _, seg := range strings.Split(body, "\n") {
		// User \n is a hard break — wrap each segment independently.
		for _, w := range wrapGraphemes(seg, bodyW) {
			lines = append(lines, blankCol+w)
		}
	}
	return header + "\n" + strings.Join(lines, "\n")
}

// isDownloadable reports whether the message renders as a click-to-download
// link instead of its (empty) text body: media attachments only, and never
// the negative placeholder IDs of optimistic local sends.
func isDownloadable(msg telegram.Message) bool {
	return msg.Media != "" && msg.Text == "" && msg.ID > 0
}

// downloadLine renders an attachment as a tview region named
// dl:<kind>:<peerID>:<msgID> — app.go's highlighted-func turns a click on
// that region into a browser download through the loopback server.
func downloadLine(msg telegram.Message) string {
	label := media.Glyph(msg)
	if label == "" {
		label = "tệp đính kèm"
	}
	return fmt.Sprintf(`["dl:%s:%d:%d"]%s · %s[""]`, msg.PeerKind, msg.PeerID, msg.ID, label, "click để tải ⬇")
}

// senderColumn returns `name` formatted to exactly `senderColWidth` display
// cells, with a trailing space: shorter names are right-padded to `width-1`
// cells, longer names are truncated to `width-2` cells + ellipsis (U+2026) +
// space. Truncation is cell-aware (CJK / emoji stay intact).
func senderColumn(name string) string {
	const ellipsis = "…"
	w := displayWidth(name)
	if w <= senderColWidth-2 {
		// Fits with ellipsis-free room: right-pad to width-1 + 1 space.
		return rightPad(name, senderColWidth-1) + " "
	}
	// Overflow: truncate to width-2 cells + ellipsis + space.
	return truncateCells(name, senderColWidth-2) + ellipsis + " "
}

// truncateCells returns the longest cell-aligned prefix of `s` that fits in
// `maxCells` display cells. Pure-prefix: never cuts a grapheme mid-cluster.
func truncateCells(s string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	var b strings.Builder
	used := 0
	state := -1
	rest := s
	for len(rest) > 0 {
		var cluster string
		var w int
		cluster, rest, w, state = uniseg.FirstGraphemeClusterInString(rest, state)
		if used+w > maxCells {
			break
		}
		b.WriteString(cluster)
		used += w
	}
	return b.String()
}

// isTviewTagContent reports whether `s` (the body between `[` and `]`)
// looks like a tview style/region tag, so callers know to skip the whole
// tag without counting it toward display cells. Strict: a `[word]` in
// chat body text is NOT a tag — only the specific shapes tview actually
// parses are accepted:
//
//	- `-`             — reset
//	- `cyan`, `red`…  — tview's short-form color/attr names (whitelisted)
//	- `"region_id"`   — region tag (anything quoted)
//	- `#rrggbb`       — hex color
//	- `:red`, `::b`   — long-form attribute prefixes
//	- `red,b`         — compound lists (comma/semicolon/colon separates)
func isTviewTagContent(s string) bool {
	if s == "-" {
		return true
	}
	if len(s) == 0 {
		return false
	}
	if s[0] == '"' {
		return true
	}
	if s[0] == '#' {
		if len(s) != 7 {
			return false
		}
		for _, r := range s[1:] {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
		return true
	}
	if s[0] == ':' {
		return true
	}
	// Compound attribute lists always carry a separator.
	if strings.ContainsAny(s, ":,;") {
		return true
	}
	return tviewShortNames[s]
}

// tviewShortNames is the whitelist of short-form tags tview actually parses
// (basic ANSI colors, their light/dark variants, and attribute toggles).
// Anything else in `[...]` is treated as visible chat text and left alone —
// otherwise we'd silently eat user-typed `[lol]` or `[citation needed]`.
//
// ponytail: this list is deliberately narrow; add new colors here if we
// ever need them, but don't loosen the heuristic — user content beats
// heuristic strip.
var tviewShortNames = map[string]bool{
	// foreground colors
	"black": true, "red": true, "green": true, "yellow": true,
	"blue": true, "magenta": true, "cyan": true, "white": true,
	"darkgray": true, "darkgrey": true,
	"lightred": true, "lightgreen": true, "lightyellow": true,
	"lightblue": true, "lightmagenta": true, "lightcyan": true,
	"lightgray": true, "lightgrey": true,
	"default": true,
	// attribute toggles
	"b": true, "u": true, "i": true, "s": true,
	"l": true, "d": true, "r": true, "f": true, "v": true,
}

// skipTviewTag consumes the `[…]` at the start of `s`. Returns the remainder
// after the closing `]`. Returns `s` unchanged if the bytes don't form a
// tag. The caller is responsible for having already consumed the opening
// `[` — `s` here is what comes AFTER the `[`.
func skipTviewTag(s string) string {
	closeIdx := strings.Index(s, "]")
	if closeIdx <= 0 || closeIdx > 32 {
		return s
	}
	if !isTviewTagContent(s[:closeIdx]) {
		return s
	}
	return s[closeIdx+1:]
}

// displayWidth returns the number of terminal cells `s` occupies, ignoring
// both ANSI CSI escape sequences (ESC ... m/K) and tview style/region
// markup ([color], [-], ["region"]). Wide grapheme clusters (CJK, emoji)
// and combining marks are counted via uniseg so the result matches what
// the terminal actually renders.
func displayWidth(s string) int {
	w := 0
	state := -1
	esc := false
	rest := s
	for len(rest) > 0 {
		var cluster string
		cluster, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
		if esc {
			// Sequence ends on the cluster containing 'm' (SGR) or 'K' (EL).
			if strings.ContainsAny(cluster, "mK") {
				esc = false
			}
			continue
		}
		if len(cluster) > 0 && cluster[0] == 0x1b {
			esc = true
			continue
		}
		if cluster == "[" {
			// Try to consume a full tview tag in one shot so we don't have
			// to track partial-tag state across iterations.
			after := skipTviewTag(rest)
			if after != rest {
				rest = after
				continue
			}
		}
		w += uniseg.StringWidth(cluster)
	}
	return w
}

// rightPad prepends spaces so `s` occupies `width` cells visually.
func rightPad(s string, width int) string {
	pad := width - displayWidth(s)
	if pad <= 0 {
		return s
	}
	return strings.Repeat(" ", pad) + s
}

// StripANSI removes both ANSI CSI escape sequences (ESC ... m/K) AND tview
// style/region markup ([color], [-], ["region"]) from `s`, leaving plain
// text. Used to clean chat text before copying to the clipboard.
//
// Kept named StripANSI for API stability — it strips more than ANSI now,
// but the surface (one call → plain text) didn't change.
func StripANSI(s string) string {
	var b strings.Builder
	state := -1
	esc := false
	rest := s
	for len(rest) > 0 {
		var cluster string
		cluster, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
		if esc {
			if strings.ContainsAny(cluster, "mK") {
				esc = false
			}
			continue
		}
		if len(cluster) > 0 && cluster[0] == 0x1b {
			esc = true
			continue
		}
		if cluster == "[" {
			after := skipTviewTag(rest)
			if after != rest {
				rest = after
				continue
			}
		}
		b.WriteString(cluster)
	}
	return b.String()
}

// selectionRegionTag is the tview region id used to highlight the visually
// selected range. Kept short so the surrounding ["..."] tags don't bloat the
// rendered text.
const selectionRegionTag = "sel"

// ApplySelection wraps the cell range [sLine,sCol]..[eLine,eCol] (1-based,
// inclusive) in tview region tags so Highlight can paint it. Coordinates are
// in display cells (not bytes), so ANSI escapes and tview markup don't shift
// them. If the range is empty (start >= end) or out of bounds, the text is
// returned unchanged.
func ApplySelection(text string, sLine, sCol, eLine, eCol int) string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return text
	}
	// Normalize so sLine,sCol comes first.
	if sLine > eLine || (sLine == eLine && sCol > eCol) {
		sLine, eLine = eLine, sLine
		sCol, eCol = eCol, sCol
	}
	if sLine < 1 || eLine < 1 || sLine > len(lines) || eLine > len(lines) {
		return text
	}
	if sCol < 1 || eCol < 1 {
		return text
	}
	if sLine == eLine && sCol >= eCol {
		return text
	}

	for i := range lines {
		lineNum := i + 1
		if lineNum < sLine || lineNum > eLine {
			continue
		}
		var fromCell, toCell int
		if lineNum == sLine {
			fromCell = sCol
		} else {
			fromCell = 1
		}
		if lineNum == eLine {
			toCell = eCol
		} else {
			toCell = displayWidth(lines[i]) + 1
		}
		if toCell <= fromCell {
			continue
		}
		lines[i] = wrapLineRangeWithRegion(lines[i], fromCell, toCell)
	}
	return strings.Join(lines, "\n")
}

// wrapLineRangeWithRegion inserts `["sel"]...[""]` around the [fromCell, toCell)
// cell range in `line`. ANSI escapes and tview markup inside the line are
// skipped during the cell count so the region aligns with what the terminal
// actually renders.
func wrapLineRangeWithRegion(line string, fromCell, toCell int) string {
	startByte, endByte := cellRangeToBytes(line, fromCell, toCell)
	if startByte < 0 || endByte < 0 || endByte <= startByte {
		return line
	}
	var b strings.Builder
	b.WriteString(line[:startByte])
	b.WriteString(`["` + selectionRegionTag + `"]`)
	b.WriteString(line[startByte:endByte])
	b.WriteString(`[""]`)
	b.WriteString(line[endByte:])
	return b.String()
}

// cellRangeToBytes returns the byte offsets [start, end) that cover the
// [fromCell, toCell) cell range in `line. Returns (-1, -1) if either bound
// can't be resolved (e.g. past end of line).
func cellRangeToBytes(line string, fromCell, toCell int) (int, int) {
	state := -1
	esc := false
	cell := 1
	startByte := -1
	endByte := -1
	original := line
	rest := line
	for len(rest) > 0 {
		var cluster string
		var w int
		cluster, rest, w, state = uniseg.FirstGraphemeClusterInString(rest, state)
		if esc {
			if strings.ContainsAny(cluster, "mK") {
				esc = false
			}
			continue
		}
		if len(cluster) > 0 && cluster[0] == 0x1b {
			esc = true
			continue
		}
		if cluster == "[" {
			after := skipTviewTag(rest)
			if after != rest {
				rest = after
				// The `[` byte lives where `cluster` came from; the tag
				// itself doesn't add cells, so cell/w stay unchanged. Skip
				// updating byte offsets — the tag isn't visible, so any
				// selection that would have landed inside the tag is
				// invalid and falls through to the next visible byte.
				continue
			}
		}
		clusterStart := len(original) - len(rest) - len(cluster)
		if startByte < 0 && cell >= fromCell {
			startByte = clusterStart
		}
		cell += w
		if endByte < 0 && cell >= toCell {
			endByte = len(original) - len(rest)
			break
		}
	}
	if startByte < 0 {
		return -1, -1
	}
	if endByte < 0 {
		endByte = len(line)
	}
	return startByte, endByte
}

// ExtractSelection returns the cell-accurate substring of `text` covered by the
// [sLine,sCol]..[eLine,eCol] range, with both ANSI escapes and tview markup
// preserved. Use StripANSI on the result to get a plain-text copy for the
// clipboard.
func ExtractSelection(text string, sLine, sCol, eLine, eCol int) string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return ""
	}
	if sLine > eLine || (sLine == eLine && sCol > eCol) {
		sLine, eLine = eLine, sLine
		sCol, eCol = eCol, sCol
	}
	if sLine < 1 || eLine < 1 || sLine > len(lines) || eLine > len(lines) {
		return ""
	}
	if sLine == eLine && sCol >= eCol {
		return ""
	}
	var parts []string
	for i := range lines {
		lineNum := i + 1
		if lineNum < sLine || lineNum > eLine {
			continue
		}
		var fromCell, toCell int
		if lineNum == sLine {
			fromCell = sCol
		} else {
			fromCell = 1
		}
		if lineNum == eLine {
			toCell = eCol
		} else {
			toCell = displayWidth(lines[i]) + 1
		}
		if toCell <= fromCell {
			continue
		}
		startByte, endByte := cellRangeToBytes(lines[i], fromCell, toCell)
		if startByte < 0 || endByte < 0 {
			continue
		}
		parts = append(parts, lines[i][startByte:endByte])
	}
	return strings.Join(parts, "\n")
}

// wrapGraphemes splits `s` so each piece is at most `width` cells wide.
// ANSI escapes and tview markup are preserved in the output but don't count
// toward the width. Wrapping iterates grapheme clusters (so emoji and
// combining marks stay intact) and breaks at cluster boundaries — no
// mid-cluster splits, no byte-boundary UTF-8 breaks.
func wrapGraphemes(s string, width int) []string {
	if width <= 0 || displayWidth(s) <= width {
		return []string{s}
	}
	var lines []string
	var cur strings.Builder
	curW := 0
	state := -1
	esc := false
	rest := s

	for len(rest) > 0 {
		var cluster string
		var w int
		cluster, rest, w, state = uniseg.FirstGraphemeClusterInString(rest, state)
		if !esc && len(cluster) > 0 && cluster[0] == 0x1b {
			esc = true
			cur.WriteString(cluster)
			continue
		}
		if esc {
			cur.WriteString(cluster)
			if strings.ContainsAny(cluster, "mK") {
				esc = false
			}
			continue
		}
		if cluster == "[" {
			// Preserve tview tags verbatim; consume them before deciding
			// whether the next visible cluster would overflow.
			closeIdx := strings.Index(rest, "]")
			if closeIdx > 0 && closeIdx <= 32 && isTviewTagContent(rest[:closeIdx]) {
				cur.WriteByte('[')
				cur.WriteString(rest[:closeIdx+1])
				rest = rest[closeIdx+1:]
				continue
			}
		}
		// Flush when adding this cluster would overflow. Allow a single
		// cluster wider than `width` (e.g. an emoji at width=1) by breaking
		// anyway rather than spinning.
		if curW > 0 && curW+w > width {
			lines = append(lines, cur.String())
			cur.Reset()
			curW = 0
		}
		cur.WriteString(cluster)
		curW += w
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	if len(lines) == 0 {
		return []string{s}
	}
	return lines
}