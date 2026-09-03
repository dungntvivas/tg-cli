package filesync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
