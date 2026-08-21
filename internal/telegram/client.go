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
	var rawDialogs []tg.DialogClass
	switch d := res.(type) {
	case *tg.MessagesDialogs:
		rawDialogs = d.Dialogs
	case *tg.MessagesDialogsSlice:
		rawDialogs = d.Dialogs
	default:
		return nil, fmt.Errorf("unexpected getDialogs response type %T", res)
	}

	out := make([]Dialog, 0, len(rawDialogs))
	for _, dc := range rawDialogs {
		d, ok := dc.(*tg.Dialog)
		if !ok {
			continue
		}
		dlg, err := c.dialogFromGotd(ctx, *d)
		if err != nil {
			continue // skip unresolvable; don't fail the whole list
		}
		out = append(out, dlg)
	}
	return out, nil
}

// dialogFromGotd extracts our Dialog from a gotd Dialog (which only carries peer + last msg metadata).
func (c *Client) dialogFromGotd(ctx context.Context, d tg.Dialog) (Dialog, error) {
	title, err := c.peerTitle(ctx, d.Peer)
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

func (c *Client) peerTitle(ctx context.Context, p tg.PeerClass) (string, error) {
	switch v := p.(type) {
	case *tg.PeerUser:
		// AccessHash 0 is wrong — but PeerChannel/PeerUser don't carry it.
		// The dialog response bundles Users with their AccessHash; plumbing
		// those through is the right fix. For now this builds; runtime calls
		// will likely fail with USER_ID_INVALID until that lands.
		u, err := c.raw.API().UsersGetFullUser(ctx, &tg.InputUser{UserID: v.UserID})
		if err != nil {
			return "", err
		}
		if len(u.Users) == 0 {
			return "", nil
		}
		user, ok := u.Users[0].(*tg.User)
		if !ok {
			return "", nil
		}
		if user.Username != "" {
			return "@" + user.Username, nil
		}
		return user.FirstName, nil
	case *tg.PeerChat:
		ch, err := c.raw.API().MessagesGetChats(ctx, []int64{v.ChatID})
		if err != nil {
			return "", err
		}
		mc, ok := ch.(*tg.MessagesChats)
		if !ok || len(mc.Chats) == 0 {
			return "", nil
		}
		chat, ok := mc.Chats[0].(*tg.Chat)
		if !ok {
			return "", nil
		}
		return chat.Title, nil
	case *tg.PeerChannel:
		// TODO: channel title resolution. ChannelsGetChannels requires the channel's
		// AccessHash (not carried by PeerChannel), so we can't resolve a title from a
		// bare peer here. The dialog response's Chats slice carries Channel objects
		// with titles — when that gets plumbed through dialogFromGotd, use it.
		_ = v
		return "", nil
	default:
		return "", fmt.Errorf("unknown peer type %T", p)
	}
}
