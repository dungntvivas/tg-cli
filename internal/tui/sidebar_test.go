package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/user/tgchat/internal/telegram"
)

func sampleDialogs() []telegram.Dialog {
	return []telegram.Dialog{
		{ID: 1, Title: "Alice", Unread: 3, LastMsg: "hi", LastTime: time.Now()},
		{ID: 2, Title: "Bob", Unread: 0, LastMsg: "ok", LastTime: time.Now()},
		{ID: 3, Title: "Team group", Unread: 0, LastMsg: "lunch?", LastTime: time.Now()},
	}
}

func TestRenderSidebar_ShowsAllTitles(t *testing.T) {
	rows := RenderSidebar(sampleDialogs(), -1)
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	for _, want := range []string{"Alice", "Bob", "Team group"} {
		found := false
		for _, r := range rows {
			if strings.Contains(r, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %q in rows %v", want, rows)
		}
	}
}

func TestRenderSidebar_UnreadMarker(t *testing.T) {
	rows := RenderSidebar(sampleDialogs(), -1)
	if !strings.HasPrefix(rows[0], "●") {
		t.Errorf("unread dialog should start with ●, got %q", rows[0])
	}
	if strings.HasPrefix(rows[1], "●") {
		t.Errorf("read dialog should not start with ●, got %q", rows[1])
	}
}

func TestRenderSidebar_UnreadCount(t *testing.T) {
	rows := RenderSidebar(sampleDialogs(), -1)
	if !strings.Contains(rows[0], "(3)") {
		t.Errorf("unread count not shown, got %q", rows[0])
	}
}

func TestRenderSidebar_SelectionMarker(t *testing.T) {
	rows := RenderSidebar(sampleDialogs(), 1)
	// selection marker should distinguish the selected row
	if rows[0] == rows[1] {
		t.Errorf("selected row not visually distinct: %v", rows)
	}
}

func TestRenderSidebar_EmptyList(t *testing.T) {
	rows := RenderSidebar(nil, -1)
	if len(rows) != 1 {
		t.Errorf("empty list should yield one placeholder row, got %v", rows)
	}
}

// TestRenderSidebar_ChatPrefixEveryRow verifies every row is prefixed with
// "chat - " so the sidebar's role ("you're talking to these people/groups")
// is self-explanatory. The same shape covers P2P partner, group, and bot —
// only the dialog's own Title differs.
func TestRenderSidebar_ChatPrefixEveryRow(t *testing.T) {
	rows := RenderSidebar(sampleDialogs(), -1)
	for _, r := range rows {
		if !strings.Contains(r, chatPrefix) {
			t.Errorf("row missing %q prefix: %q", chatPrefix, r)
		}
	}
}

// TestRenderSidebar_PrefixBeforeUnreadBadge checks the exact format:
// marker + "chat - " + title + " (N)". The unread badge stays AFTER the
// title so a long title doesn't push it off-screen.
func TestRenderSidebar_PrefixBeforeUnreadBadge(t *testing.T) {
	rows := RenderSidebar(sampleDialogs(), -1)
	// First row: "● chat - Alice (3)"
	want := "● " + chatPrefix + "Alice (3)"
	if rows[0] != want {
		t.Errorf("rows[0] = %q, want %q", rows[0], want)
	}
	// Third row (group): no unread, "  chat - Team group"
	if !strings.Contains(rows[2], chatPrefix+"Team group") {
		t.Errorf("row 2 missing %qTeam group: %q", chatPrefix, rows[2])
	}
}
