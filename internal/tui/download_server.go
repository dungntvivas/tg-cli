package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/user/tgchat/internal/telegram"
)

// dlServer exposes the logged-in Telegram session's media over loopback HTTP
// so chat-pane links can open in the user's browser. Telegram has no direct
// download URLs — files stream through MTProto — so this tiny proxy is what
// makes "click to download" possible at all.
//
// Security: binds 127.0.0.1 only, and every path must carry a random
// per-session token, so other local processes can't pull files by guessing.
type dlServer struct {
	api   telegram.API
	token string
}

// startDownloadServer binds a loopback port and serves until ctx is done.
// Returns the base URL (port chosen by the OS) links should be built on.
func startDownloadServer(ctx context.Context, api telegram.API) (string, error) {
	s := &dlServer{api: api, token: randToken()}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen loopback: %w", err)
	}
	srv := &http.Server{Handler: s}
	go srv.Serve(ln) // handler errors are logged by net/http; server dies with the session anyway
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	// The base URL carries the /dl/<token> prefix: openDownloadLink appends
	// only /<kind>/<peer>/<msg>, so every link it builds is pre-authorized.
	return "http://127.0.0.1:" + strconv.Itoa(ln.Addr().(*net.TCPAddr).Port) + "/dl/" + s.token, nil
}

// randToken returns a 128-bit random hex string — the per-session secret.
func randToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand failing means the OS entropy source is broken
	}
	return hex.EncodeToString(b)
}

// ServeHTTP handles GET /dl/{token}/{kind}/{peerID}/{msgID} by streaming the
// attachment with browser-friendly headers. The path is parsed manually so
// the server also works when invoked without a pattern-routing mux.
func (s *dlServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/dl/"), "/")
	if len(parts) != 4 || parts[0] != s.token {
		http.NotFound(w, r)
		return
	}
	peerID, err := strconv.ParseInt(parts[2], 10, 64)
	msgID, idErr := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || idErr != nil {
		http.NotFound(w, r)
		return
	}
	peer := telegram.Peer{ID: peerID, Kind: parts[1]}

	info, err := s.api.Stat(r.Context(), peer, msgID)
	if err != nil {
		log.Printf("download stat: %v", err)
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition",
		mime.FormatMediaType("attachment", map[string]string{"filename": info.Name}))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	if _, err := s.api.Download(r.Context(), peer, msgID, w); err != nil {
		// Headers are already sent; all we can do is log and drop the
		// connection body short — browsers will surface a truncated file.
		log.Printf("download stream: %v", err)
	}
}

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
