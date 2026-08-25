package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"

	"github.com/user/tgchat/internal/telegram"
)

// newTestApp builds an App with the bare widgets handleIncoming touches: the
// chat TextView (active-peer path) plus the sidebar + a FakeAPI (non-active
// path triggers a dialogs refresh). No Application, no input — keeps the
// tests free of tview's event loop.
func newTestApp(dialogs []telegram.Dialog) (*App, *telegram.FakeAPI) {
	api := &telegram.FakeAPI{
		DialogsFn: func(ctx context.Context) ([]telegram.Dialog, error) {
			return dialogs, nil
		},
	}
	a := &App{
		chat:       tview.NewTextView(),
		chatHeader: tview.NewTextView(),
		sidebar:    tview.NewList(),
		api:        api,
		ctx:        context.Background(),
		activePeer: telegram.Peer{ID: 7, Kind: "user"},
	}
	return a, api
}

func TestHandleIncoming_AppendsForActivePeer(t *testing.T) {
	a, _ := newTestApp(nil)
	a.handleIncoming(telegram.Message{
		ID: 100, PeerID: 7, PeerKind: "user", Sender: "Alice", Text: "hi",
		Time: time.Now(),
	})
	if len(a.messages) != 1 || a.messages[0].ID != 100 {
		t.Errorf("messages = %+v, want one entry with ID=100", a.messages)
	}
	if a.chatRaw == "" {
		t.Error("chatRaw empty after append")
	}
}

// TestHandleIncoming_ActivePeer_MarksRead verifies a live message that
// arrives in the open chat fires MarkRead so the server-side unread
// badge clears immediately — the user is reading the stream in real time.
func TestHandleIncoming_ActivePeer_MarksRead(t *testing.T) {
	markReadCalls := 0
	var markedPeer telegram.Peer
	api := &telegram.FakeAPI{
		MarkReadFn: func(ctx context.Context, peer telegram.Peer) error {
			markReadCalls++
			markedPeer = peer
			return nil
		},
	}
	a := &App{
		chat:       tview.NewTextView(),
		chatHeader: tview.NewTextView(),
		api:        api,
		ctx:        context.Background(),
		activePeer: telegram.Peer{ID: 7, Kind: "user"},
	}
	a.handleIncoming(telegram.Message{
		ID: 101, PeerID: 7, PeerKind: "user", Text: "live",
	})
	if markReadCalls != 1 {
		t.Errorf("MarkRead calls = %d, want 1 (live msg in open chat must mark read)", markReadCalls)
	}
	if markedPeer.ID != 7 || markedPeer.Kind != "user" {
		t.Errorf("MarkRead peer = %+v, want {ID:7 Kind:user}", markedPeer)
	}
}

// TestHandleIncoming_OtherPeer_NoMarkRead ensures we don't mark-read for
// messages in OTHER dialogs — only the active chat triggers MarkRead on
// live update. Other chats refresh the sidebar so the unread count
// stays accurate until the user opens them.
func TestHandleIncoming_OtherPeer_NoMarkRead(t *testing.T) {
	markReadCalls := 0
	dialogsCalls := 0
	api := &telegram.FakeAPI{
		MarkReadFn: func(ctx context.Context, peer telegram.Peer) error {
			markReadCalls++
			return nil
		},
		DialogsFn: func(ctx context.Context) ([]telegram.Dialog, error) {
			dialogsCalls++
			return nil, nil // triggers refreshDialogs branch on non-active peer
		},
	}
	a := &App{
		chat:       tview.NewTextView(),
		chatHeader: tview.NewTextView(),
		sidebar:    tview.NewList(),
		api:        api,
		ctx:        context.Background(),
		activePeer: telegram.Peer{ID: 7, Kind: "user"},
	}
	a.handleIncoming(telegram.Message{
		ID: 200, PeerID: 99, PeerKind: "user", Text: "from other chat",
	})
	if markReadCalls != 0 {
		t.Errorf("MarkRead calls = %d, want 0 (non-active peer must NOT mark read)", markReadCalls)
	}
	if dialogsCalls == 0 {
		t.Errorf("refreshDialogs not called on non-active peer (sidebar won't update)")
	}
}

// TestHandleIncoming_MarkReadFailure_NonFatal: a mark-read failure (e.g.
// transient network) must not stop the chat from rendering. The message
// is still appended; we just surface a toast and let the next refresh
// pick up the server-side state.
func TestHandleIncoming_MarkReadFailure_NonFatal(t *testing.T) {
	api := &telegram.FakeAPI{
		MarkReadFn: func(ctx context.Context, peer telegram.Peer) error {
			return errors.New("rate limited")
		},
	}
	a := &App{
		chat:       tview.NewTextView(),
		chatHeader: tview.NewTextView(),
		status:     tview.NewTextView(),
		api:        api,
		ctx:        context.Background(),
		activePeer: telegram.Peer{ID: 7, Kind: "user"},
	}
	a.handleIncoming(telegram.Message{
		ID: 300, PeerID: 7, PeerKind: "user", Text: "still rendered",
	})
	if len(a.messages) != 1 {
		t.Errorf("messages = %+v, want 1 entry (mark-read failure must not block render)", a.messages)
	}
}

func TestHandleIncoming_SkipsOtherPeerAndRefreshesDialogs(t *testing.T) {
	dialogs := []telegram.Dialog{
		{ID: 7, Kind: "user", Title: "Alice"},
		{ID: 99, Kind: "user", Title: "Bob"},
	}
	a, api := newTestApp(dialogs)
	calls := 0
	api.DialogsFn = func(ctx context.Context) ([]telegram.Dialog, error) {
		calls++
		return dialogs, nil
	}
	a.handleIncoming(telegram.Message{
		ID: 100, PeerID: 99, PeerKind: "user", Text: "from Bob",
	})
	if len(a.messages) != 0 {
		t.Errorf("message from wrong peer was appended: %+v", a.messages)
	}
	if calls != 1 {
		t.Errorf("expected refreshDialogs to call api.Dialogs once, got %d", calls)
	}
	// Sidebar should now reflect the new list (still both dialogs).
	if a.sidebar.GetItemCount() != 2 {
		t.Errorf("sidebar item count = %d, want 2", a.sidebar.GetItemCount())
	}
}

func TestHandleIncoming_SkipsDifferentKind(t *testing.T) {
	a, _ := newTestApp(nil)
	// Same numeric ID but group instead of user — should not append and
	// should hit the dialogs refresh branch.
	a.handleIncoming(telegram.Message{
		ID: 7, PeerID: 7, PeerKind: "group", Text: "x",
	})
	if len(a.messages) != 0 {
		t.Errorf("different-kind peer was appended: %+v", a.messages)
	}
}

func TestHandleIncoming_DedupesByID(t *testing.T) {
	a, _ := newTestApp(nil)
	// First append (e.g. optimistic sendMessage result).
	a.messages = []telegram.Message{{ID: 42, PeerID: 7, PeerKind: "user", Text: "echo", Outgoing: true}}
	a.chatRaw = RenderHistory(a.messages, chatPaneWidth(a.chat))
	// Server echoes the same message via an update we forgot to filter.
	a.handleIncoming(telegram.Message{
		ID: 42, PeerID: 7, PeerKind: "user", Text: "echo", Outgoing: true,
	})
	if len(a.messages) != 1 {
		t.Errorf("duplicate appended: messages = %+v", a.messages)
	}
}

// TestRefreshDialogs_PreservesActiveIdx verifies active selection survives a
// list reorder: the active peer at index 1 moves to index 0 because a new
// incoming message bubbles its dialog to the top, and activeIdx must follow.
func TestRefreshDialogs_PreservesActiveIdx(t *testing.T) {
	dialogsBefore := []telegram.Dialog{
		{ID: 99, Kind: "user", Title: "Bob"},
		{ID: 7, Kind: "user", Title: "Alice"}, // activeIdx=1
	}
	dialogsAfter := []telegram.Dialog{
		{ID: 7, Kind: "user", Title: "Alice"}, // bumped to top
		{ID: 99, Kind: "user", Title: "Bob"},
	}
	api := &telegram.FakeAPI{
		DialogsFn: func(ctx context.Context) ([]telegram.Dialog, error) {
			return dialogsAfter, nil
		},
	}
	a := &App{
		sidebar: tview.NewList(),
		api:     api,
		dialogs: dialogsBefore,
		activeIdx: 1,
		ctx:     context.Background(),
	}
	a.refreshDialogs()
	if a.activeIdx != 0 {
		t.Errorf("activeIdx = %d, want 0 (followed Alice to top)", a.activeIdx)
	}
	if a.sidebar.GetCurrentItem() != 0 {
		t.Errorf("sidebar current = %d, want 0", a.sidebar.GetCurrentItem())
	}
}

// TestLoadHistory_CallsMarkReadAndRefreshesDialogs verifies opening a dialog
// triggers MarkRead (so the server's unread badge clears) AND refreshes the
// sidebar so the cached dialog list reflects the cleared unread state.
func TestLoadHistory_CallsMarkReadAndRefreshesDialogs(t *testing.T) {
	dialogs := []telegram.Dialog{
		{ID: 7, Kind: "user", Title: "Alice", Unread: 5},
	}
	historyCalls := 0
	markReadCalls := 0
	dialogsCalls := 0
	var markedPeer telegram.Peer
	api := &telegram.FakeAPI{
		HistoryFn: func(ctx context.Context, peer telegram.Peer, limit int) ([]telegram.Message, error) {
			historyCalls++
			return []telegram.Message{{ID: 100, PeerID: 7, PeerKind: "user", Text: "hi"}}, nil
		},
		MarkReadFn: func(ctx context.Context, peer telegram.Peer) error {
			markReadCalls++
			markedPeer = peer
			return nil
		},
		DialogsFn: func(ctx context.Context) ([]telegram.Dialog, error) {
			dialogsCalls++
			return dialogs, nil
		},
	}
	a := &App{
		chat:       tview.NewTextView(),
		chatHeader: tview.NewTextView(),
		sidebar:    tview.NewList(),
		api:        api,
		ctx:        context.Background(),
		dialogs:    dialogs,
		activeIdx:  0,
	}
	a.loadHistory(50)
	if historyCalls != 1 {
		t.Errorf("History calls = %d, want 1", historyCalls)
	}
	if markReadCalls != 1 {
		t.Errorf("MarkRead calls = %d, want 1", markReadCalls)
	}
	if markedPeer.ID != 7 || markedPeer.Kind != "user" {
		t.Errorf("MarkRead peer = %+v, want {ID:7, Kind:user}", markedPeer)
	}
	if dialogsCalls < 1 {
		// refreshDialogs is what fetches the post-read dialog list. Called
		// at least once at the end of loadHistory.
		t.Errorf("Dialogs calls = %d, want >= 1 (refresh after mark-read)", dialogsCalls)
	}
}

// TestLoadHistory_MarkReadFailureDoesNotBlockUI verifies a mark-read failure
// (e.g. transient network) surfaces as a toast but doesn't stop the chat
// from rendering.
func TestLoadHistory_MarkReadFailureDoesNotBlockUI(t *testing.T) {
	dialogs := []telegram.Dialog{{ID: 7, Kind: "user", Title: "Alice"}}
	api := &telegram.FakeAPI{
		HistoryFn: func(ctx context.Context, peer telegram.Peer, limit int) ([]telegram.Message, error) {
			return []telegram.Message{{ID: 1, PeerID: 7, PeerKind: "user", Text: "hi"}}, nil
		},
		MarkReadFn: func(ctx context.Context, peer telegram.Peer) error {
			return errors.New("rate limited")
		},
	}
	a := &App{
		chat:       tview.NewTextView(),
		chatHeader: tview.NewTextView(),
		sidebar:    tview.NewList(),
		status:     tview.NewTextView(),
		api:        api,
		ctx:        context.Background(),
		dialogs:    dialogs,
		activeIdx:  0,
	}
	a.loadHistory(50)
	if len(a.messages) != 1 {
		t.Errorf("messages = %+v, want 1 entry (mark-read failure should not block UI)", a.messages)
	}
}

// TestSetChatHeader_UpdatesTitle covers the one-line banner above the
// chat pane: opening a dialog must show "chat - <title>" so the user
// always knows which conversation they're reading.
func TestSetChatHeader_UpdatesTitle(t *testing.T) {
	a := &App{chatHeader: tview.NewTextView()}
	a.setChatHeader("Alice")
	if got := a.chatHeader.GetText(true); got != chatHeaderPrefix+"Alice" {
		t.Errorf("header text = %q, want %q", got, chatHeaderPrefix+"Alice")
	}
}

// TestSetChatHeader_EmptyFallsBackToPlaceholder: an unset title shouldn't
// render as a dangling "chat - " — show "(no chat open)" so the layout
// stays stable before any dialog is opened.
func TestSetChatHeader_EmptyFallsBackToPlaceholder(t *testing.T) {
	a := &App{chatHeader: tview.NewTextView()}
	a.setChatHeader("")
	if got := a.chatHeader.GetText(true); !strings.Contains(got, "(no chat open)") {
		t.Errorf("header text = %q, want placeholder for empty title", got)
	}
}

// TestSetChatHeader_NilSafe: build order matters — loadHistory can be
// reached before chatHeader is wired in some paths. setChatHeader must
// no-op instead of panicking so future refactors can change the init
// order without breaking the chat path.
func TestSetChatHeader_NilSafe(t *testing.T) {
	a := &App{}
	// Should not panic.
	a.setChatHeader("Alice")
}
