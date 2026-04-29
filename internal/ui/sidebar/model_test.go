package sidebar

import (
	"strings"
	"testing"
)

func TestSidebarView(t *testing.T) {
	channels := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel", UnreadCount: 0},
		{ID: "C2", Name: "random", Type: "channel", UnreadCount: 3},
		{ID: "C3", Name: "alice", Type: "dm", Presence: "active"},
	}

	m := New(channels)
	view := m.View(20, 25) // height=20, width=25

	if !strings.Contains(view, "general") {
		t.Error("expected 'general' in view")
	}
	if !strings.Contains(view, "random") {
		t.Error("expected 'random' in view")
	}
}

func TestSidebarNavigation(t *testing.T) {
	channels := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel"},
		{ID: "C3", Name: "eng", Type: "channel"},
	}

	m := New(channels)
	// Default selection is now the synthetic Threads row; step off it to
	// reach the first channel.
	m.MoveDown()
	if m.SelectedID() != "C1" {
		t.Error("expected C1 selected after stepping off the Threads row")
	}

	m.MoveDown()
	if m.SelectedID() != "C2" {
		t.Error("expected C2 after move down")
	}

	m.MoveDown()
	m.MoveDown() // should stop at bottom
	if m.SelectedID() != "C3" {
		t.Error("expected C3 at bottom")
	}

	m.MoveUp()
	if m.SelectedID() != "C2" {
		t.Error("expected C2 after move up")
	}
}

func TestThreadsItem_DefaultSelected(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "design", Type: "channel"},
	})
	if !m.IsThreadsSelected() {
		t.Errorf("expected Threads entry to be selected by default (top of list)")
	}
}

func TestThreadsItem_MoveDownLeavesIt(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "design", Type: "channel"},
	})
	m.MoveDown()
	if m.IsThreadsSelected() {
		t.Errorf("MoveDown should leave the Threads entry")
	}
	item, ok := m.SelectedItem()
	if !ok || item.ID != "C1" {
		t.Errorf("first channel should be selected, got %+v ok=%v", item, ok)
	}
}

func TestThreadsItem_MoveUpReturnsToIt(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
	})
	m.MoveDown()
	if m.IsThreadsSelected() {
		t.Fatalf("precondition: should be on a channel")
	}
	m.MoveUp()
	if !m.IsThreadsSelected() {
		t.Errorf("MoveUp from first channel should land on Threads entry")
	}
}

func TestThreadsItem_UnreadBadgeRenders(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	m.SetThreadsUnreadCount(3)
	out := m.View(10, 30)
	if !strings.Contains(out, "Threads") {
		t.Errorf("View should contain 'Threads': %q", out)
	}
	if !strings.Contains(out, "3") {
		t.Errorf("View should contain unread count '3': %q", out)
	}
}

func TestThreadsItem_SelectedItemFalseWhenOnThreadsRow(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	if _, ok := m.SelectedItem(); ok {
		t.Errorf("SelectedItem should return ok=false when Threads row is selected")
	}
}

func TestThreadsItem_SelectByIDClearsThreadsSelection(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	if !m.IsThreadsSelected() {
		t.Fatal("precondition")
	}
	m.SelectByID("C1")
	if m.IsThreadsSelected() {
		t.Errorf("SelectByID should clear Threads selection")
	}
}

func TestSidebarFilter(t *testing.T) {
	channels := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel"},
		{ID: "C3", Name: "eng", Type: "channel"},
	}

	m := New(channels)
	m.SetFilter("gen")

	visible := m.VisibleItems()
	if len(visible) != 1 {
		t.Errorf("expected 1 filtered result, got %d", len(visible))
	}
	if visible[0].Name != "general" {
		t.Errorf("expected 'general', got %q", visible[0].Name)
	}

	m.SetFilter("")
	visible = m.VisibleItems()
	if len(visible) != 3 {
		t.Errorf("expected 3 items after clear filter, got %d", len(visible))
	}
}
