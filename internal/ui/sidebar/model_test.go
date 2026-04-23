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
	if m.SelectedID() != "C1" {
		t.Error("expected C1 selected initially")
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
