package telegram

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

// imageExts are sent as photos rather than documents: Telegram shows them
// inline, which is what sharing a screenshot is for. Everything else keeps
// its bytes and filename as a document.
var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true}

func isImage(name string) bool {
	return imageExts[strings.ToLower(filepath.Ext(name))]
}

// SendFile uploads the file at path and posts it to peer, with caption as the
// message text when non-empty. The returned Message mirrors Send's echo:
// enough for the caller to append it to its own view without a round trip.
func (c *Client) SendFile(ctx context.Context, peer Peer, path, caption string) (Message, error) {
	inputPeer, err := c.inputPeer(peer)
	if err != nil {
		return Message{}, err
	}
	// FromPath streams the file in chunks — no full-file buffering, so a
	// large attachment doesn't balloon memory.
	f, err := uploader.NewUploader(c.raw.API()).FromPath(ctx, path)
	if err != nil {
		return Message{}, fmt.Errorf("upload %s: %w", filepath.Base(path), err)
	}

	name := filepath.Base(path)
	var opts []styling.StyledTextOption
	if caption != "" {
		opts = append(opts, styling.Plain(caption))
	}

	sender := message.NewSender(c.raw.API()).To(inputPeer)
	var res tg.UpdatesClass
	if isImage(name) {
		res, err = sender.Media(ctx, message.UploadedPhoto(f, opts...))
	} else {
		res, err = sender.Media(ctx, message.UploadedDocument(f, opts...).Filename(name))
	}
	if err != nil {
		return Message{}, fmt.Errorf("send %s: %w", name, err)
	}

	// Media mirrors what mediaLabel would report for the same message coming
	// back through an update, so every surface describes it identically.
	label := name
	if isImage(name) {
		label = "photo"
	}
	return Message{
		ID:       int64(messageIDFromUpdates(res)),
		PeerID:   peer.ID,
		PeerKind: peer.Kind,
		Sender:   "You",
		Text:     caption,
		Media:    label,
		Time:     time.Now(),
		Outgoing: true,
	}, nil
}

// messageIDFromUpdates digs the new message's ID out of whichever
// UpdatesClass variant Telegram returned. 0 when it isn't there — the
// message was still sent, we just can't link to it.
func messageIDFromUpdates(res tg.UpdatesClass) int {
	switch u := res.(type) {
	case *tg.UpdateShortSentMessage:
		return u.ID
	case *tg.UpdateShortMessage:
		return u.ID
	case *tg.UpdatesCombined:
		return updateMessageID(u.Updates)
	case *tg.Updates:
		return updateMessageID(u.Updates)
	}
	return 0
}

func updateMessageID(updates []tg.UpdateClass) int {
	for _, x := range updates {
		if m, ok := x.(*tg.UpdateMessageID); ok {
			return m.ID
		}
	}
	return 0
}
