// Package tui renders the terminal UI for tgchat.
package tui

import (
	"fmt"
	"strings"

	"github.com/user/tgchat/internal/telegram"
)

// FormatMessage renders a single message as a 2-line block:
//   <marker> <Sender>  HH:MM
//   <indented text, possibly multi-line>
//
// Outgoing messages get a right-aligned marker (›) and an accent color so the
// user can tell their own messages from the peer's at a glance.
func FormatMessage(msg telegram.Message) string {
	if msg.Outgoing {
		return formatOutgoing(msg)
	}
	return formatIncoming(msg)
}

func formatIncoming(msg telegram.Message) string {
	const senderColor = "\033[36m" // cyan
	const timeColor = "\033[90m"   // bright black (dim)
	const reset = "\033[0m"
	header := fmt.Sprintf("%s %s%s%s  %s%s%s",
		" ", senderColor, msg.Sender, reset,
		timeColor, msg.Time.Format("15:04"), reset)
	indent := "  "
	body := indent + strings.ReplaceAll(msg.Text, "\n", "\n"+indent)
	return header + "\n" + body
}

func formatOutgoing(msg telegram.Message) string {
	const youColor = "\033[35m" // magenta — distinguish from peer color
	const timeColor = "\033[90m"
	const bodyColor = "\033[37m" // light gray
	const reset = "\033[0m"
	header := fmt.Sprintf("› %sYou%s  %s%s%s",
		youColor, reset,
		timeColor, msg.Time.Format("15:04"), reset)
	indent := "  "
	body := bodyColor + indent + strings.ReplaceAll(msg.Text, "\n", "\n"+indent) + reset
	return header + "\n" + body
}