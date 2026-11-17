package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/workspace"
)

// TestRail_ToggleHides covers #89: with several workspaces ctrl+g hides
// the rail (no columns, and h from the sidebar stays put), the status
// bar names the active workspace, and a second ctrl+g brings it back.
func TestRail_ToggleHides(t *testing.T) {
	a := newTestApp(t, withWorkspaces(
		workspace.WorkspaceItem{ID: "T1", Name: "Acme", Initials: "AC"},
		workspace.WorkspaceItem{ID: "T2", Name: "Beta", Initials: "BE"},
	), withRender())
	ctrlG := keyMod('g', tea.ModCtrl)

	a.focusedPanel = PanelWorkspace
	a.Update(ctrlG)
	_ = a.View()
	if got := a.layout.RailWidth(); got != 0 {
		t.Errorf("hidden: rail width = %d, want 0", got)
	}
	if a.focusedPanel != PanelSidebar {
		t.Errorf("hiding the focused rail left focus on panel %v, want the sidebar", a.focusedPanel)
	}
	if view := a.View().Content; !strings.Contains(view, " AC ") {
		t.Errorf("hidden: status bar lacks the active workspace's initials")
	}
	a.FocusLeft()
	if a.focusedPanel != PanelSidebar {
		t.Errorf("h from the sidebar moved focus to panel %v; with the rail hidden it is the left edge", a.focusedPanel)
	}

	a.Update(ctrlG)
	_ = a.View()
	if got, want := a.layout.RailWidth(), a.workspaceRail.Width(); got != want {
		t.Errorf("shown again: rail width = %d, want %d", got, want)
	}
	if got := a.statusWorkspaceLabel(); got != "" {
		t.Errorf("shown again: status workspace label = %q, want empty", got)
	}
}

// show_workspace_rail = false starts with the rail hidden.
func TestRail_HiddenByConfig(t *testing.T) {
	a := newTestApp(t, withWorkspaces(
		workspace.WorkspaceItem{ID: "T1", Name: "Acme", Initials: "AC"},
		workspace.WorkspaceItem{ID: "T2", Name: "Beta", Initials: "BE"},
	))
	a.SetShowWorkspaceRail(false)
	if got := a.railWidth(); got != 0 {
		t.Errorf("rail width = %d, want 0", got)
	}
}
