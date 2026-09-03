package media

import (
	"fmt"

	"github.com/user/tgchat/internal/telegram"
)

// URL returns the loopback download link for m's attachment, or "" when there
// is nothing to link: no media, no running server, or an optimistic local
// message that the server has no real ID to fetch.
func URL(base string, m telegram.Message) string {
	if base == "" || m.Media == "" || m.ID <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d/%d", base, m.PeerKind, m.PeerID, m.ID)
}
