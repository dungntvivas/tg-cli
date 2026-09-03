package media

import "github.com/user/tgchat/internal/telegram"

// Truncate cuts s to at most max runes, appending an ellipsis when
// truncated. Rune-safe so Vietnamese and other multibyte text isn't cut
// mid-character.
func Truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// Glyph describes an attachment ("📷 Ảnh", "📎 hoa_don.pdf") or "" when
// nothing specific can be said. Shared by toasts, chat-pane download links
// and the mirrored files so every surface describes media identically.
func Glyph(m telegram.Message) string {
	switch m.Media {
	case "photo":
		return "📷 Ảnh"
	case "video":
		return "🎬 Video"
	case "video_note":
		return "📹 Video tin nhắn"
	case "voice":
		return "🎤 Tin nhắn thoại"
	case "audio":
		return "🎵 Nhạc"
	case "sticker":
		return "🌟 Sticker"
	case "gif":
		return "🎞 GIF"
	case "", "media", "file":
		return ""
	default:
		return "📎 " + Truncate(m.Media, 100) // filename from telegram layer
	}
}
