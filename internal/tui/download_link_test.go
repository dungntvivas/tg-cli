package tui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"

	"github.com/user/tgchat/internal/telegram"
)

// TestFormatMessage_MediaRendersDownloadRegion verifies media-only messages
// render as a clickable tview region carrying peer/message coordinates,
// instead of an empty body line.
func TestFormatMessage_MediaRendersDownloadRegion(t *testing.T) {
	m := telegram.Message{ID: 204, PeerID: 99, PeerKind: "user", Sender: "Bob", Media: "hoa_don.pdf"}
	out := FormatMessage(m, 80)
	if !strings.Contains(out, `["dl:user:99:204"]`) {
		t.Errorf("no download region in output:\n%s", out)
	}
	if !strings.Contains(out, "hoa_don.pdf") || !strings.Contains(out, "click để tải") {
		t.Errorf("link text missing filename/call-to-action:\n%s", out)
	}

	// Plain text must NOT grow a region.
	plain := FormatMessage(telegram.Message{ID: 1, PeerID: 99, PeerKind: "user", Text: "hi"}, 80)
	if strings.Contains(plain, `"dl:`) {
		t.Errorf("text message unexpectedly wrapped in download region:\n%s", plain)
	}

	// Photo without filename gets a friendly label.
	photo := FormatMessage(telegram.Message{ID: 205, PeerID: 99, PeerKind: "group", Media: "photo"}, 80)
	if !strings.Contains(photo, `["dl:group:99:205"]`) || !strings.Contains(photo, "Ảnh") {
		t.Errorf("photo link wrong:\n%s", photo)
	}
}

// TestOpenDownloadLink_OpensBrowser verifies the click handler builds the
// tokened URL and hands it to the browser opener; malformed regions are
// ignored (toast only).
func TestOpenDownloadLink_OpensBrowser(t *testing.T) {
	var opened []string
	old := openInBrowser
	openInBrowser = func(url string) error { opened = append(opened, url); return nil }
	t.Cleanup(func() { openInBrowser = old })

	a := &App{
		status: tview.NewTextView(),
		dlBase: "http://127.0.0.1:1234/dl/tok9",
	}
	a.openDownloadLink(`"dl:group:55:77"`)
	if len(opened) != 1 || opened[0] != "http://127.0.0.1:1234/dl/tok9/group/55/77" {
		t.Errorf("opened = %v, want one call with the full tokened URL", opened)
	}

	a.openDownloadLink("garbage")
	if len(opened) != 1 {
		t.Errorf("malformed region should not open anything, got %v", opened)
	}
}
