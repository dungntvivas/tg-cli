package tui

import (
	"os"
	"os/exec"
	"strings"

	"github.com/user/tgchat/internal/telegram"
)

// pushNotify is the fire-and-forget hook handleIncoming uses for desktop
// pushes on messages from non-active chats. A package var so tests can
// intercept it (see notify_test.go capturePush).
var pushNotify = sendWindowsToast

// sendWindowsToast shows a Windows toast notification by driving WinRT's
// ToastNotificationManager through PowerShell — the platform already ships
// everything needed, so no third-party dependency. The PowerShell AppUserModelID
// is used as the notifier ID, the standard trick for unsigned scripts: it
// works on stock Win10/11 with no Start Menu registration.
//
// Title/body travel via env vars instead of string interpolation to keep
// quoting and injection out of the picture entirely.
//
// On non-Windows this simply fails (powershell not found); callers ignore the
// error — a failed notification must never disturb the chat UI.
func sendWindowsToast(title, body string) error {
	const script = `
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
$t = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$x = $t.GetElementsByTagName('text')
$x.Item(0).AppendChild($t.CreateTextNode($env:TG_NOTIFY_TITLE)) | Out-Null
$x.Item(1).AppendChild($t.CreateTextNode($env:TG_NOTIFY_BODY)) | Out-Null
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe').Show([Windows.UI.Notifications.ToastNotification]::new($t))
`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(), "TG_NOTIFY_TITLE="+title, "TG_NOTIFY_BODY="+body)
	return cmd.Run()
}

// TruncateText cuts s to at most max runes, appending an ellipsis when
// truncated. Rune-safe so Vietnamese and other multibyte text isn't cut
// mid-character.
func TruncateText(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// notifyBody builds the toast body for an incoming message: the flattened
// text when there is one, otherwise a short description of the attachment —
// photos/files carry no Text at the telegram layer, and a blank notification
// tells the user nothing.
func notifyBody(m telegram.Message) string {
	if s := strings.TrimSpace(m.Text); s != "" {
		return TruncateText(strings.ReplaceAll(s, "\n", " "), 120)
	}
	if g := mediaGlyph(m); g != "" {
		return g
	}
	return "(tin nhắn)"
}

// mediaGlyph describes an attachment ("📷 Ảnh", "📎 hoa_don.pdf") or ""
// when nothing specific can be said. Shared by toasts and chat-pane
// download links so both surfaces describe media identically.
func mediaGlyph(m telegram.Message) string {
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
		return "📎 " + TruncateText(m.Media, 100) // filename from telegram layer
	}
}
