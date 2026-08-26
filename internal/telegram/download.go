package telegram

import (
	"context"
	"fmt"
	"io"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
)

// Stat describes the media attached to message msgID in peer: filename and
// byte size for HTTP headers.
func (c *Client) Stat(ctx context.Context, peer Peer, msgID int64) (MediaInfo, error) {
	mm, err := c.fetchMessage(ctx, peer, msgID)
	if err != nil {
		return MediaInfo{}, err
	}
	return mediaInfo(mm.Media), nil
}

// Download streams the media attached to message msgID in peer into w.
//
// The message is re-fetched at download time (not cached) so its
// file_reference is fresh — Telegram expires them, and a stale one fails
// with FILE_REFERENCE_EXPIRED. One extra API round-trip per click is
// negligible next to the file transfer itself.
func (c *Client) Download(ctx context.Context, peer Peer, msgID int64, w io.Writer) (MediaInfo, error) {
	mm, err := c.fetchMessage(ctx, peer, msgID)
	if err != nil {
		return MediaInfo{}, err
	}
	info := mediaInfo(mm.Media)
	loc, err := fileLocation(mm.Media)
	if err != nil {
		return info, err
	}
	if _, err := downloader.NewDownloader().Download(c.raw.API(), loc).Stream(ctx, w); err != nil {
		return info, fmt.Errorf("stream media: %w", err)
	}
	return info, nil
}

// fetchMessage re-fetches one message by ID in peer. Basic chats/users use
// messages.getMessages; channels need channels.getMessages with an
// InputChannel — same split as every other peer-scoped call.
func (c *Client) fetchMessage(ctx context.Context, peer Peer, msgID int64) (*tg.Message, error) {
	api := c.raw.API()
	id := tg.InputMessageClass(&tg.InputMessageID{ID: int(msgID)})
	var msgs []tg.MessageClass
	if peer.Kind == "channel" {
		res, err := api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{ChannelID: peer.ID, AccessHash: peer.AccessHash},
			ID:      []tg.InputMessageClass{id},
		})
		if err != nil {
			return nil, fmt.Errorf("get channel message %d: %w", msgID, err)
		}
		msgs = messagesOfClass(res)
	} else {
		res, err := api.MessagesGetMessages(ctx, []tg.InputMessageClass{id})
		if err != nil {
			return nil, fmt.Errorf("get message %d: %w", msgID, err)
		}
		msgs = messagesOfClass(res)
	}
	for _, mc := range msgs {
		if mm, ok := mc.(*tg.Message); ok && int64(mm.ID) == msgID && mm.Media != nil {
			return mm, nil
		}
	}
	return nil, fmt.Errorf("message %d not found or has no media", msgID)
}

// messagesOfClass normalizes the MessagesMessages* response variants gotd
// returns into a plain message slice (same shape as the Dialogs switch).
func messagesOfClass(res tg.MessagesMessagesClass) []tg.MessageClass {
	switch v := res.(type) {
	case *tg.MessagesMessages:
		return v.Messages
	case *tg.MessagesMessagesSlice:
		return v.Messages
	case *tg.MessagesChannelMessages:
		return v.Messages
	default:
		return nil
	}
}

// mediaInfo extracts name+size from a message's attachment. Documents keep
// their real filename; photos are always JPEG so they get a generated name
// sized by their largest stored variant (Telegram stores no single total).
func mediaInfo(media tg.MessageMediaClass) MediaInfo {
	switch v := media.(type) {
	case *tg.MessageMediaPhoto:
		name := "photo.jpg"
		var size int64
		if ph, ok := v.Photo.(*tg.Photo); ok {
			size = largestVariantSize(ph.Sizes)
			if ph.Date > 0 {
				name = fmt.Sprintf("photo_%d.jpg", ph.ID)
			}
		}
		return MediaInfo{Name: name, Size: size}
	case *tg.MessageMediaDocument:
		doc, ok := v.Document.(*tg.Document)
		if !ok {
			return MediaInfo{Name: "file"}
		}
		name := "file"
		for _, attr := range doc.Attributes {
			if f, ok := attr.(*tg.DocumentAttributeFilename); ok {
				name = f.FileName
				break
			}
		}
		return MediaInfo{Name: name, Size: doc.Size}
	default:
		return MediaInfo{}
	}
}

// fileLocation builds the InputFileLocation gotd's downloader streams from.
func fileLocation(media tg.MessageMediaClass) (tg.InputFileLocationClass, error) {
	switch v := media.(type) {
	case *tg.MessageMediaPhoto:
		ph, ok := v.Photo.(*tg.Photo)
		if !ok {
			return nil, fmt.Errorf("unsupported photo type %T", v.Photo)
		}
		return &tg.InputPhotoFileLocation{
			ID:            ph.ID,
			AccessHash:    ph.AccessHash,
			FileReference: ph.FileReference,
			ThumbSize:     largestVariantType(ph.Sizes),
		}, nil
	case *tg.MessageMediaDocument:
		doc, ok := v.Document.(*tg.Document)
		if !ok {
			return nil, fmt.Errorf("unsupported document type %T", v.Document)
		}
		return &tg.InputDocumentFileLocation{
			ID:            doc.ID,
			AccessHash:    doc.AccessHash,
			FileReference: doc.FileReference,
		}, nil
	default:
		return nil, fmt.Errorf("message has no downloadable media")
	}
}

// largestVariantSize returns the byte size of a photo's biggest variant.
// Progressive variants list cumulative chunk counts — the last entry is the
// full-size figure.
func largestVariantSize(sizes []tg.PhotoSizeClass) int64 {
	var best int64
	for _, s := range sizes {
		switch sz := s.(type) {
		case *tg.PhotoSize:
			if int64(sz.Size) > best {
				best = int64(sz.Size)
			}
		case *tg.PhotoSizeProgressive:
			for _, n := range sz.Sizes {
				if int64(n) > best {
					best = int64(n)
				}
			}
		}
	}
	return best
}

// largestVariantType returns the type string ("x", "y", ...) of a photo's
// biggest variant — InputPhotoFileLocation downloads THAT variant, so it
// must name the highest resolution available. Falls back to "x".
func largestVariantType(sizes []tg.PhotoSizeClass) string {
	best, bestPixels := "", 0
	for _, s := range sizes {
		typ := ""
		pixels := 0
		switch sz := s.(type) {
		case *tg.PhotoSize:
			typ, pixels = sz.Type, sz.W*sz.H
		case *tg.PhotoSizeProgressive:
			typ, pixels = sz.Type, sz.W*sz.H
		}
		if pixels >= bestPixels && typ != "" {
			best, bestPixels = typ, pixels
		}
	}
	if best == "" {
		best = "x"
	}
	return best
}
