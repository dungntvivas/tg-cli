package tui

import "github.com/user/tgchat/internal/telegram"

// RenderSidebar returns one string per dialog for a tview list primitive.
// selected=-1 means no selection. Format:
//
//	"● Alice (3)"
//	"  Bob"
//	"▶ Team group"   ← selected row uses ▶ marker
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
		out[i] = marker + d.Title + suffix
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
