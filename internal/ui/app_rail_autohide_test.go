package ui

import (
	"testing"

	"github.com/gammons/slk/internal/ui/workspace"
)

// TestRail_HiddenWithOneWorkspace covers #216: with a single workspace
// the rail takes no columns, so the sidebar starts at the left edge.
// Two or more workspaces keep it.
func TestRail_HiddenWithOneWorkspace(t *testing.T) {
	one := []workspace.WorkspaceItem{{ID: "T1", Name: "Acme", Initials: "AC"}}
	two := append(one, workspace.WorkspaceItem{ID: "T2", Name: "Beta", Initials: "BE"})

	a := newTestApp(t, withWorkspaces(one...), withRender())
	if got := a.layout.RailWidth(); got != 0 {
		t.Fatalf("one workspace: rail width = %d, want 0", got)
	}
	if p, _, _, _ := a.layout.PanelAt(0, 1, a.height, true, false); p != PanelSidebar {
		t.Errorf("one workspace: column 0 hit-tests to panel %v, want the sidebar", p)
	}

	a.SetWorkspaces(two)
	_ = a.View()
	if got, want := a.layout.RailWidth(), a.workspaceRail.Width(); got != want {
		t.Errorf("two workspaces: rail width = %d, want %d", got, want)
	}
}
