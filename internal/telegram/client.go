package telegram

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

// Client wraps gotd/td behind our API interface.
type Client struct {
	raw     *telegram.Client
	storage session.Storage
}

// New constructs a Client. sessionFile is the path to a JSON file (gotd-managed).
// Call Run(ctx) to actually connect.
func New(ctx context.Context, appID int, apiHash, sessionFile string) (*Client, error) {
	storage := &session.FileStorage{Path: sessionFile}
	raw := telegram.NewClient(appID, apiHash, telegram.Options{
		SessionStorage: storage,
	})
	return &Client{raw: raw, storage: storage}, nil
}

// Raw exposes the underlying gotd client. main.go uses it to hand the client
// to auth.Run for first-run authentication; nothing else above this package
// should touch it.
func (c *Client) Raw() *telegram.Client {
	return c.raw
}

// Run starts the client and blocks until ctx is done. Auth must already be complete.
func (c *Client) Run(ctx context.Context) error {
	return c.raw.Run(ctx, func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})
}

// SelfName returns the current user's display name (e.g. "@alice" or "Alice").
// Requires client.Run() to have been started so the gotd client is connected.
func (c *Client) SelfName(ctx context.Context) (string, error) {
	u, err := c.raw.Self(ctx)
	if err != nil {
		return "", fmt.Errorf("fetch self: %w", err)
	}
	if u.Username != "" {
		return "@" + u.Username, nil
	}
	return u.FirstName, nil
}

func (c *Client) Close() error {
	// gotd client has no explicit Close; storage closes when GC'd.
	// Future: explicit cleanup if needed.
	return nil
}

// Dialogs returns the user's recent conversations via gotd's messages.getDialogs.
// Maps gotd types to our plain Dialog struct.
func (c *Client) Dialogs(ctx context.Context) ([]Dialog, error) {
	api := c.raw.API()
	res, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		Limit: 100,
	})
	if err != nil {
		return nil, fmt.Errorf("get dialogs: %w", err)
	}

	// gotd returns one of several slice variants; normalize to []DialogClass.
	// The same response bundles Users/Chats with full objects (including access_hash),
	// which peerTitle uses to avoid per-peer API calls.
	var rawDialogs []tg.DialogClass
	var users []tg.UserClass
	var chats []tg.ChatClass
	switch d := res.(type) {
	case *tg.MessagesDialogs:
		rawDialogs = d.Dialogs
		users = d.Users
		chats = d.Chats
	case *tg.MessagesDialogsSlice:
		rawDialogs = d.Dialogs
		users = d.Users
		chats = d.Chats
	default:
		return nil, fmt.Errorf("unexpected getDialogs response type %T", res)
	}

	userByID := make(map[int64]*tg.User, len(users))
	for _, u := range users {
		if u, ok := u.(*tg.User); ok {
			userByID[u.ID] = u
		}
	}
	chatByID := make(map[int64]tg.ChatClass, len(chats))
	for _, ch := range chats {
		chatByID[ch.GetID()] = ch
	}

	out := make([]Dialog, 0, len(rawDialogs))
	for _, dc := range rawDialogs {
		d, ok := dc.(*tg.Dialog)
		if !ok {
			continue
		}
		dlg, err := c.dialogFromGotd(ctx, *d, userByID, chatByID)
		if err != nil {
			continue // skip unresolvable; don't fail the whole list
		}
		out = append(out, dlg)
	}
	return out, nil
}

// dialogFromGotd extracts our Dialog from a gotd Dialog (which only carries peer + last msg metadata).
func (c *Client) dialogFromGotd(ctx context.Context, d tg.Dialog, users map[int64]*tg.User, chats map[int64]tg.ChatClass) (Dialog, error) {
	title, err := c.peerTitle(ctx, d.Peer, users, chats)
	if err != nil {
		return Dialog{}, err
	}
	return Dialog{
		ID:         peerID(d.Peer),
		Kind:       peerKind(d.Peer),
		Title:      title,
		Unread:     d.UnreadCount,
		AccessHash: c.accessHash(d.Peer, users, chats),
	}, nil
}

// accessHash extracts the access_hash for user/channel peers from the bundled
// Users/Chats maps. Returns 0 for groups (which don't need one) or unknown peers.
func (c *Client) accessHash(p tg.PeerClass, users map[int64]*tg.User, chats map[int64]tg.ChatClass) int64 {
	switch v := p.(type) {
	case *tg.PeerUser:
		if u, ok := users[v.UserID]; ok {
			return u.AccessHash
		}
	case *tg.PeerChannel:
		if ch, ok := chats[v.ChannelID]; ok {
			if cc, ok := ch.(*tg.Channel); ok {
				if ah, ok := cc.GetAccessHash(); ok {
					return ah
				}
				return cc.AccessHash
			}
			if cf, ok := ch.(*tg.ChannelForbidden); ok {
				return cf.GetAccessHash()
			}
		}
	}
	return 0
}

// History returns the most recent `limit` messages from `peer` (oldest-first within the window).
func (c *Client) History(ctx context.Context, peer Peer, limit int) ([]Message, error) {
	inputPeer, err := c.inputPeer(peer)
	if err != nil {
		return nil, err
	}
	res, err := c.raw.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:  inputPeer,
		Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}

	// gotd returns one of several slice variants; normalize. Each carries
	// bundled Users/Chats — use those to resolve sender names instead of
	// per-peer API calls (Task 10 fix).
	var msgs []tg.MessageClass
	var users []tg.UserClass
	var chats []tg.ChatClass
	switch m := res.(type) {
	case *tg.MessagesMessages:
		msgs = m.Messages
		users = m.Users
		chats = m.Chats
	case *tg.MessagesMessagesSlice:
		msgs = m.Messages
		users = m.Users
		chats = m.Chats
	case *tg.MessagesChannelMessages:
		msgs = m.Messages
		users = m.Users
		chats = m.Chats
	default:
		return nil, fmt.Errorf("unexpected history response type %T", res)
	}

	userByID := make(map[int64]*tg.User, len(users))
	for _, u := range users {
		if u, ok := u.(*tg.User); ok {
			userByID[u.ID] = u
		}
	}
	chatByID := make(map[int64]tg.ChatClass, len(chats))
	for _, ch := range chats {
		chatByID[ch.GetID()] = ch
	}

	out := make([]Message, 0, len(msgs))
	for _, mc := range msgs {
		if mm, ok := mc.(*tg.Message); ok {
			out = append(out, c.messageFromGotd(ctx, mm, userByID, chatByID))
		}
	}
	return out, nil
}

func (c *Client) messageFromGotd(ctx context.Context, m *tg.Message, users map[int64]*tg.User, chats map[int64]tg.ChatClass) Message {
	sender := "unknown"
	if m.FromID != nil {
		if title, _ := c.peerTitle(ctx, m.FromID, users, chats); title != "" {
			sender = title
		} else {
			sender = fmt.Sprint(peerID(m.FromID))
		}
	}
	if m.Out {
		sender = "You"
	}
	return Message{
		ID:       int64(m.ID),
		Sender:   sender,
		Text:     m.Message,
		Time:     time.Unix(int64(m.Date), 0),
		Outgoing: m.Out,
	}
}

// Send posts `text` to `peer` and returns the echoed message.
func (c *Client) Send(ctx context.Context, peer Peer, text string) (Message, error) {
	inputPeer, err := c.inputPeer(peer)
	if err != nil {
		return Message{}, err
	}
	res, err := c.raw.API().MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
		Peer:     inputPeer,
		Message:  text,
		RandomID: randInt64(),
	})
	if err != nil {
		return Message{}, fmt.Errorf("send: %w", err)
	}
	echo := func(id int) Message {
		return Message{ID: int64(id), Sender: "You", Text: text, Time: time.Now(), Outgoing: true}
	}
	// gotd returns UpdatesClass — different concrete types per destination.
	// Pull UpdateMessageID (groups/channels) or fall through to UpdateShortMessage (P2P).
	switch u := res.(type) {
	case *tg.UpdateShortMessage:
		return echo(u.ID), nil
	case *tg.UpdatesCombined:
		for _, x := range u.Updates {
			if m, ok := x.(*tg.UpdateMessageID); ok {
				return echo(m.ID), nil
			}
		}
	case *tg.Updates:
		for _, x := range u.Updates {
			if m, ok := x.(*tg.UpdateMessageID); ok {
				return echo(m.ID), nil
			}
		}
	}
	return echo(0), nil
}

func (c *Client) inputPeer(p Peer) (tg.InputPeerClass, error) {
	switch p.Kind {
	case "user":
		if p.AccessHash == 0 {
			return nil, fmt.Errorf("inputPeer: %s peer %d has no access_hash; refresh dialogs", p.Kind, p.ID)
		}
		return &tg.InputPeerUser{UserID: p.ID, AccessHash: p.AccessHash}, nil
	case "group":
		return &tg.InputPeerChat{ChatID: p.ID}, nil
	case "channel":
		if p.AccessHash == 0 {
			return nil, fmt.Errorf("inputPeer: %s peer %d has no access_hash; refresh dialogs", p.Kind, p.ID)
		}
		return &tg.InputPeerChannel{ChannelID: p.ID, AccessHash: p.AccessHash}, nil
	default:
		return nil, fmt.Errorf("unknown peer kind %q", p.Kind)
	}
}

func randInt64() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int64(binary.LittleEndian.Uint64(b[:]))
}

func peerID(p tg.PeerClass) int64 {
	switch v := p.(type) {
	case *tg.PeerUser:
		return v.UserID
	case *tg.PeerChat:
		return v.ChatID
	case *tg.PeerChannel:
		return v.ChannelID
	default:
		return 0
	}
}

func peerKind(p tg.PeerClass) string {
	switch p.(type) {
	case *tg.PeerUser:
		return "user"
	case *tg.PeerChat:
		return "group"
	case *tg.PeerChannel:
		return "channel"
	default:
		return ""
	}
}

func (c *Client) peerTitle(ctx context.Context, p tg.PeerClass, users map[int64]*tg.User, chats map[int64]tg.ChatClass) (string, error) {
	switch v := p.(type) {
	case *tg.PeerUser:
		user, ok := users[v.UserID]
		if !ok {
			return "", nil
		}
		if user.Username != "" {
			return "@" + user.Username, nil
		}
		return user.FirstName, nil
	case *tg.PeerChat:
		ch, ok := chats[v.ChatID]
		if !ok {
			return "", nil
		}
		switch c := ch.(type) {
		case *tg.Chat:
			return c.Title, nil
		case *tg.ChatForbidden:
			return c.Title, nil
		}
		return "", nil
	case *tg.PeerChannel:
		// TODO: channel title resolution — out of scope for this task.
		// The bundled Chats slice carries Channel objects with titles, so this is
		// a straightforward lookup, but we're deferring until the Dialog struct's
		// channel story is finalized.
		_ = v
		return "", nil
	default:
		return "", fmt.Errorf("unknown peer type %T", p)
	}
}
