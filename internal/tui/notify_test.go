package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/user/tgchat/internal/telegram"
)

// capturePush swaps pushNotify for a recording stub so tests can assert what
// would have been pushed without spawning real Windows toasts. Returns a
// channel delivering "title|body" per push.
func capturePush(t *testing.T) <-chan string {
	t.Helper()
	ch := make(chan string, 1)
	old := pushNotify
	pushNotify = func(title, body string) error {
		select {
		case ch <- title + "|" + body:
		default:
		}
		return nil
	}
	t.Cleanup(func() { pushNotify = old })
	return ch
}

// TestHandleIncoming_OtherPeer_PushesWithDialogTitle verifies a live message
// from a non-active dialog fires a Windows push carrying the dialog's own
// title (not the raw "User 42" sender fallback) and a flattened message body.
func TestHandleIncoming_OtherPeer_PushesWithDialogTitle(t *testing.T) {
	dialogs := []telegram.Dialog{
		{ID: 7, Kind: "user", Title: "Alice"},
		{ID: 99, Kind: "group", Title: "Bob Group"},
	}
	a, _ := newTestApp(dialogs)
	ch := capturePush(t)
	a.handleIncoming(telegram.Message{
		ID: 200, PeerID: 99, PeerKind: "group", Sender: "User 42",
		Text: "hello\nworld",
	})
	// Blocking receive doubles as the sync point for the fire-and-forget goroutine.
	got := <-ch
	if want := "Bob Group|hello world"; got != want {
		t.Errorf("push = %q, want %q", got, want)
	}
}

// TestHandleIncoming_OtherPeer_UnknownTitleFallsBackToSender: if the dialog
// isn't in the cached list yet (e.g. Dialogs fetch failed), the raw sender
// name is used rather than an empty title.
func TestHandleIncoming_OtherPeer_UnknownTitleFallsBackToSender(t *testing.T) {
	a, _ := newTestApp(nil)
	ch := capturePush(t)
	a.handleIncoming(telegram.Message{
		ID: 201, PeerID: 55, PeerKind: "user", Sender: "User 55", Text: "hi",
	})
	if got := <-ch; got != "User 55|hi" {
		t.Errorf("push = %q, want %q", got, "User 55|hi")
	}
}

// TestHandleIncoming_MutedDialog_NoPush: groups whose notifications are muted
// server-side (the user turned them off in Telegram settings) must NOT get a
// local Windows push.
func TestHandleIncoming_MutedDialog_NoPush(t *testing.T) {
	dialogs := []telegram.Dialog{
		{ID: 99, Kind: "group", Title: "Quiet Group", Muted: true},
	}
	a, _ := newTestApp(dialogs)
	ch := capturePush(t)
	a.handleIncoming(telegram.Message{
		ID: 202, PeerID: 99, PeerKind: "group", Sender: "User 42", Text: "silence please",
	})
	select {
	case got := <-ch:
		t.Errorf("muted dialog pushed anyway: %q", got)
	case <-time.After(50 * time.Millisecond):
		// no push — correct
	}
}

// TestHandleIncoming_ActivePeer_NoPush: the user is reading this chat live;
// a desktop notification would be noise.
func TestHandleIncoming_ActivePeer_NoPush(t *testing.T) {
	a, _ := newTestApp(nil)
	ch := capturePush(t)
	a.handleIncoming(telegram.Message{
		ID: 203, PeerID: 7, PeerKind: "user", Sender: "Alice", Text: "reading you now",
	})
	select {
	case got := <-ch:
		t.Errorf("active chat pushed anyway: %q", got)
	case <-time.After(50 * time.Millisecond):
		// no push — correct
	}
}

// TestHandleIncoming_LongMessage_TruncatesBody keeps toast bodies bounded —
// a 10k-char wall of text must not render as a screen-filling notification.
func TestHandleIncoming_LongMessage_TruncatesBody(t *testing.T) {
	a, _ := newTestApp([]telegram.Dialog{{ID: 99, Kind: "user", Title: "Bob"}})
	ch := capturePush(t)
	a.handleIncoming(telegram.Message{
		ID: 204, PeerID: 99, PeerKind: "user", Sender: "x",
		Text: strings.Repeat("a", 500),
	})
	got := <-ch
	title, body, _ := strings.Cut(got, "|")
	if title != "Bob" {
		t.Errorf("title = %q, want Bob", title)
	}
	if len(body) > 121+3 || !strings.HasSuffix(body, "…") { // 121 runes + ellipsis
		t.Errorf("body length = %d runes, want <=124 ending in '…'", len([]rune(body)))
	}
}

// TestHandleIncoming_MediaMessages_DescribedInBody: photos/files have no
// text — the toast must say WHAT arrived instead of rendering blank.
func TestHandleIncoming_MediaMessages_DescribedInBody(t *testing.T) {
	cases := []struct {
		name string
		msg  telegram.Message
		want string
	}{
		{"photo", telegram.Message{ID: 1, PeerID: 99, PeerKind: "user", Media: "photo"}, "📷 Ảnh"},
		{"named file", telegram.Message{ID: 2, PeerID: 99, PeerKind: "user", Media: "hoa_don.pdf"}, "📎 hoa_don.pdf"},
		{"voice", telegram.Message{ID: 3, PeerID: 99, PeerKind: "user", Media: "voice"}, "🎤 Tin nhắn thoại"},
		{"video", telegram.Message{ID: 4, PeerID: 99, PeerKind: "user", Media: "video"}, "🎬 Video"},
		{"sticker", telegram.Message{ID: 5, PeerID: 99, PeerKind: "user", Media: "sticker"}, "🌟 Sticker"},
		{"text beats media", telegram.Message{ID: 6, PeerID: 99, PeerKind: "user", Media: "photo", Text: "caption"}, "caption"},
		{"unknown falls back", telegram.Message{ID: 7, PeerID: 99, PeerKind: "user"}, "(tin nhắn)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newTestApp([]telegram.Dialog{{ID: 99, Kind: "user", Title: "Bob"}})
			ch := capturePush(t)
			a.handleIncoming(tc.msg)
			title, body, _ := strings.Cut(<-ch, "|")
			if title != "Bob" {
				t.Errorf("title = %q, want Bob", title)
			}
			if body != tc.want {
				t.Errorf("body = %q, want %q", body, tc.want)
			}
		})
	}
}

// TestSendWindowsToast_Smoke fires a REAL Windows toast through the
// production path. Opt-in because it pops up on screen:
//
//	TG_TOAST_SMOKE=1 go test ./internal/tui/ -run TestSendWindowsToast_Smoke -v
func TestSendWindowsToast_Smoke(t *testing.T) {
	if os.Getenv("TG_TOAST_SMOKE") == "" {
		t.Skip("opt-in: set TG_TOAST_SMOKE=1 to fire a real toast")
	}
	if err := sendWindowsToast("tgchat", "kiểm tra push local ✓"); err != nil {
		t.Fatalf("real toast failed: %v", err)
	}
}
