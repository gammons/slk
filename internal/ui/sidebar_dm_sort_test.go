package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/sidebar"
)

// Clicking the Direct Messages header's sort control flips the section
// between recent and alphabetical order.
func TestSidebarDMsSortControlClick(t *testing.T) {
	a := newTestApp(t, withSize(100, 40))
	a.sidebarVisible = true
	a.focusedPanel = PanelSidebar
	a.SetSidebarStyle(SidebarStyle{SortDMsByRecency: true})
	a.SetChannels([]sidebar.ChannelItem{
		{ID: "D1", Name: "alice", Type: "dm"},
		{ID: "D2", Name: "bob", Type: "dm"},
	})

	row, col := sortControlAt(t, a.View().Content, "⇅")
	a.Update(tea.MouseClickMsg{X: col, Y: row, Button: tea.MouseLeft})

	sortControlAt(t, a.View().Content, "A-Z")
}

func sortControlAt(t *testing.T, frame, glyph string) (row, col int) {
	t.Helper()
	for y, line := range strings.Split(frame, "\n") {
		if !strings.Contains(line, glyph) {
			continue
		}
		if !strings.Contains(ansi.Strip(line), "Direct Messages") {
			continue
		}
		i := strings.Index(line, glyph)
		return y, ansi.StringWidth(line[:i])
	}
	t.Fatalf("no %q control on the Direct Messages header", glyph)
	return -1, -1
}
