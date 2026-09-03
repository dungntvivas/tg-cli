// Package filesync mirrors Telegram conversations as text files an editor
// can read and write: one file per conversation, with an editable compose
// area at the bottom. Saving the file sends that area as a message.
package filesync

import (
	"fmt"
	"strings"

	"github.com/user/tgchat/internal/media"
	"github.com/user/tgchat/internal/telegram"
)

// Marker separates the read-only log above from the compose area below.
// Everything after it is what gets sent when the user saves the file.
const Marker = "--- gõ dưới đây ---"

// contIndent is the width of the "[HH:MM] " prefix. Continuation lines of a
// multi-line message are indented by it so the text column stays aligned.
// Fixed rather than aligned to the sender name, so the indent is
// deterministic.
const contIndent = "        "

// Render builds the complete file content for one conversation: the log, a
// blank line, the marker, and nothing after it. Callers that want to keep an
// in-progress draft append it themselves.
func Render(msgs []telegram.Message, dlBase string) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(line(m, dlBase))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString(Marker)
	b.WriteByte('\n')
	return b.String()
}

// line renders one message as "[HH:MM] <sender>: <body>".
func line(m telegram.Message, dlBase string) string {
	sender := m.Sender
	if m.Outgoing {
		// The telegram layer says "You"; the file is Vietnamese.
		sender = "Bạn"
	}
	body := m.Text
	if body == "" {
		body = mediaBody(m, dlBase)
	}
	parts := strings.Split(body, "\n")
	out := fmt.Sprintf("[%s] %s: %s", m.Time.Format("15:04"), sender, parts[0])
	for _, p := range parts[1:] {
		out += "\n" + contIndent + p
	}
	return out
}

// mediaBody describes an attachment and appends its download link, so
// Ctrl+click in the editor saves the file through the loopback server.
func mediaBody(m telegram.Message, dlBase string) string {
	label := media.Glyph(m)
	if label == "" {
		label = "📎 tệp đính kèm"
	}
	if u := media.URL(dlBase, m); u != "" {
		return label + " " + u
	}
	return label
}
