package workspace

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestWorkspaceRailView(t *testing.T) {
	m := New([]WorkspaceItem{
		{ID: "T1", Name: "Acme Corp", Initials: "AC", HasUnread: false},
		{ID: "T2", Name: "Beta Inc", Initials: "BI", HasUnread: true},
	}, 0)

	view := m.View(20) // 20 rows height
	if !strings.Contains(view, "AC") {
		t.Error("expected 'AC' in view")
	}
	if !strings.Contains(view, "BI") {
		t.Error("expected 'BI' in view")
	}
}

// TestWorkspaceRailInitialsAligned: the unread dot sits left of the
// initials and the keyboard cursor right of them, and neither moves the
// initials' column.
func TestWorkspaceRailInitialsAligned(t *testing.T) {
	m := New([]WorkspaceItem{
		{ID: "T1", Initials: "AC"},
		{ID: "T2", Initials: "BI", HasUnread: true},
		{ID: "T3", Initials: "CD"},
	}, 0)
	m.SetFocused(true)
	m.SetCursor(2)
	cols := map[string]int{}
	for _, line := range strings.Split(ansi.Strip(m.View(10)), "\n") {
		for _, ini := range []string{"AC", "BI", "CD"} {
			if i := strings.Index(line, ini); i >= 0 {
				cols[ini] = len([]rune(line[:i]))
				if ini == "BI" && !strings.Contains(line[:i], "●") {
					t.Errorf("unread row %q: dot is not left of the initials", line)
				}
				if ini == "CD" && !strings.Contains(line[i:], "◂") {
					t.Errorf("cursor row %q: cursor is not right of the initials", line)
				}
			}
		}
	}
	if cols["AC"] != cols["BI"] || cols["BI"] != cols["CD"] {
		t.Errorf("initials columns differ: %v", cols)
	}
}

// TestWorkspaceRailScrollsToCursor: with more workspaces than fit, the
// rail scrolls to keep the cursor (or, unfocused, the selected workspace)
// drawn, and clicks map to the rows actually drawn.
func TestWorkspaceRailScrollsToCursor(t *testing.T) {
	var items []WorkspaceItem
	for _, ini := range []string{"W1", "W2", "W3", "W4", "W5", "W6", "W7", "W8"} {
		items = append(items, WorkspaceItem{ID: ini, Initials: ini})
	}
	m := New(items, 0)
	const h = 12 // 10 rows inside the padding: 5 items with gaps
	m.SetHeight(h)
	view := func() string { return ansi.Strip(m.View(h)) }
	if v := view(); !strings.Contains(v, "W1") || strings.Contains(v, "W6") {
		t.Fatalf("unscrolled rail should draw W1-W5:\n%s", v)
	}

	m.SetFocused(true)
	m.SetCursor(7) // G
	v := view()
	if !strings.Contains(v, "W8 ◂") || strings.Contains(v, "W3") {
		t.Fatalf("cursor on W8: want W4-W8 drawn with the cursor on W8:\n%s", v)
	}
	if it, ok := m.ClickAt(1); !ok || it.ID != "W4" {
		t.Errorf("ClickAt(1) on the scrolled rail = %q, %v; want W4", it.ID, ok)
	}
	if _, ok := m.ClickAt(11); ok {
		t.Error("ClickAt below the last drawn item should miss")
	}

	m.SetCursor(1) // back up past the top of the window
	if v := view(); !strings.Contains(v, "W2 ◂") {
		t.Fatalf("cursor on W2 is not drawn:\n%s", v)
	}
	m.SetHeight(6) // shrink: 2 items fit
	if v := view(); !strings.Contains(v, "W2 ◂") {
		t.Fatalf("after a resize the cursor on W2 is not drawn:\n%s", v)
	}
	m.SetFocused(false) // unfocused: follow the selected workspace (W1)
	if v := view(); !strings.Contains(v, "W1") {
		t.Fatalf("unfocused rail should show the selected W1:\n%s", v)
	}
}

func TestWorkspaceRailSelect(t *testing.T) {
	m := New([]WorkspaceItem{
		{ID: "T1", Name: "Acme", Initials: "AC"},
		{ID: "T2", Name: "Beta", Initials: "BE"},
	}, 0)

	if m.SelectedID() != "T1" {
		t.Error("expected T1 selected initially")
	}

	m.Select(1)
	if m.SelectedID() != "T2" {
		t.Error("expected T2 selected after Select(1)")
	}
}

// TestClickAt asserts ClickAt's mapping from rail-local y to workspace
// item using the rail's View() layout: row 0 is the top padding, row
// 1 is item 0, row 2 is the gap between items, row 3 is item 1, and
// so on (Padding(1,0) above and "\n\n"-joined item rows).
func TestClickAt(t *testing.T) {
	m := New([]WorkspaceItem{
		{ID: "T1", Name: "Acme", Initials: "AC"},
		{ID: "T2", Name: "Beta", Initials: "BE"},
		{ID: "T3", Name: "Gamma", Initials: "GA"},
	}, 0)

	cases := []struct {
		name   string
		y      int
		wantID string
		wantOK bool
	}{
		{"top padding", 0, "", false},
		{"first item", 1, "T1", true},
		{"gap between items 0 and 1", 2, "", false},
		{"second item", 3, "T2", true},
		{"gap between items 1 and 2", 4, "", false},
		{"third item", 5, "T3", true},
		{"below last item", 6, "", false},
		{"well below content", 99, "", false},
		{"negative y", -1, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := m.ClickAt(tc.y)
			if ok != tc.wantOK {
				t.Fatalf("ClickAt(%d) ok=%v want %v", tc.y, ok, tc.wantOK)
			}
			if got.ID != tc.wantID {
				t.Errorf("ClickAt(%d) ID=%q want %q", tc.y, got.ID, tc.wantID)
			}
		})
	}
}

func TestClickAt_EmptyRail(t *testing.T) {
	m := New(nil, 0)
	if _, ok := m.ClickAt(1); ok {
		t.Error("ClickAt on empty rail must return ok=false")
	}
}

func TestNameByID(t *testing.T) {
	m := New([]WorkspaceItem{
		{ID: "T1", Name: "SWAP", Initials: "SW"},
		{ID: "T2", Name: "Home", Initials: "HO"},
	}, 0)
	cases := map[string]string{
		"T1":        "SWAP",
		"T2":        "Home",
		"T-missing": "",
		"":          "",
	}
	for id, want := range cases {
		if got := m.NameByID(id); got != want {
			t.Errorf("NameByID(%q) = %q want %q", id, got, want)
		}
	}
}

func TestOtherUnreadCount_NoReader(t *testing.T) {
	m := New([]WorkspaceItem{{ID: "T1"}}, 0)
	if got := m.OtherUnreadCount("T1"); got != 0 {
		t.Errorf("OtherUnreadCount with no reader = %d want 0", got)
	}
	// Removing the reader drops what it last reported.
	m.SetUnreadReader(func() []string { return []string{"T2"} })
	m.RefreshUnreads()
	m.SetUnreadReader(nil)
	if got := m.OtherUnreadCount("T1"); got != 0 {
		t.Errorf("OtherUnreadCount after removing the reader = %d want 0", got)
	}
}

func TestOtherUnreadCount(t *testing.T) {
	m := New([]WorkspaceItem{
		{ID: "T1"}, {ID: "T2"}, {ID: "T3"},
	}, 0)
	m.SetUnreadReader(func() []string { return []string{"T1", "T2", "T3"} })
	m.RefreshUnreads()

	cases := []struct {
		activeID string
		want     int
	}{
		{"T1", 2},
		{"T2", 2},
		{"T3", 2},
		{"T-missing", 3}, // active workspace not in unread set: counts all
		{"", 3},          // empty active: counts all; caller is responsible
	}
	for _, tc := range cases {
		if got := m.OtherUnreadCount(tc.activeID); got != tc.want {
			t.Errorf("OtherUnreadCount(%q) = %d want %d", tc.activeID, got, tc.want)
		}
	}
}

func TestOtherUnreadCount_EmptyReaderResult(t *testing.T) {
	m := New([]WorkspaceItem{{ID: "T1"}, {ID: "T2"}}, 0)
	m.SetUnreadReader(func() []string { return nil })
	m.RefreshUnreads()
	if got := m.OtherUnreadCount("T1"); got != 0 {
		t.Errorf("OtherUnreadCount with empty reader = %d want 0", got)
	}
}

// The reader runs SQLite queries on the UI goroutine (railUnreadWorkspaces
// in cmd/slk), so a refresh followed by OtherUnreadCount, which is what
// App.notifyReadStateChanged does, must read it once, not twice.
// OtherUnreadCount counts the set the last RefreshUnreads read.
func TestOtherUnreadCount_UsesLastRefreshWithoutReading(t *testing.T) {
	m := New([]WorkspaceItem{{ID: "T1"}, {ID: "T2"}, {ID: "T3"}}, 0)
	calls := 0
	unread := []string{"T2", "T3"}
	m.SetUnreadReader(func() []string { calls++; return unread })

	m.RefreshUnreads()
	unread = nil // a reader call from here on would change the answer
	if got := m.OtherUnreadCount("T1"); got != 2 {
		t.Errorf("OtherUnreadCount(T1) = %d want 2 (from the last refresh)", got)
	}
	if calls != 1 {
		t.Errorf("reader calls = %d want 1 (RefreshUnreads only)", calls)
	}
}
