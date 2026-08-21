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