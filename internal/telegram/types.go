// Package telegram wraps the gotd/td MTProto client behind a small interface.
// gotd types MUST NOT leak above this package boundary.
package telegram

import (
	"context"
	"time"
)

// Dialog is one conversation in the user's dialog list.
type Dialog struct {
	ID         int64
	Kind       string // "user", "group", or "channel" — needed by TUI to know peer type
	Title      string
	Unread     int
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
	// OnMessage registers a callback fired for each newly-received message.
	// Multiple calls replace the previous handler. The handler is called from
	// gotd's update goroutine — implementations must not block.
	OnMessage(handler func(Message))
}

// FakeAPI lets tests script API behavior via function fields. Nil fields return zero values with no error.
type FakeAPI struct {
	DialogsFn   func(ctx context.Context) ([]Dialog, error)
	HistoryFn   func(ctx context.Context, peer Peer, limit int) ([]Message, error)
	SendFn      func(ctx context.Context, peer Peer, text string) (Message, error)
	OnMessageFn func(handler func(Message))
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

func (f *FakeAPI) OnMessage(handler func(Message)) {
	if f.OnMessageFn != nil {
		f.OnMessageFn(handler)
	}
}
