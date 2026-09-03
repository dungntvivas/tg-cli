package telegram

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

// Client wraps gotd/td behind our API interface.
type Client struct {
	raw     *telegram.Client
	storage session.Storage

	mu sync.Mutex
	// handlers are every callback registered via OnMessage. Both the TUI and
	// filesync subscribe, so registration appends rather than replaces —
	// replacing would silently deafen whoever registered first.
	handlers []func(Message)
}

// New constructs a Client. sessionFile is the path to a JSON file (gotd-managed).
// Call Run(ctx, f) to actually connect; f is invoked once the connection is
// ready and stays connected.
func New(ctx context.Context, appID int, apiHash, sessionFile string) (*Client, error) {
	storage := &session.FileStorage{Path: sessionFile}
	c := &Client{storage: storage}
	// UpdateHandler wires us into gotd's update stream. We install it eagerly so
	// gotd's setDefaults keeps NoUpdates=false (subscribes to updates). The
	// user's OnMessage callback is registered separately — until one is set
	// the handler is a no-op.
	raw := telegram.NewClient(appID, apiHash, telegram.Options{
		SessionStorage: storage,
		UpdateHandler: telegram.UpdateHandlerFunc(c.handleUpdate),
	})
	c.raw = raw
	return c, nil
}

// Raw exposes the underlying gotd client. main.go uses it to hand the client
// to auth.Run for first-run authentication; nothing else above this package
// should touch it.
func (c *Client) Raw() *telegram.Client {
	return c.raw
}

// Run starts the client and runs f when the connection is ready. f receives
// the same ctx and must not return until ctx is cancelled (otherwise Run will
// tear down the connection).
//
// Why f is required: gotd's Run performs a `replaceConn` inside (to load the
// persisted auth key). If a caller invokes on `c.raw` before that replace has
// happened, it can hit the *stale* conn — which never has its `gotConfig`
// signal fired because nothing starts it. Passing f into Run closes that race:
// f is only invoked after `c.ready.Ready()` fires, which only happens once the
// post-replace conn has finished init. (See telegram/client.go onReady and
// telegram/session.go restoreConnection.)
func (c *Client) Run(ctx context.Context, f func(ctx context.Context) error) error {
	return c.raw.Run(ctx, f)
}

// SelfName returns the current user's display name (e.g. "@alice" or
// "Alice Smith"). Requires client.Run() to have been started so the gotd
// client is connected.
func (c *Client) SelfName(ctx context.Context) (string, error) {
	u, err := c.raw.Self(ctx)
	if err != nil {
		return "", fmt.Errorf("fetch self: %w", err)
	}
	if u.Username != "" {
		return "@" + u.Username, nil
	}
	return fullName(u.FirstName, u.LastName), nil
}

// fullName joins FirstName + LastName with a single space when both are
// present. Telegram stores them separately; we want both so users with
// distinct family + given names (the common case) are not truncated to
// the given name only. Edge cases:
//
//	"Alice", ""       → "Alice"
//	"",     "Smith"   → "Smith"
//	"",     ""        → ""            (caller falls back to its own placeholder)
//	"Alice", "Smith"  → "Alice Smith"
func fullName(first, last string) string {
	switch {
	case first != "" && last != "":
		return first + " " + last
	default:
		return first + last
	}
}

func (c *Client) Close() error {
	// gotd client has no explicit Close; storage closes when GC'd.
	return nil
}

// OnMessage registers a callback fired for each newly-received message.
// Every registered handler is called, in registration order. Called from
// gotd's update goroutine; implementations must not block.
func (c *Client) OnMessage(handler func(Message)) {
	c.mu.Lock()
	c.handlers = append(c.handlers, handler)
	c.mu.Unlock()
}

// handleUpdate is the gotd UpdateHandler entry point. It walks every
// tg.UpdatesClass variant, extracts new chat messages, and forwards them to
// the registered OnMessage handler.
//
// gotd's higher-level UpdateDispatcher DOES NOT route
// UpdateShortMessage / UpdateShortChatMessage / UpdateShortSentMessage (see
// tg/tl_handlers_gen.go) — those are the variants Telegram uses for P2P
// chats and small groups, which is exactly where we want to see updates. So
// we implement the dispatch ourselves over the raw UpdatesClass interface.
func (c *Client) handleUpdate(ctx context.Context, u tg.UpdatesClass) error {
	c.dispatchUpdatesClass(ctx, u)
	return nil
}

// dispatchUpdatesClass walks one update container and fires OnMessage for
// each new message it contains. Outgoing messages (m.Out / v.Out) are
// skipped — they're already returned by Send() and we'd otherwise duplicate.
func (c *Client) dispatchUpdatesClass(ctx context.Context, u tg.UpdatesClass) {
	switch v := u.(type) {
	case *tg.UpdateShortMessage:
		// P2P message (1-on-1 chat). UserID is the OTHER party — for incoming
		// (Out=false) it's the sender, for outgoing (Out=true) it's the peer
		// we sent to. Skip outgoing to avoid duplicating Send's echo.
		if v.Out {
			return
		}
		c.fire(Message{
			ID:       int64(v.ID),
			PeerID:   v.UserID,
			PeerKind: "user",
			Sender:   fmt.Sprintf("User %d", v.UserID),
			Text:     v.Message,
			Time:     time.Unix(int64(v.Date), 0),
			Outgoing: false,
		})
	case *tg.UpdateShortChatMessage:
		// Group chat message. FromID is the sender user, ChatID is the group.
		// Skip outgoing (we already saw it via Send's UpdateMessageID).
		if v.Out {
			return
		}
		c.fire(Message{
			ID:       int64(v.ID),
			PeerID:   v.ChatID,
			PeerKind: "group",
			Sender:   fmt.Sprintf("User %d", v.FromID),
			Text:     v.Message,
			Time:     time.Unix(int64(v.Date), 0),
			Outgoing: false,
		})
	case *tg.UpdateShortSentMessage:
		// Echo of our own P2P send (the response to messages.sendMessage for
		// users carries this). Send already returns the Message; ignore.
		return
	case *tg.UpdateShort:
		c.dispatchSingleUpdate(ctx, v.Update, nil, nil)
	case *tg.Updates:
		users, chats := indexUsersAndChats(v.Users, v.Chats)
		for _, x := range v.Updates {
			c.dispatchSingleUpdate(ctx, x, users, chats)
		}
	case *tg.UpdatesCombined:
		users, chats := indexUsersAndChats(v.Users, v.Chats)
		for _, x := range v.Updates {
			c.dispatchSingleUpdate(ctx, x, users, chats)
		}
	}
}

// dispatchSingleUpdate extracts a Message from one update entry. Only the
// new-message variants are interesting for live chat; everything else
// (typing indicators, read receipts, etc.) is silently ignored.
func (c *Client) dispatchSingleUpdate(ctx context.Context, u tg.UpdateClass, users map[int64]*tg.User, chats map[int64]tg.ChatClass) {
	switch v := u.(type) {
	case *tg.UpdateNewMessage:
		mm, ok := v.Message.(*tg.Message)
		if !ok {
			return // empty/service message — not chat content
		}
		m := c.messageFromGotd(ctx, mm, users, chats)
		if mm.PeerID != nil {
			m.PeerID = peerID(mm.PeerID)
			m.PeerKind = peerKind(mm.PeerID)
		}
		if mm.Out {
			// Already appended optimistically by sendMessage.
			return
		}
		c.fire(m)
	case *tg.UpdateNewChannelMessage:
		mm, ok := v.Message.(*tg.Message)
		if !ok {
			return
		}
		m := c.messageFromGotd(ctx, mm, users, chats)
		if mm.PeerID != nil {
			m.PeerID = peerID(mm.PeerID)
			m.PeerKind = peerKind(mm.PeerID)
		}
		if mm.Out {
			return
		}
		c.fire(m)
	}
}

// fire invokes every registered handler. The slice is copied under the lock
// so OnMessage can register concurrently without racing the iteration.
func (c *Client) fire(m Message) {
	c.mu.Lock()
	hs := make([]func(Message), len(c.handlers))
	copy(hs, c.handlers)
	c.mu.Unlock()
	for _, h := range hs {
		h(m)
	}
}

// Dialogs returns the user's recent conversations via gotd's messages.getDialogs.
// Maps gotd types to our plain Dialog struct.
func (c *Client) Dialogs(ctx context.Context) ([]Dialog, error) {
	api := c.raw.API()
	// OffsetPeer is required by the TL schema; pass InputPeerEmpty for the
	// first page (no pagination cursor yet).
	res, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      100,
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
	title, err := peerTitle(ctx, d.Peer, users, chats)
	if err != nil {
		return Dialog{}, err
	}
	// Muted mirrors the user's per-dialog notification setting: mute_until is
	// a unix timestamp, muted while it's in the future.
	muted := false
	if mu, ok := d.NotifySettings.GetMuteUntil(); ok && mu > int(time.Now().Unix()) {
		muted = true
	}
	// Only user peers can be bots; the bundled User object already carries
	// the flag, so no extra API call is needed.
	bot := false
	if pu, ok := d.Peer.(*tg.PeerUser); ok {
		if u, ok := users[pu.UserID]; ok {
			bot = u.Bot
		}
	}
	return Dialog{
		ID:         peerID(d.Peer),
		Kind:       peerKind(d.Peer),
		Title:      title,
		Unread:     d.UnreadCount,
		Muted:      muted,
		Bot:        bot,
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
// Each returned Message carries PeerID/PeerKind so callers can route incoming
// live updates back to the same window.
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
			m := c.messageFromGotd(ctx, mm, userByID, chatByID)
			// Peer info is the same for every message in this page — stamp
			// it directly instead of relying on the per-message PeerID,
			// which on private chats can be empty for outgoing messages.
			m.PeerID = peer.ID
			m.PeerKind = peer.Kind
			out = append(out, m)
		}
	}
	// Telegram returns messages newest-first (the first item is the most
	// recent). Reverse to oldest-first so the chat view scrolls naturally:
	// newest message at the bottom, matches how a chat is read.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (c *Client) messageFromGotd(ctx context.Context, m *tg.Message, users map[int64]*tg.User, chats map[int64]tg.ChatClass) Message {
	if m.Out {
		return Message{
			ID:       int64(m.ID),
			Sender:   "You",
			Text:     m.Message,
			Media:    mediaLabel(m.Media),
			Time:     time.Unix(int64(m.Date), 0),
			Outgoing: true,
		}
	}
	sender := c.resolveSender(ctx, m, users, chats)
	return Message{
		ID:       int64(m.ID),
		Sender:   sender,
		Text:     m.Message,
		Media:    mediaLabel(m.Media),
		Time:     time.Unix(int64(m.Date), 0),
		Outgoing: false,
	}
}

// mediaLabel summarizes a message's attachment in one short token so text-less
// surfaces (Windows toasts) can say what arrived instead of showing blank.
// Named documents keep their filename — far more useful than a bare "file".
// Returns "" when the message is plain text.
func mediaLabel(media tg.MessageMediaClass) string {
	switch v := media.(type) {
	case nil:
		return "" // plain text
	case *tg.MessageMediaPhoto:
		return "photo"
	case *tg.MessageMediaDocument:
		doc, ok := v.Document.(*tg.Document)
		if !ok {
			return "file" // DocumentEmpty / unsupported wrapper
		}
		for _, attr := range doc.Attributes {
			switch a := attr.(type) {
			case *tg.DocumentAttributeFilename:
				return a.FileName
			case *tg.DocumentAttributeAudio:
				if a.Voice {
					return "voice"
				}
				return "audio"
			case *tg.DocumentAttributeVideo:
				if a.RoundMessage {
					return "video_note"
				}
				return "video"
			case *tg.DocumentAttributeSticker:
				return "sticker"
			case *tg.DocumentAttributeAnimated:
				return "gif"
			}
		}
		return "file"
	default:
		return "media" // geo, poll, contact, ... — something non-text at least
	}
}

// resolveSender turns a `tg.Message` into a display name. Tries the sender
// in this order:
//
//  1. m.FromID — explicit sender (groups/channels always have it).
//  2. m.PeerID — for P2P messages Telegram often omits FromID because the
//     peer IS the sender; without this fallback we'd show "User <id>" or
//     a literal "unknown" placeholder for incoming P2P chats.
//  3. The bundled Users/Chats map first (cheap), then a final
//     `"User <id>"` fallback so we never show a literal "unknown".
func (c *Client) resolveSender(ctx context.Context, m *tg.Message, users map[int64]*tg.User, chats map[int64]tg.ChatClass) string {
	if title, ok := resolvePeerTitle(ctx, m.FromID, users, chats); ok {
		return title
	}
	if title, ok := resolvePeerTitle(ctx, m.PeerID, users, chats); ok {
		return title
	}
	id := peerID(m.FromID)
	if id == 0 {
		id = peerID(m.PeerID)
	}
	if id == 0 {
		return "User"
	}
	return fmt.Sprintf("User %d", id)
}

// resolvePeerTitle is a tiny wrapper around peerTitle that returns
// (title, ok) so the caller can decide between this title and a fallback.
// Returns ok=false for empty titles and lookup misses (peerType not in the
// bundled map). Errors from peerTitle (unknown peer type) are swallowed —
// the caller has its own fallback chain.
func resolvePeerTitle(ctx context.Context, p tg.PeerClass, users map[int64]*tg.User, chats map[int64]tg.ChatClass) (string, bool) {
	if p == nil {
		return "", false
	}
	title, err := peerTitle(ctx, p, users, chats)
	if err != nil || title == "" {
		return "", false
	}
	return title, true
}

// Send posts `text` to `peer` and returns the echoed message. ID/PeerID/PeerKind
// are filled in from the server response so callers can prepend it to their
// local message list without a follow-up History() call.
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
		return Message{
			ID:       int64(id),
			PeerID:   peer.ID,
			PeerKind: peer.Kind,
			Sender:   "You",
			Text:     text,
			Time:     time.Now(),
			Outgoing: true,
		}
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

// MarkRead notifies Telegram that all messages in `peer` have been read.
// MaxID=0 per the TL schema means "no upper bound" — i.e. everything.
// TUI calls this when the user opens a dialog so the unread badge clears.
func (c *Client) MarkRead(ctx context.Context, peer Peer) error {
	inputPeer, err := c.inputPeer(peer)
	if err != nil {
		return err
	}
	_, err = c.raw.API().MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{
		Peer:   inputPeer,
		MaxID:  0,
	})
	if err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	return nil
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

// indexUsersAndChats builds the lookup maps dispatchSingleUpdate uses to
// resolve sender names without extra API calls. Returns nil maps if the
// container didn't bundle any (e.g. UpdateShort).
func indexUsersAndChats(users []tg.UserClass, chats []tg.ChatClass) (map[int64]*tg.User, map[int64]tg.ChatClass) {
	u := make(map[int64]*tg.User, len(users))
	for _, x := range users {
		if uu, ok := x.(*tg.User); ok {
			u[uu.ID] = uu
		}
	}
	c := make(map[int64]tg.ChatClass, len(chats))
	for _, x := range chats {
		c[x.GetID()] = x
	}
	return u, c
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

func peerTitle(ctx context.Context, p tg.PeerClass, users map[int64]*tg.User, chats map[int64]tg.ChatClass) (string, error) {
	switch v := p.(type) {
	case *tg.PeerUser:
		user, ok := users[v.UserID]
		if !ok {
			return "", nil
		}
		if user.Username != "" {
			return "@" + user.Username, nil
		}
		return fullName(user.FirstName, user.LastName), nil
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
		ch, ok := chats[v.ChannelID]
		if !ok {
			return "", nil
		}
		switch c := ch.(type) {
		case *tg.Channel:
			return c.Title, nil
		case *tg.ChannelForbidden:
			return c.Title, nil
		}
		return "", nil
	default:
		return "", fmt.Errorf("unknown peer type %T", p)
	}
}
