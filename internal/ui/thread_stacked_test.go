package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// stackedApp is normalOpts at w×30, sized through the real
// tea.WindowSizeMsg path, with the channel pane focused on a selected
// message.
func stackedApp(t *testing.T, w int, extra ...testOpt) *App {
	t.Helper()
	opts := append(normalOpts(), withWindowSize(w, 30))
	a := newTestApp(t, append(opts, extra...)...)
	focusMessages(t, a)
	return a
}

// assertFront checks which content panes the last View() drew, read
// from the bands it stored for mouse hit-testing.
func assertFront(t *testing.T, a *App, wantChannel, wantThread bool) {
	t.Helper()
	gotChannel := a.layout.msgEnd > a.layout.sidebarEnd
	gotThread := a.layout.threadEnd > a.layout.msgEnd
	if gotChannel != wantChannel || gotThread != wantThread {
		t.Fatalf("drawn: channel=%v thread=%v, want channel=%v thread=%v (bands %+v)",
			gotChannel, gotThread, wantChannel, wantThread, *a.layout)
	}
	if a.layout.threadEnd != a.width {
		t.Fatalf("bands end at %d, want the terminal width %d", a.layout.threadEnd, a.width)
	}
}

func TestStacked_FocusDecidesWhichPaneIsDrawn(t *testing.T) {
	a := stackedApp(t, 120)

	updateAndRender(t, a, keyCode(tea.KeyEnter))
	if !a.threadVisible || a.focusedPanel != PanelThread {
		t.Fatalf("Enter: threadVisible=%v focus=%v, want open and focused", a.threadVisible, a.focusedPanel)
	}
	assertFront(t, a, false, true)
	if !strings.Contains(statusbarText(a), "> Thread") {
		t.Errorf("status bar = %q, want the thread indicator", statusbarText(a))
	}

	updateAndRender(t, a, keyMod(tea.KeyTab, tea.ModShift)) // thread → channel
	if a.focusedPanel != PanelMessages || !a.threadVisible {
		t.Fatalf("shift+tab: focus=%v threadVisible=%v, want channel focused, thread still open", a.focusedPanel, a.threadVisible)
	}
	assertFront(t, a, true, false)
	if !strings.Contains(statusbarText(a), "> Thread") {
		t.Errorf("status bar = %q, want the thread indicator while the thread is behind", statusbarText(a))
	}

	updateAndRender(t, a, keyCode(tea.KeyTab)) // channel → thread
	if a.focusedPanel != PanelThread {
		t.Fatalf("tab: focus=%v, want PanelThread", a.focusedPanel)
	}
	assertFront(t, a, false, true)

	updateAndRender(t, a, keyCode(tea.KeyTab)) // thread → sidebar
	if a.focusedPanel != PanelSidebar {
		t.Fatalf("tab: focus=%v, want PanelSidebar", a.focusedPanel)
	}
	assertFront(t, a, false, true) // the last content pane stays in front

	updateAndRender(t, a, keyCode(tea.KeyEscape))
	if a.threadVisible {
		t.Fatal("Esc did not close the thread")
	}
	assertFront(t, a, true, false)
}

func TestStacked_WideTerminalShowsBoth(t *testing.T) {
	a := stackedApp(t, 200)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	assertFront(t, a, true, true)
	if w := a.layout.threadEnd - a.layout.msgEnd; w != 82 {
		t.Errorf("thread band = %d cols, want 82 (80 + border)", w)
	}
}

// Credit: the Enter → fetch → replies-land shape is mkozjak's, from
// PR #246. Before this change the reply was dropped because the render
// between Enter and the fetch result had auto-hidden the pane (#223).
func TestStacked_RepliesLandAt120(t *testing.T) {
	a := stackedApp(t, 120)
	selected, _ := a.messagepane.SelectedMessage()
	reply := messages.MessageItem{TS: "6.0", ThreadTS: selected.TS, UserName: "bob", Text: "a narrow reply"}
	a.setThreadFetcherForTest(func(ids.ChannelID, ids.ThreadTS) core.Msg {
		return ThreadRepliesLoadedMsg{ThreadTS: selected.TS, Replies: []messages.MessageItem{reply}}
	})

	_, cmd := a.Update(keyCode(tea.KeyEnter))
	_ = a.View()
	for _, msg := range drainBatch(cmd) {
		if msg != nil {
			updateAndRender(t, a, msg)
		}
	}
	if got := a.threadPanel.Replies(); len(got) != 1 || got[0].TS != reply.TS {
		t.Fatalf("thread replies = %+v, want the fetched reply", got)
	}
	if !strings.Contains(ansi.Strip(a.View().Content), "a narrow reply") {
		t.Error("the fetched reply is not on screen")
	}
}

func TestStacked_ThreadsViewKeepsTheListInFront(t *testing.T) {
	sums := []cache.ThreadSummary{{
		ChannelID: "C1", ChannelName: "general", ChannelType: "channel",
		ThreadTS: "50.0", ParentTS: "50.0", ParentUserID: "U1",
		ParentText: "hello", ReplyCount: 1,
	}}
	a := stackedApp(t, 120, withThreadsView(sums))

	updateAndRender(t, a, ThreadsViewActivatedMsg{})
	if a.view != ViewThreads || !a.threadVisible || a.focusedPanel != PanelMessages {
		t.Fatalf("activation: view=%v threadVisible=%v focus=%v", a.view, a.threadVisible, a.focusedPanel)
	}
	assertFront(t, a, true, false)

	updateAndRender(t, a, keyCode(tea.KeyEnter))
	if a.focusedPanel != PanelThread {
		t.Fatalf("Enter: focus=%v, want PanelThread", a.focusedPanel)
	}
	assertFront(t, a, false, true)

	updateAndRender(t, a, keyCode(tea.KeyEscape))
	if a.threadVisible || a.view != ViewThreads {
		t.Fatalf("Esc: threadVisible=%v view=%v, want closed, still in the Threads view", a.threadVisible, a.view)
	}
	assertFront(t, a, true, false)
}

func TestStacked_ViewDoesNotMutateState(t *testing.T) {
	for _, w := range []int{80, 120, 200} {
		a := stackedApp(t, w)
		updateAndRender(t, a, keyCode(tea.KeyEnter))
		type snap struct {
			visible bool
			focus   Panel
			front   Panel
		}
		before := snap{a.threadVisible, a.focusedPanel, a.stackFront}
		_ = a.View()
		_ = a.View()
		if after := (snap{a.threadVisible, a.focusedPanel, a.stackFront}); after != before {
			t.Errorf("width %d: View changed state %+v → %+v", w, before, after)
		}
	}
}

// Review focus 1.
func TestStacked_ResizeAcrossThreshold(t *testing.T) {
	a := stackedApp(t, 200)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	updateAndRender(t, a, keyMod(tea.KeyTab, tea.ModShift))
	assertFront(t, a, true, true)

	updateAndRender(t, a, tea.WindowSizeMsg{Width: 120, Height: 30})
	if !a.threadVisible {
		t.Fatal("narrowing closed the thread")
	}
	assertFront(t, a, true, false)

	updateAndRender(t, a, tea.WindowSizeMsg{Width: 200, Height: 30})
	assertFront(t, a, true, true)
}

// Review focus 2.
func TestStacked_ChannelSwitchClosesThread(t *testing.T) {
	a := stackedApp(t, 120)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	assertFront(t, a, false, true)

	updateAndRender(t, a, ChannelSelectedMsg{ID: "C2", Name: "random", Type: "channel"})
	if a.threadVisible {
		t.Fatal("switching channel left the thread open")
	}
	assertFront(t, a, true, false)
}

// Review focus 3.
func TestStacked_SidebarToggleUnstacks(t *testing.T) {
	a := stackedApp(t, 140)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	assertFront(t, a, false, true)

	a.ToggleSidebar()
	_ = a.View()
	assertFront(t, a, true, true)
}

// Review focus 4.
func TestStacked_InsertModeKeepsThreadInFront(t *testing.T) {
	a := stackedApp(t, 80)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	updateAndRender(t, a, keyPress('i'))
	if a.mode != ModeInsert || a.focusedPanel != PanelThread {
		t.Fatalf("i: mode=%v focus=%v, want insert in the thread", a.mode, a.focusedPanel)
	}
	assertFront(t, a, false, true)
}

// Final review F1: `i` from the sidebar while the thread is drawn
// alone (stacked, thread in front) must go to the thread compose, not
// jump the channel back in front.
func TestStacked_InsertFromSidebarGoesToThreadWhenAlone(t *testing.T) {
	a := stackedApp(t, 120)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	updateAndRender(t, a, keyCode(tea.KeyTab)) // thread -> sidebar
	if a.focusedPanel != PanelSidebar {
		t.Fatalf("precondition: focus=%v, want PanelSidebar", a.focusedPanel)
	}
	updateAndRender(t, a, keyPress('i'))
	if a.mode != ModeInsert || a.focusedPanel != PanelThread {
		t.Fatalf("i: mode=%v focus=%v, want insert in the thread", a.mode, a.focusedPanel)
	}
	assertFront(t, a, false, true)
}

// Final review F1 (side by side, unchanged behavior): `i` from the
// sidebar with both panes drawn still goes to the channel compose.
func TestStacked_InsertFromSidebarSideBySideGoesToChannel(t *testing.T) {
	a := stackedApp(t, 200)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	updateAndRender(t, a, keyCode(tea.KeyTab)) // thread -> sidebar
	if a.focusedPanel != PanelSidebar {
		t.Fatalf("precondition: focus=%v, want PanelSidebar", a.focusedPanel)
	}
	updateAndRender(t, a, keyPress('i'))
	if a.focusedPanel != PanelMessages {
		t.Fatalf("i: focus=%v, want PanelMessages (side-by-side unchanged)", a.focusedPanel)
	}
}

// Final review F1b: hiding the sidebar while it is focused and the
// thread is drawn alone must focus the thread, not force the channel
// back in front.
func TestStacked_ToggleSidebarHideKeepsLoneThreadInFront(t *testing.T) {
	a := stackedApp(t, 120)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	updateAndRender(t, a, keyCode(tea.KeyTab)) // thread -> sidebar
	if a.focusedPanel != PanelSidebar {
		t.Fatalf("precondition: focus=%v, want PanelSidebar", a.focusedPanel)
	}
	a.ToggleSidebar()
	_ = a.View()
	if a.focusedPanel != PanelThread {
		t.Fatalf("ToggleSidebar: focus=%v, want PanelThread", a.focusedPanel)
	}
	// At 120 with the sidebar hidden the panes are still stacked
	// (< 130 needed for both), so the thread stays the lone pane
	// drawn.
	assertFront(t, a, false, true)
}

// Review focus 5.
func TestStacked_TinyTerminalsRender(t *testing.T) {
	for w := 30; w <= 60; w++ {
		a := stackedApp(t, w)
		updateAndRender(t, a, keyCode(tea.KeyEnter))
		if !a.threadVisible || a.layout.threadEnd <= a.layout.msgEnd {
			t.Fatalf("width %d: thread not drawn", w)
		}
	}
}
