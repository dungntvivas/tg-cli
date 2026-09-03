package filesync

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/user/tgchat/internal/telegram"
)

const (
	// maxDialogs bounds the startup fan-out: an account with hundreds of
	// dialogs would otherwise fire hundreds of History calls and trip
	// Telegram's flood-wait.
	maxDialogs = 20
	// historyDepth is enough context to follow a conversation while keeping
	// the file small enough for an editor to open instantly.
	historyDepth = 200
	// pollInterval is below human perception for a save → send round trip.
	pollInterval = 500 * time.Millisecond
)

// peerKey indexes conversations by the pair that identifies them; PeerID
// alone collides across kinds.
type peerKey struct {
	id   int64
	kind string
}

// syncer, not sync: chatfile.go imports the stdlib sync package, and a
// package-scope type of the same name would collide with that import.
type syncer struct {
	api    telegram.API
	dlBase string
	files  []*chatFile
	byPeer map[peerKey]*chatFile
}

// Run mirrors the most recent conversations into dir and keeps them in sync
// until ctx is done. It blocks; callers run it in a goroutine alongside the
// TUI. Returning an error means filesync is off — the TUI is unaffected.
func Run(ctx context.Context, api telegram.API, dlBase, dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create chat dir: %w", err)
	}
	dialogs, err := api.Dialogs(ctx)
	if err != nil {
		return fmt.Errorf("list dialogs: %w", err)
	}
	sort.Slice(dialogs, func(i, j int) bool {
		return dialogs[i].LastTime.After(dialogs[j].LastTime)
	})
	if len(dialogs) > maxDialogs {
		dialogs = dialogs[:maxDialogs]
	}

	s := &syncer{api: api, dlBase: dlBase, byPeer: map[peerKey]*chatFile{}}
	taken := map[string]bool{}
	for _, d := range dialogs {
		peer := telegram.Peer{ID: d.ID, Kind: d.Kind, AccessHash: d.AccessHash}
		msgs, err := api.History(ctx, peer, historyDepth)
		if err != nil {
			// One unreachable dialog must not cost the user the other 19.
			log.Printf("filesync: history %q: %v", d.Title, err)
			continue
		}
		c := &chatFile{
			path: filepath.Join(dir, fileName(d.Title, d.ID, taken)),
			peer: peer,
			msgs: msgs,
		}
		c.mu.Lock()
		err = c.writeWithDraft(dlBase, "")
		c.mu.Unlock()
		if err != nil {
			log.Printf("filesync: write %q: %v", c.path, err)
			continue
		}
		s.byPeer[peerKey{d.ID, d.Kind}] = c
		s.files = append(s.files, c)
	}

	// byPeer is fully built before the handler is registered and never
	// written afterwards, so it needs no lock of its own.
	api.OnMessage(s.onMessage)
	s.poll(ctx)
	return nil
}

// onMessage routes a live message into its file. Runs on gotd's update
// goroutine — chatFile.add takes the per-conversation lock.
func (s *syncer) onMessage(m telegram.Message) {
	c, ok := s.byPeer[peerKey{m.PeerID, m.PeerKind}]
	if !ok {
		return // outside the synced set; still visible in the TUI
	}
	if err := c.add(m, s.dlBase); err != nil {
		log.Printf("filesync: append %q: %v", c.path, err)
	}
}

// poll watches for user saves. mtime polling rather than fsnotify: it costs
// no dependency, and fsnotify on Windows emits duplicate events that would
// need the same debounce anyway.
func (s *syncer) poll(ctx context.Context) {
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, c := range s.files {
				c.checkAndSend(ctx, s.api, s.dlBase)
			}
		}
	}
}
