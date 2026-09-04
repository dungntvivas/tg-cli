package filesync

import (
	"testing"
	"time"

	"github.com/user/tgchat/internal/telegram"
)

func at(h, m int) time.Time {
	return time.Date(2026, 9, 3, h, m, 0, 0, time.Local)
}

// TestRender covers the whole file shape the user sees in the editor:
// incoming and outgoing lines, multi-line indentation, a media line with its
// download URL, and the compose marker at the bottom.
func TestRender(t *testing.T) {
	msgs := []telegram.Message{
		{ID: 1, PeerID: 88, PeerKind: "user", Sender: "Nam", Text: "ok mày đâu rồi", Time: at(10, 23)},
		{ID: 2, PeerID: 88, PeerKind: "user", Sender: "You", Text: "tôi đến ngay\nkẹt xe quá", Time: at(10, 25), Outgoing: true},
		{ID: 1204, PeerID: 88, PeerKind: "user", Sender: "Nam", Media: "báo_cáo.pdf", Time: at(10, 31)},
	}
	want := "[10:23] Nam: ok mày đâu rồi\n" +
		"[10:25] Bạn: tôi đến ngay\n" +
		"        kẹt xe quá\n" +
		"[10:31] Nam: 📎 báo_cáo.pdf http://127.0.0.1:5417/dl/a3f/user/88/1204\n" +
		"\n" +
		"--- gõ dưới đây ---\n"

	if got := Render(msgs, "http://127.0.0.1:5417/dl/a3f"); got != want {
		t.Errorf("Render =\n%q\nwant\n%q", got, want)
	}
}

// TestRender_EmptyConversation still writes the marker — the user must be
// able to start typing in a chat with no history.
func TestRender_EmptyConversation(t *testing.T) {
	want := "\n--- gõ dưới đây ---\n"
	if got := Render(nil, ""); got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

// TestRender_MediaWithoutServer degrades to a label when the download server
// never started — a bare label beats a broken link.
func TestRender_MediaWithoutServer(t *testing.T) {
	msgs := []telegram.Message{{ID: 5, PeerID: 88, PeerKind: "user", Sender: "Nam", Media: "photo", Time: at(9, 0)}}
	want := "[09:00] Nam: 📷 Ảnh\n\n--- gõ dưới đây ---\n"
	if got := Render(msgs, ""); got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

// TestRender_MediaWithCaption: a photo or file sent with a caption must show
// BOTH — before this, Text won and the download link vanished entirely, so a
// captioned attachment was unreachable from the file.
func TestRender_MediaWithCaption(t *testing.T) {
	msgs := []telegram.Message{{
		ID: 1204, PeerID: 88, PeerKind: "user", Sender: "Nam",
		Media: "báo cáo.pdf", Text: "gửi anh xem giúp nhé", Time: at(10, 31),
	}}
	want := "[10:31] Nam: 📎 báo cáo.pdf http://x/dl/t/user/88/1204\n" +
		"        gửi anh xem giúp nhé\n" +
		"\n--- gõ dưới đây ---\n"
	if got := Render(msgs, "http://x/dl/t"); got != want {
		t.Errorf("Render =\n%q\nwant\n%q", got, want)
	}
}

// TestRender_MultiLineCaption keeps every caption line under the same indent.
func TestRender_MultiLineCaption(t *testing.T) {
	msgs := []telegram.Message{{
		ID: 7, PeerID: 88, PeerKind: "user", Sender: "You", Outgoing: true,
		Media: "photo", Text: "dòng 1\ndòng 2", Time: at(8, 5),
	}}
	want := "[08:05] Bạn: 📷 Ảnh http://x/dl/t/user/88/7\n" +
		"        dòng 1\n" +
		"        dòng 2\n" +
		"\n--- gõ dưới đây ---\n"
	if got := Render(msgs, "http://x/dl/t"); got != want {
		t.Errorf("Render =\n%q\nwant\n%q", got, want)
	}
}
