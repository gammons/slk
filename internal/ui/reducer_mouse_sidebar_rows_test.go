// internal/ui/reducer_mouse_sidebar_rows_test.go
package ui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/sidebar"
)

// TestSidebarClickAgreesWithEnter is the invariant the sidebar's two
// activation paths must satisfy: every row Enter acts on must respond
// to a click on that same row, and do the same thing.
//
// They had drifted. handleEnter dispatches on the Threads row, on a
// section header (toggling its collapse state), and on a channel,
// while reduceMouseClick handled only channels and Threads -- so
// clicking a section header moved the sidebar selection, drew the
// highlight, and then returned nil.
//
// The sweep is the point. Asserting one row kind at a time would not
// have caught this, because the row that breaks is always the one
// nobody thought to write a case for; this walks every row the sidebar
// renders and compares the two paths on each.
func TestSidebarClickAgreesWithEnter(t *testing.T) {
	newApp := func() *App {
		a := NewApp()
		a.width = 160
		a.height = 40
		a.activeTeamID = "T1"
		a.sidebarVisible = true
		a.focusedPanel = PanelSidebar
		a.sidebar.SetItems([]sidebar.ChannelItem{
			{ID: "C1", Name: "general", Type: "channel"},
			{ID: "C2", Name: "random", Type: "channel"},
		})
		return a
	}

	const maxRows = 12
	activated := 0

	for y := 0; y < maxRows; y++ {
		// Two independent apps in identical state: one driven by
		// Enter, one by a click on the same row. Sharing one app would
		// let the first activation change what the second sees.
		byEnter, byClick := newApp(), newApp()
		_, _ = byEnter.View(), byClick.View()

		// Put the Enter-driven app's selection on the row under test
		// the same way a click would. ClickAt moves the selection for
		// every row kind; ok=false merely means "not a channel", which
		// is exactly what the other branches dispatch on.
		byEnter.sidebar.ClickAt(y)
		enterMsgs := drainCmd(byEnter.handleEnter())

		// X must land inside the sidebar band, past the workspace rail
		// -- reduceMouseClick routes on x before it looks at y. Y is
		// offset by one for the top border.
		clickMsgs := drainCmd(reduceMouseClick(byClick, tea.MouseClickMsg{
			Button: tea.MouseLeft, X: byClick.layout.RailWidth() + 1, Y: y + 1,
		}))

		if fmt.Sprintf("%T", firstMsg(enterMsgs)) != fmt.Sprintf("%T", firstMsg(clickMsgs)) {
			t.Errorf("row y=%d: Enter produced %T but click produced %T",
				y, firstMsg(enterMsgs), firstMsg(clickMsgs))
			continue
		}
		if firstMsg(enterMsgs) != nil {
			activated++
		}
		// Rows that act without emitting a message -- a section header
		// toggling its own collapse state -- must still leave the two
		// sidebars rendering identically.
		if got, want := byClick.sidebar.View(30, 30), byEnter.sidebar.View(30, 30); got != want {
			t.Errorf("row y=%d: click and Enter left different sidebar state", y)
		}
	}

	if activated == 0 {
		t.Fatal("swept every row and none activated on either path; the fixture renders no actionable rows")
	}
}

func firstMsg(msgs []tea.Msg) tea.Msg {
	if len(msgs) == 0 {
		return nil
	}
	return msgs[0]
}
