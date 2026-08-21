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

func (c *Client) Dialogs(ctx context.Context) ([]Dialog, error) {
	return nil, fmt.Errorf("Dialogs: not yet implemented")
}

func (c *Client) History(ctx context.Context, peer Peer, limit int) ([]Message, error) {
	return nil, fmt.Errorf("History: not yet implemented")
}

func (c *Client) Send(ctx context.Context, peer Peer, text string) (Message, error) {
	return Message{}, fmt.Errorf("Send: not yet implemented")
}
