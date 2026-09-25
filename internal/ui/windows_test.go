package ui

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ui/wintree"
)

func newWideTestApp(t *testing.T) *App {
	t.Helper()
	return newTestApp(t, withSize(200, 50))
}

func TestSplitWindow_CreatesAndFocusesNewWindow(t *testing.T) {
	a := newWideTestApp(t)
	if cmd := a.splitWindow(wintree.SplitSideBySide); cmd != nil {
		t.Fatal("successful split should not toast")
	}
	if a.wins.Len() != 2 {
		t.Fatalf("Len = %d, want 2", a.wins.Len())
	}
	if got := a.wins.Leaves(); a.focusedWin != got[1] {
		t.Fatalf("focusedWin = %v, want new window %v", a.focusedWin, got[1])
	}
	if a.focusedPanel != PanelMessages {
		t.Fatalf("focusedPanel = %v, want PanelMessages", a.focusedPanel)
	}
}

func TestSplitWindow_NoRoomToasts(t *testing.T) {
	a := NewApp()
	a.width = 60 // messages region too narrow for two columns
	a.height = 50
	cmd := a.splitWindow(wintree.SplitSideBySide)
	if cmd == nil {
		t.Fatal("expected toast-clear cmd")
	}
	if a.wins.Len() != 1 {
		t.Fatalf("Len = %d, want 1 (split refused)", a.wins.Len())
	}
	if out := a.statusbar.View(120); !strings.Contains(out, "Not enough room") {
		t.Fatalf("expected 'Not enough room' toast:\n%s", out)
	}
}

func TestCloseWindow_LastWindowToasts(t *testing.T) {
	a := newWideTestApp(t)
	cmd := a.closeWindow()
	if cmd == nil {
		t.Fatal("expected toast-clear cmd")
	}
	if out := a.statusbar.View(120); !strings.Contains(out, "Cannot close last window") {
		t.Fatalf("expected 'Cannot close last window' toast:\n%s", out)
	}
}

func TestCloseWindow_FocusFallsToNeighbor(t *testing.T) {
	a := newWideTestApp(t)
	first := a.focusedWin
	_ = a.splitWindow(wintree.SplitSideBySide)
	_ = a.closeWindow()
	if a.wins.Len() != 1 {
		t.Fatalf("Len = %d, want 1", a.wins.Len())
	}
	if a.focusedWin != first {
		t.Fatalf("focusedWin = %v, want %v", a.focusedWin, first)
	}
}

func TestChannelSelected_UpdatesFocusedWindowChannel(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C9", Name: "incidents", Type: "channel"})
	ch, ok := a.wins.Channel(a.focusedWin)
	if !ok || ch.ID != "C9" || ch.Name != "incidents" {
		t.Fatalf("focused window channel = %+v, want C9/incidents", ch)
	}
}

func TestWorkspaceSwitch_ResetsWindowTree(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	_ = a.splitWindow(wintree.SplitSideBySide)
	if a.wins.Len() != 2 {
		t.Fatalf("Len = %d, want 2", a.wins.Len())
	}
	// Simulate a workspace switch with the minimal msg the reducer
	// accepts (nil Channels takes the empty-workspace branch; all
	// other nil slices/maps are tolerated — see app_test.go's
	// TestApp_WorkspaceSwitchResetsView).
	_, _ = a.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "Other", Channels: nil})
	if a.wins.Len() != 1 {
		t.Fatalf("Len = %d, want 1 (tree must reset on workspace switch)", a.wins.Len())
	}
	if ch, _ := a.wins.Channel(a.focusedWin); ch.ID != "" {
		t.Fatalf("root window channel = %+v, want empty after reset", ch)
	}
}

func TestCommands_SpVspQOnly(t *testing.T) {
	a := newWideTestApp(t)
	_ = executeCommand(a, "vsp")
	if a.wins.Len() != 2 {
		t.Fatalf("after :vsp Len = %d, want 2", a.wins.Len())
	}
	_ = executeCommand(a, "sp")
	if a.wins.Len() != 3 {
		t.Fatalf("after :sp Len = %d, want 3", a.wins.Len())
	}
	_ = executeCommand(a, "q")
	if a.wins.Len() != 2 {
		t.Fatalf("after :q Len = %d, want 2", a.wins.Len())
	}
	_ = executeCommand(a, "only")
	if a.wins.Len() != 1 {
		t.Fatalf("after :only Len = %d, want 1", a.wins.Len())
	}
	_ = executeCommand(a, "vsp")
	_ = executeCommand(a, "on") // alias
	if a.wins.Len() != 1 {
		t.Fatalf("after :on Len = %d, want 1", a.wins.Len())
	}
}
