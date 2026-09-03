package filesync

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/user/tgchat/internal/telegram"
)

// chatFile is one conversation's on-disk mirror.
//
// The invariant that makes the single-file layout safe:
//
//	above the marker — msgs is the source of truth, the file is output
//	below the marker — the file is the source of truth, we only ever clear it
//
// So the log is never parsed back: a stale editor buffer saved over it is
// repaired by the next write, and the user's in-progress draft is never
// touched except to clear it after a successful send.
type chatFile struct {
	path string
	peer telegram.Peer

	mu   sync.Mutex
	msgs []telegram.Message
	// mtime is the file's modification time right after our own write. The
	// poll loop skips files still carrying it — that is how a
	// write → notice → resend loop is avoided.
	mtime time.Time
}

// draftOf returns the compose area: everything after the last marker,
// trimmed. "" when the marker is missing — the next write restores it.
func draftOf(content string) string {
	i := strings.LastIndex(content, Marker)
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(content[i+len(Marker):])
}

// write rewrites the file from msgs, preserving whatever draft is currently
// on disk. Caller holds c.mu.
func (c *chatFile) write(dlBase string) error {
	draft := ""
	if b, err := os.ReadFile(c.path); err == nil {
		draft = draftOf(string(b))
	}
	return c.writeWithDraft(dlBase, draft)
}

// writeWithDraft writes log + marker + draft via a temp file and a rename, so
// an editor reading concurrently never sees a half-written file. Caller holds
// c.mu.
func (c *chatFile) writeWithDraft(dlBase, draft string) error {
	content := Render(c.msgs, dlBase)
	if draft != "" {
		content += draft + "\n"
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return err
	}
	if fi, err := os.Stat(c.path); err == nil {
		c.mtime = fi.ModTime()
	}
	return nil
}

// add appends a message and rewrites the file. Dedups by ID because our own
// sends are appended optimistically and may echo back through updates.
func (c *chatFile) add(m telegram.Message, dlBase string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.msgs {
		if e.ID == m.ID && m.ID != 0 {
			return nil
		}
	}
	c.msgs = append(c.msgs, m)
	return c.write(dlBase)
}
