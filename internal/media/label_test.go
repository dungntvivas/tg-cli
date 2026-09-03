package media

import "testing"

// TestTruncate covers the rune-safe truncation helper directly,
// including multibyte characters (must count runes, not bytes).
func TestTruncate(t *testing.T) {
	if got := Truncate("short", 10); got != "short" {
		t.Errorf("under limit: got %q", got)
	}
	if got := Truncate("abcdefghij", 4); got != "abcd…" {
		t.Errorf("over limit: got %q, want abcd…", got)
	}
	if got := Truncate("tiếng Việt đấy", 7); got != "tiếng V…" {
		t.Errorf("multibyte: got %q, want tiếng V…", got)
	}
}
