// Package tui renders the terminal UI for tgchat.
package tui

import (
	"fmt"
	"strings"

	"github.com/rivo/uniseg"

	"github.com/user/tgchat/internal/telegram"
)

// FormatMessage renders one message as a small block.
//
// Incoming messages are left-aligned (no padding). Outgoing messages are
// right-aligned within `width` columns: long lines are pre-wrapped so each
// visible line stays flush against the right edge.
//
// `width` is the chat pane's drawable width (rect width minus border). It is
// ignored for incoming messages. A non-positive `width` skips wrapping and
// padding (safe fallback when the view hasn't been laid out yet).
func FormatMessage(msg telegram.Message, width int) string {
	if msg.Outgoing {
		return formatOutgoing(msg, width)
	}
	return formatIncoming(msg)
}

func formatIncoming(msg telegram.Message) string {
	const senderColor = "\033[36m" // cyan
	const reset = "\033[0m"
	header := fmt.Sprintf("%s%s%s", senderColor, msg.Sender, reset)
	indent := "  "
	body := indent + strings.ReplaceAll(msg.Text, "\n", "\n"+indent)
	return header + "\n" + body
}

// formatOutgoing renders outgoing text with each visible line right-aligned in
// `width` columns. The "› You" header is one line; the body may span multiple
// pre-wrapped lines. Each user-supplied newline starts a new wrapped segment
// (hard break), so wrapping never splits across user lines.
func formatOutgoing(msg telegram.Message, width int) string {
	const youColor = "\033[35m"  // magenta
	const bodyColor = "\033[37m" // light gray
	const reset = "\033[0m"
	const indent = "  "

	header := "› You"
	if width > 0 {
		header = youColor + rightPad(header, width) + reset
	} else {
		header = youColor + header + reset
	}

	bodyRaw := indent + strings.ReplaceAll(msg.Text, "\n", "\n"+indent)
	if width <= 0 {
		return header + "\n" + bodyColor + bodyRaw + reset
	}
	var lines []string
	for _, seg := range strings.Split(bodyRaw, "\n") {
		for _, w := range wrapGraphemes(seg, width) {
			lines = append(lines, bodyColor+rightPad(w, width)+reset)
		}
	}
	return header + "\n" + strings.Join(lines, "\n")
}

// displayWidth returns the number of terminal cells `s` occupies, ignoring
// ANSI CSI escape sequences (ESC[...m / ESC[K). Wide grapheme clusters
// (CJK, emoji) and combining marks are counted via uniseg so the result
// matches what the terminal actually renders.
func displayWidth(s string) int {
	w := 0
	state := -1
	esc := false
	for len(s) > 0 {
		var cluster string
		cluster, s, _, state = uniseg.FirstGraphemeClusterInString(s, state)
		if !esc && len(cluster) > 0 && cluster[0] == 0x1b {
			esc = true
			continue
		}
		if esc {
			// Sequence ends on the cluster containing 'm' (SGR) or 'K' (EL).
			if strings.ContainsAny(cluster, "mK") {
				esc = false
			}
			continue
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

// StripANSI removes ANSI CSI escape sequences (ESC ... m/K) from `s`, leaving
// plain text. Used to clean chat text before copying to the clipboard.
func StripANSI(s string) string {
	var b strings.Builder
	state := -1
	esc := false
	for len(s) > 0 {
		var cluster string
		cluster, s, _, state = uniseg.FirstGraphemeClusterInString(s, state)
		if !esc && len(cluster) > 0 && cluster[0] == 0x1b {
			esc = true
			continue
		}
		if esc {
			if strings.ContainsAny(cluster, "mK") {
				esc = false
			}
			continue
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
// in display cells (not bytes), so ANSI escape sequences don't shift them.
// If the range is empty (start >= end) or out of bounds, the text is returned
// unchanged.
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
// cell range in `line`. ANSI escapes inside the line are skipped during the
// cell count so the region aligns with what the terminal actually renders.
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
// [fromCell, toCell) cell range in `line`. Returns (-1, -1) if either bound
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
		if !esc && len(cluster) > 0 && cluster[0] == 0x1b {
			esc = true
			continue
		}
		if esc {
			if strings.ContainsAny(cluster, "mK") {
				esc = false
			}
			continue
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
// [sLine,sCol]..[eLine,eCol] range, with ANSI escapes preserved. Use
// StripANSI on the result to get a plain-text copy for the clipboard.
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

// wrapGraphemes splits `s` so each piece is at most `width` cells wide. ANSI
// escapes are preserved but don't count toward the width. Wrapping iterates
// grapheme clusters (so emoji and combining marks stay intact) and breaks at
// cluster boundaries — no mid-cluster splits, no byte-boundary UTF-8 breaks.
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