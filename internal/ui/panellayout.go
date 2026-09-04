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
// Auto-hide: if there isn't room for both the messages pane (≥40 cols)
// AND the thread pane (≥30 cols), Compute returns ThreadAutoHidden=true.
// The CALLER is responsible for flipping threadVisible=false and
// stealing focus from PanelThread — Compute can't do that without
// reaching back into App.
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

	// ThreadAutoHidden is true when Compute had to hide the thread
	// pane to keep the messages pane at its 40-col minimum. The caller
	// must flip its own threadVisible to false and steal focus from
	// PanelThread if focused there.
	ThreadAutoHidden bool
}

// Compute resolves the per-frame layout. Stores the resulting
// horizontal bands so subsequent PanelAt calls reflect the new layout.
//
// Width algorithm (preserved verbatim from the prior in-View code):
//   - rail consumes railWidth (caller supplies; comes from
//     workspaceRail.Width()).
//   - sidebar, when visible, consumes sidebarWidth + 2 cols of border.
//   - thread, when visible, consumes 35% of (width - rail - sidebar)
//     plus 2 cols of border; minimums are 40 cols messages + 30 cols
//     thread or thread auto-hides.
//   - messages consumes whatever's left, with a floor of 10.
//
// Border bits are 2 cols on each non-rail pane (1 col left + 1 col
// right rounded border).
func (l *panelLayout) Compute(width, height, railWidth, sidebarWidth int, sidebarVisible, threadVisible bool) panelLayoutFrame {
	const (
		statusHeight = 1
		paneBorder   = 2 // left + right border cols
		minMsgWidth  = 40
		minThreadW   = 30
		floorMsgW    = 10
	)
	contentHeight := height - statusHeight

	sbWidth := 0
	sbBorder := 0
	if sidebarVisible {
		sbWidth = sidebarWidth
		sbBorder = paneBorder
	}

	msgAreaWidth := width - railWidth - sbWidth - sbBorder

	msgBorder := paneBorder
	threadWidth := 0
	threadBorder := 0
	autoHidden := false

	if threadVisible {
		threadBorder = paneBorder
		threadWidth = msgAreaWidth * 35 / 100
		msgPaneWidth := msgAreaWidth - threadWidth - msgBorder - threadBorder
		if msgPaneWidth < minMsgWidth || threadWidth < minThreadW {
			autoHidden = true
			threadWidth = 0
			threadBorder = 0
		}
	}

	msgWidth := msgAreaWidth - msgBorder - threadWidth - threadBorder
	if msgWidth < floorMsgW {
		msgWidth = floorMsgW
	}

	// Store bands for PanelAt. When sidebar / thread are hidden their
	// "end" coordinates collapse onto the prior band's end so PanelAt
	// can branch on bands alone (visibility flags are still passed
	// to PanelAt for defense, but with consistent bands they're
	// redundant).
	l.railWidth = railWidth
	l.sidebarEnd = railWidth + sbWidth + sbBorder
	l.msgEnd = l.sidebarEnd + msgWidth + msgBorder
	if threadVisible && !autoHidden && threadWidth > 0 {
		l.threadEnd = l.msgEnd + threadWidth + threadBorder
	} else {
		l.threadEnd = l.msgEnd
	}

	return panelLayoutFrame{
		RailWidth:        railWidth,
		SidebarWidth:     sbWidth,
		SidebarBorder:    sbBorder,
		MsgWidth:         msgWidth,
		MsgBorder:        msgBorder,
		ThreadWidth:      threadWidth,
		ThreadBorder:     threadBorder,
		ContentHeight:    contentHeight,
		ThreadAutoHidden: autoHidden,
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
