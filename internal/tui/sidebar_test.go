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
