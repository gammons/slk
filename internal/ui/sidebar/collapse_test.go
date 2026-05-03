package sidebar

import (
	"strings"
	"testing"
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
	// Cursor: Threads → Direct Messages header. Toggle it: should collapse.
	m.MoveDown()
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
		{ID: "C1", Name: "general", Type: "channel", UnreadCount: 2},
		{ID: "C2", Name: "random", Type: "channel", UnreadCount: 5},
	})
	// Channels is collapsed by default; aggregate badge should sum to 7.
	view := m.View(15, 30)
	if !strings.Contains(view, "•7") {
		t.Errorf("collapsed header should show aggregate badge •7, got:\n%s", view)
	}
	// Expand, the badge disappears (per-channel dots take over).
	m.ToggleCollapse("Channels")
	view = m.View(15, 30)
	if strings.Contains(view, "•7") {
		t.Errorf("expanded header should not carry an aggregate badge:\n%s", view)
	}
}

func TestSelectByID_AutoExpandsCollapsedSection(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
	})
	if !m.IsCollapsed("Channels") {
		t.Fatal("precondition: Channels section should be collapsed by default")
	}
	m.SelectByID("C1")
	if m.IsCollapsed("Channels") {
		t.Errorf("SelectByID into a collapsed section should auto-expand it")
	}
	if m.SelectedID() != "C1" {
		t.Errorf("SelectByID should land the cursor on C1, got %q", m.SelectedID())
	}
}

// TestSelectByID_AutoExpandsCollapsedSection_SlackMode regresses a bug
// where SelectByID's auto-expand path consulted the name-keyed
// `collapsed` map directly instead of routing through IsCollapsed /
// ToggleCollapse, which dispatch by mode. In Slack mode the collapse
// state lives in `collapseByID`, so the direct-map path silently
// failed to expand the section and the cursor never moved.
//
// User-visible symptom (before the fix): with use_slack_sections=true
// (the default), Ctrl+T-finding a channel inside a collapsed Slack
// section did nothing.
func TestSelectByID_AutoExpandsCollapsedSection_SlackMode(t *testing.T) {
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

	if m.IsCollapsed("A") {
		t.Errorf("SelectByID into a collapsed Slack-mode section should auto-expand it")
	}
	if m.SelectedID() != "C1" {
		t.Errorf("SelectByID should land the cursor on C1, got %q", m.SelectedID())
	}
}

func TestToggleCollapse_PreservesCursorOnHeader(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "D1", Name: "alice", Type: "dm"},
	})
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
