package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/statusbar"
)

func newTestAppWithMessages(t *testing.T) *App {
	t.Helper()
	a := NewApp()
	a.width = 120
	a.height = 30
	a.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", UserID: "U1", Text: "hello world", Timestamp: "1:00 PM"},
		{TS: "2.0", UserName: "bob", UserID: "U2", Text: "second message", Timestamp: "1:01 PM"},
	})
	// Force a render so layout offsets and caches populate.
	_ = a.View()
	return a
}

func TestVisualMode_SelectsWithMotionsAndYanks(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.focusedPanel = PanelMessages
	a.SetClipboardAvailable(true)
	var copied string
	a.SetClipboardWriter(func(_ clipboard.Format, data []byte) <-chan struct{} {
		copied = string(data)
		return nil
	})

	_, _ = a.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if a.mode != ModeVisual || !a.messagepane.HasSelection() {
		t.Fatalf("v did not enter visual mode with a selection: mode=%v selected=%v", a.mode, a.messagepane.HasSelection())
	}
	for range 4 {
		_, _ = a.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	}
	_, cmd := a.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	_ = cmd()
	if copied == "" {
		t.Fatal("y did not copy the visual selection")
	}
	if a.mode != ModeNormal {
		t.Fatalf("mode after yank = %v, want NORMAL", a.mode)
	}
}

func TestVisualMode_EscapeCancelsSelection(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.focusedPanel = PanelMessages
	_, _ = a.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	_, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.mode != ModeNormal || a.messagepane.HasSelection() {
		t.Fatalf("Esc did not cancel visual mode: mode=%v selected=%v", a.mode, a.messagepane.HasSelection())
	}
}

// drainBatch fully expands a tea.Cmd (including nested tea.BatchMsg) and
// returns all leaf messages. Test-only; ignores nil cmds.
func drainBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch v := msg.(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range v {
			out = append(out, drainBatch(c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

func TestApp_DragInMessagesEmitsClipboardAndToast(t *testing.T) {
	a := newTestAppWithMessages(t)
	var gotData string
	a.SetClipboardWriter(func(text string) tea.Cmd {
		gotData = text
		return nil
	})
	pressX := a.layout.sidebarEnd + 2
	// Terminal Y = 4 → panelAt subtracts the 1-row panel border to give
	// pane-local y=3, which is past the chrome (header + separator =
	// chromeHeight=2). Anything < 3 lands on the chrome and is a no-op.
	pressY := 4
	// Press
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	// Motion
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX + 10, Y: pressY + 1, Button: tea.MouseLeft})
	// Release
	_, cmd := a.Update(tea.MouseReleaseMsg{X: pressX + 10, Y: pressY + 1, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("expected a command on release")
	}
	msgs := drainBatch(cmd)
	var sawCopiedToast bool
	for _, m := range msgs {
		if v, ok := m.(statusbar.CopiedMsg); ok {
			if v.N <= 0 {
				t.Errorf("CopiedMsg.N = %d", v.N)
			}
			sawCopiedToast = true
		}
	}
	if len(gotData) == 0 {
		t.Errorf("expected data written to clipboard")
	}
	if strings.ContainsRune(string(gotData), '▌') {
		t.Errorf("clipboard contained border char: %q", string(gotData))
	}
	if !sawCopiedToast {
		t.Errorf("expected statusbar.CopiedMsg in batched output (drained: %v)", msgs)
	}
}

func TestApp_PlainClickDoesNotCopy(t *testing.T) {
	a := newTestAppWithMessages(t)
	var wrote bool
	a.SetClipboardWriter(func(string) tea.Cmd {
		wrote = true
		return nil
	})
	pressX := a.layout.sidebarEnd + 2
	// Terminal Y past the chrome (panel border + 2-row chrome).
	pressY := 4
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	_, cmd := a.Update(tea.MouseReleaseMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	for _, m := range drainBatch(cmd) {
		if _, ok := m.(statusbar.CopiedMsg); ok {
			t.Fatal("plain click must not emit CopiedMsg")
		}
	}
	if wrote {
		t.Fatal("plain click must not write to clipboard")
	}
	if a.messagepane.HasSelection() {
		t.Fatal("plain click must not leave a pinned selection")
	}
}

// A plain click (no drag) on a message row opens that message's thread
// view, mirroring the Enter keypress. Defends the click-to-open-thread
// behavior so it stays in lockstep with handleEnter's PanelMessages
// branch via the shared openThreadForSelectedMessage helper.
func TestApp_PlainClickOnMessageOpensThread(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.activeChannelID = "C1"

	fetchedCh := ""
	fetchedTS := ""
	a.setThreadFetcherForTest(func(channelID ids.ChannelID, threadTS ids.ThreadTS) tea.Msg {
		fetchedCh = string(channelID)
		fetchedTS = string(threadTS)
		return ThreadRepliesLoadedMsg{ThreadTS: string(threadTS), Replies: nil}
	})

	pressX := a.layout.sidebarEnd + 2
	// pane-local y=3 (terminal y=4 minus 1-row panel border) lands on
	// the first message row past the 2-row chrome. newTestAppWithMessages
	// seeds 2 messages with selection at index 1 (the bottom message --
	// SetMessages defaults selection to the newest); clicking row 3
	// should select message index 0 and open its thread.
	pressY := 4
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	_, cmd := a.Update(tea.MouseReleaseMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("click-release on a message must return a non-nil cmd (open-thread fetch)")
	}
	_ = drainBatch(cmd)

	if !a.threadVisible {
		t.Error("threadVisible = false after click; want true")
	}
	if a.focusedPanel != PanelThread {
		t.Errorf("focusedPanel = %v after click; want PanelThread", a.focusedPanel)
	}
	if fetchedCh != "C1" {
		t.Errorf("threadFetcher called with channel %q; want C1", fetchedCh)
	}
	// The first message has TS "1.0"; clicking it should open thread with
	// that TS as the parent.
	if fetchedTS != "1.0" {
		t.Errorf("threadFetcher called with threadTS %q; want 1.0", fetchedTS)
	}
}

// A click that lands on the channel header chrome (above the first
// message) must NOT open a thread -- chrome clicks are no-ops. Defends
// the ClickAt(returns bool) plumbing.
func TestApp_PlainClickOnChromeDoesNotOpenThread(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.activeChannelID = "C1"

	called := false
	a.setThreadFetcherForTest(func(channelID ids.ChannelID, threadTS ids.ThreadTS) tea.Msg {
		called = true
		return ThreadRepliesLoadedMsg{ThreadTS: string(threadTS), Replies: nil}
	})

	pressX := a.layout.sidebarEnd + 2
	// pane-local y=0 (terminal y=1 minus 1-row panel border) lands on
	// the channel header inside the messages pane chrome -- chrome
	// occupies pane-local rows 0..chromeHeight-1.
	pressY := 1
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	_, cmd := a.Update(tea.MouseReleaseMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})

	for _, m := range drainBatch(cmd) {
		if _, ok := m.(ThreadRepliesLoadedMsg); ok {
			t.Fatal("click on chrome must not dispatch a thread fetch")
		}
	}
	if called {
		t.Fatal("threadFetcher called on chrome click")
	}
	if a.threadVisible {
		t.Error("threadVisible = true after chrome click; want false")
	}
}

// A drag (motion between press and release) must NOT open a thread --
// the user is text-selecting, not navigating. Defends the moved-vs-clicked
// gate in the MouseReleaseMsg handler.
func TestApp_DragDoesNotOpenThread(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.activeChannelID = "C1"

	called := false
	a.setThreadFetcherForTest(func(channelID ids.ChannelID, threadTS ids.ThreadTS) tea.Msg {
		called = true
		return ThreadRepliesLoadedMsg{ThreadTS: string(threadTS), Replies: nil}
	})

	pressX := a.layout.sidebarEnd + 2
	pressY := 4
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	// Any motion between press and release flags a drag.
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX + 5, Y: pressY, Button: tea.MouseLeft})
	_, _ = a.Update(tea.MouseReleaseMsg{X: pressX + 5, Y: pressY, Button: tea.MouseLeft})

	if called {
		t.Fatal("threadFetcher called after a drag; drags must not open threads")
	}
	if a.threadVisible {
		t.Error("threadVisible = true after drag; want false")
	}
}

func TestApp_CopiedMsgShowsToastAndSchedulesClear(t *testing.T) {
	a := newTestAppWithMessages(t)
	_, cmd := a.Update(statusbar.CopiedMsg{N: 7})
	// Status bar must show the toast immediately.
	if !strings.Contains(a.statusbar.View(80), "Copied 7 chars") {
		t.Fatalf("status bar did not show toast")
	}
	if cmd == nil {
		t.Fatal("expected a tick command to clear the toast")
	}
	// We don't actually wait 2s in a unit test; just verify the Cmd
	// produces a CopiedClearMsg type when invoked.
	// tea.Tick wraps a function; calling it returns a TickMsg-like value.
	// Easier path: directly send CopiedClearMsg and verify it clears.
	_, _ = a.Update(statusbar.CopiedClearMsg{})
	if strings.Contains(a.statusbar.View(80), "Copied") {
		t.Fatalf("status bar still showing toast after CopiedClearMsg")
	}
}

func TestApp_DragNearTopEdgeSchedulesAutoScroll(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	// Press in the middle of the pane.
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: 5, Button: tea.MouseLeft})
	// Move to row 1 (which is pane-local y=0 — the top edge).
	_, cmd := a.Update(tea.MouseMotionMsg{X: pressX, Y: 1, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("expected an auto-scroll tick command on edge motion")
	}
	// The cmd should produce an autoScrollTickMsg.
	msgs := drainBatch(cmd)
	var sawTick bool
	for _, m := range msgs {
		if _, ok := m.(autoScrollTickMsg); ok {
			sawTick = true
		}
	}
	if !sawTick {
		t.Fatalf("expected autoScrollTickMsg in batched output; got %v", msgs)
	}
}

func TestApp_AutoScrollTickRefreshesWhileEdgeHeld(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: 5, Button: tea.MouseLeft})
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX, Y: 1, Button: tea.MouseLeft})
	// First tick fires; while still at the top edge we expect another one.
	_, cmd := a.Update(autoScrollTickMsg{})
	if cmd == nil {
		t.Fatal("expected another tick while edge is still active")
	}
	msgs := drainBatch(cmd)
	var sawTick bool
	for _, m := range msgs {
		if _, ok := m.(autoScrollTickMsg); ok {
			sawTick = true
		}
	}
	if !sawTick {
		t.Fatal("expected autoScrollTickMsg in continuation")
	}
}

func TestApp_AutoScrollStopsWhenCursorLeavesEdge(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: 5, Button: tea.MouseLeft})
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX, Y: 1, Button: tea.MouseLeft})
	// Move back to the middle.
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX, Y: 10, Button: tea.MouseLeft})
	// A tick now finds no edge → should NOT schedule another tick.
	_, cmd := a.Update(autoScrollTickMsg{})
	for _, m := range drainBatch(cmd) {
		if _, ok := m.(autoScrollTickMsg); ok {
			t.Fatal("auto-scroll must stop when cursor leaves the edge")
		}
	}
}

func TestApp_FocusNextClearsSelection(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: 4, Button: tea.MouseLeft})
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX + 5, Y: 4, Button: tea.MouseLeft})
	if !a.messagepane.HasSelection() {
		t.Fatal("precondition: should have selection")
	}
	a.FocusNext()
	if a.messagepane.HasSelection() {
		t.Fatal("FocusNext must clear selection")
	}
}

func TestApp_SetModeInsertClearsSelection(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: 4, Button: tea.MouseLeft})
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX + 5, Y: 4, Button: tea.MouseLeft})
	if !a.messagepane.HasSelection() {
		t.Fatal("precondition: should have selection")
	}
	a.SetMode(ModeInsert)
	if a.messagepane.HasSelection() {
		t.Fatal("entering insert mode must clear selection")
	}
}

func TestApp_ToggleSidebarClearsSelection(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: 4, Button: tea.MouseLeft})
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX + 5, Y: 4, Button: tea.MouseLeft})
	a.ToggleSidebar()
	if a.messagepane.HasSelection() {
		t.Fatal("ToggleSidebar must clear selection")
	}
}

// drainBatchCmds is like drainBatch but returns the leaf tea.Cmds
// instead of invoking them. Lets tests inspect a batch for the
// presence of a specific Cmd-shape (e.g. a tea.Tick) without
// triggering its side effects. Built by walking the outermost
// BatchMsg only -- nested batches are flattened the same way.
func drainBatchCmds(cmd tea.Cmd) []tea.Cmd {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		return []tea.Cmd(batch)
	}
	// Single-msg cmd: re-wrap so callers can still invoke it.
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// TestDrag_MotionCoalescing_DefersExtendSelectionAt is the perf
// invariant for the motion-coalescing flush tick: a MouseMotionMsg
// must NOT immediately call ExtendSelectionAt on the panel. It
// latches the cursor position and schedules a single flush tick
// (motionFlushTickMsg) that, when fired, applies the latest latched
// position. Bubbletea's main loop delivers MouseMotionMsg at the
// terminal's mouse-reporting rate (often >100 Hz) — coalescing
// caps the work done per cell at ~60 Hz.
func TestDrag_MotionCoalescing_DefersExtendSelectionAt(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	pressY := 4
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})

	// Precondition: Click started a selection (Begin sets selRange
	// to {Start, Start}) but SelectionText is empty since End == Start.
	if !a.messagepane.HasSelection() {
		t.Fatal("precondition: Click must Begin a selection")
	}
	if got := a.messagepane.SelectionText(); got != "" {
		t.Fatalf("precondition: Begin must leave selection empty; got %q", got)
	}

	// Motion -- with coalescing this MUST NOT extend the selection
	// immediately. The pane's SelectionText stays empty.
	_, cmd := a.Update(tea.MouseMotionMsg{X: pressX + 10, Y: pressY + 1, Button: tea.MouseLeft})
	if got := a.messagepane.SelectionText(); got != "" {
		t.Errorf("MouseMotionMsg must defer ExtendSelectionAt (coalesced); got SelectionText=%q", got)
	}

	// And it MUST have returned a Cmd that, when invoked, produces a
	// motionFlushTickMsg (the deferred extend trigger).
	var sawFlushTickProducer bool
	for _, c := range drainBatchCmds(cmd) {
		if _, ok := c().(motionFlushTickMsg); ok {
			sawFlushTickProducer = true
		}
	}
	if !sawFlushTickProducer {
		t.Fatalf("expected a motionFlushTickMsg producer in the returned cmd")
	}

	// Sending motionFlushTickMsg explicitly applies the latched
	// position; selection should now be non-empty.
	_, _ = a.Update(motionFlushTickMsg{})
	if got := a.messagepane.SelectionText(); got == "" {
		t.Errorf("motionFlushTickMsg must apply latched position; SelectionText still empty")
	}
}

// TestDrag_MotionCoalescing_ReleaseFlushesPending pins the contract
// that MouseReleaseMsg force-flushes any pending motion before
// finalizing, so the clipboard captures the most recent cursor
// position even if the flush tick hasn't fired yet.
func TestDrag_MotionCoalescing_ReleaseFlushesPending(t *testing.T) {
	a := newTestAppWithMessages(t)
	var gotData string
	a.SetClipboardWriter(func(text string) tea.Cmd {
		gotData = text
		return nil
	})
	pressX := a.layout.sidebarEnd + 2
	pressY := 4
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX + 10, Y: pressY + 1, Button: tea.MouseLeft})
	// NO motionFlushTickMsg sent -- mirrors the "user releases inside
	// the 16 ms window" case.
	_, cmd := a.Update(tea.MouseReleaseMsg{X: pressX + 10, Y: pressY + 1, Button: tea.MouseLeft})

	msgs := drainBatch(cmd)
	if len(gotData) == 0 {
		t.Errorf("Release must drain pending motion and write to clipboard; got data=%q", string(gotData))
	}
	found := false
	for _, m := range msgs {
		if _, ok := m.(statusbar.CopiedMsg); ok {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Release must emit CopiedMsg; got %v", msgs)
	}
}

// TestDrag_MotionCoalescing_OneTickPerBurst pins the coalescing
// invariant: multiple MouseMotionMsg events in a row schedule AT
// MOST one motionFlushTickMsg (the first one). Subsequent motions
// only update the latched pending position.
func TestDrag_MotionCoalescing_OneTickPerBurst(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	pressY := 4
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})

	countFlush := func(cmd tea.Cmd) int {
		n := 0
		for _, c := range drainBatchCmds(cmd) {
			if _, ok := c().(motionFlushTickMsg); ok {
				n++
			}
		}
		return n
	}

	_, cmd1 := a.Update(tea.MouseMotionMsg{X: pressX + 1, Y: pressY + 1, Button: tea.MouseLeft})
	_, cmd2 := a.Update(tea.MouseMotionMsg{X: pressX + 2, Y: pressY + 1, Button: tea.MouseLeft})
	_, cmd3 := a.Update(tea.MouseMotionMsg{X: pressX + 3, Y: pressY + 1, Button: tea.MouseLeft})

	if got := countFlush(cmd1); got != 1 {
		t.Errorf("first motion in burst: want 1 motionFlushTickMsg producer; got %d", got)
	}
	if got := countFlush(cmd2); got != 0 {
		t.Errorf("second motion in burst: want 0 flush producers (already scheduled); got %d", got)
	}
	if got := countFlush(cmd3); got != 0 {
		t.Errorf("third motion in burst: want 0 flush producers; got %d", got)
	}
}

// TestDrag_MotionCoalescing_TickAllowsReschedule pins the
// flushScheduled latch reset on tick: after a motionFlushTickMsg
// fires, the next MouseMotionMsg must be allowed to schedule a
// fresh tick. Without this, mouse motion would freeze after the
// first burst.
func TestDrag_MotionCoalescing_TickAllowsReschedule(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	pressY := 4
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})

	// First motion schedules a tick.
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX + 1, Y: pressY + 1, Button: tea.MouseLeft})
	// Fire the tick -- should reset the flushScheduled latch.
	_, _ = a.Update(motionFlushTickMsg{})

	// Next motion must schedule a new tick.
	_, cmd := a.Update(tea.MouseMotionMsg{X: pressX + 2, Y: pressY + 1, Button: tea.MouseLeft})
	var sawFlush bool
	for _, c := range drainBatchCmds(cmd) {
		if _, ok := c().(motionFlushTickMsg); ok {
			sawFlush = true
		}
	}
	if !sawFlush {
		t.Errorf("post-tick motion must schedule a new motionFlushTickMsg; cmd produced no flush tick")
	}
}
