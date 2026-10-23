package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

// TestMouseClick_OnComposeEntersInsert pins the click-to-compose
// feature: a click inside the channel pane's compose region enters
// insert mode with the channel compose focused, exactly as `i` does.
func TestMouseClick_OnComposeEntersInsert(t *testing.T) {
	app := newTestApp(t, withSize(120, 60), withActiveChannel("C123"), withMessages(messages.MessageItem{
		TS: "1.0", UserName: "alice", Text: "hello",
	}))
	_ = app.View()
	_ = app.View()

	top := app.layout.msgComposeTop
	if top <= 0 {
		t.Fatal("msgComposeTop not recorded after View(); the channel pane did not draw a compose region")
	}
	// A row inside the compose box, one past its spacer line.
	chrome := app.messagepane.ChromeHeight()
	x := app.layout.sidebarEnd + 5
	y := 1 + chrome + top + 1
	_, cmd := app.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	_ = cmd

	if app.mode != ModeInsert {
		t.Errorf("mode = %v; want ModeInsert after clicking the compose region", app.mode)
	}
	if app.focusedPanel != PanelMessages {
		t.Errorf("focusedPanel = %v; want PanelMessages (the channel compose)", app.focusedPanel)
	}
}

// TestMouseClick_AboveComposeDoesNotInsert asserts the region boundary:
// a click on a message row above the compose region does not enter
// insert mode.
func TestMouseClick_AboveComposeDoesNotInsert(t *testing.T) {
	app := newTestApp(t, withSize(120, 60), withActiveChannel("C123"), withMessages(messages.MessageItem{
		TS: "1.0", UserName: "alice", Text: "hello",
	}))
	_ = app.View()
	_ = app.View()

	top := app.layout.msgComposeTop
	if top <= 0 {
		t.Fatal("msgComposeTop not recorded after View()")
	}
	chrome := app.messagepane.ChromeHeight()
	x := app.layout.sidebarEnd + 5
	y := 1 + chrome + 1 // first message row
	_, cmd := app.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	_ = cmd

	if app.mode == ModeInsert {
		t.Error("mode = ModeInsert after clicking a message row above the compose region; want it unchanged")
	}
}

// TestMouseClick_OnFirstComposeRowEntersInsert pins the coordinate
// frame: the recorded top is in PanelAt's paneY frame (top border
// already subtracted), so the FIRST compose row - the spacer above the
// box - passes the router's >= check. An off-by-one here silently
// makes the first row a selection anchor instead.
func TestMouseClick_OnFirstComposeRowEntersInsert(t *testing.T) {
	app := newTestApp(t, withSize(120, 60), withActiveChannel("C123"), withMessages(messages.MessageItem{
		TS: "1.0", UserName: "alice", Text: "hello",
	}))
	_ = app.View()
	_ = app.View()

	top := app.layout.msgComposeTop
	if top <= 0 {
		t.Fatal("msgComposeTop not recorded after View()")
	}
	chrome := app.messagepane.ChromeHeight()
	x := app.layout.sidebarEnd + 5
	y := 1 + chrome + top // the first row of the recorded region
	_, cmd := app.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	_ = cmd

	if app.mode != ModeInsert {
		t.Errorf("mode = %v; want ModeInsert after clicking the first compose row", app.mode)
	}
}

// TestMouseClick_ShortTerminalMessageRowsStayClickable pins the clamp
// order: on a terminal too short for the compose box, the renderer
// still draws the minimum three message rows, and clicking them must
// not start typing - the compose top is recorded from the clamped
// height, not the raw one.
func TestMouseClick_ShortTerminalMessageRowsStayClickable(t *testing.T) {
	// Height small enough that contentHeight - 2 - bottomHeight < 3.
	app := newTestApp(t, withSize(120, 12), withActiveChannel("C123"), withMessages(messages.MessageItem{
		TS: "1.0", UserName: "alice", Text: "hello",
	}))
	_ = app.View()
	_ = app.View()

	chrome := app.messagepane.ChromeHeight()
	x := app.layout.sidebarEnd + 5
	// The first message row, always drawn thanks to the minimum clamp.
	y := 1 + chrome + 1
	_, cmd := app.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	_ = cmd

	if app.mode == ModeInsert {
		t.Error("mode = ModeInsert after clicking a message row on a short terminal; the compose top was recorded before the clamp")
	}
}
