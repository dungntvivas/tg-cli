package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

// TestFakeAPI_OnMessage verifies FakeAPI implements the API surface and that
// OnMessage routes through the OnMessageFn field for tests to script.
func TestFakeAPI_OnMessage(t *testing.T) {
	var got Message
	api := &FakeAPI{
		OnMessageFn: func(h func(Message)) { h(Message{ID: 1, Text: "x"}) },
	}
	// Drive through the OnMessage entry point — FakeAPI.OnMessage invokes
	// OnMessageFn when set, which exercises the same wiring tests use to
	// script live updates.
	api.OnMessage(func(m Message) { got = m })
	if got.ID != 1 || got.Text != "x" {
		t.Errorf("got %+v, want ID=1 Text=x", got)
	}
}

// TestDispatch_UpdateShortMessageIncoming verifies a P2P incoming message
// (Out=false) is forwarded to the OnMessage handler with PeerID/PeerKind
// filled in.
func TestDispatch_UpdateShortMessageIncoming(t *testing.T) {
	c := &Client{}
	var got Message
	c.OnMessage(func(m Message) { got = m })
	if err := c.handleUpdate(context.Background(), &tg.UpdateShortMessage{
		ID:      7,
		UserID:  42,
		Message: "hi",
		Date:    1700000000,
		Out:     false,
	}); err != nil {
		t.Fatalf("handleUpdate: %v", err)
	}
	if got.ID != 7 || got.PeerID != 42 || got.PeerKind != "user" {
		t.Errorf("got %+v, want ID=7 PeerID=42 PeerKind=user", got)
	}
	if got.Text != "hi" {
		t.Errorf("Text = %q, want hi", got.Text)
	}
	if got.Outgoing {
		t.Error("incoming UpdateShortMessage should set Outgoing=false")
	}
	if !got.Time.Equal(time.Unix(1700000000, 0)) {
		t.Errorf("Time = %v, want Unix 1700000000", got.Time)
	}
}

// TestDispatch_UpdateShortMessageOutgoingSkipped verifies outgoing P2P
// messages are NOT forwarded — Send() already returns them.
func TestDispatch_UpdateShortMessageOutgoingSkipped(t *testing.T) {
	c := &Client{}
	calls := 0
	c.OnMessage(func(m Message) { calls++ })
	if err := c.handleUpdate(context.Background(), &tg.UpdateShortMessage{
		ID: 7, UserID: 42, Message: "echoed back", Out: true,
	}); err != nil {
		t.Fatalf("handleUpdate: %v", err)
	}
	if calls != 0 {
		t.Errorf("outgoing UpdateShortMessage should not fire handler, got %d calls", calls)
	}
}

// TestDispatch_UpdateShortSentMessageSkipped verifies the dedicated echo
// update for our own P2P send is dropped (Send() already returns it).
func TestDispatch_UpdateShortSentMessageSkipped(t *testing.T) {
	c := &Client{}
	calls := 0
	c.OnMessage(func(m Message) { calls++ })
	_ = c.handleUpdate(context.Background(), &tg.UpdateShortSentMessage{
		ID: 9, Date: 1700000000,
	})
	if calls != 0 {
		t.Errorf("UpdateShortSentMessage should not fire handler, got %d calls", calls)
	}
}

// TestDispatch_UpdateShortChatMessageIncoming verifies group chat incoming
// messages carry the chat peer (ChatID, "group").
func TestDispatch_UpdateShortChatMessageIncoming(t *testing.T) {
	c := &Client{}
	var got Message
	c.OnMessage(func(m Message) { got = m })
	_ = c.handleUpdate(context.Background(), &tg.UpdateShortChatMessage{
		ID: 11, ChatID: 999, FromID: 7, Message: "gm", Date: 1700000000, Out: false,
	})
	if got.PeerID != 999 || got.PeerKind != "group" {
		t.Errorf("got %+v, want PeerID=999 PeerKind=group", got)
	}
	if got.Outgoing || got.Text != "gm" {
		t.Errorf("unexpected message fields: %+v", got)
	}
}

// TestDispatch_UpdateNewMessageIncoming verifies an Updates container with a
// single UpdateNewMessage (Out=false) forwards one Message with the
// message's own PeerID/PeerKind.
func TestDispatch_UpdateNewMessageIncoming(t *testing.T) {
	c := &Client{}
	var got Message
	c.OnMessage(func(m Message) { got = m })
	_ = c.handleUpdate(context.Background(), &tg.Updates{
		Updates: []tg.UpdateClass{
			&tg.UpdateNewMessage{
				Message: &tg.Message{
					ID:      42,
					Message: "ping",
					Date:    1700000000,
					PeerID:  &tg.PeerUser{UserID: 7},
					Out:     false,
				},
				Pts: 1,
			},
		},
	})
	if got.ID != 42 || got.PeerID != 7 || got.PeerKind != "user" || got.Text != "ping" {
		t.Errorf("got %+v", got)
	}
}

// TestDispatch_UpdateNewMessageOutgoingSkipped verifies Updates carrying our
// own sent message don't duplicate the optimistic append from sendMessage.
func TestDispatch_UpdateNewMessageOutgoingSkipped(t *testing.T) {
	c := &Client{}
	calls := 0
	c.OnMessage(func(m Message) { calls++ })
	_ = c.handleUpdate(context.Background(), &tg.Updates{
		Updates: []tg.UpdateClass{
			&tg.UpdateNewMessage{
				Message: &tg.Message{
					ID: 42, Message: "ping", Date: 1700000000,
					PeerID: &tg.PeerUser{UserID: 7}, Out: true,
				},
			},
		},
	})
	if calls != 0 {
		t.Errorf("outgoing UpdateNewMessage should not fire handler, got %d calls", calls)
	}
}

// TestDispatch_UpdateNewChannelMessageIncoming verifies channel updates use
// PeerChannel and "channel" kind. Wrapped in UpdateShort because individual
// UpdateClass entries arrive via the *Short wrapper.
func TestDispatch_UpdateNewChannelMessageIncoming(t *testing.T) {
	c := &Client{}
	var got Message
	c.OnMessage(func(m Message) { got = m })
	_ = c.handleUpdate(context.Background(), &tg.UpdateShort{
		Update: &tg.UpdateNewChannelMessage{
			Message: &tg.Message{
				ID: 1, Message: "broadcast", Date: 1700000000,
				PeerID: &tg.PeerChannel{ChannelID: 555},
			},
		},
	})
	if got.PeerID != 555 || got.PeerKind != "channel" {
		t.Errorf("got %+v, want PeerID=555 PeerKind=channel", got)
	}
}

// TestDispatch_NoHandler verifies an update with no registered OnMessage is
// silently dropped (no panic, no error).
func TestDispatch_NoHandler(t *testing.T) {
	c := &Client{}
	if err := c.handleUpdate(context.Background(), &tg.UpdateShortMessage{
		ID: 1, UserID: 1, Message: "x", Date: 1,
	}); err != nil {
		t.Errorf("handleUpdate without handler: %v", err)
	}
}

// TestOnMessage_FansOutToEveryHandler: the TUI and filesync both subscribe.
// Registering a second handler must not silence the first.
func TestOnMessage_FansOutToEveryHandler(t *testing.T) {
	c := &Client{}
	var got []string
	c.OnMessage(func(Message) { got = append(got, "first") })
	c.OnMessage(func(Message) { got = append(got, "second") })
	_ = c.handleUpdate(context.Background(), &tg.UpdateShortMessage{
		ID: 1, UserID: 1, Message: "x", Date: 1,
	})
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Errorf("handlers fired = %v, want [first second]", got)
	}
}

// TestDispatch_P2PIncoming_FallsBackToPeer is the regression test for the
// "unknown sender" bug. In P2P chats Telegram often omits m.FromID because
// the peer IS the sender — without the PeerID fallback, messageFromGotd
// would have left the literal string "unknown" in the chat pane.
func TestDispatch_P2PIncoming_FallsBackToPeer(t *testing.T) {
	c := &Client{}
	var got Message
	c.OnMessage(func(m Message) { got = m })
	_ = c.handleUpdate(context.Background(), &tg.Updates{
		Users: []tg.UserClass{&tg.User{ID: 7, FirstName: "Alice"}},
		Updates: []tg.UpdateClass{
			&tg.UpdateNewMessage{
				Message: &tg.Message{
					ID:      42,
					Message: "hi",
					Date:    1700000000,
					PeerID:  &tg.PeerUser{UserID: 7},
					// FromID intentionally nil — P2P case.
					Out: false,
				},
			},
		},
	})
	if got.Sender != "Alice" {
		t.Errorf("Sender = %q, want Alice (P2P must fall back to PeerID)", got.Sender)
	}
}

// TestDispatch_P2PIncoming_NoBundleUsesUserFallback: when the Updates
// container bundles no Users (rare but possible), the sender should still
// resolve to "User <id>" rather than the literal string "unknown".
func TestDispatch_P2PIncoming_NoBundleUsesUserFallback(t *testing.T) {
	c := &Client{}
	var got Message
	c.OnMessage(func(m Message) { got = m })
	_ = c.handleUpdate(context.Background(), &tg.Updates{
		Updates: []tg.UpdateClass{
			&tg.UpdateNewMessage{
				Message: &tg.Message{
					ID: 1, Message: "hi", Date: 1700000000,
					PeerID: &tg.PeerUser{UserID: 99},
					Out:    false,
				},
			},
		},
	})
	if got.Sender == "unknown" {
		t.Errorf("Sender = %q, must never be the placeholder 'unknown'", got.Sender)
	}
	if got.Sender != "User 99" {
		t.Errorf("Sender = %q, want %q", got.Sender, "User 99")
	}
}

// TestDispatch_GroupSender_UsesFromID: in group chats FromID is the actual
// sender (different from PeerID which is the group itself). Sender must
// resolve from FromID, not the chat's PeerID/Title.
func TestDispatch_GroupSender_UsesFromID(t *testing.T) {
	c := &Client{}
	var got Message
	c.OnMessage(func(m Message) { got = m })
	_ = c.handleUpdate(context.Background(), &tg.Updates{
		Users: []tg.UserClass{&tg.User{ID: 11, FirstName: "Bob"}},
		Chats: []tg.ChatClass{&tg.Chat{ID: 500, Title: "Family"}},
		Updates: []tg.UpdateClass{
			&tg.UpdateNewMessage{
				Message: &tg.Message{
					ID: 1, Message: "hi all", Date: 1700000000,
					PeerID: &tg.PeerChat{ChatID: 500},
					FromID: &tg.PeerUser{UserID: 11},
					Out:    false,
				},
			},
		},
	})
	if got.Sender != "Bob" {
		t.Errorf("Sender = %q, want Bob (group sender must come from FromID)", got.Sender)
	}
}

// TestFullName_TableCases covers the four shape combinations Telegram
// produces: both names, first only, last only, and empty (caller handles).
func TestFullName_TableCases(t *testing.T) {
	cases := []struct {
		first, last, want string
	}{
		{"Alice", "Smith", "Alice Smith"},
		{"Alice", "", "Alice"},
		{"", "Smith", "Smith"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := fullName(c.first, c.last); got != c.want {
			t.Errorf("fullName(%q, %q) = %q, want %q", c.first, c.last, got, c.want)
		}
	}
}

// TestDispatch_P2PIncoming_BothNamesCombined verifies the user-facing fix:
// when Telegram sends both FirstName and LastName, the chat pane shows
// "FirstName LastName", not just the given name.
func TestDispatch_P2PIncoming_BothNamesCombined(t *testing.T) {
	c := &Client{}
	var got Message
	c.OnMessage(func(m Message) { got = m })
	_ = c.handleUpdate(context.Background(), &tg.Updates{
		Users: []tg.UserClass{&tg.User{ID: 7, FirstName: "Nguyễn Văn", LastName: "A"}},
		Updates: []tg.UpdateClass{
			&tg.UpdateNewMessage{
				Message: &tg.Message{
					ID: 1, Message: "hi", Date: 1700000000,
					PeerID: &tg.PeerUser{UserID: 7}, Out: false,
				},
			},
		},
	})
	want := "Nguyễn Văn A"
	if got.Sender != want {
		t.Errorf("Sender = %q, want %q (both names must be shown)", got.Sender, want)
	}
}
