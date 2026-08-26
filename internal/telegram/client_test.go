package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

// TestDialogFromGotd_MutedFromNotifySettings verifies Dialog.Muted mirrors the
// server-side notification setting: a dialog with MuteUntil in the future is
// muted, an expired or absent MuteUntil is not. The TUI uses this to skip
// Windows pushes for groups the user already silenced in Telegram.
func TestDialogFromGotd_MutedFromNotifySettings(t *testing.T) {
	c := &Client{}
	ctx := context.Background()
	chats := map[int64]tg.ChatClass{
		5: &tg.Chat{ID: 5, Title: "Quiet Group"},
	}

	mk := func(muteUntil int) tg.Dialog {
		d := tg.Dialog{Peer: &tg.PeerChat{ChatID: 5}}
		if muteUntil != 0 {
			d.NotifySettings.SetMuteUntil(muteUntil)
		}
		return d
	}

	cases := []struct {
		name      string
		muteUntil int
		wantMuted bool
	}{
		{"future mute = muted", int(time.Now().Add(time.Hour).Unix()), true},
		{"expired mute = not muted", int(time.Now().Add(-time.Hour).Unix()), false},
		{"no mute setting = not muted", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.dialogFromGotd(ctx, mk(tc.muteUntil), nil, chats)
			if err != nil {
				t.Fatalf("dialogFromGotd: %v", err)
			}
			if got.Title != "Quiet Group" {
				t.Errorf("Title = %q, want Quiet Group", got.Title)
			}
			if got.Muted != tc.wantMuted {
				t.Errorf("Muted = %v, want %v", got.Muted, tc.wantMuted)
			}
		})
	}
}
