package telegram

import (
	"context"
	"fmt"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

// Client wraps gotd/td behind our API interface.
type Client struct {
	raw     *telegram.Client
	self    *tg.User
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

// Run starts the client and blocks until ctx is done. Auth must already be complete.
func (c *Client) Run(ctx context.Context) error {
	return c.raw.Run(ctx, func(ctx context.Context) error {
		self, err := c.raw.Self(ctx)
		if err != nil {
			return fmt.Errorf("fetch self: %w", err)
		}
		c.self = self
		<-ctx.Done()
		return nil
	})
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
		ID:     peerID(d.Peer),
		Kind:   peerKind(d.Peer),
		Title:  title,
		Unread: d.UnreadCount,
	}, nil
}

func (c *Client) History(ctx context.Context, peer Peer, limit int) ([]Message, error) {
	return nil, fmt.Errorf("History: not yet implemented")
}

func (c *Client) Send(ctx context.Context, peer Peer, text string) (Message, error) {
	return Message{}, fmt.Errorf("Send: not yet implemented")
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
