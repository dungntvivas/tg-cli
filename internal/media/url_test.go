package media

import (
	"testing"

	"github.com/user/tgchat/internal/telegram"
)

// TestURL: filesync writes these links straight into text files, so the shape
// must match exactly what the server routes on.
func TestURL(t *testing.T) {
	msg := telegram.Message{ID: 1204, PeerID: 88, PeerKind: "user", Media: "bao_cao.pdf"}
	cases := []struct {
		name string
		base string
		msg  telegram.Message
		want string
	}{
		{"builds link", "http://127.0.0.1:5417/dl/a3f", msg, "http://127.0.0.1:5417/dl/a3f/user/88/1204"},
		{"no server", "", msg, ""},
		{"no media", "http://x/dl/t", telegram.Message{ID: 1204, PeerID: 88, PeerKind: "user"}, ""},
		{"optimistic local id", "http://x/dl/t", telegram.Message{ID: 0, PeerID: 88, PeerKind: "user", Media: "photo"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := URL(tc.base, tc.msg); got != tc.want {
				t.Errorf("URL = %q, want %q", got, tc.want)
			}
		})
	}
}
