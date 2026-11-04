package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

func TestThreadParentJump_SameChannelSelectsParent(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")
	_ = app.openThreadPanel(messages.MessageItem{TS: "2.0", Text: "two"}, "C1", "2.0")
	app.focusedPanel = PanelThread

	if cmd := app.handleNormalMode(keyPress('p')); cmd != nil {
		t.Fatalf("same-channel jump must complete in place, got a cmd: %v", cmd)
	}
	if app.threadVisible {
		t.Error("thread still open after p")
	}
	if app.focusedPanel != PanelMessages {
		t.Errorf("focus = %v, want the messages pane", app.focusedPanel)
	}
	if sel, ok := app.messagepane.SelectedMessage(); !ok || sel.TS != "2.0" {
		t.Fatalf("selected = %+v ok=%v, want the thread's message 2.0", sel, ok)
	}
}

// TestThreadParentJump_OtherChannelOpensIt: a thread opened from the
// Threads or Activity view belongs to a channel that is not on screen.
func TestThreadParentJump_OtherChannelOpensIt(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	_ = app.openThreadPanel(messages.MessageItem{TS: "10.0", Text: "ten"}, "C2", "10.0")
	app.focusedPanel = PanelThread

	cmd := app.handleNormalMode(keyPress('p'))
	if cmd == nil {
		t.Fatal("expected a channel switch")
	}
	cs, ok := cmd().(ChannelSelectedMsg)
	if !ok || cs.ID != "C2" {
		t.Fatalf("want ChannelSelectedMsg for C2, got %#v", cmd())
	}
	driveJump(app, func() tea.Msg { return cs })
	if app.activeChannelID != "C2" {
		t.Fatalf("active channel = %q, want C2", app.activeChannelID)
	}
	if sel, ok := app.messagepane.SelectedMessage(); !ok || sel.TS != "10.0" {
		t.Fatalf("selected = %+v ok=%v, want the thread's message 10.0", sel, ok)
	}
	if app.threadVisible {
		t.Error("thread still open after p")
	}
}

// TestThreadParentJump_BackJumpReturnsToThread: the back-jump after p
// returns to the thread that was being read.
func TestThreadParentJump_BackJumpReturnsToThread(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	_ = app.openThreadPanel(messages.MessageItem{TS: "10.0", Text: "ten"}, "C2", "10.0")

	driveJump(app, app.handleNormalMode(keyPress('p')))
	want := Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0", ThreadTS: "10.0"}
	if app.backJump == nil || *app.backJump != want {
		t.Fatalf("back-jump slot = %+v, want the thread %+v", app.backJump, want)
	}
	driveJump(app, pressBackJump(app))
	if !app.threadVisible || app.threadPanel.ThreadTS() != "10.0" {
		t.Errorf("back-jump did not reopen thread 10.0 (visible=%v ts=%q)", app.threadVisible, app.threadPanel.ThreadTS())
	}
}

// TestThreadParentJump_BackJumpFromTheChannelPane: p with the channel
// pane focused still records the thread, not the pane's selection.
func TestThreadParentJump_BackJumpFromTheChannelPane(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")
	_ = app.openThreadPanel(messages.MessageItem{TS: "10.0", Text: "ten"}, "C2", "10.0")
	app.focusedPanel = PanelMessages

	driveJump(app, app.handleNormalMode(keyPress('p')))
	want := Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0", ThreadTS: "10.0"}
	if app.backJump == nil || *app.backJump != want {
		t.Fatalf("back-jump slot = %+v, want the thread %+v", app.backJump, want)
	}
	driveJump(app, pressBackJump(app))
	if !app.threadVisible || app.threadPanel.ThreadTS() != "10.0" {
		t.Errorf("back-jump did not reopen thread 10.0 (visible=%v ts=%q)", app.threadVisible, app.threadPanel.ThreadTS())
	}
}

func TestThreadParentJump_NoThreadIsNoop(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")
	if cmd := app.handleNormalMode(keyPress('p')); cmd != nil {
		t.Fatalf("p with no thread open returned a cmd: %v", cmd)
	}
	if sel, _ := app.messagepane.SelectedMessage(); sel.TS != "1.0" {
		t.Errorf("p with no thread open moved the selection to %q", sel.TS)
	}
	if app.backJump != nil {
		t.Error("p with no thread open recorded a back-jump")
	}
}
