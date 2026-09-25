// internal/ui/panellayout.go
//
// Per-frame layout geometry + mouse hit-testing.
//
// Phase 2j of the SOLID refactor of internal/ui/app.go: extracts the
// seven layout fields (layoutRailWidth, layoutSidebarEnd, layoutMsgEnd,
// layoutThreadEnd, layoutMsgHeight, layoutSidebarHeight,
// layoutThreadHeight) and the panelAt hit-test method out of App.
//
// Layout is a two-step dance with View():
//
//  1. View calls Compute(...) at frame start. Compute resolves the
//     per-pane widths/borders for the current terminal size and
//     visibility flags, stores the resulting horizontal bands for
//     subsequent PanelAt calls, and returns a panelLayoutFrame the
//     caller uses to drive rendering.
//
//  2. As each pane renders, it calls SetSidebarHeight / SetMsgHeight /
//     SetThreadHeight with the chrome-stripped content height. These
//     feed PageHeight() which pageSize / halfPageSize consult.
//
// Stacking: when a thread is open and there is room for the messages
// pane (≥40 cols) AND an 80-col thread pane side by side, both are
// drawn. Otherwise the two stack: only one is drawn, across the whole
// content area, and the caller's threadFront decides which. Compute is
// pure geometry; App.threadInFront owns the focus rule.
package ui

// panelLayout owns the per-frame layout state.
type panelLayout struct {
	// Horizontal bands set by Compute, used for mouse hit-testing.
	// Each "End" is the exclusive upper bound of the band starting
	// where the previous one left off.
	railWidth  int
	sidebarEnd int // railWidth + sidebarWidth + sidebarBorder
	msgEnd     int // sidebarEnd + msgWidth + msgBorder
	threadEnd  int // msgEnd + threadWidth + threadBorder (or msgEnd if hidden)

	// Per-pane content heights set during pane rendering, used by
	// pageSize / halfPageSize. Subtract any chrome (borders, headers,
	// compose box) — these are the heights of the SCROLLABLE region
	// only.
	sidebarHeight int
	msgHeight     int
	threadHeight  int
}

func newPanelLayout() *panelLayout { return &panelLayout{} }

// panelLayoutFrame is the per-frame output of Compute. The caller
// (App.View) uses these widths/borders to drive panel rendering.
type panelLayoutFrame struct {
	RailWidth     int
	SidebarWidth  int
	SidebarBorder int
	MsgWidth      int
	MsgBorder     int
	ThreadWidth   int
	ThreadBorder  int
	ContentHeight int // height minus the 1-row status bar
}

// Compute resolves the per-frame layout. Stores the resulting
// horizontal bands so subsequent PanelAt calls reflect the new layout.
//
// Width algorithm:
//   - rail consumes railWidth (caller supplies; comes from
//     workspaceRail.Width()).
//   - sidebar, when visible, consumes sidebarWidth + 2 cols of border.
//   - thread, when visible and there is room for both panes, consumes
//     max(35% of (width - rail - sidebar), 80) plus 2 cols of border,
//     capped so messages keeps 40. Without room, the panes stack and
//     only the front one (threadFront) is drawn, across the whole area.
//   - whichever pane is drawn — messages side by side, or whichever
//     pane is in front when stacked — consumes whatever's left, with
//     a floor of 10.
//
// Border bits are 2 cols on each non-rail pane (1 col left + 1 col
// right rounded border).
func (l *panelLayout) Compute(width, height, railWidth, sidebarWidth int, sidebarVisible, threadVisible, threadFront bool) panelLayoutFrame {
	const (
		statusHeight = 1
		paneBorder   = 2 // left + right border cols
		minMsgWidth  = 40
		minThreadW   = 80
		floorPaneW   = 10
	)
	contentHeight := height - statusHeight

	sbWidth := 0
	sbBorder := 0
	if sidebarVisible {
		sbWidth = sidebarWidth
		sbBorder = paneBorder
	}
	msgAreaWidth := width - railWidth - sbWidth - sbBorder

	var msgWidth, msgBorder, threadWidth, threadBorder int
	switch {
	case !threadVisible:
		msgWidth, msgBorder = msgAreaWidth-paneBorder, paneBorder
	case msgAreaWidth-2*paneBorder >= minMsgWidth+minThreadW:
		threadWidth = max(msgAreaWidth*35/100, minThreadW)
		threadWidth = min(threadWidth, msgAreaWidth-2*paneBorder-minMsgWidth)
		threadBorder, msgBorder = paneBorder, paneBorder
		msgWidth = msgAreaWidth - msgBorder - threadWidth - threadBorder
	case threadFront:
		threadWidth, threadBorder = msgAreaWidth-paneBorder, paneBorder
	default:
		msgWidth, msgBorder = msgAreaWidth-paneBorder, paneBorder
	}
	if msgBorder > 0 {
		msgWidth = max(msgWidth, floorPaneW)
	}
	if threadBorder > 0 {
		threadWidth = max(threadWidth, floorPaneW)
	}

	// Bands for PanelAt and the mouse routers. A pane that is not drawn
	// has a zero-width band, so its range can never match.
	l.railWidth = railWidth
	l.sidebarEnd = railWidth + sbWidth + sbBorder
	l.msgEnd = l.sidebarEnd + msgWidth + msgBorder
	l.threadEnd = l.msgEnd + threadWidth + threadBorder

	return panelLayoutFrame{
		RailWidth:     railWidth,
		SidebarWidth:  sbWidth,
		SidebarBorder: sbBorder,
		MsgWidth:      msgWidth,
		MsgBorder:     msgBorder,
		ThreadWidth:   threadWidth,
		ThreadBorder:  threadBorder,
		ContentHeight: contentHeight,
	}
}

// PanelAt classifies the (x, y) coordinate into the panel under the
// cursor and returns pane-local content coordinates (after subtracting
// layout offsets and the 1-row top border). ok=false means the cursor
// is outside the messages/thread panes (status bar, rail, sidebar, or
// past the rightmost band) — drag selection is not supported there.
//
// sidebarVisible / threadVisible are passed defensively: with bands
// set by Compute they're redundant (hidden panes have collapsed bands),
// but accepting them preserves the original behavior when callers feed
// in stale bands or directly seed layout state (Phase 0 tests do this).
func (l *panelLayout) PanelAt(x, y, height int, sidebarVisible, threadVisible bool) (panel Panel, paneX, paneY int, ok bool) {
	if y >= height-1 {
		return PanelWorkspace, 0, 0, false // status bar
	}
	switch {
	case x < l.railWidth:
		return PanelWorkspace, 0, 0, false
	case sidebarVisible && x < l.sidebarEnd:
		return PanelSidebar, 0, 0, false
	case x < l.msgEnd:
		// Messages pane content: subtract the message-pane left edge
		// (after sidebar) and account for the panel's top border (1 row).
		return PanelMessages, x - l.sidebarEnd - 1, y - 1, true
	case threadVisible && x < l.threadEnd:
		return PanelThread, x - l.msgEnd - 1, y - 1, true
	}
	return PanelWorkspace, 0, 0, false
}

// PageHeight returns the cached content height of the given panel
// (populated during render via SetSidebarHeight / SetMsgHeight /
// SetThreadHeight). Returns 0 for PanelWorkspace; callers should
// floor that to a sane minimum.
func (l *panelLayout) PageHeight(panel Panel) int {
	switch panel {
	case PanelSidebar:
		return l.sidebarHeight
	case PanelMessages:
		return l.msgHeight
	case PanelThread:
		return l.threadHeight
	}
	return 0
}

// SetSidebarHeight records the sidebar pane's chrome-stripped content
// height for subsequent PageHeight queries.
func (l *panelLayout) SetSidebarHeight(h int) { l.sidebarHeight = h }

// SetMsgHeight records the messages pane's chrome-stripped content
// height.
func (l *panelLayout) SetMsgHeight(h int) { l.msgHeight = h }

// SetThreadHeight records the thread pane's chrome-stripped content
// height.
func (l *panelLayout) SetThreadHeight(h int) { l.threadHeight = h }

// RailWidth / SidebarEnd / MsgEnd / ThreadEnd are read by mouse
// click/wheel handlers that route to the correct panel via a parallel
// if/else chain (currently not unified with PanelAt; that's a Phase 4
// reducer concern).
func (l *panelLayout) RailWidth() int  { return l.railWidth }
func (l *panelLayout) SidebarEnd() int { return l.sidebarEnd }
func (l *panelLayout) MsgEnd() int     { return l.msgEnd }
func (l *panelLayout) ThreadEnd() int  { return l.threadEnd }
