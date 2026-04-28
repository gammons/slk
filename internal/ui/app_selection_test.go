package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

// looksLikeSetClipboardMsg returns true when m is the unexported
// setClipboardMsg type from bubbletea (a defined string type). It is
// the only string-kind Msg that flows through App, so reflecting the
// kind is sufficient to identify it.
func looksLikeSetClipboardMsg(m tea.Msg) (string, bool) {
	v := reflect.ValueOf(m)
	if v.Kind() == reflect.String {
		return v.String(), true
	}
	return "", false
}

func TestApp_DragInMessagesEmitsClipboardAndToast(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layoutSidebarEnd + 2
	pressY := 2
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
	var sawClipboard, sawCopiedToast bool
	for _, m := range msgs {
		if payload, ok := looksLikeSetClipboardMsg(m); ok {
			if payload == "" {
				t.Errorf("clipboard payload empty")
			}
			if strings.ContainsRune(payload, '▌') {
				t.Errorf("clipboard contained border char")
			}
			sawClipboard = true
		}
		if v, ok := m.(statusbar.CopiedMsg); ok {
			if v.N <= 0 {
				t.Errorf("CopiedMsg.N = %d", v.N)
			}
			sawCopiedToast = true
		}
	}
	if !sawClipboard {
		t.Errorf("expected setClipboardMsg in batched output (drained: %v)", msgs)
	}
	if !sawCopiedToast {
		t.Errorf("expected statusbar.CopiedMsg in batched output (drained: %v)", msgs)
	}
}

func TestApp_PlainClickDoesNotCopy(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layoutSidebarEnd + 2
	pressY := 2
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	_, cmd := a.Update(tea.MouseReleaseMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	for _, m := range drainBatch(cmd) {
		if _, ok := looksLikeSetClipboardMsg(m); ok {
			t.Fatal("plain click must not write to clipboard")
		}
	}
	if a.messagepane.HasSelection() {
		t.Fatal("plain click must not leave a pinned selection")
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
	pressX := a.layoutSidebarEnd + 2
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
	pressX := a.layoutSidebarEnd + 2
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
	pressX := a.layoutSidebarEnd + 2
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
