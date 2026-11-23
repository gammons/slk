package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// CellMetricsChangedMsg drops every image-bearing render cache, forces
// the sixel painter's next plan, and re-delivers the current window size
// so bubbletea redraws the whole screen -- an erase can only cover
// cells, and pixels painted at the old cell size reached past their
// placement's footprint, so the text under the erased excess has to be
// rewritten too.
func TestCellMetricsChangedMsg_ForcesRepaintAndResendsSize(t *testing.T) {
	a := newTestApp(t, withSize(120, 40), withRender())
	// A real resize has happened before any metrics change can: it is
	// what sets a.width/a.height.
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a.forceSixelRepaint = false

	cmd, handled := reduceIO(a, CellMetricsChangedMsg{CellW: 9, CellH: 20})
	if !handled {
		t.Fatal("reduceIO did not handle CellMetricsChangedMsg")
	}
	if !a.forceSixelRepaint {
		t.Error("forceSixelRepaint not set; the painter would keep the pixels painted at the old size")
	}

	sizes := deliveredMsgs[tea.WindowSizeMsg](t, cmd)
	if len(sizes) != 1 {
		t.Fatalf("delivered %d WindowSizeMsgs, want 1", len(sizes))
	}
	if sizes[0].Width != 120 || sizes[0].Height != 40 {
		t.Errorf("redelivered %dx%d, want the App's current 120x40", sizes[0].Width, sizes[0].Height)
	}
}

// The redelivered size is the App's current one, so a duplicate-size
// message reaches bubbletea's renderer (which full-redraws) without
// re-arming the App's own sixel force flag through the resize arm.
func TestCellMetricsChangedMsg_DuplicateSizeDoesNotReArmForce(t *testing.T) {
	a := newTestApp(t, withSize(120, 40), withRender())
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	cmd, _ := reduceIO(a, CellMetricsChangedMsg{CellW: 9, CellH: 20})
	if !a.forceSixelRepaint {
		t.Fatal("expected the metrics change to arm the sixel force")
	}
	// The next published frame consumes the flag, as finalizeSixelView
	// does.
	a.forceSixelRepaint = false
	msgs := deliveredMsgs[tea.WindowSizeMsg](t, cmd)
	for _, m := range msgs {
		a.Update(m)
	}
	if a.forceSixelRepaint {
		t.Error("the duplicate WindowSizeMsg re-armed forceSixelRepaint")
	}
}
