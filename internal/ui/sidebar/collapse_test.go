package sidebar

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/cache"
)

func TestNew_DefaultCollapsesChannelsSection(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "D1", Name: "alice", Type: "dm"},
	})
	if !m.IsCollapsed("Channels") {
		t.Errorf("Channels section should be collapsed by default")
	}
	if m.IsCollapsed("Direct Messages") {
		t.Errorf("Direct Messages section should be expanded by default")
	}
	view := m.View(20, 30)
	if strings.Contains(view, "general") {
		t.Errorf("collapsed Channels section should hide its rows; view contained 'general':\n%s", view)
	}
	if !strings.Contains(view, "alice") {
		t.Errorf("expanded Direct Messages section should show its rows; view did not contain 'alice':\n%s", view)
	}
}

func TestToggleCollapse_OnSelectedHeader(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "D1", Name: "alice", Type: "dm"},
	})
	// Cursor: Threads → Activity → Direct Messages header. Toggle it:
	// should collapse.
	m.MoveDown() // Activity
	m.MoveDown() // Direct Messages header
	name, ok := m.IsSectionHeaderSelected()
	if !ok || name != "Direct Messages" {
		t.Fatalf("expected DM header selected, got name=%q ok=%v", name, ok)
	}
	if m.IsCollapsed("Direct Messages") {
		t.Fatalf("precondition: DM section should start expanded")
	}
	if !m.ToggleCollapseSelected() {
		t.Fatalf("ToggleCollapseSelected should report success when on a header")
	}
	if !m.IsCollapsed("Direct Messages") {
		t.Errorf("DM section should be collapsed after toggle")
	}
	view := m.View(20, 30)
	if strings.Contains(view, "alice") {
		t.Errorf("collapsed DM section should hide 'alice':\n%s", view)
	}
	// Toggle again to expand.
	m.ToggleCollapseSelected()
	view = m.View(20, 30)
	if !strings.Contains(view, "alice") {
		t.Errorf("re-expanded DM section should show 'alice':\n%s", view)
	}
}

func TestToggleCollapse_NotOnHeader_IsNoop(t *testing.T) {
	m := New([]ChannelItem{{ID: "D1", Name: "alice", Type: "dm"}})
	// Cursor on Threads row.
	if !m.IsThreadsSelected() {
		t.Fatal("precondition: Threads should be selected")
	}
	if m.ToggleCollapseSelected() {
		t.Errorf("ToggleCollapseSelected on the Threads row should report false")
	}
}

func TestNavigation_SkipsCollapsedSectionItems(t *testing.T) {
	// Two sections both with channels. Collapse one and verify j/k
	// never lands on its hidden children.
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel"},
		{ID: "D1", Name: "alice", Type: "dm"},
	})
	// Default: Channels collapsed, DM expanded.
	// Walk from Threads forward and collect all channel IDs we land on.
	visited := map[string]bool{}
	for i := 0; i < 20; i++ {
		if id := m.SelectedID(); id != "" {
			visited[id] = true
		}
		m.MoveDown()
	}
	if visited["C1"] || visited["C2"] {
		t.Errorf("collapsed Channels section's items should not be visited; got %v", visited)
	}
	if !visited["D1"] {
		t.Errorf("expanded DM section's item should be reachable; got %v", visited)
	}
}

func TestCollapsedHeader_ShowsAggregateUnreadBadge(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel"},
	})
	// After the read-state sync rewrite, the aggregate counts
	// channels-with-unreads (not the sum of per-channel counts), and
	// the DB (read via readStateReader) is the source of truth.
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true},
			"C2": {HasUnread: true},
		}
	})
	// Channels is collapsed by default; aggregate badge should count 2.
	view := m.View(15, 30)
	if !strings.Contains(view, "•2") {
		t.Errorf("collapsed header should show aggregate badge •2, got:\n%s", view)
	}
	// Expand, the badge disappears (per-channel dots take over).
	m.ToggleCollapse("Channels")
	view = m.View(15, 30)
	if strings.Contains(view, "•2") {
		t.Errorf("expanded header should not carry an aggregate badge:\n%s", view)
	}
}

// Opening a channel inside a collapsed section shows that row alone
// under the header; the section stays collapsed.
func TestSelectByID_RevealsOnlyThatRow(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "deploys", Type: "channel"},
	})
	m.SelectByID("C1")
	if !m.IsCollapsed("Channels") {
		t.Error("SelectByID should not expand the collapsed section")
	}
	if m.SelectedID() != "C1" {
		t.Errorf("SelectByID should land the cursor on C1, got %q", m.SelectedID())
	}
	view := m.View(20, 30)
	if !strings.Contains(view, "general") || strings.Contains(view, "deploys") {
		t.Errorf("only the opened channel should show:\n%s", view)
	}
	// Once active it stays, and the previous active one hides again.
	m.SetActiveChannelID("C1")
	m.SelectByID("C2")
	m.SetActiveChannelID("C2")
	view = m.View(20, 30)
	if strings.Contains(view, "general") || !strings.Contains(view, "deploys") {
		t.Errorf("only the active channel should show:\n%s", view)
	}
}

// TestSelectByID_RevealsInCollapsedSection_SlackMode regresses a bug
// where SelectByID consulted the name-keyed `collapsed` map directly
// instead of routing through IsCollapsed, which dispatches by mode. In
// Slack mode the collapse state lives in `collapseByID`, so Ctrl+T into
// a collapsed Slack section did nothing.
func TestSelectByID_RevealsInCollapsedSection_SlackMode(t *testing.T) {
	items := []ChannelItem{{ID: "C1", Name: "general", Type: "channel", Section: "A"}}
	provider := &fakeProvider{
		ready:    true,
		sections: []SectionMeta{{ID: "A", Name: "Alerts", Type: "standard"}},
	}
	m := New(items)
	m.SetSectionsProvider(provider)
	m.ToggleCollapse("A") // Slack-mode toggle writes collapseByID["A"]=true
	if !m.IsCollapsed("A") {
		t.Fatal("precondition: section A should be collapsed in Slack mode")
	}

	m.SelectByID("C1")

	if !m.IsCollapsed("A") {
		t.Errorf("SelectByID should not expand the collapsed Slack-mode section")
	}
	if m.SelectedID() != "C1" {
		t.Errorf("SelectByID should land the cursor on C1, got %q", m.SelectedID())
	}
}

func TestStartCollapsed(t *testing.T) {
	items := []ChannelItem{
		{ID: "D1", Name: "alice", Type: "dm"},
		{ID: "C1", Name: "general", Type: "channel", Section: "A"},
	}
	m := New(items)
	m.SetStartCollapsed(true)
	if !m.IsCollapsed("Direct Messages") {
		t.Error("config mode: DMs should start collapsed")
	}
	m.ToggleCollapse("Direct Messages")
	if m.IsCollapsed("Direct Messages") {
		t.Error("first toggle should expand a section that started collapsed")
	}

	s := New(items)
	s.SetStartCollapsed(true)
	s.SetSectionsProvider(&fakeProvider{
		ready:    true,
		sections: []SectionMeta{{ID: "A", Name: "Alerts", Type: "standard"}},
	})
	if !s.IsCollapsed("A") {
		t.Error("Slack mode: section A should start collapsed")
	}
	if view := s.View(20, 30); strings.Contains(view, "general") {
		t.Errorf("collapsed section should hide its rows:\n%s", view)
	}
}

func TestToggleCollapse_PreservesCursorOnHeader(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "D1", Name: "alice", Type: "dm"},
	})
	m.MoveDown() // onto Activity row
	m.MoveDown() // onto DM header
	if name, _ := m.IsSectionHeaderSelected(); name != "Direct Messages" {
		t.Fatalf("precondition: expected DM header, got %q", name)
	}
	m.ToggleCollapseSelected() // collapse DMs
	// Cursor should still be on the DM header.
	if name, ok := m.IsSectionHeaderSelected(); !ok || name != "Direct Messages" {
		t.Errorf("cursor should remain on DM header after collapse; got name=%q ok=%v", name, ok)
	}
	m.ToggleCollapseSelected() // expand again
	if name, ok := m.IsSectionHeaderSelected(); !ok || name != "Direct Messages" {
		t.Errorf("cursor should remain on DM header after expand; got name=%q ok=%v", name, ok)
	}
}

func TestIsThreadsSelected_FalseOnSectionHeader(t *testing.T) {
	m := New([]ChannelItem{{ID: "D1", Name: "alice", Type: "dm"}})
	m.MoveDown()
	if m.IsThreadsSelected() {
		t.Errorf("Threads should not be reported selected when cursor is on a section header")
	}
	if _, ok := m.SelectedItem(); ok {
		t.Errorf("SelectedItem should return ok=false when cursor is on a section header")
	}
}

func TestShowUnreadInCollapsed(t *testing.T) {
	state := map[string]cache.ReadState{}
	newModel := func(show bool) Model {
		m := New([]ChannelItem{
			{ID: "C1", Name: "general", Type: "channel"},
			{ID: "C2", Name: "deploys", Type: "channel"},
			{ID: "C3", Name: "quiet", Type: "channel", IsMuted: true},
		})
		m.SetReadStateReader(func() map[string]cache.ReadState { return state })
		m.SetShowUnreadInCollapsed(show)
		return m
	}
	state["C2"] = cache.ReadState{HasUnread: true}
	state["C3"] = cache.ReadState{HasUnread: true}

	off := newModel(false)
	if view := off.View(20, 30); strings.Contains(view, "deploys") {
		t.Errorf("option off: collapsed section should hide unread 'deploys':\n%s", view)
	}

	m := newModel(true)
	view := m.View(20, 30)
	if !strings.Contains(view, "deploys") {
		t.Errorf("unread 'deploys' should show in collapsed Channels:\n%s", view)
	}
	if strings.Contains(view, "general") || strings.Contains(view, "quiet") {
		t.Errorf("read and muted channels should stay hidden:\n%s", view)
	}
	// The row is navigable, and selecting it does not expand the section.
	m.SelectByID("C2")
	if m.SelectedID() != "C2" || !m.IsCollapsed("Channels") {
		t.Errorf("SelectByID: selected=%q collapsed=%v", m.SelectedID(), m.IsCollapsed("Channels"))
	}

	// Read state flips arrive through Invalidate; the row follows them.
	state["C1"] = cache.ReadState{HasUnread: true}
	state["C2"] = cache.ReadState{}
	m.SetActiveChannelID("C2") // being read: stays shown
	m.Invalidate()
	view = m.View(20, 30)
	if !strings.Contains(view, "general") || !strings.Contains(view, "deploys") {
		t.Errorf("new unread 'general' and active 'deploys' should show:\n%s", view)
	}
	m.SetActiveChannelID("C1")
	if view = m.View(20, 30); strings.Contains(view, "deploys") {
		t.Errorf("read, inactive 'deploys' should hide again:\n%s", view)
	}
}

// A redraw that sees read state nav has not caught up with yet must not
// draw a row the user cannot select.
func TestShowUnreadInCollapsed_DrawsOnlyNavRows(t *testing.T) {
	state := map[string]cache.ReadState{}
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	m.SetReadStateReader(func() map[string]cache.ReadState { return state })
	m.SetShowUnreadInCollapsed(true)
	_ = m.View(20, 30)

	state["C1"] = cache.ReadState{HasUnread: true}
	m.InvalidateCache() // a redraw for another reason, before Invalidate
	if view := m.View(20, 30); strings.Contains(view, "general") {
		t.Errorf("row not in nav should not be drawn yet:\n%s", view)
	}
	m.Invalidate()
	if view := m.View(20, 30); !strings.Contains(view, "general") {
		t.Errorf("row should show once nav is rebuilt:\n%s", view)
	}
}

func TestShowUnreadInCollapsed_CursorFallsBackToHeader(t *testing.T) {
	state := map[string]cache.ReadState{"C2": {HasUnread: true}}
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "deploys", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState { return state })
	m.SetShowUnreadInCollapsed(true)
	m.SelectByID("C2")
	if m.SelectedID() != "C2" {
		t.Fatalf("precondition: C2 selected, got %q", m.SelectedID())
	}

	state["C2"] = cache.ReadState{} // read elsewhere
	m.Invalidate()
	if name, ok := m.IsSectionHeaderSelected(); !ok || name != "Channels" {
		t.Errorf("cursor should fall back to the Channels header, got name=%q ok=%v", name, ok)
	}
}
