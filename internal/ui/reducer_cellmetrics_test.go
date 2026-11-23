package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// CellMetricsChangedMsg drops every image-bearing render cache and arms
// the sixel painter's next plan. It deliberately does NOT touch the
// window size: the full text redraw that restores the cells the erase
// blanks is cmd/slk's tea.ClearScreen, sent right after this message.
// A re-delivered WindowSizeMsg would race the real resize the cell
// change usually arrives with, and the resize arm would accept the
// stale dimensions as the terminal's.
func TestCellMetricsChangedMsg_InvalidatesWithoutTouchingSize(t *testing.T) {
	a := newTestApp(t, withSize(120, 40), withRender())
	// A real resize has happened before any metrics change can: it is
	// what sets a.width/a.height.
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a.forceSixelRepaint = false

	cmd, handled := reduceIO(a, CellMetricsChangedMsg{CellW: 9, CellH: 20})
	if !handled {
		t.Fatal("reduceIO did not handle CellMetricsChangedMsg")
	}
	if cmd != nil {
		t.Errorf("reduceIO returned %T, want no command (the redraw is cmd/slk's tea.ClearScreen)", cmd)
	}
	if !a.forceSixelRepaint {
		t.Error("forceSixelRepaint not set; the painter would keep the pixels painted at the old size")
	}
	if a.width != 120 || a.height != 40 {
		t.Errorf("the App's size moved to %dx%d, want it untouched at 120x40", a.width, a.height)
	}
}
