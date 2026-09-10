// internal/ui/reducer_modal_click_test.go
package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/help"
	"github.com/gammons/slk/internal/ui/searchresults"
)

func openChannelFinder(t *testing.T) *App {
	t.Helper()
	return newTestApp(t,
		withSize(80, 24),
		// Descending LastVisited keeps these in declared order under the
		// empty-query sort, so row N maps to items[N].
		withChannelFinderOpen(
			channelfinder.Item{ID: "C1", Name: "alpha", Type: "channel", Joined: true, LastVisited: 300},
			channelfinder.Item{ID: "C2", Name: "bravo", Type: "channel", Joined: true, LastVisited: 200},
			channelfinder.Item{ID: "C3", Name: "charlie", Type: "channel", Joined: true, LastVisited: 100},
		),
		withMode(ModeChannelFinder),
	)
}

// boxOrigin returns the top-left screen coords of the active channel
// finder box, mirroring overlay.DimmedOverlay's centering.
func channelFinderOrigin(app *App) (int, int) {
	w, h := app.channelFinder.BoxSize(app.width, app.height)
	x := (app.width - w) / 2
	y := (app.height - h) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

// TestModalClick_OnRowActivates verifies that clicking a list row selects
// that item and activates it (Enter), closing the modal.
func TestModalClick_OnRowActivates(t *testing.T) {
	app := openChannelFinder(t)
	startX, startY := channelFinderOrigin(app)

	// Row offset 2 from the first list row. NewApp pins a synthetic
	// "Threads" row at the top, so offset 0=Threads, 1=C1, 2=C2.
	clickY := startY + 5 + 2
	clickX := startX + 3

	cmd := reduceMouseClick(app, tea.MouseClickMsg{Button: tea.MouseLeft, X: clickX, Y: clickY})
	if cmd == nil {
		t.Fatal("clicking a row should return an activation cmd")
	}
	msg := cmd()
	sel, ok := msg.(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("expected ChannelSelectedMsg, got %T", msg)
	}
	if sel.ID != "C2" {
		t.Errorf("clicked second row, selected ID = %q, want C2", sel.ID)
	}
	if app.channelFinder.IsVisible() {
		t.Error("finder should close after activating a row")
	}
	if app.mode != ModeNormal {
		t.Errorf("mode = %v after activation, want ModeNormal", app.mode)
	}
}

// TestModalClick_OutsideDismisses verifies that clicking outside the modal
// box dismisses it (Esc path) without selecting anything.
func TestModalClick_OutsideDismisses(t *testing.T) {
	app := openChannelFinder(t)

	// Top-left corner is outside the centered box.
	reduceMouseClick(app, tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: 0})

	if app.channelFinder.IsVisible() {
		t.Error("finder should be dismissed by an outside click")
	}
	if app.mode != ModeNormal {
		t.Errorf("mode = %v after outside click, want ModeNormal", app.mode)
	}
}

// TestModalClick_InsideNonRowIsNoop verifies that clicking inside the box
// but not on a list row (e.g. the title area) neither activates nor
// dismisses the modal.
func TestModalClick_InsideNonRowIsNoop(t *testing.T) {
	app := openChannelFinder(t)
	startX, startY := channelFinderOrigin(app)

	// Title row sits at box-local y=2, above the list (y>=5).
	cmd := reduceMouseClick(app, tea.MouseClickMsg{Button: tea.MouseLeft, X: startX + 3, Y: startY + 2})
	if cmd != nil {
		t.Errorf("clicking the title area should not return a cmd, got %v", cmd)
	}
	if !app.channelFinder.IsVisible() {
		t.Error("finder should stay open when clicking inside but not on a row")
	}
	if app.mode != ModeChannelFinder {
		t.Errorf("mode = %v, want ModeChannelFinder (unchanged)", app.mode)
	}
}

// TestModalClick_WorkspaceSearchRowActivates verifies the ctrl+f search
// modal is registered as a click target: clicking a result row selects
// it and activates (Enter -> navigate), rather than falling through to
// the dismiss-on-any-click default.
func TestModalClick_WorkspaceSearchRowActivates(t *testing.T) {
	app := NewApp()
	app.width = 80
	app.height = 24
	app.activeChannelID = "C1"
	// Lookup hit = member channel; a miss would toast instead of navigate.
	app.setChannelLookupFuncForTest(func(id ids.ChannelID) (string, string, bool) {
		return "dev", "channel", id == "C3"
	})
	app.searchResults.Open()
	app.SetMode(ModeWorkspaceSearch)
	app.searchResults.HandleKey("q")
	app.searchResults.HandleKey("enter")
	app.searchResults.SetResults([]searchresults.Item{
		{ChannelID: "C2", ChannelName: "ops", TS: "2.0"},
		{ChannelID: "C3", ChannelName: "dev", TS: "3.0"},
	}, 2)

	w, h := app.searchResults.BoxSize(app.width, app.height)
	startX := (app.width - w) / 2
	startY := (app.height - h) / 2

	// Second result row: rows are four lines tall (metadata, two
	// snippet lines, blank separator), so its first line is at
	// box-local y = listTopOffset(5) + rowLines(4).
	cmd := reduceMouseClick(app, tea.MouseClickMsg{Button: tea.MouseLeft, X: startX + 3, Y: startY + 5 + 4})
	if cmd == nil {
		t.Fatal("clicking a result row should return an activation cmd")
	}
	var selected *ChannelSelectedMsg
	for _, m := range drainCmd(cmd) {
		if cs, ok := m.(ChannelSelectedMsg); ok {
			selected = &cs
		}
	}
	if selected == nil || selected.ID != "C3" {
		t.Fatalf("clicked second row, got %+v", selected)
	}
	if app.searchResults.IsVisible() || app.mode != ModeNormal {
		t.Errorf("modal should close after activation: visible=%v mode=%v",
			app.searchResults.IsVisible(), app.mode)
	}
}

// TestModalClick_HelpOutsideDismisses guards the no-activation modal path:
// help has no Enter action, but an outside click must still dismiss it.
func TestModalClick_HelpOutsideDismisses(t *testing.T) {
	app := NewApp()
	app.width = 80
	app.height = 24
	app.help.SetEntries([]help.Entry{{Key: "j", Desc: "down"}, {Key: "k", Desc: "up"}})
	app.help.Open()
	app.SetMode(ModeHelp)

	reduceMouseClick(app, tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: 0})

	if app.help.IsVisible() {
		t.Error("help should be dismissed by an outside click")
	}
	if app.mode != ModeNormal {
		t.Errorf("mode = %v after outside click, want ModeNormal", app.mode)
	}
}
