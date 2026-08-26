package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

// TestMediaLabel verifies the short label messageFromGotd attaches to
// non-text content — the only media signal consumers (toasts, previews) get.
func TestMediaLabel(t *testing.T) {
	cases := []struct {
		name  string
		media tg.MessageMediaClass
		want  string
	}{
		{"photo", &tg.MessageMediaPhoto{}, "photo"},
		{"unnamed document", &tg.MessageMediaDocument{Document: &tg.Document{}}, "file"},
		{"named document", &tg.MessageMediaDocument{Document: &tg.Document{
			Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeFilename{FileName: "report.pdf"}},
		}}, "report.pdf"},
		{"voice note", &tg.MessageMediaDocument{Document: &tg.Document{
			Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeAudio{Voice: true}},
		}}, "voice"},
		{"music", &tg.MessageMediaDocument{Document: &tg.Document{
			Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeAudio{}},
		}}, "audio"},
		{"video", &tg.MessageMediaDocument{Document: &tg.Document{
			Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeVideo{}},
		}}, "video"},
		{"round video note", &tg.MessageMediaDocument{Document: &tg.Document{
			Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeVideo{RoundMessage: true}},
		}}, "video_note"},
		{"sticker", &tg.MessageMediaDocument{Document: &tg.Document{
			Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeSticker{}},
		}}, "sticker"},
		{"gif/animation", &tg.MessageMediaDocument{Document: &tg.Document{
			Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeAnimated{}},
		}}, "gif"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mediaLabel(tc.media); got != tc.want {
				t.Errorf("mediaLabel = %q, want %q", got, tc.want)
			}
		})
	}
	if got := mediaLabel(nil); got != "" {
		t.Errorf("mediaLabel(nil) = %q, want empty (plain text)", got)
	}
}

// TestMessageFromGotd_CarriesMediaLabel: the converted Message must expose
// the label so downstream surfaces can describe attachments.
func TestMessageFromGotd_CarriesMediaLabel(t *testing.T) {
	c := &Client{}
	mm := &tg.Message{
		ID:      9,
		PeerID:  &tg.PeerUser{UserID: 7},
		Message: "",
		Media:   &tg.MessageMediaPhoto{},
	}
	got := c.messageFromGotd(context.Background(), mm, nil, nil)
	if got.Media != "photo" || got.Text != "" {
		t.Errorf("Media = %q Text = %q, want photo/empty", got.Media, got.Text)
	}
}

// TestMediaInfo verifies name/size extraction used for HTTP download headers:
// documents keep their real filename and byte size; photos get a generated
// name and the size of their largest variant.
func TestMediaInfo(t *testing.T) {
	cases := []struct {
		name     string
		media    tg.MessageMediaClass
		wantName string
		wantSize int64
	}{
		{"named document", &tg.MessageMediaDocument{Document: &tg.Document{
			ID: 1, AccessHash: 2, Size: 12345,
			Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeFilename{FileName: "hoa_don.pdf"}},
		}}, "hoa_don.pdf", 12345},
		{"unnamed document", &tg.MessageMediaDocument{Document: &tg.Document{
			ID: 1, AccessHash: 2, Size: 99,
		}}, "file", 99},
		{"photo picks largest variant", &tg.MessageMediaPhoto{Photo: &tg.Photo{
			ID: 3, AccessHash: 4,
			Sizes: []tg.PhotoSizeClass{
				&tg.PhotoSize{Type: "s", W: 100, H: 100, Size: 900},
				&tg.PhotoSizeProgressive{Type: "x", W: 800, H: 600, Sizes: []int{1000, 5000, 20000}},
			},
		}}, "photo.jpg", 20000},
		{"plain text has no media info", nil, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mediaInfo(tc.media)
			if got.Name != tc.wantName || got.Size != tc.wantSize {
				t.Errorf("mediaInfo = %+v, want {%q %d}", got, tc.wantName, tc.wantSize)
			}
		})
	}
}

// TestFileLocation verifies the InputFileLocation built for each media kind,
// including the largest thumbnail type for photos (that's what gets fetched).
func TestFileLocation(t *testing.T) {
	loc, err := fileLocation(&tg.MessageMediaDocument{Document: &tg.Document{
		ID: 11, AccessHash: 22, FileReference: []byte{9},
	}})
	if err != nil {
		t.Fatalf("document location: %v", err)
	}
	doc, ok := loc.(*tg.InputDocumentFileLocation)
	if !ok || doc.ID != 11 || doc.AccessHash != 22 || len(doc.FileReference) != 1 {
		t.Errorf("doc location = %#v, err %v", loc, err)
	}

	ploc, err := fileLocation(&tg.MessageMediaPhoto{Photo: &tg.Photo{
		ID: 33, AccessHash: 44, FileReference: []byte{7},
		Sizes: []tg.PhotoSizeClass{
			&tg.PhotoSize{Type: "s", W: 90, H: 90},
			&tg.PhotoSizeProgressive{Type: "y", W: 1280, H: 720},
		},
	}})
	if err != nil {
		t.Fatalf("photo location: %v", err)
	}
	ph, ok := ploc.(*tg.InputPhotoFileLocation)
	if !ok || ph.ID != 33 || ph.AccessHash != 44 || ph.ThumbSize != "y" {
		t.Errorf("photo location = %#v (ThumbSize %v)", ploc, ph.ThumbSize)
	}

	if _, err := fileLocation(nil); err == nil {
		t.Error("nil media should error")
	}
}

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
