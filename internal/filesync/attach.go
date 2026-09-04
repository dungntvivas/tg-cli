package filesync

import "strings"

// parseDraft splits a compose area into a file send or a text send.
//
// A draft whose FIRST line starts with "@" is a file: everything after the @
// is the path (so names with spaces work, unlike a whitespace-delimited
// scan), and the remaining lines are the caption. Anything else is plain
// text — restricting @ to the first line is what keeps "gửi cho @dungvivas"
// and email addresses from being mistaken for attachments.
//
// Returns (path, caption, "") for a file, or ("", "", text) for text.
func parseDraft(draft string) (path, caption, text string) {
	first, rest, _ := strings.Cut(draft, "\n")
	if !strings.HasPrefix(first, "@") {
		return "", "", draft
	}
	// Trim quotes: "Copy Path as..." in some tools wraps the path.
	path = strings.Trim(strings.TrimSpace(strings.TrimPrefix(first, "@")), `"`)
	if path == "" {
		return "", "", draft // a bare "@" is just text
	}
	return path, strings.TrimSpace(rest), ""
}
