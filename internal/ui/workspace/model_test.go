package workspace

import (
	"strings"
	"testing"
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
