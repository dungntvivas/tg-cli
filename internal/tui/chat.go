package tui

import (
	"strings"

	"github.com/user/tgchat/internal/telegram"
)

// RenderHistory returns the entire history view as one block of text.
// Messages are concatenated top-to-bottom (newest at the bottom).
//
// `width` is the chat pane's drawable width — it's forwarded to FormatMessage
// so outgoing messages can be right-aligned within the pane.
func RenderHistory(messages []telegram.Message, width int) string {
	if len(messages) == 0 {
		return "(no messages) — type /history to load"
	}
	parts := make([]string, len(messages))
	for i, m := range messages {
		parts[i] = FormatMessage(m, width)
	}
	return strings.Join(parts, "\n\n")
}