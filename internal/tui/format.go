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
func FormatMessage(msg telegram.Message) string {
	marker := " "
	if msg.Outgoing {
		marker = "›"
	}
	header := fmt.Sprintf("%s %s  %s", marker, msg.Sender, msg.Time.Format("15:04"))
	indent := "  "
	body := indent + strings.ReplaceAll(msg.Text, "\n", "\n"+indent)
	return header + "\n" + body
}