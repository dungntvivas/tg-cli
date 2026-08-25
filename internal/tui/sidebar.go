package tui

import "github.com/user/tgchat/internal/telegram"

// chatPrefix is the sidebar row label that signals "this is a chat you're
// talking to". The label makes the sidebar's role self-explanatory in
// screenshots/recordings and frees the chat pane's sender column to stay
// narrow (it doesn't have to disambiguate per-message senders inside a
// group from the conversation itself).
const chatPrefix = "chat - "

// RenderSidebar returns one string per dialog for a tview list primitive.
// selected=-1 means no selection. Format:
//
//	"● chat - @alice (3)"
//	"  chat - Bob"
//	"▶ chat - Team group"   ← selected row uses ▶ marker
//
// For P2P the title is the chat partner (FirstName or @username); for
// groups/channels it's the chat title. So the same `chat - X` shape covers
// all three of "chat partner / group / bot" — the prefix labels the row
// type and the suffix is the dialog's own display name.
func RenderSidebar(dialogs []telegram.Dialog, selected int) []string {
	if len(dialogs) == 0 {
		return []string{"(no dialogs — press Ctrl+C to quit)"}
	}
	out := make([]string, len(dialogs))
	for i, d := range dialogs {
		marker := "  "
		if i == selected {
			marker = "▶ "
		} else if d.Unread > 0 {
			marker = "● "
		}
		suffix := ""
		if d.Unread > 0 {
			suffix = " (" + itoa(d.Unread) + ")"
		}
		out[i] = marker + chatPrefix + d.Title + suffix
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
