package filesync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/user/tgchat/internal/telegram"
)

func dumpAPI(dialogs []telegram.Dialog, hist map[int64][]telegram.Message) *telegram.FakeAPI {
	return &telegram.FakeAPI{
		DialogsFn: func(ctx context.Context) ([]telegram.Dialog, error) { return dialogs, nil },
		HistoryFn: func(ctx context.Context, peer telegram.Peer, limit int) ([]telegram.Message, error) {
			return hist[peer.ID], nil
		},
	}
}

// TestRun_DumpsDialogsToFiles: opening the folder must show the recent
// conversations with their history already in place.
func TestRun_DumpsDialogsToFiles(t *testing.T) {
	dir := t.TempDir()
	api := dumpAPI(
		[]telegram.Dialog{
			{ID: 88, Kind: "user", Title: "Nam", LastTime: time.Now()},
			{ID: 55, Kind: "group", Title: "Nhóm Dev", LastTime: time.Now().Add(-time.Hour)},
		},
		map[int64][]telegram.Message{
			88: {{ID: 1, Sender: "Nam", Text: "chào"}},
			55: {{ID: 2, Sender: "Linh", Text: "deploy chưa"}},
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	go Run(ctx, api, "", dir)
	waitForFile(t, filepath.Join(dir, "Nhóm Dev.md"))
	cancel()

	for name, want := range map[string]string{"Nam.md": "chào", "Nhóm Dev.md": "deploy chưa"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(b), want) {
			t.Errorf("%s missing %q:\n%s", name, want, b)
		}
		if !strings.Contains(string(b), Marker) {
			t.Errorf("%s missing the compose marker", name)
		}
	}
}

// TestRun_CapsDialogCount: accounts with hundreds of dialogs must not fire
// hundreds of History calls at startup and trip flood-wait.
func TestRun_CapsDialogCount(t *testing.T) {
	dir := t.TempDir()
	var dialogs []telegram.Dialog
	for i := 0; i < maxDialogs+5; i++ {
		dialogs = append(dialogs, telegram.Dialog{
			ID: int64(i), Kind: "user", Title: fmt.Sprintf("chat%d", i),
			LastTime: time.Now().Add(-time.Duration(i) * time.Minute),
		})
	}
	// Atomic: Run counts on its own goroutine while the test reads.
	var histCalls atomic.Int64
	api := &telegram.FakeAPI{
		DialogsFn: func(ctx context.Context) ([]telegram.Dialog, error) { return dialogs, nil },
		HistoryFn: func(ctx context.Context, peer telegram.Peer, limit int) ([]telegram.Message, error) {
			histCalls.Add(1)
			return nil, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	go Run(ctx, api, "", dir)
	waitFor(t, func() bool { return histCalls.Load() >= maxDialogs })
	cancel()

	if got := histCalls.Load(); got != maxDialogs {
		t.Errorf("History called %d times, want %d", got, maxDialogs)
	}
}

// TestRun_IncomingMessageAppendsToFile: live updates land in the file
// without the user touching the TUI.
func TestRun_IncomingMessageAppendsToFile(t *testing.T) {
	dir := t.TempDir()
	registered := make(chan func(telegram.Message), 1)
	api := dumpAPI(
		[]telegram.Dialog{{ID: 88, Kind: "user", Title: "Nam", LastTime: time.Now()}},
		map[int64][]telegram.Message{88: nil},
	)
	api.OnMessageFn = func(h func(telegram.Message)) { registered <- h }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, api, "", dir)
	handler := <-registered

	handler(telegram.Message{ID: 9, PeerID: 88, PeerKind: "user", Sender: "Nam", Text: "tin mới đến"})

	waitFor(t, func() bool {
		b, _ := os.ReadFile(filepath.Join(dir, "Nam.md"))
		return strings.Contains(string(b), "tin mới đến")
	})
}

// TestRun_IgnoresUnsyncedPeer: messages from chats outside the synced set
// must not create stray files.
func TestRun_IgnoresUnsyncedPeer(t *testing.T) {
	dir := t.TempDir()
	registered := make(chan func(telegram.Message), 1)
	api := dumpAPI(
		[]telegram.Dialog{{ID: 88, Kind: "user", Title: "Nam", LastTime: time.Now()}},
		map[int64][]telegram.Message{88: nil},
	)
	api.OnMessageFn = func(h func(telegram.Message)) { registered <- h }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, api, "", dir)
	handler := <-registered

	handler(telegram.Message{ID: 9, PeerID: 999, PeerKind: "user", Sender: "Ai đó", Text: "lạ"})
	time.Sleep(50 * time.Millisecond)

	if n := mdCount(t, dir); n != 1 {
		t.Errorf("dir has %d chat files, want only the synced one", n)
	}
}

// TestRun_PollSendsSavedDraft is the end-to-end path the user actually
// exercises: edit the file in an editor, save, message goes out.
func TestRun_PollSendsSavedDraft(t *testing.T) {
	dir := t.TempDir()
	sent := make(chan string, 1)
	api := dumpAPI(
		[]telegram.Dialog{{ID: 88, Kind: "user", Title: "Nam", LastTime: time.Now()}},
		map[int64][]telegram.Message{88: nil},
	)
	api.SendFn = func(ctx context.Context, peer telegram.Peer, text string) (telegram.Message, error) {
		sent <- text
		return telegram.Message{ID: 10, Text: text, Outgoing: true}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, api, "", dir)
	path := filepath.Join(dir, "Nam.md")
	waitForFile(t, path)

	b, _ := os.ReadFile(path)
	os.WriteFile(path, append(b, []byte("gửi từ editor\n")...), 0o600)
	future := time.Now().Add(2 * time.Second)
	os.Chtimes(path, future, future)

	select {
	case got := <-sent:
		if got != "gửi từ editor" {
			t.Errorf("sent %q, want %q", got, "gửi từ editor")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("draft was never sent")
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	waitFor(t, func() bool { _, err := os.Stat(path); return err == nil })
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within 3s")
}

// TestRun_SkipsBotDialogs: bot chats are notification feeds, not
// conversations — they would bury the real chats in the folder.
func TestRun_SkipsBotDialogs(t *testing.T) {
	dir := t.TempDir()
	api := dumpAPI(
		[]telegram.Dialog{
			{ID: 88, Kind: "user", Title: "Nam", LastTime: time.Now()},
			{ID: 77, Kind: "user", Title: "GitHubBot", Bot: true, LastTime: time.Now().Add(-time.Minute)},
		},
		map[int64][]telegram.Message{
			88: {{ID: 1, Sender: "Nam", Text: "chào"}},
			77: {{ID: 2, Sender: "GitHubBot", Text: "build passed"}},
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, api, "", dir)
	waitForFile(t, filepath.Join(dir, "Nam.md"))
	time.Sleep(50 * time.Millisecond)

	if _, err := os.Stat(filepath.Join(dir, "GitHubBot.md")); err == nil {
		t.Error("bot dialog was mirrored, want it skipped")
	}
	if n := mdCount(t, dir); n != 1 {
		t.Errorf("dir has %d chat files, want only the human conversation", n)
	}
}

// TestRun_BotMessagesAreIgnored: a bot's live updates must not resurrect it
// as a file either.
func TestRun_BotMessagesAreIgnored(t *testing.T) {
	dir := t.TempDir()
	registered := make(chan func(telegram.Message), 1)
	api := dumpAPI(
		[]telegram.Dialog{
			{ID: 88, Kind: "user", Title: "Nam", LastTime: time.Now()},
			{ID: 77, Kind: "user", Title: "GitHubBot", Bot: true, LastTime: time.Now().Add(-time.Minute)},
		},
		map[int64][]telegram.Message{88: nil, 77: nil},
	)
	api.OnMessageFn = func(h func(telegram.Message)) { registered <- h }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, api, "", dir)
	handler := <-registered

	handler(telegram.Message{ID: 9, PeerID: 77, PeerKind: "user", Sender: "GitHubBot", Text: "build passed"})
	time.Sleep(50 * time.Millisecond)

	if n := mdCount(t, dir); n != 1 {
		t.Errorf("dir has %d chat files, want only the human conversation", n)
	}
}

// TestMaxDialogs pins the cap: 50 is what the user asked for, and a silent
// drop back to a smaller number would quietly hide conversations.
func TestMaxDialogs(t *testing.T) {
	if maxDialogs != 50 {
		t.Errorf("maxDialogs = %d, want 50", maxDialogs)
	}
}

// TestRun_WritesEditorSettings: VS Code sorts the explorer alphabetically by
// default, which buries the active conversation. Shipping a workspace
// settings file is what makes "most recent on top" work without renaming
// files on every message.
func TestRun_WritesEditorSettings(t *testing.T) {
	dir := t.TempDir()
	api := dumpAPI(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, api, "", dir)

	settings := filepath.Join(dir, ".vscode", "settings.json")
	waitForFile(t, settings)

	b, _ := os.ReadFile(settings)
	for _, want := range []string{`"explorer.sortOrder": "modified"`, `"files.saveConflictResolution": "overwriteFileOnDisk"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("settings.json missing %s:\n%s", want, b)
		}
	}
}

// TestRun_KeepsExistingEditorSettings: the folder is the user's workspace —
// their own settings must survive a restart.
func TestRun_KeepsExistingEditorSettings(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".vscode"), 0o700)
	settings := filepath.Join(dir, ".vscode", "settings.json")
	os.WriteFile(settings, []byte(`{"editor.fontSize": 20}`), 0o600)

	api := dumpAPI([]telegram.Dialog{{ID: 88, Kind: "user", Title: "Nam", LastTime: time.Now()}},
		map[int64][]telegram.Message{88: nil})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, api, "", dir)
	waitForFile(t, filepath.Join(dir, "Nam.md"))

	b, _ := os.ReadFile(settings)
	if string(b) != `{"editor.fontSize": 20}` {
		t.Errorf("existing settings overwritten:\n%s", b)
	}
}

// TestRun_FileMtimeMatchesLastMessage: "sort by modified" only orders the
// folder correctly if mtime reflects the conversation's recency. The initial
// dump writes newest-first, so without this the freshest chat would carry the
// OLDEST mtime of the batch — exactly backwards.
func TestRun_FileMtimeMatchesLastMessage(t *testing.T) {
	dir := t.TempDir()
	newest := time.Now().Add(-1 * time.Minute).Truncate(time.Second)
	oldest := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	api := dumpAPI(
		[]telegram.Dialog{
			{ID: 88, Kind: "user", Title: "Nam", LastTime: newest},
			{ID: 55, Kind: "group", Title: "Cũ", LastTime: oldest},
		},
		map[int64][]telegram.Message{88: nil, 55: nil},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, api, "", dir)
	waitForFile(t, filepath.Join(dir, "Cũ.md"))

	fiNew, err := os.Stat(filepath.Join(dir, "Nam.md"))
	if err != nil {
		t.Fatalf("stat Nam.md: %v", err)
	}
	fiOld, err := os.Stat(filepath.Join(dir, "Cũ.md"))
	if err != nil {
		t.Fatalf("stat Cũ.md: %v", err)
	}
	if !fiNew.ModTime().After(fiOld.ModTime()) {
		t.Errorf("Nam mtime %v not after Cũ mtime %v — explorer would sort them backwards",
			fiNew.ModTime(), fiOld.ModTime())
	}
	if d := fiNew.ModTime().Sub(newest); d > time.Second || d < -time.Second {
		t.Errorf("Nam mtime = %v, want ≈ LastTime %v", fiNew.ModTime(), newest)
	}
}

// TestRun_BackdatedMtimeDoesNotLookLikeAUserSave: the loop guard compares the
// mtime we recorded after our own write. Backdating the file must update that
// record too, or the very first poll mistakes it for an edit.
func TestRun_BackdatedMtimeDoesNotLookLikeAUserSave(t *testing.T) {
	dir := t.TempDir()
	var sendCalls atomic.Int64
	api := dumpAPI(
		[]telegram.Dialog{{ID: 88, Kind: "user", Title: "Nam", LastTime: time.Now().Add(-time.Hour)}},
		map[int64][]telegram.Message{88: {{ID: 1, Sender: "Nam", Text: "chào"}}},
	)
	api.SendFn = func(ctx context.Context, peer telegram.Peer, text string) (telegram.Message, error) {
		sendCalls.Add(1)
		return telegram.Message{ID: 2, Text: text}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, api, "", dir)
	waitForFile(t, filepath.Join(dir, "Nam.md"))

	// Long enough for several poll ticks.
	time.Sleep(3 * pollInterval)

	if n := sendCalls.Load(); n != 0 {
		t.Errorf("Send called %d times with no user edit, want 0", n)
	}
}

// mdCount counts mirrored conversations, ignoring the .vscode settings
// folder filesync also writes.
func mdCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}
