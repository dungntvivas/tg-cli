package filesync

import (
	"fmt"
	"strings"
)

// forbidden are the characters Windows rejects in a file name. Telegram
// titles routinely contain "/" and ":".
const forbidden = `\/:*?"<>|`

// fileName turns a dialog title into a safe base name, disambiguating
// collisions with the peer id so two chats called "Support" stay separate.
// taken is updated in place.
func fileName(title string, id int64, taken map[string]bool) string {
	name := strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(forbidden, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(title))
	// Windows silently drops trailing dots and spaces from file names, which
	// would turn "Nam." and "Nam" into the same file.
	name = strings.TrimRight(name, " .")
	if name == "" {
		name = "chat"
	}
	if taken[name] {
		name = fmt.Sprintf("%s (%d)", name, id)
	}
	taken[name] = true
	return name + ".md"
}
