package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/sidebar"
)

func newResizeApp(t *testing.T, enabled bool) *App {
	t.Helper()
	a := newTestApp(t, withSize(160, 40),
		withChannels(sidebar.ChannelItem{ID: "C1", Name: "general", Type: "channel"}), withRender())
	a.SetSidebarStyle(SidebarStyle{MouseResize: enabled})
	return a
}

func TestSidebarResize_DragMovesTheBorder(t *testing.T) {
	a := newResizeApp(t, true)
	before := a.sidebar.Width()
	border := a.layout.SidebarEnd() - 1

	a.Update(tea.MouseClickMsg{X: border, Y: 5, Button: tea.MouseLeft})
	a.Update(tea.MouseMotionMsg{X: border + 10, Y: 5, Button: tea.MouseLeft})
	a.Update(tea.MouseReleaseMsg{X: border + 10, Y: 5, Button: tea.MouseLeft})
	_ = a.View()

	if got := a.sidebar.Width(); got != before+10 {
		t.Fatalf("width after a 10-column drag = %d, want %d", got, before+10)
	}
	if got := a.layout.SidebarEnd() - 1; got != border+10 {
		t.Errorf("border at %d, want it under the pointer at %d", got, border+10)
	}
	if a.sidebarResizeFrom != 0 {
		t.Error("release left the resize gesture active")
	}
}

func TestSidebarResize_OffByDefault(t *testing.T) {
	a := newResizeApp(t, false)
	before := a.sidebar.Width()
	border := a.layout.SidebarEnd() - 1

	a.Update(tea.MouseClickMsg{X: border, Y: 5, Button: tea.MouseLeft})
	a.Update(tea.MouseMotionMsg{X: border + 10, Y: 5, Button: tea.MouseLeft})
	a.Update(tea.MouseReleaseMsg{X: border + 10, Y: 5, Button: tea.MouseLeft})

	if got := a.sidebar.Width(); got != before {
		t.Errorf("width changed to %d with mouse_resize off", got)
	}
}

// TestSidebarResize_LostReleaseEndsTheDrag: a release outside the window
// never arrives; the next motion without the button ends the drag, so a
// later text-selection drag doesn't resize the sidebar.
func TestSidebarResize_LostReleaseEndsTheDrag(t *testing.T) {
	a := newResizeApp(t, true)
	before := a.sidebar.Width()
	border := a.layout.SidebarEnd() - 1

	a.Update(tea.MouseClickMsg{X: border, Y: 5, Button: tea.MouseLeft})
	a.Update(tea.MouseMotionMsg{X: border + 4, Y: 5})
	a.Update(tea.MouseMotionMsg{X: border + 10, Y: 8, Button: tea.MouseLeft})

	if got := a.sidebar.Width(); got != before || a.sidebarResizeFrom != 0 {
		t.Errorf("width %d (want %d), gesture from %d after the release was lost", got, before, a.sidebarResizeFrom)
	}
}

func TestSidebarResize_SavesOnlyAChangedWidth(t *testing.T) {
	a := newResizeApp(t, true)
	var saved []int
	wireWidthSaver(a, func(w int) { saved = append(saved, w) })
	border := a.layout.SidebarEnd() - 1

	a.Update(tea.MouseClickMsg{X: border, Y: 5, Button: tea.MouseLeft})
	a.Update(tea.MouseReleaseMsg{X: border, Y: 5, Button: tea.MouseLeft})
	if len(saved) != 0 {
		t.Fatalf("a plain click on the border saved %v", saved)
	}
	a.Update(tea.MouseClickMsg{X: border, Y: 5, Button: tea.MouseLeft})
	a.Update(tea.MouseMotionMsg{X: border + 3, Y: 5, Button: tea.MouseLeft})
	a.Update(tea.MouseReleaseMsg{X: border + 3, Y: 5, Button: tea.MouseLeft})
	if want := []int{a.sidebar.Width()}; len(saved) != 1 || saved[0] != want[0] {
		t.Errorf("saved %v after a 3-column drag, want %v", saved, want)
	}
}
