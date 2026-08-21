// Package tui renders the terminal UI for tgchat.
package tui

import (
	"fmt"
	"strings"

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
// pre-wrapped lines.
func formatOutgoing(msg telegram.Message, width int) string {
	const youColor = "\033[35m"  // magenta
	const bodyColor = "\033[37m" // light gray
	const reset = "\033[0m"
	const indent = "  "

	// Header: "› You" — short, never wraps in practice.
	header := "› You"
	if width > 0 {
		header = youColor + rightPad(header, width) + reset
	} else {
		header = youColor + header + reset
	}

	// Body: prepend indent to each user-supplied line, wrap to `width`, then
	// pad each wrapped line so every line ends at the right column.
	bodyRaw := indent + strings.ReplaceAll(msg.Text, "\n", "\n"+indent)
	if width <= 0 {
		return header + "\n" + bodyColor + bodyRaw + reset
	}
	wrapped := wrapVisible(bodyRaw, width)
	for i, line := range wrapped {
		wrapped[i] = bodyColor + rightPad(line, width) + reset
	}
	return header + "\n" + strings.Join(wrapped, "\n")
}

// visibleWidth returns the number of terminal cells `s` occupies, ignoring
// ANSI CSI escape sequences (ESC[...m / ESC[K).
func visibleWidth(s string) int {
	w := 0
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
		w++
	}
	return w
}

// rightPad prepends spaces so `s` occupies `width` cells visually.
func rightPad(s string, width int) string {
	pad := width - visibleWidth(s)
	if pad <= 0 {
		return s
	}
	return strings.Repeat(" ", pad) + s
}

// wrapVisible splits `s` so each piece is at most `width` cells wide. ANSI
// escapes are preserved but don't count toward the width. Wrapping is greedy:
// characters are taken until the next one would push the line past `width`.
func wrapVisible(s string, width int) []string {
	if width <= 0 || visibleWidth(s) <= width {
		return []string{s}
	}
	var lines []string
	var cur strings.Builder
	curW := 0
	inEscape := false

	for _, r := range s {
		if r == 0x1b {
			inEscape = true
			cur.WriteRune(r)
			continue
		}
		if inEscape {
			cur.WriteRune(r)
			if r == 'm' || r == 'K' {
				inEscape = false
			}
			continue
		}
		if curW >= width {
			lines = append(lines, cur.String())
			cur.Reset()
			curW = 0
		}
		cur.WriteRune(r)
		curW++
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	if len(lines) == 0 {
		return []string{s}
	}
	return lines
}