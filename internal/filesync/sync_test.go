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

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir has %d files, want only the synced one", len(entries))
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
