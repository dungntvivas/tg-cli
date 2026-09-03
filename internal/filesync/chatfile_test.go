package filesync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/user/tgchat/internal/telegram"
)

// TestDraftOf: the compose area is everything after the last marker.
func TestDraftOf(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"typed draft", "[10:00] Nam: hi\n\n--- gõ dưới đây ---\ncòn 5 phút nữa nhé\n", "còn 5 phút nữa nhé"},
		{"empty area", "[10:00] Nam: hi\n\n--- gõ dưới đây ---\n", ""},
		{"whitespace only", "\n--- gõ dưới đây ---\n\n   \n", ""},
		{"multi-line draft", "\n--- gõ dưới đây ---\ndòng 1\ndòng 2", "dòng 1\ndòng 2"},
		{"marker missing", "[10:00] Nam: hi\n", ""},
		{"marker text inside a message", "[10:00] Nam: --- gõ dưới đây ---\n\n--- gõ dưới đây ---\nx", "x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := draftOf(tc.content); got != tc.want {
				t.Errorf("draftOf = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestChatFile_WritePreservesDraft: an incoming message rewrites the log but
// must not wipe what the user is halfway through typing.
func TestChatFile_WritePreservesDraft(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Nam.md")
	c := &chatFile{path: path}
	if err := c.writeWithDraft("", "đang gõ dở"); err != nil {
		t.Fatalf("writeWithDraft: %v", err)
	}

	if err := c.add(telegram.Message{ID: 1, Sender: "Nam", Text: "tin mới"}, ""); err != nil {
		t.Fatalf("add: %v", err)
	}

	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "tin mới") {
		t.Errorf("log lost the new message:\n%s", b)
	}
	if got := draftOf(string(b)); got != "đang gõ dở" {
		t.Errorf("draft = %q, want it preserved", got)
	}
}

// TestChatFile_StaleSaveIsRepaired is the invariant that makes the
// single-file layout safe: memory owns the log, so a stale editor buffer
// saved over it is corrected by the next write.
func TestChatFile_StaleSaveIsRepaired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Nam.md")
	c := &chatFile{path: path, msgs: []telegram.Message{
		{ID: 1, Sender: "Nam", Text: "tin cũ"},
		{ID: 2, Sender: "Nam", Text: "tin mới"},
	}}
	if err := c.writeWithDraft("", ""); err != nil {
		t.Fatalf("writeWithDraft: %v", err)
	}
	// The user's editor had a stale buffer and saved only the first message.
	os.WriteFile(path, []byte("[00:00] Nam: tin cũ\n\n"+Marker+"\n"), 0o600)

	if err := c.add(telegram.Message{ID: 3, Sender: "Nam", Text: "tin mới nhất"}, ""); err != nil {
		t.Fatalf("add: %v", err)
	}

	b, _ := os.ReadFile(path)
	for _, want := range []string{"tin cũ", "tin mới", "tin mới nhất"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("log missing %q after stale save:\n%s", want, b)
		}
	}
}

// TestChatFile_AddDedupes: our own send appends optimistically, and the
// server may echo the same message back through the update stream.
func TestChatFile_AddDedupes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Nam.md")
	c := &chatFile{path: path}
	m := telegram.Message{ID: 7, Sender: "Nam", Text: "một lần thôi"}
	c.add(m, "")
	c.add(m, "")

	b, _ := os.ReadFile(path)
	if n := strings.Count(string(b), "một lần thôi"); n != 1 {
		t.Errorf("message appears %d times, want 1", n)
	}
}

// TestCheckAndSend_SendsDraftAndClearsIt is the core round trip: user types
// below the marker, saves, the text is sent verbatim and the area is emptied.
func TestCheckAndSend_SendsDraftAndClearsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Nam.md")
	c := &chatFile{path: path, peer: telegram.Peer{ID: 88, Kind: "user"}}
	c.writeWithDraft("", "")

	var sentTo telegram.Peer
	var sentText string
	api := &telegram.FakeAPI{
		SendFn: func(ctx context.Context, peer telegram.Peer, text string) (telegram.Message, error) {
			sentTo, sentText = peer, text
			return telegram.Message{ID: 11, Sender: "You", Text: text, Outgoing: true}, nil
		},
	}

	// Simulate the editor saving a draft.
	b, _ := os.ReadFile(path)
	os.WriteFile(path, append(b, []byte("còn 5 phút nữa nhé\n")...), 0o600)
	touch(t, path)

	c.checkAndSend(context.Background(), api, "")

	if sentText != "còn 5 phút nữa nhé" {
		t.Errorf("sent %q, want the draft verbatim", sentText)
	}
	if sentTo.ID != 88 {
		t.Errorf("sent to peer %d, want 88", sentTo.ID)
	}
	after, _ := os.ReadFile(path)
	if got := draftOf(string(after)); got != "" {
		t.Errorf("compose area = %q, want empty after send", got)
	}
	if !strings.Contains(string(after), "Bạn: còn 5 phút nữa nhé") {
		t.Errorf("sent message not in the log:\n%s", after)
	}
}

// TestCheckAndSend_MultiLineDraftIsOneMessage: the whole compose area is a
// single message, newlines preserved.
func TestCheckAndSend_MultiLineDraftIsOneMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Nam.md")
	c := &chatFile{path: path, peer: telegram.Peer{ID: 88, Kind: "user"}}
	c.writeWithDraft("", "")

	var calls int
	var sentText string
	api := &telegram.FakeAPI{
		SendFn: func(ctx context.Context, peer telegram.Peer, text string) (telegram.Message, error) {
			calls++
			sentText = text
			return telegram.Message{ID: 12, Text: text, Outgoing: true}, nil
		},
	}
	b, _ := os.ReadFile(path)
	os.WriteFile(path, append(b, []byte("dòng 1\ndòng 2\n")...), 0o600)
	touch(t, path)

	c.checkAndSend(context.Background(), api, "")

	if calls != 1 {
		t.Errorf("Send called %d times, want 1", calls)
	}
	if sentText != "dòng 1\ndòng 2" {
		t.Errorf("sent %q, want both lines in one message", sentText)
	}
}

// TestCheckAndSend_OwnWriteDoesNotResend is the loop guard: our own rewrite
// changes the file too, and must not be mistaken for a user save.
func TestCheckAndSend_OwnWriteDoesNotResend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Nam.md")
	c := &chatFile{path: path, peer: telegram.Peer{ID: 88, Kind: "user"}}
	// A draft is on disk, and we preserved it during our own write.
	c.writeWithDraft("", "đang gõ dở")

	var calls int
	api := &telegram.FakeAPI{
		SendFn: func(ctx context.Context, peer telegram.Peer, text string) (telegram.Message, error) {
			calls++
			return telegram.Message{ID: 13, Text: text}, nil
		},
	}
	c.checkAndSend(context.Background(), api, "")

	if calls != 0 {
		t.Errorf("Send called %d times after our own write, want 0", calls)
	}
}

// TestCheckAndSend_FailureKeepsDraft: a send error must not eat the user's
// text, and must be visible in the file.
func TestCheckAndSend_FailureKeepsDraft(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Nam.md")
	c := &chatFile{path: path, peer: telegram.Peer{ID: 88, Kind: "user"}}
	c.writeWithDraft("", "")

	api := &telegram.FakeAPI{
		SendFn: func(ctx context.Context, peer telegram.Peer, text string) (telegram.Message, error) {
			return telegram.Message{}, errors.New("FLOOD_WAIT_30")
		},
	}
	b, _ := os.ReadFile(path)
	os.WriteFile(path, append(b, []byte("tin quan trọng\n")...), 0o600)
	touch(t, path)

	c.checkAndSend(context.Background(), api, "")

	after, _ := os.ReadFile(path)
	if got := draftOf(string(after)); got != "tin quan trọng" {
		t.Errorf("draft = %q, want it kept for retry", got)
	}
	if !strings.Contains(string(after), "FLOOD_WAIT_30") {
		t.Errorf("error not surfaced in the file:\n%s", after)
	}
}

// TestCheckAndSend_DeletedFileIsRecreated: the folder is a view, not storage.
func TestCheckAndSend_DeletedFileIsRecreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Nam.md")
	c := &chatFile{path: path, msgs: []telegram.Message{{ID: 1, Sender: "Nam", Text: "còn đây"}}}
	c.writeWithDraft("", "")
	os.Remove(path)

	c.checkAndSend(context.Background(), &telegram.FakeAPI{}, "")

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not recreated: %v", err)
	}
	if !strings.Contains(string(b), "còn đây") {
		t.Errorf("recreated file lost the log:\n%s", b)
	}
}

// touch bumps a file's mtime past our recorded one. Writes within the same
// filesystem timestamp tick would otherwise look like our own write.
func touch(t *testing.T, path string) {
	t.Helper()
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}
