// internal/ui/app_panelat_test.go
//
// Phase 0 characterization tests for App.panelAt. Pins the
// (x, y, layoutBands, sidebar/thread visibility) → (panel, paneX,
// paneY, ok) mapping so the upcoming panelLayout extraction can't
// silently shift hit-test boundaries.
//
// Geometry recap (matches panelLayout.PanelAt in panellayout.go):
// rail | sidebar | messages | thread, with the 1-row status bar at
// y == height-1.
//
//	x < layout.railWidth                                   → PanelWorkspace, ok=false
//	sidebarVisible && x < layout.sidebarEnd                → PanelSidebar, ok=false
//	x < layout.msgEnd                                      → PanelMessages
//	                                                         paneX = x - layout.sidebarEnd - 1
//	                                                         paneY = y - 1, ok=true
//	threadVisible && x < layout.threadEnd                  → PanelThread
//	                                                         paneX = x - layout.msgEnd - 1
//	                                                         paneY = y - 1, ok=true
//	y >= height-1                                          → PanelWorkspace, ok=false
package ui

import "testing"

// newPanelAtApp builds an App pre-loaded with a fixed layout matching
// what View() would compute for: width=100, rail=3, sidebar=20, msg=50,
// thread=25, height=24. Borders are 2 cols on each non-rail pane.
//
//	rail   sidebar       messages              thread
//	[0..3) [3..3+20+2=25) [25..25+50+2=77)     [77..77+25+2=104)  width=100 (clipped)
func newPanelAtApp() *App {
	a := NewApp()
	a.width = 100
	a.height = 24
	a.sidebarVisible = true
	a.threadVisible = true
	a.layout.railWidth = 3
	a.layout.sidebarEnd = 25
	a.layout.msgEnd = 77
	a.layout.threadEnd = 104
	return a
}

func TestPanelAtStatusBarRowIsNotInteractive(t *testing.T) {
	a := newPanelAtApp()
	// y == height-1 is the status row.
	panel, _, _, ok := a.panelAt(40, a.height-1)
	if ok {
		t.Errorf("status row click must return ok=false; got panel=%v", panel)
	}
	if panel != PanelWorkspace {
		t.Errorf("status row panel: want PanelWorkspace sentinel, got %v", panel)
	}
}

func TestPanelAtWorkspaceRailReturnsNotOk(t *testing.T) {
	a := newPanelAtApp()
	for _, x := range []int{0, 1, 2} {
		panel, _, _, ok := a.panelAt(x, 5)
		if ok {
			t.Errorf("x=%d (rail): want ok=false, got ok=true panel=%v", x, panel)
		}
		if panel != PanelWorkspace {
			t.Errorf("x=%d: want PanelWorkspace, got %v", x, panel)
		}
	}
}

func TestPanelAtSidebarReturnsNotOkWhenVisible(t *testing.T) {
	a := newPanelAtApp()
	// Sidebar band is [layoutRailWidth=3, layoutSidebarEnd=25)
	for _, x := range []int{3, 12, 24} {
		panel, _, _, ok := a.panelAt(x, 5)
		if ok {
			t.Errorf("x=%d (sidebar): want ok=false, got ok=true", x)
		}
		if panel != PanelSidebar {
			t.Errorf("x=%d: want PanelSidebar, got %v", x, panel)
		}
	}
}

func TestPanelAtSidebarHiddenFallsThroughToMessages(t *testing.T) {
	a := newPanelAtApp()
	a.sidebarVisible = false
	// When sidebar is hidden, the sidebar-band guard short-circuits and
	// x lands in the messages band (still bounded by layoutMsgEnd=77).
	// Note: in real layouts the caller adjusts layoutSidebarEnd; this
	// test pins the panelAt branch behavior in isolation.
	panel, _, _, ok := a.panelAt(10, 5)
	if !ok {
		t.Fatalf("x=10 with sidebar hidden: want ok=true, got false")
	}
	if panel != PanelMessages {
		t.Errorf("x=10 with sidebar hidden: want PanelMessages, got %v", panel)
	}
}

func TestPanelAtMessagesPaneStripsBorderOffsets(t *testing.T) {
	a := newPanelAtApp()
	// First column at the messages band boundary: x=layoutSidebarEnd=25.
	// The reported paneX is `x - sidebarEnd - 1` (sidebarEnd offset +
	// left-border column). For x=sidebarEnd=25 this resolves to -1,
	// indicating the click landed on the border column rather than on
	// content. paneY similarly subtracts the top-border row.
	const wantPx = -1 // 25 (input x) - 25 (sidebarEnd) - 1 (left border)
	const wantPy = 6  // 7  (input y) - 1  (top border)
	panel, px, py, ok := a.panelAt(25, 7)
	if !ok || panel != PanelMessages {
		t.Fatalf("panel/ok: want (PanelMessages,true), got (%v,%v)", panel, ok)
	}
	if px != wantPx {
		t.Errorf("paneX at x=25: want %d, got %d", wantPx, px)
	}
	if py != wantPy {
		t.Errorf("paneY at y=7: want %d, got %d", wantPy, py)
	}
}

func TestPanelAtMessagesPaneInteriorCoordinates(t *testing.T) {
	a := newPanelAtApp()
	// Pick a clearly interior point.
	panel, px, py, ok := a.panelAt(40, 10)
	if !ok || panel != PanelMessages {
		t.Fatalf("want messages/ok, got panel=%v ok=%v", panel, ok)
	}
	if px != 40-25-1 {
		t.Errorf("paneX: want %d, got %d", 40-25-1, px)
	}
	if py != 10-1 {
		t.Errorf("paneY: want %d, got %d", 10-1, py)
	}
}

func TestPanelAtThreadPane(t *testing.T) {
	a := newPanelAtApp()
	// Thread band is [layoutMsgEnd=77, layoutThreadEnd=104).
	panel, px, py, ok := a.panelAt(80, 5)
	if !ok || panel != PanelThread {
		t.Fatalf("want thread/ok, got panel=%v ok=%v", panel, ok)
	}
	if px != 80-77-1 {
		t.Errorf("paneX: want %d, got %d", 80-77-1, px)
	}
	if py != 5-1 {
		t.Errorf("paneY: want %d, got %d", 5-1, py)
	}
}

func TestPanelAtThreadHiddenReturnsNotOk(t *testing.T) {
	a := newPanelAtApp()
	a.threadVisible = false
	// x=80 would be in the thread band, but threadVisible=false
	// falls out of all cases → PanelWorkspace sentinel, ok=false.
	panel, _, _, ok := a.panelAt(80, 5)
	if ok {
		t.Errorf("thread hidden: want ok=false, got ok=true panel=%v", panel)
	}
	if panel != PanelWorkspace {
		t.Errorf("thread hidden, x past msgEnd: want PanelWorkspace, got %v", panel)
	}
}

func TestPanelAtBeyondThreadEndReturnsNotOk(t *testing.T) {
	a := newPanelAtApp()
	// x past layoutThreadEnd: outside any band.
	panel, _, _, ok := a.panelAt(150, 5)
	if ok {
		t.Errorf("x past thread end: want ok=false, got ok=true panel=%v", panel)
	}
	if panel != PanelWorkspace {
		t.Errorf("x past thread end: want PanelWorkspace, got %v", panel)
	}
}

func TestPanelAtBoundaryBetweenSidebarAndMessages(t *testing.T) {
	a := newPanelAtApp()
	// layoutSidebarEnd is the FIRST x belonging to the messages band.
	// x=layoutSidebarEnd-1 is the LAST sidebar column.
	if _, _, _, ok := a.panelAt(a.layout.sidebarEnd-1, 5); ok {
		t.Error("x=sidebarEnd-1 should still be the sidebar (ok=false)")
	}
	panel, _, _, ok := a.panelAt(a.layout.sidebarEnd, 5)
	if !ok || panel != PanelMessages {
		t.Errorf("x=sidebarEnd: want messages/ok, got panel=%v ok=%v", panel, ok)
	}
}

func TestPanelAtBoundaryBetweenMessagesAndThread(t *testing.T) {
	a := newPanelAtApp()
	// Last messages column.
	panel, _, _, ok := a.panelAt(a.layout.msgEnd-1, 5)
	if !ok || panel != PanelMessages {
		t.Errorf("x=msgEnd-1: want messages/ok, got panel=%v ok=%v", panel, ok)
	}
	// First thread column.
	panel, _, _, ok = a.panelAt(a.layout.msgEnd, 5)
	if !ok || panel != PanelThread {
		t.Errorf("x=msgEnd: want thread/ok, got panel=%v ok=%v", panel, ok)
	}
}
