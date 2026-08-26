package tui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/user/tgchat/internal/telegram"
)

func newTestDlAPI() (*dlServer, *telegram.FakeAPI) {
	api := &telegram.FakeAPI{
		StatFn: func(ctx context.Context, peer telegram.Peer, msgID int64) (telegram.MediaInfo, error) {
			return telegram.MediaInfo{Name: "báo_cáo.pdf", Size: 5}, nil
		},
		DownloadFn: func(ctx context.Context, peer telegram.Peer, msgID int64, w io.Writer) (telegram.MediaInfo, error) {
			w.Write([]byte("hello"))
			return telegram.MediaInfo{Name: "báo_cáo.pdf", Size: 5}, nil
		},
	}
	return &dlServer{api: api, token: "tok123"}, api
}

// TestDownloadServer_ServesAttachment verifies the happy path: correct peer/
// message reach the API, headers carry the original filename and length, and
// the body is the streamed file content.
func TestDownloadServer_ServesAttachment(t *testing.T) {
	s, _ := newTestDlAPI()
	req := httptest.NewRequest("GET", "/dl/tok123/user/99/204", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	res := rec.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	cd := res.Header.Get("Content-Disposition")
	// Non-ASCII names are RFC 5987 percent-encoded by mime.FormatMediaType —
	// browsers decode that back to báo_cáo.pdf on save.
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, "filename*=utf-8''b%C3%A1o_c%C3%A1o.pdf") {
		t.Errorf("Content-Disposition = %q, want attachment with encoded báo_cáo.pdf", cd)
	}
	body, _ := io.ReadAll(res.Body)
	if string(body) != "hello" {
		t.Errorf("body = %q, want hello", body)
	}
}

// TestDownloadServer_RejectsWrongToken: the per-session token gates every
// request — other local processes must not be able to pull files by guessing
// a URL.
func TestDownloadServer_RejectsWrongToken(t *testing.T) {
	s, _ := newTestDlAPI()
	req := httptest.NewRequest("GET", "/dl/wrongtoken/user/99/204", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for bad token", rec.Code)
	}
}

// TestDownloadServer_UpstreamFailureIsNotFound surfaces API failures (expired
// reference, missing message) as a plain 404 instead of a scary stack.
func TestDownloadServer_UpstreamFailureIsNotFound(t *testing.T) {
	s, api := newTestDlAPI()
	api.StatFn = func(ctx context.Context, peer telegram.Peer, msgID int64) (telegram.MediaInfo, error) {
		return telegram.MediaInfo{}, errors.New("FILE_REFERENCE_EXPIRED")
	}
	req := httptest.NewRequest("GET", "/dl/tok123/group/55/7", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 on upstream failure", rec.Code)
	}
}

// TestStartDownloadServer_ListensLoopback verifies the real listener binds
// 127.0.0.1 on an OS-chosen port and answers through the same handler.
func TestStartDownloadServer_ListensLoopback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base, err := startDownloadServer(ctx, &telegram.FakeAPI{})
	if err != nil {
		t.Fatalf("startDownloadServer: %v", err)
	}
	if !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Errorf("baseURL = %q, want loopback http address", base)
	}
	resp, err := http.Get(base + "/dl/nope/user/1/1")
	if err != nil {
		t.Fatalf("GET %s: %v", base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (bad token)", resp.StatusCode)
	}
}

// TestStartDownloadServer_BaseURLOpensFile is the END-TO-END regression for
// the 404-on-click bug: openDownloadLink builds its URL as
// "<base>/<kind>/<peer>/<msg>" — the returned base MUST therefore already
// carry the /dl/<token> prefix, or every click lands on the token guard.
func TestStartDownloadServer_BaseURLOpensFile(t *testing.T) {
	api := &telegram.FakeAPI{
		StatFn: func(ctx context.Context, peer telegram.Peer, msgID int64) (telegram.MediaInfo, error) {
			return telegram.MediaInfo{Name: "f.bin", Size: 2}, nil
		},
		DownloadFn: func(ctx context.Context, peer telegram.Peer, msgID int64, w io.Writer) (telegram.MediaInfo, error) {
			w.Write([]byte("ok"))
			return telegram.MediaInfo{Name: "f.bin", Size: 2}, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base, err := startDownloadServer(ctx, api)
	if err != nil {
		t.Fatalf("startDownloadServer: %v", err)
	}
	// Exactly the URL shape openDownloadLink produces.
	resp, err := http.Get(base + "/user/7/42")
	if err != nil {
		t.Fatalf("GET %s: %v", base, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Errorf("GET %s/user/7/42 = %d %q, want 200 \"ok\"", base, resp.StatusCode, body)
	}
}
