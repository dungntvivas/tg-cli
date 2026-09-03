package filesync

import "testing"

// TestFileName: dialog titles are arbitrary user text and must survive
// becoming a Windows file name.
func TestFileName(t *testing.T) {
	taken := map[string]bool{}
	cases := []struct {
		title string
		id    int64
		want  string
	}{
		{"Nam", 1, "Nam.md"},
		{"Team A/B", 2, "Team A_B.md"},
		{`bad:*?"<>|\ chars`, 3, "bad________ chars.md"},
		{"Nam", 4, "Nam (4).md"}, // collision with the first entry
		{"   ", 5, "chat.md"},    // blank title
		{"trailing dot.", 6, "trailing dot.md"},
	}
	for _, tc := range cases {
		if got := fileName(tc.title, tc.id, taken); got != tc.want {
			t.Errorf("fileName(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}
