package filesync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/tgchat/internal/telegram"
)

// TestParseDraft: a compose area whose first line starts with @ is a file
// send. Everything after the @ is the path, so names with spaces survive;
// the remaining lines are the caption.
func TestParseDraft(t *testing.T) {
	cases := []struct {
		name        string
		draft       string
		wantPath    string
		wantCaption string
		wantText    string
	}{
		{"plain text", "chào bạn", "", "", "chào bạn"},
		{"file only", `@D:\work\a.pdf`, `D:\work\a.pdf`, "", ""},
		{"path with spaces", `@D:\work\báo cáo quý 3.pdf`, `D:\work\báo cáo quý 3.pdf`, "", ""},
		{"file with caption", "@a.png\ngửi anh xem nhé", "a.png", "gửi anh xem nhé", ""},
		{"multi-line caption", "@a.png\ndòng 1\ndòng 2", "a.png", "dòng 1\ndòng 2", ""},
		{"quoted path", `@"D:\a b\c.pdf"`, `D:\a b\c.pdf`, "", ""},
		{"space after @", `@  D:\a.pdf  `, `D:\a.pdf`, "", ""},
		// @ only counts on the FIRST line — otherwise a message mentioning a
		// username or an email would be mistaken for a file send.
		{"@ later in the draft", "gửi cho\n@dungvivas nhé", "", "", "gửi cho\n@dungvivas nhé"},
		{"@ mid-line", "gửi @D:\\a.pdf nhé", "", "", "gửi @D:\\a.pdf nhé"},
		{"bare @", "@", "", "", "@"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, caption, text := parseDraft(tc.draft)
			if path != tc.wantPath || caption != tc.wantCaption || text != tc.wantText {
				t.Errorf("parseDraft(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tc.draft, path, caption, text, tc.wantPath, tc.wantCaption, tc.wantText)
			}
		})
	}
}

// TestCheckAndSend_SendsFile is the feature: @path in the compose area
// uploads that file instead of sending its text.
func TestCheckAndSend_SendsFile(t *testing.T) {
	dir := t.TempDir()
	attachment := filepath.Join(dir, "báo cáo.pdf")
	os.WriteFile(attachment, []byte("%PDF"), 0o600)

	path := filepath.Join(dir, "Nam.md")
	c := &chatFile{path: path, peer: telegram.Peer{ID: 88, Kind: "user"}}
	c.writeWithDraft("", "")

	var gotPath, gotCaption string
	var textSends int
	api := &telegram.FakeAPI{
		SendFileFn: func(ctx context.Context, peer telegram.Peer, p, caption string) (telegram.Message, error) {
			gotPath, gotCaption = p, caption
			return telegram.Message{ID: 20, Sender: "You", Text: caption, Media: "báo cáo.pdf", Outgoing: true}, nil
		},
		SendFn: func(ctx context.Context, peer telegram.Peer, text string) (telegram.Message, error) {
			textSends++
			return telegram.Message{}, nil
		},
	}

	appendDraft(t, path, "@"+attachment+"\ngửi anh xem nhé\n")
	c.checkAndSend(context.Background(), api, "")

	if gotPath != attachment {
		t.Errorf("uploaded %q, want %q", gotPath, attachment)
	}
	if gotCaption != "gửi anh xem nhé" {
		t.Errorf("caption = %q, want the trailing lines", gotCaption)
	}
	if textSends != 0 {
		t.Errorf("Send called %d times, want 0 — the draft was a file", textSends)
	}
	after, _ := os.ReadFile(path)
	if got := draftOf(string(after)); got != "" {
		t.Errorf("compose area = %q, want empty after send", got)
	}
	if !strings.Contains(string(after), "📎 báo cáo.pdf") {
		t.Errorf("attachment not in the log:\n%s", after)
	}
}

// TestCheckAndSend_RelativePathResolvesAgainstChatDir: the chat folder is the
// user's workspace root, so @anh.png should mean the file sitting next to
// the conversation.
func TestCheckAndSend_RelativePathResolvesAgainstChatDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "anh.png"), []byte("PNG"), 0o600)

	path := filepath.Join(dir, "Nam.md")
	c := &chatFile{path: path, peer: telegram.Peer{ID: 88, Kind: "user"}}
	c.writeWithDraft("", "")

	var gotPath string
	api := &telegram.FakeAPI{
		SendFileFn: func(ctx context.Context, peer telegram.Peer, p, caption string) (telegram.Message, error) {
			gotPath = p
			return telegram.Message{ID: 21, Media: "photo", Outgoing: true}, nil
		},
	}

	appendDraft(t, path, "@anh.png\n")
	c.checkAndSend(context.Background(), api, "")

	if want := filepath.Join(dir, "anh.png"); gotPath != want {
		t.Errorf("uploaded %q, want %q", gotPath, want)
	}
}

// TestCheckAndSend_MissingFileKeepsDraft: a typo in the path must not reach
// Telegram at all, and must leave the draft alone so it can be corrected.
func TestCheckAndSend_MissingFileKeepsDraft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Nam.md")
	c := &chatFile{path: path, peer: telegram.Peer{ID: 88, Kind: "user"}}
	c.writeWithDraft("", "")

	var uploads int
	api := &telegram.FakeAPI{
		SendFileFn: func(ctx context.Context, peer telegram.Peer, p, caption string) (telegram.Message, error) {
			uploads++
			return telegram.Message{}, nil
		},
	}

	appendDraft(t, path, "@khong-ton-tai.pdf\n")
	c.checkAndSend(context.Background(), api, "")

	if uploads != 0 {
		t.Errorf("SendFile called %d times for a missing file, want 0", uploads)
	}
	after, _ := os.ReadFile(path)
	if got := draftOf(string(after)); got != "@khong-ton-tai.pdf" {
		t.Errorf("draft = %q, want it kept so the path can be fixed", got)
	}
	if !strings.Contains(string(after), "không tìm thấy file") {
		t.Errorf("missing file not reported in the log:\n%s", after)
	}
}

// appendDraft simulates the editor writing a draft under the marker and
// bumping the mtime past our recorded one.
func appendDraft(t *testing.T, path, draft string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(b, []byte(draft)...), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	touch(t, path)
}
