// Package telegram wraps the gotd/td MTProto client behind a small interface.
// gotd types MUST NOT leak above this package boundary.
package telegram

import (
	"context"
	"io"
	"time"
)

// MediaInfo describes a downloadable attachment — enough for HTTP headers
// (filename + byte length) before streaming starts.
type MediaInfo struct {
	Name string
	Size int64
}

// Dialog is one conversation in the user's dialog list.
type Dialog struct {
	ID         int64
	Kind       string // "user", "group", or "channel" — needed by TUI to know peer type
	Title      string
	Unread     int
	Muted      bool // server-side notifications muted (NotifySettings.MuteUntil in the future) — TUI skips Windows pushes for these
	Bot        bool // peer is a bot account — filesync skips these, they are noise in a folder of conversations
	LastMsg    string
	LastTime   time.Time
	AccessHash int64 // required to address users/channels via the API
}

// Message is one chat message.
//
// PeerID/PeerKind identify which chat this message belongs to so the TUI can
// route incoming updates to the currently visible dialog. They mirror the
// fields of a Peer, embedded directly to keep the API struct flat.
type Message struct {
	ID       int64
	PeerID   int64
	PeerKind string // "user", "group", or "channel" — matches Peer.Kind
	Sender   string // display name; "You" if Outgoing
	Text     string
	// Media describes non-text content for surfaces that can't render it
	// (desktop toasts, previews): "" for plain text, else a kind keyword
	// ("photo", "voice", "video", ...) or the filename of a named document.
	Media    string
	Time     time.Time
	Outgoing bool
}

// Peer identifies a chat target. Kind is "user", "group", or "channel".
// AccessHash is required for user/channel destinations; 0 for group chats.
type Peer struct {
	ID         int64
	Kind       string
	AccessHash int64
}

// API is the surface the TUI consumes. *Client implements it; tests use *FakeAPI.
type API interface {
	Dialogs(ctx context.Context) ([]Dialog, error)
	History(ctx context.Context, peer Peer, limit int) ([]Message, error)
	Send(ctx context.Context, peer Peer, text string) (Message, error)
	// MarkRead notifies the server that all messages in `peer` are read.
	// TUI calls this when opening a dialog so the unread badge clears.
	MarkRead(ctx context.Context, peer Peer) error
	// Stat describes the media attached to message msgID in peer.
	Stat(ctx context.Context, peer Peer, msgID int64) (MediaInfo, error)
	// Download streams that media into w (no full-file buffering).
	Download(ctx context.Context, peer Peer, msgID int64, w io.Writer) (MediaInfo, error)
	// OnMessage registers a callback fired for each newly-received message.
	// Every registered handler is called, in registration order — the TUI and
	// filesync both subscribe. The handler is called from gotd's update
	// goroutine — implementations must not block.
	OnMessage(handler func(Message))
}

// FakeAPI lets tests script API behavior via function fields. Nil fields return zero values with no error.
type FakeAPI struct {
	DialogsFn   func(ctx context.Context) ([]Dialog, error)
	HistoryFn   func(ctx context.Context, peer Peer, limit int) ([]Message, error)
	SendFn      func(ctx context.Context, peer Peer, text string) (Message, error)
	MarkReadFn  func(ctx context.Context, peer Peer) error
	StatFn      func(ctx context.Context, peer Peer, msgID int64) (MediaInfo, error)
	DownloadFn  func(ctx context.Context, peer Peer, msgID int64, w io.Writer) (MediaInfo, error)
	OnMessageFn func(handler func(Message))
}

func (f *FakeAPI) Stat(ctx context.Context, peer Peer, msgID int64) (MediaInfo, error) {
	if f.StatFn == nil {
		return MediaInfo{}, nil
	}
	return f.StatFn(ctx, peer, msgID)
}

func (f *FakeAPI) Download(ctx context.Context, peer Peer, msgID int64, w io.Writer) (MediaInfo, error) {
	if f.DownloadFn == nil {
		return MediaInfo{}, nil
	}
	return f.DownloadFn(ctx, peer, msgID, w)
}

func (f *FakeAPI) Dialogs(ctx context.Context) ([]Dialog, error) {
	if f.DialogsFn == nil {
		return nil, nil
	}
	return f.DialogsFn(ctx)
}

func (f *FakeAPI) History(ctx context.Context, peer Peer, limit int) ([]Message, error) {
	if f.HistoryFn == nil {
		return nil, nil
	}
	return f.HistoryFn(ctx, peer, limit)
}

func (f *FakeAPI) Send(ctx context.Context, peer Peer, text string) (Message, error) {
	if f.SendFn == nil {
		return Message{}, nil
	}
	return f.SendFn(ctx, peer, text)
}

func (f *FakeAPI) MarkRead(ctx context.Context, peer Peer) error {
	if f.MarkReadFn == nil {
		return nil
	}
	return f.MarkReadFn(ctx, peer)
}

func (f *FakeAPI) OnMessage(handler func(Message)) {
	if f.OnMessageFn != nil {
		f.OnMessageFn(handler)
	}
}
