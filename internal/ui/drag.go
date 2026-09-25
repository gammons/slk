// internal/ui/drag.go
//
// Mouse-drag selection FSM state.
//
// Phase 2h of the SOLID refactor of internal/ui/app.go: extracts the
// dragState struct + its primitive transitions out of App. The four
// Update arms that drive the FSM (MouseClickMsg, MouseMotionMsg,
// autoScrollTickMsg, MouseReleaseMsg) stay on App because they touch
// sub-models (messagepane, threadPanel) and dispatch tea.Cmds — but
// they now go through this controller for every state read and
// mutation.
//
// State machine:
//
//	IDLE                  panel == PanelWorkspace (zero value)
//	  │ MouseClickMsg on PanelMessages / PanelThread (Begin)
//	  ▼
//	PRESS_NOT_MOVED       panel set, moved == false
//	  │ MouseMotionMsg    (Extend → moved = true)
//	  ▼
//	DRAGGING              moved == true
//	  │ optionally: cursor at pane edge (ClaimAutoScroll → autoscroll
//	  │             tick chain starts), self-terminates on ClearAutoScroll
//	  │ MouseReleaseMsg   (Finish → IDLE; caller branches on moved
//	  ▼                    flag to either commit selection or treat as
//	IDLE                   a plain click)
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/statusbar"
)

// dragState captures an in-progress mouse drag. The originating panel
// (PanelMessages or PanelThread; PanelWorkspace == idle) clamps where
// the drag's selection extends to — leaving the pane pins the extend
// at the last known position inside it.
//
// clickedMessage records whether the press landed on a real message
// row (vs chrome or empty space). MouseReleaseMsg consults it on
// plain-click finalization: if a plain click landed on a message, that
// message's thread is opened (mirrors the Enter keypress).
//
// autoScrollActive is the once-claim guard for the edge-autoscroll
// tea.Tick chain; ClaimAutoScroll returns true exactly once until
// ClearAutoScroll resets it.
//
// pendingX/Y/hasPending + flushScheduled implement motion coalescing.
// MouseMotionMsg latches the latest cursor position into pending* and
// schedules a single motionFlushTickMsg at most once per
// motionFlushInterval. The tick applies the latched position via
// panel.ExtendSelectionAt -- collapsing a burst of cell-motion events
// (terminals routinely fire >100 Hz; some 1000 Hz) into one selection
// update per tick. MouseReleaseMsg force-flushes pending so the final
// selection captures the most recent cursor cell even if the tick has
// not fired yet.
type dragState struct {
	panel            Panel
	pressX, pressY   int
	lastX, lastY     int
	moved            bool
	autoScrollActive bool
	clickedMessage   bool

	pendingX, pendingY int
	hasPending         bool
	flushScheduled     bool
}

func newDragState() *dragState { return &dragState{} }

// IsActive reports whether a drag is in progress on a real pane.
func (d *dragState) IsActive() bool {
	return d.panel == PanelMessages || d.panel == PanelThread
}

// Panel returns the originating panel. Meaningless when !IsActive.
func (d *dragState) Panel() Panel { return d.panel }

// LastPos returns the most recent cursor position recorded by Extend
// (or the initial press position if no motion has occurred yet).
func (d *dragState) LastPos() (x, y int) { return d.lastX, d.lastY }

// Begin records a fresh press on panel at (px, py). Any prior drag
// state is overwritten.
func (d *dragState) Begin(panel Panel, px, py int) {
	*d = dragState{
		panel:  panel,
		pressX: px, pressY: py,
		lastX: px, lastY: py,
	}
}

// SetClickedMessage records whether the press landed on a real message
// row. Called immediately after Begin from the MouseClickMsg arm on
// PanelMessages.
func (d *dragState) SetClickedMessage(b bool) { d.clickedMessage = b }

// Extend updates the cursor position to (px, py) and marks the drag
// as moved. If panel doesn't match the originating drag panel, the
// position is clamped to the previous lastX/lastY (pinning extension
// at the last known coordinates inside the originating pane).
// Returns the effective (lastX, lastY) after clamping.
func (d *dragState) Extend(panel Panel, px, py int) (x, y int) {
	if panel != d.panel {
		px, py = d.lastX, d.lastY
	}
	d.lastX, d.lastY = px, py
	d.moved = true
	return px, py
}

// ClaimAutoScroll flips on the autoscroll-in-flight gate. Returns
// true on first call (caller schedules an autoScrollTickMsg);
// false if a chain is already in flight (caller does nothing).
func (d *dragState) ClaimAutoScroll() bool {
	if d.autoScrollActive {
		return false
	}
	d.autoScrollActive = true
	return true
}

// ClearAutoScroll resets the autoscroll-in-flight gate. Called from
// the autoScrollTickMsg arm when the cursor leaves the pane edge or
// the drag ends.
func (d *dragState) ClearAutoScroll() { d.autoScrollActive = false }

// Finish returns the captured release context and resets the state
// to idle. Called from MouseReleaseMsg.
func (d *dragState) Finish() (moved bool, panel Panel, clickedMessage bool) {
	moved = d.moved
	panel = d.panel
	clickedMessage = d.clickedMessage
	*d = dragState{}
	return
}

// autoScrollTickInterval is the cadence for the edge-autoscroll
// tick chain while a drag is held against the top/bottom edge of a
// scrollable pane. 50ms is fast enough to feel responsive but slow
// enough not to overshoot small message lists.
const autoScrollTickInterval = 50 * time.Millisecond

// autoScrollTickCmd schedules the next autoScrollTickMsg. The chain
// self-terminates when the drag ends or the cursor leaves the edge
// (see Handle's autoScrollTickMsg arm).
func autoScrollTickCmd() tea.Cmd {
	return tea.Tick(autoScrollTickInterval, func(time.Time) tea.Msg {
		return autoScrollTickMsg{}
	})
}

// motionFlushInterval is the cadence for the mouse-motion coalescing
// flush tick. 16ms = ~60 Hz: fast enough that selection still feels
// instantaneous, slow enough to collapse the >100 Hz cell-motion
// stream most terminals emit while a button is held into a single
// ExtendSelectionAt + render per tick.
const motionFlushInterval = 16 * time.Millisecond

// motionFlushTickMsg is the tick that drains dragState.hasPending
// into the panel's selection. Scheduled by the MouseMotionMsg arm
// (at most once per motionFlushInterval via flushScheduled).
type motionFlushTickMsg struct{}

// motionFlushTickCmd schedules the next motionFlushTickMsg. See
// Handle's MouseMotionMsg / motionFlushTickMsg arms.
func motionFlushTickCmd() tea.Cmd {
	return tea.Tick(motionFlushInterval, func(time.Time) tea.Msg {
		return motionFlushTickMsg{}
	})
}

// Handle is the drag-FSM reducer for App.Update (Phase 4c). Owns
// the three Update arms that read/mutate drag state:
//
//   - tea.MouseMotionMsg  -- extend the selection + maybe start
//     the autoscroll chain when the cursor hits an edge.
//   - autoScrollTickMsg   -- one tick of the chain: scroll the
//     originating pane, re-extend the selection, reschedule.
//   - tea.MouseReleaseMsg -- finalize: plain click (open thread or
//     clear selection) vs drag (copy selection to clipboard).
//
// tea.MouseClickMsg and tea.MouseWheelMsg deliberately do NOT route
// through here. MouseClick is a multi-panel router (workspace rail,
// sidebar, channels, reactions, image preview, drag-begin) whose
// drag-begin is only one of many outcomes; MouseWheel is pure
// viewport scrolling unrelated to drag. Both belong in a future
// reducer_mouse.go (Phase 4m) once their non-drag responsibilities
// have a home.
//
// Returns (nil, false) for any other message type.
func (d *dragState) Handle(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case tea.MouseMotionMsg:
		if a.bootstrap.IsLoading() {
			return nil, true
		}
		if m.Button != tea.MouseLeft {
			return nil, true
		}
		if !d.IsActive() {
			return nil, true
		}
		panel, px, py, _ := a.panelAt(m.X, m.Y)
		// Drag bookkeeping (lastX/lastY/moved) MUST run every motion
		// event even when the panel-level ExtendSelectionAt is deferred
		// to the flush tick: MouseReleaseMsg branches on d.moved to
		// distinguish drag-finalize from plain-click, and the
		// autoScrollTickMsg chain reads d.LastPos() to extend selection
		// while held against an edge.
		//
		// Extend ALSO clamps the (px, py) to the originating pane when
		// the cursor leaves it, pinning the latched pending position at
		// the last in-pane coordinates -- the same behavior we had pre-
		// coalescing.
		px, py = d.Extend(panel, px, py)
		// Latch the latest position for the coalescing flush tick.
		// Subsequent motion events in the same window overwrite this
		// without scheduling additional ticks.
		d.pendingX, d.pendingY = px, py
		d.hasPending = true

		// Edge auto-scroll detection stays inline (NOT coalesced):
		// the autoscroll chain is already throttled to one in-flight
		// tick via ClaimAutoScroll + has its own 50ms cadence, and
		// keeping it inline preserves edge-drag responsiveness even
		// when the user is moving slowly (one motion event per cell).
		var hint int
		switch d.Panel() {
		case PanelMessages:
			hint = a.messagepane.ScrollHintForDrag(py)
		case PanelThread:
			hint = a.threadPanel.ScrollHintForDrag(py)
		}
		var cmds []tea.Cmd
		if hint != 0 && d.ClaimAutoScroll() {
			cmds = append(cmds, autoScrollTickCmd())
		}
		if !d.flushScheduled {
			d.flushScheduled = true
			cmds = append(cmds, motionFlushTickCmd())
		}
		switch len(cmds) {
		case 0:
			return nil, true
		case 1:
			return cmds[0], true
		default:
			return tea.Batch(cmds...), true
		}

	case motionFlushTickMsg:
		_ = m
		// Tick consumed -- allow a future MouseMotionMsg to schedule
		// the next one. Drop pending if the drag ended (e.g. the
		// MouseReleaseMsg arm already drained it and reset state).
		d.flushScheduled = false
		if !d.IsActive() {
			d.hasPending = false
			return nil, true
		}
		if !d.hasPending {
			return nil, true
		}
		d.hasPending = false
		px, py := d.pendingX, d.pendingY
		switch d.Panel() {
		case PanelMessages:
			a.messagepane.ExtendSelectionAt(py, px)
		case PanelThread:
			a.threadPanel.ExtendSelectionAt(py, px)
		}
		return nil, true

	case autoScrollTickMsg:
		_ = m
		// If the drag ended (release clears the drag state),
		// self-terminate.
		if !d.IsActive() {
			d.ClearAutoScroll()
			return nil, true
		}
		lastX, lastY := d.LastPos()
		var hint int
		switch d.Panel() {
		case PanelMessages:
			hint = a.messagepane.ScrollHintForDrag(lastY)
		case PanelThread:
			hint = a.threadPanel.ScrollHintForDrag(lastY)
		}
		if hint == 0 {
			// Cursor left the edge -- stop ticking. Re-entering the
			// edge in a future motion event will re-arm the loop.
			d.ClearAutoScroll()
			return nil, true
		}
		switch d.Panel() {
		case PanelMessages:
			if hint < 0 {
				a.messagepane.ScrollUp(1)
			} else {
				a.messagepane.ScrollDown(1)
			}
			a.messagepane.ExtendSelectionAt(lastY, lastX)
		case PanelThread:
			if hint < 0 {
				a.threadPanel.ScrollUp(1)
			} else {
				a.threadPanel.ScrollDown(1)
			}
			a.threadPanel.ExtendSelectionAt(lastY, lastX)
		}
		// Schedule the next tick. autoScrollActive remains true.
		return autoScrollTickCmd(), true

	case tea.MouseReleaseMsg:
		_ = m
		if !d.IsActive() {
			return nil, true
		}
		// Drain any pending coalesced motion BEFORE finalizing so
		// the panel's selRange.End (and therefore the clipboard
		// text we're about to emit) reflects the most recent
		// cursor position even if motionFlushTickMsg has not fired
		// since the last MouseMotionMsg.
		if d.hasPending {
			switch d.Panel() {
			case PanelMessages:
				a.messagepane.ExtendSelectionAt(d.pendingY, d.pendingX)
			case PanelThread:
				a.threadPanel.ExtendSelectionAt(d.pendingY, d.pendingX)
			}
			d.hasPending = false
		}
		moved, panel, clickedMessage := d.Finish()
		if !moved {
			// Plain click -- drop any previous pinned selection.
			switch panel {
			case PanelMessages:
				a.messagepane.ClearSelection()
				// Treat a click on a real message row as Enter:
				// open that message's thread. Clicks that missed
				// (chrome, empty space) leave the panel as-is.
				if clickedMessage {
					if cmd := a.openThreadForSelectedMessage(); cmd != nil {
						return cmd, true
					}
				}
			case PanelThread:
				a.threadPanel.ClearSelection()
			}
			return nil, true
		}
		var (
			text string
			ok   bool
		)
		switch panel {
		case PanelMessages:
			text, ok = a.messagepane.EndSelection()
		case PanelThread:
			text, ok = a.threadPanel.EndSelection()
		}
		if !(ok && text != "") {
			return nil, true
		}
		n := len([]rune(text))
		return tea.Batch(
			a.clipboardWrite(text),
			func() tea.Msg { return statusbar.CopiedMsg{N: n} },
		), true
	}
	return nil, false
}
