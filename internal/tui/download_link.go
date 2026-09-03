package tui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// openDownloadLink handles a click on a chat-pane download region: builds the
// tokened loopback URL and opens it in the default browser, which then saves
// the file through our dlServer.
func (a *App) openDownloadLink(region string) {
	// Region id is "dl:<kind>:<peerID>:<msgID>"; tview hands back the quoted
	// form ("dl:..."), so strip quotes before splitting.
	parts := strings.Split(strings.Trim(region, `"`), ":")
	if len(parts) != 4 {
		a.toast("link tải không hợp lệ")
		return
	}
	if a.dlBase == "" {
		a.toast("server tải chưa chạy — khởi động lại app")
		return
	}
	url := fmt.Sprintf("%s/%s/%s/%s", a.dlBase, parts[1], parts[2], parts[3])
	a.toast("đang mở " + parts[3] + " trong trình duyệt…")
	if err := openInBrowser(url); err != nil {
		a.toast(fmt.Sprintf("mở trình duyệt lỗi: %v", err))
	}
}

// openInBrowser is a var so tests can capture the URL instead of launching
// a real browser window.
var openInBrowser = openInBrowserOS

// openInBrowserOS opens url with the platform default browser (same
// exec-per-platform pattern as copyToClipboard).
func openInBrowserOS(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
