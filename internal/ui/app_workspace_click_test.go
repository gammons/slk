package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/ui/workspace"
)

// TestApp_ClickOnWorkspaceRailSwitches asserts that clicking a
// workspace tile in the rail invokes the registered workspace service
// with the clicked team ID, and that clicking the currently-active
// tile is a no-op (no switcher call, no command).
func TestApp_ClickOnWorkspaceRailSwitches(t *testing.T) {
	a := NewApp()
	a.width = 120
	a.height = 30
	a.SetWorkspaces([]workspace.WorkspaceItem{
		{ID: "T1", Name: "Acme", Initials: "AC"},
		{ID: "T2", Name: "Beta", Initials: "BE"},
		{ID: "T3", Name: "Gamma", Initials: "GA"},
	})
	// Force layout so layoutRailWidth populates.
	_ = a.View()
	if a.layout.railWidth <= 0 {
		t.Fatalf("layoutRailWidth should be populated after View(); got %d", a.layout.railWidth)
	}
	// Workspace 0 is selected by default; we expect a click on
	// workspace 1 (tile at Y=3) to trigger the switcher.
	var lastSwitch string
	a.setWorkspaceSwitcherForTest(func(teamID string) tea.Msg {
		lastSwitch = teamID
		return nil
	})

	// Click the second tile. The rail has no top border; tiles
	// occupy rail rows 1, 3, 5, ... (row 0 is top padding).
	clickX := 0
	clickY := 3
	_, cmd := a.Update(tea.MouseClickMsg{X: clickX, Y: clickY, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("expected a tea.Cmd for the workspace-rail click; got nil")
	}
	_ = drainBatch(cmd)
	if lastSwitch != "T2" {
		t.Errorf("workspaceSwitcher called with %q, want %q", lastSwitch, "T2")
	}

	// Clicking the currently-selected workspace (row 1, T1) should NOT
	// trigger a switch (the existing 1-9 keybind path has the same
	// guard; the click handler mirrors it).
	lastSwitch = ""
	a.workspaceRail.SelectByID("T1")
	_, cmd = a.Update(tea.MouseClickMsg{X: 0, Y: 1, Button: tea.MouseLeft})
	if cmd != nil {
		drained := drainBatch(cmd)
		// Allow a nil-emitting cmd, but no actual switch should occur.
		_ = drained
	}
	if lastSwitch != "" {
		t.Errorf("clicking the active workspace must not switch; got switch to %q", lastSwitch)
	}
}

// TestApp_ClickOnWorkspaceRailGapDoesNothing asserts that a click on
// the gap row between two tiles is a no-op (no switch).
func TestApp_ClickOnWorkspaceRailGapDoesNothing(t *testing.T) {
	a := NewApp()
	a.width = 120
	a.height = 30
	a.SetWorkspaces([]workspace.WorkspaceItem{
		{ID: "T1", Name: "Acme", Initials: "AC"},
		{ID: "T2", Name: "Beta", Initials: "BE"},
	})
	_ = a.View()

	called := false
	a.setWorkspaceSwitcherForTest(func(teamID string) tea.Msg {
		called = true
		return nil
	})

	// Y=2 is the gap row between tiles 0 (row 1) and 1 (row 3).
	_, cmd := a.Update(tea.MouseClickMsg{X: 0, Y: 2, Button: tea.MouseLeft})
	if cmd != nil {
		_ = drainBatch(cmd)
	}
	if called {
		t.Error("workspaceSwitcher must not be invoked for a click on the gap between tiles")
	}
}
