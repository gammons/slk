// internal/ui/jump_mark_test.go
//
// Tests for the ' chord: cross-channel and same-channel
// jumps, thread-reply jumps, surrounding-history loads, and the three
// refusal paths (unset letter, foreign workspace, unresolvable
// channel), plus the cross-channel reversibility scenario.
package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

func jumpMarkTestApp(t *testing.T) *App {
	t.Helper()
	app := NewApp()
	app.activeTeamID = "T1"
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		return string(channelID) + "-name", "channel", true
	})
	app.setChannelCacheReaderForTest(func(channelID ids.ChannelID) []messages.MessageItem {
		switch channelID {
		case "C1":
			return []messages.MessageItem{{TS: "1.0", Text: "one"}, {TS: "2.0", Text: "two"}}
		case "C2":
			return []messages.MessageItem{{TS: "10.0", Text: "ten"}}
		case "C3":
			return []messages.MessageItem{{TS: "20.0", Text: "twenty"}}
		}
		return nil
	})
	app.setChannelSyncedAtReaderForTest(func(channelID ids.ChannelID) int64 {
		return time.Now().Unix()
	})
	return app
}

func seedMark(t *testing.T, app *App, letter string, loc Location) {
	t.Helper()
	if err := app.marks.Set("T1", letter, Mark{Location: loc, Letter: letter}); err != nil {
		t.Fatalf("seed mark %s: %v", letter, err)
	}
}

// pressJumpChord presses ' and then the letter through App.Update,
// returning the cmd the letter produced. With the jump overlay on (the
// default), ' opens the overlay and the letter reaches ModeMarks
// through the real mode dispatch.
func pressJumpChord(app *App, letter rune) tea.Cmd {
	app.Update(keyPress('\''))
	_, cmd := app.Update(keyPress(letter))
	return cmd
}

// pressBackJump presses ' twice (the back-jump) through App.Update,
// returning the cmd the second press produced.
func pressBackJump(app *App) tea.Cmd {
	app.Update(keyPress('\''))
	_, cmd := app.Update(keyPress('\''))
	return cmd
}

// driveJump feeds a jump/back-jump command through the program loop:
// cross-channel jumps dispatch a ChannelSelectedMsg that must be
// reduced for the navigation to complete. In-place jumps (nil cmd) are
// already complete.
func driveJump(app *App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if cs, ok := cmd().(ChannelSelectedMsg); ok {
		_, done := app.Update(cs)
		drainCmd(done)
	}
}

func TestJumpMark_OtherChannelSelectsMessage(t *testing.T) {
	app := jumpMarkTestApp(t)
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})

	cmd := pressJumpChord(app, 'a')
	if cmd == nil {
		t.Fatal("expected a jump command")
	}
	cs, ok := cmd().(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", cmd())
	}
	if cs.ID != "C2" {
		t.Fatalf("jump selected channel %q, want C2", cs.ID)
	}
	_, done := app.Update(cs)
	drainCmd(done)

	if app.activeChannelID != "C2" {
		t.Fatalf("active channel = %q, want C2", app.activeChannelID)
	}
	sel, ok := app.messagepane.SelectedMessage()
	if !ok || sel.TS != "10.0" {
		t.Fatalf("selected = %+v ok=%v, want the marked message 10.0", sel, ok)
	}
}

func TestJumpMark_SameChannelSelectsWithoutReload(t *testing.T) {
	app := jumpMarkTestApp(t)
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C1", MessageTS: "1.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	// Move away from the marked message; the buffer stays loaded.
	app.messagepane.SelectByTS("2.0")
	before := len(app.messagepane.Messages())

	if cmd := pressJumpChord(app, 'a'); cmd != nil {
		t.Fatalf("same-channel jump must complete in place, got a cmd: %v", cmd)
	}
	sel, ok := app.messagepane.SelectedMessage()
	if !ok || sel.TS != "1.0" {
		t.Fatalf("selected = %+v ok=%v, want the marked message 1.0", sel, ok)
	}
	if got := len(app.messagepane.Messages()); got != before {
		t.Fatalf("buffer was replaced by the jump: %d messages before, %d after", before, got)
	}
}

func TestJumpMark_ThreadReplyOpensThreadAndSelectsReply(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.SetThreadService(core.NewThreadService(core.ThreadServiceFuncs{
		CacheRead: func(channelID ids.ChannelID, threadTS ids.ThreadTS) []messages.MessageItem {
			return []messages.MessageItem{
				{TS: "P1", ThreadTS: "P1", Text: "parent"},
				{TS: "R1", ThreadTS: "P1", Text: "reply 1"},
				{TS: "R2", ThreadTS: "P1", Text: "reply 2"},
			}
		},
	}))
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "R1", ThreadTS: "P1"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})

	cmd := pressJumpChord(app, 'a')
	if cmd == nil {
		t.Fatal("expected a jump command")
	}
	cs, ok := cmd().(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", cmd())
	}
	_, done := app.Update(cs)
	// Drive the async thread replies through the loop. A jump's
	// thread loads arrive wrapped in the permalink envelope.
	for _, m := range drainCmd(done) {
		switch m.(type) {
		case ThreadRepliesLoadedMsg, permalinkThreadResultMsg:
			app.Update(m)
		}
	}

	if !app.threadVisible {
		t.Fatal("jump to a marked thread reply must open the thread panel")
	}
	if app.threadPanel.ThreadTS() != "P1" || app.threadPanel.ChannelID() != "C2" {
		t.Fatalf("opened thread = %s in %s, want P1 in C2", app.threadPanel.ThreadTS(), app.threadPanel.ChannelID())
	}
	if got := app.threadPanel.SelectedReply(); got == nil || got.TS != "R1" {
		t.Fatalf("selected reply = %+v, want the marked reply R1", got)
	}
}

func TestJumpMark_OutsideLoadedHistoryFetchesAround(t *testing.T) {
	var gotChannel string
	var gotTS string
	app := jumpMarkTestApp(t)
	setChannelFetchAroundForTest(app, func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
		gotChannel = string(channelID)
		gotTS = string(ts)
		return MessagesAroundLoadedMsg{
			ChannelID: string(channelID),
			TargetTS:  string(ts),
			Messages:  []messages.MessageItem{{TS: "50.0", Text: "marked"}},
		}
	})
	// The marked message is not in C2's cached window.
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "50.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})

	cmd := pressJumpChord(app, 'a')
	if cmd == nil {
		t.Fatal("expected a jump command")
	}
	cs, ok := cmd().(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", cmd())
	}
	_, done := app.Update(cs)
	drainCmd(done)

	if gotChannel != "C2" || gotTS != "50.0" {
		t.Fatalf("FetchAround(%s, %s), want (C2, 50.0)", gotChannel, gotTS)
	}
}

func TestJumpMark_UnsetLetterToasts(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})

	cmd := pressJumpChord(app, 'z')
	if cmd == nil {
		t.Fatal("expected a toast for an unset mark")
	}
	if !strings.Contains(app.statusbar.View(80), "not set") {
		t.Fatalf("expected an unset-mark toast, got %q", app.statusbar.View(80))
	}
}

func TestJumpMark_ForeignWorkspaceRefusedAndRetained(t *testing.T) {
	app := jumpMarkTestApp(t)
	// A mark stored under the active workspace but naming another one.
	seedMark(t, app, "a", Location{TeamID: "T2", ChannelID: "C9", MessageTS: "1.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")

	cmd := pressJumpChord(app, 'a')
	if cmd == nil {
		t.Fatal("expected a toast for the foreign-workspace refusal")
	}
	if !strings.Contains(app.statusbar.View(80), "Cross-workspace") {
		t.Fatalf("expected a cross-workspace toast, got %q", app.statusbar.View(80))
	}
	// Position unchanged and the mark retained.
	if app.activeChannelID != "C1" {
		t.Fatalf("active channel changed to %q, want C1", app.activeChannelID)
	}
	if sel, _ := app.messagepane.SelectedMessage(); sel.TS != "1.0" {
		t.Fatalf("selection moved on refusal: %+v", sel)
	}
	if _, ok, _ := app.marks.Load("T1", "a"); !ok {
		t.Fatal("the refused mark must be retained")
	}
}

func TestJumpMark_MessageGoneOpensChannelToastsAndRetains(t *testing.T) {
	app := jumpMarkTestApp(t)
	setChannelFetchAroundForTest(app, func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
		return MessagesAroundLoadedMsg{
			ChannelID: string(channelID),
			TargetTS:  string(ts),
			Messages:  []messages.MessageItem{{TS: "11.0", Text: "unrelated"}},
		}
	})
	// The marked message is neither cached nor in the fetched window.
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "50.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})

	cmd := pressJumpChord(app, 'a')
	cs, ok := cmd().(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", cmd())
	}
	_, done := app.Update(cs)
	var toast string
	for _, m := range drainCmd(done) {
		if am, ok := m.(MessagesAroundLoadedMsg); ok {
			_, nested := app.Update(am)
			for _, m2 := range drainCmd(nested) {
				if tm, ok := m2.(ToastMsg); ok {
					toast = tm.Text
				}
			}
		}
	}
	if !strings.Contains(toast, "Message not found") {
		t.Fatalf("expected a not-found toast, got %q", toast)
	}
	if app.activeChannelID != "C2" {
		t.Fatalf("the channel must still open, active = %q, want C2", app.activeChannelID)
	}
	if _, ok, _ := app.marks.Load("T1", "a"); !ok {
		t.Fatal("a mark whose message is gone must be retained")
	}
}

func TestJumpMark_UnresolvableChannelLeavesInPlace(t *testing.T) {
	app := jumpMarkTestApp(t)
	// C9 no longer resolves.
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		if channelID == "C9" {
			return "", "", false
		}
		return string(channelID) + "-name", "channel", true
	})
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C9", MessageTS: "1.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")

	cmd := pressJumpChord(app, 'a')
	if cmd == nil {
		t.Fatal("expected a toast for the unresolvable channel")
	}
	if !strings.Contains(app.statusbar.View(80), "unavailable") {
		t.Fatalf("expected an unavailable-channel toast, got %q", app.statusbar.View(80))
	}
	if app.activeChannelID != "C1" {
		t.Fatalf("active channel changed to %q, want C1 (left in place)", app.activeChannelID)
	}
	if sel, _ := app.messagepane.SelectedMessage(); sel.TS != "1.0" {
		t.Fatalf("selection moved on refusal: %+v", sel)
	}
	if _, ok, _ := app.marks.Load("T1", "a"); !ok {
		t.Fatal("the refused mark must be retained")
	}
}

// A cross-channel mark jump records the departure position in the nav
// history, so Ctrl+H after the jump returns to the pre-jump message.
func TestJumpMark_BackReturnsToPreJumpPosition(t *testing.T) {
	app := jumpMarkTestApp(t)
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0") // the pre-jump position

	// Jump to the mark in C2.
	cmd := pressJumpChord(app, 'a')
	cs, ok := cmd().(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", cmd())
	}
	_, done := app.Update(cs)
	drainCmd(done)
	if sel, _ := app.messagepane.SelectedMessage(); sel.TS != "10.0" {
		t.Fatalf("setup: jump landed on %+v, want 10.0", sel)
	}

	// Back restores C1 at the message the user was on before the jump.
	backCmd := app.navigateBack()
	if backCmd == nil {
		t.Fatal("expected a back walk after a cross-channel mark jump")
	}
	backCS, ok := backCmd().(ChannelSelectedMsg)
	if !ok || !backCS.FromHistory {
		t.Fatalf("back walk produced %T FromHistory=%v", backCmd(), ok)
	}
	_, done = app.Update(backCS)
	drainCmd(done)
	sel, _ := app.messagepane.SelectedMessage()
	if sel.TS != "1.0" {
		t.Fatalf("back after mark jump restored %q, want the pre-jump message 1.0", sel.TS)
	}
}

func TestJumpMark_NonLetterCancelsSilently(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{"digit", tea.KeyPressMsg{Code: '5', Text: "5"}},
		{"escape", tea.KeyPressMsg{Code: tea.KeyEscape}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := jumpMarkTestApp(t)
			app.SetShowJumpOverlay(false) // silent pending-key flow
			app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})

			app.Update(tea.KeyPressMsg{Code: '\'', Text: "'"})
			if _, cmd := app.Update(tc.msg); cmd != nil {
				t.Fatalf("cancel produced a cmd: %v", cmd)
			}
			if app.pendingJumpMark {
				t.Fatal("pendingJumpMark must be cleared by the cancel")
			}
		})
	}
}

func TestJumpMark_BacktickAliasArmsChord(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.SetShowJumpOverlay(false) // the alias arms the silent pending-key flow
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})

	app.Update(tea.KeyPressMsg{Code: '`', Text: "`"})
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if cmd == nil {
		t.Fatal("backtick must arm the jump chord")
	}
	if _, ok := cmd().(ChannelSelectedMsg); !ok {
		t.Fatalf("backtick jump produced %T, want ChannelSelectedMsg", cmd())
	}
}

func TestJumpMark_ModeChangeDisarmsPendingChord(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.SetShowJumpOverlay(false) // the pending-key flow is what SetMode must disarm
	app.Update(tea.KeyPressMsg{Code: '\'', Text: "'"})
	if !app.pendingJumpMark {
		t.Fatal("setup: pendingJumpMark should be armed")
	}
	// A global intercept that forces a mode (e.g. ctrl+c quit-confirm)
	// must not leave the chord armed to swallow the next letter.
	app.SetMode(ModeInsert)
	if app.pendingJumpMark {
		t.Fatal("SetMode must disarm the pending jump chord")
	}
}

func TestBackJump_CrossChannelReturnsToDeparture(t *testing.T) {
	app := jumpMarkTestApp(t)
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")

	driveJump(app, pressJumpChord(app, 'a'))
	if sel, _ := app.messagepane.SelectedMessage(); sel.TS != "10.0" {
		t.Fatalf("setup: jump landed on %+v, want 10.0", sel)
	}

	driveJump(app, pressBackJump(app))
	if app.activeChannelID != "C1" {
		t.Fatalf("back-jump landed in %q, want C1", app.activeChannelID)
	}
	if sel, _ := app.messagepane.SelectedMessage(); sel.TS != "1.0" {
		t.Fatalf("back-jump selected %q, want the departed message 1.0", sel.TS)
	}
}

func TestBackJump_SameChannelReturnsToDeparture(t *testing.T) {
	app := jumpMarkTestApp(t)
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C1", MessageTS: "2.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")

	// Jump within C1 to the marked message 2.0 (in-place, no reload).
	if cmd := pressJumpChord(app, 'a'); cmd != nil {
		t.Fatalf("setup: same-channel jump dispatched a cmd: %v", cmd)
	}
	if sel, _ := app.messagepane.SelectedMessage(); sel.TS != "2.0" {
		t.Fatalf("setup: jump landed on %+v, want 2.0", sel)
	}

	// '' returns to the departed message 1.0, still in the same channel.
	if cmd := pressBackJump(app); cmd != nil {
		t.Fatalf("same-channel back-jump must complete in place, got a cmd: %v", cmd)
	}
	if app.activeChannelID != "C1" {
		t.Fatalf("active channel changed to %q, want C1", app.activeChannelID)
	}
	if sel, _ := app.messagepane.SelectedMessage(); sel.TS != "1.0" {
		t.Fatalf("back-jump selected %q, want the departed message 1.0", sel.TS)
	}
}

func TestBackJump_OnlyLatestDepartureHeld(t *testing.T) {
	app := jumpMarkTestApp(t)
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"})
	seedMark(t, app, "b", Location{TeamID: "T1", ChannelID: "C3", MessageTS: "20.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")

	driveJump(app, pressJumpChord(app, 'a')) // C1@1.0 -> C2@10.0
	driveJump(app, pressJumpChord(app, 'b')) // C2@10.0 -> C3@20.0

	driveJump(app, pressBackJump(app))
	if app.activeChannelID != "C2" {
		t.Fatalf("back-jump landed in %q, want C2 (the second departure)", app.activeChannelID)
	}
	if sel, _ := app.messagepane.SelectedMessage(); sel.TS != "10.0" {
		t.Fatalf("back-jump selected %q, want 10.0 (the second departure)", sel.TS)
	}
}

func TestBackJump_RepeatToggles(t *testing.T) {
	app := jumpMarkTestApp(t)
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")

	driveJump(app, pressJumpChord(app, 'a')) // -> C2@10.0, slot=C1@1.0

	driveJump(app, pressBackJump(app)) // -> C1@1.0, slot=C2@10.0
	if sel, _ := app.messagepane.SelectedMessage(); sel.TS != "1.0" {
		t.Fatalf("first back-jump landed on %q, want 1.0", sel.TS)
	}

	driveJump(app, pressBackJump(app)) // -> C2@10.0, slot=C1@1.0
	if app.activeChannelID != "C2" {
		t.Fatalf("second back-jump landed in %q, want C2", app.activeChannelID)
	}
	if sel, _ := app.messagepane.SelectedMessage(); sel.TS != "10.0" {
		t.Fatalf("second back-jump selected %q, want the mark 10.0", sel.TS)
	}
}

func TestBackJump_NothingRecordedIsSilentNoOp(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")

	if cmd := pressBackJump(app); cmd != nil {
		t.Fatalf("back-jump with nothing recorded produced a cmd: %v", cmd)
	}
	if app.pendingJumpMark {
		t.Fatal("pendingJumpMark must be cleared by the back-jump")
	}
	if sel, _ := app.messagepane.SelectedMessage(); sel.TS != "1.0" {
		t.Fatalf("selection moved on a no-op back-jump: %+v", sel)
	}
}

func TestBackJump_NavHistoryStillRecordsCrossChannelJump(t *testing.T) {
	app := jumpMarkTestApp(t)
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")

	driveJump(app, pressJumpChord(app, 'a'))

	stack := app.navHistory.Stack("T1")
	if stack == nil || len(stack.entries) < 2 {
		t.Fatalf("nav history after jump has %d entries, want the departing channel and the arrival", len(stack.entries))
	}
	dep, arr := stack.entries[len(stack.entries)-2], stack.entries[len(stack.entries)-1]
	if dep.ChannelID != "C1" || dep.MessageTS != "1.0" {
		t.Fatalf("departing entry = %+v, want C1 at the pre-jump message 1.0", dep)
	}
	if arr.ChannelID != "C2" {
		t.Fatalf("arriving entry = %+v, want C2", arr)
	}
}

func TestBackJump_ClearedOnWorkspaceSwitch(t *testing.T) {
	app := jumpMarkTestApp(t)
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")

	driveJump(app, pressJumpChord(app, 'a'))
	if app.backJump == nil {
		t.Fatal("setup: the back-jump slot should be recorded")
	}

	_, _ = app.Update(WorkspaceSwitchedMsg{TeamID: "T2"})
	if app.backJump != nil {
		t.Fatal("the back-jump slot must be cleared on workspace switch")
	}
}

// A refused jump departs nothing, so it must not write the back-jump
// slot. Without this the slot would point at the location the user
// never left, and a later back-jump would "return" them somewhere they still
// are — or, worse, overwrite a slot from an earlier real jump.
func TestBackJump_RefusedJumpDoesNotWriteSlot(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		if channelID == "C9" {
			return "", "", false // unresolvable
		}
		return string(channelID) + "-name", "channel", true
	})
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C9", MessageTS: "1.0"})
	seedMark(t, app, "z", Location{TeamID: "TOTHER", ChannelID: "C1", MessageTS: "1.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})

	// Unset letter.
	pressJumpChord(app, 'q')
	if app.backJump != nil {
		t.Fatalf("unset-letter refusal wrote the slot: %+v", app.backJump)
	}
	// Foreign workspace.
	pressJumpChord(app, 'z')
	if app.backJump != nil {
		t.Fatalf("foreign-workspace refusal wrote the slot: %+v", app.backJump)
	}
	// Unresolvable channel.
	pressJumpChord(app, 'a')
	if app.backJump != nil {
		t.Fatalf("unresolvable-channel refusal wrote the slot: %+v", app.backJump)
	}
}

// A jump taken from the Threads list, or with the thread panel focused,
// must bring the user to the message. The in-place completion path
// (same channel, already loaded) skips ChannelSelectedMsg and with it
// the view/focus reset that arm performs — without doing it in
// applyLocation, the selection moves in a pane the user is not looking
// at and the jump reads as "nothing happened".
func TestJumpMark_FromThreadsViewShowsTheMessage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		view    View
		focused Panel
	}{
		{"threads list", ViewThreads, PanelMessages},
		{"threads list, thread panel focused", ViewThreads, PanelThread},
		{"channel view, thread panel focused", ViewChannels, PanelThread},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := jumpMarkTestApp(t)
			app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
			app.messagepane.SetMessages([]messages.MessageItem{
				{TS: "1.0", Text: "old"},
				{TS: "2.0", Text: "new"},
			})
			app.messagepane.SelectByTS("2.0")
			seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C1", MessageTS: "1.0"})

			app.view = tc.view
			app.focusedPanel = tc.focused

			pressJumpChord(app, 'a')

			if app.view != ViewChannels {
				t.Errorf("view = %v, want ViewChannels — the jump must be visible", app.view)
			}
			if app.focusedPanel != PanelMessages {
				t.Errorf("focusedPanel = %v, want PanelMessages", app.focusedPanel)
			}
			if sel, ok := app.messagepane.SelectedMessage(); !ok || sel.TS != "1.0" {
				t.Errorf("selected = %+v ok=%v, want the marked message 1.0", sel, ok)
			}
		})
	}
}

// The first jump into a channel must survive the authoritative load.
//
// A channel switch renders the cache first (best-effort) and then
// replaces the buffer when the network fetch lands. If the pending
// target is forgotten on the best-effort pass, SetMessages resets the
// selection to the newest message and nothing re-applies it — the jump
// visibly lands on the target and then snaps to the bottom. A second
// jump to the same channel looked fine because it completes in place
// with no fetch behind it.
func TestJumpMark_SurvivesAuthoritativeReload(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.activeChannelID = "C2"
	buffer := []messages.MessageItem{
		{TS: "1.0", Text: "the marked one"},
		{TS: "2.0", Text: "newer"},
		{TS: "3.0", Text: "newest"},
	}
	app.messagepane.SetMessages(buffer)
	app.pendingLinkNav = &pendingLinkNav{Location: Location{
		TeamID: "T1", ChannelID: "C2", MessageTS: "1.0",
	}}

	// Best-effort pass: the cache render already holds the target, so
	// it is selected — but a network fetch is still in flight.
	app.completePendingLinkNav("C2", false)
	if sel, ok := app.messagepane.SelectedMessage(); !ok || sel.TS != "1.0" {
		t.Fatalf("after the cache render, selected = %+v ok=%v, want 1.0", sel, ok)
	}
	if app.pendingLinkNav == nil {
		t.Fatal("the target must stay pending while a fetch is still coming")
	}

	// The authoritative load replaces the buffer, resetting the
	// selection to the newest message.
	app.messagepane.SetMessages(buffer)
	app.completePendingLinkNav("C2", true)

	if sel, ok := app.messagepane.SelectedMessage(); !ok || sel.TS != "1.0" {
		t.Fatalf("after the authoritative load, selected = %+v ok=%v — the jump snapped away from the marked message", sel, ok)
	}
	if app.pendingLinkNav != nil {
		t.Error("the target should be cleared once the authoritative pass has applied it")
	}
}

// Jumping to a channel-level mark while a thread panel is open must
// close that panel. The in-place path skips ChannelSelectedMsg, which
// is where CloseThread normally runs, so without doing it here the
// selection moves in the messages pane while the thread the user was
// reading stays open beside it.
func TestJumpMark_InPlaceClosesOpenThreadPanel(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "old"},
		{TS: "2.0", Text: "new"},
	})
	app.messagepane.SelectByTS("2.0")
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C1", MessageTS: "1.0"})

	// Reading a thread in the Threads view when the jump is taken.
	app.threadPanel.SetThread(
		messages.MessageItem{TS: "P1", ThreadTS: "P1", Text: "parent"},
		[]messages.MessageItem{{TS: "R1", ThreadTS: "P1", Text: "reply"}},
		"C1", "P1")
	app.threadVisible = true
	app.view = ViewThreads
	app.focusedPanel = PanelThread

	pressJumpChord(app, 'a')

	if app.threadVisible {
		t.Error("the thread panel must close: a channel-level jump left the previous thread on screen")
	}
	if app.view != ViewChannels || app.focusedPanel != PanelMessages {
		t.Errorf("view=%v panel=%v, want ViewChannels / PanelMessages", app.view, app.focusedPanel)
	}
	if sel, ok := app.messagepane.SelectedMessage(); !ok || sel.TS != "1.0" {
		t.Errorf("selected = %+v ok=%v, want the marked message 1.0", sel, ok)
	}
}

// A jump's ChannelSelectedMsg is queued. If a workspace switch is
// reduced first, the selection must not apply the old workspace's
// channel in the new one.
func TestJumpMark_QueuedSelectionDroppedAfterWorkspaceSwitch(t *testing.T) {
	app := jumpMarkTestApp(t)
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})

	cmd := pressJumpChord(app, 'a')
	if cmd == nil {
		t.Fatal("expected a jump command")
	}
	queued := cmd()

	app.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "Beta"})
	app.Update(queued)

	if app.activeTeamID != "T2" {
		t.Fatalf("active team = %q, want T2", app.activeTeamID)
	}
	if app.activeChannelID == "C2" {
		t.Fatal("T1's channel C2 was selected inside T2")
	}
}

// While an upload is in flight, no in-app jump may move the selection,
// the thread panel, focus, the history cursor or the back-jump slot —
// whether it would complete in place or switch channel.
func TestJumpMark_UploadInFlightRefusesNavigation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(a *App)
		keys  []tea.Msg
	}{
		{
			name: "same-channel mark",
			setup: func(a *App) {
				seedMark(t, a, "a", Location{TeamID: "T1", ChannelID: "C1", MessageTS: "100.0"})
			},
			keys: []tea.Msg{keyPress('\''), keyPress('a')},
		},
		{
			name: "other-channel mark",
			setup: func(a *App) {
				seedMark(t, a, "b", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"})
			},
			keys: []tea.Msg{keyPress('\''), keyPress('b')},
		},
		{
			name: "back-jump",
			setup: func(a *App) {
				a.backJump = &Location{TeamID: "T1", ChannelID: "C1", MessageTS: "100.0"}
			},
			keys: []tea.Msg{keyPress('\''), keyPress('\'')},
		},
		{
			name: "history back",
			setup: func(a *App) {
				a.navHistory.Push("T1", Location{TeamID: "T1", ChannelID: "C0"})
				a.navHistory.Push("T1", Location{TeamID: "T1", ChannelID: "C1"})
			},
			keys: []tea.Msg{keyMod('h', tea.ModCtrl)},
		},
		{
			name: "same-channel permalink",
			setup: func(a *App) {
				a.workspaceDomains["T1"] = "acme"
				a.messagepane.SetMessages([]messages.MessageItem{
					{TS: "100.0", Text: "parent"},
					{TS: "200.0", Text: "other"},
					{TS: "1779284733.270139", Text: "linked"},
				})
				a.messagepane.SelectByTS("200.0")
			},
			keys: []tea.Msg{OpenLinkMsg{URL: "https://acme.slack.com/archives/C1/p1779284733270139"}},
		},
		{
			// A jump queued just before the upload began.
			name: "queued jump selection",
			setup: func(a *App) {
				a.pendingLinkNav = &pendingLinkNav{Location: Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"}}
			},
			keys: []tea.Msg{ChannelSelectedMsg{ID: "C2", Name: "C2-name", Type: "channel", TeamID: "T1"}},
		},
		{
			// A history walk to another position in the active channel,
			// queued just before the upload began: the target is the
			// channel on screen, so the switch looks accepted.
			name: "queued same-channel history walk",
			setup: func(a *App) {
				a.pendingLinkNav = &pendingLinkNav{Location: Location{TeamID: "T1", ChannelID: "C1", MessageTS: "100.0"}}
			},
			keys: []tea.Msg{ChannelSelectedMsg{ID: "C1", Name: "C1-name", Type: "channel", FromHistory: true, TeamID: "T1"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, _ := uploadGuardApp(t)
			a.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
				return string(channelID) + "-name", "channel", true
			})
			a.SetMode(ModeNormal)
			a.focusedPanel = PanelMessages
			a.messagepane.SelectByTS("200.0")
			tc.setup(a)
			backJump := a.backJump
			cursor := -1
			if s := a.navHistory.Stack("T1"); s != nil {
				cursor = s.cursor
			}

			var cmd tea.Cmd
			for _, k := range tc.keys {
				_, cmd = a.Update(k)
			}
			firstBatchCmd(t, cmd)

			if got := statusbarText(a); !strings.Contains(got, "Upload in progress") {
				t.Errorf("status bar = %q, want it to contain %q", got, "Upload in progress")
			}
			if a.activeChannelID != "C1" {
				t.Errorf("active channel = %q, want C1", a.activeChannelID)
			}
			if sel, ok := a.messagepane.SelectedMessage(); !ok || sel.TS != "200.0" {
				t.Errorf("selected = %+v ok=%v, want 200.0", sel, ok)
			}
			if !a.threadVisible || a.threadPanel.ThreadTS() != "100.0" {
				t.Errorf("thread = (visible %v, ts %q), want (true, %q)", a.threadVisible, a.threadPanel.ThreadTS(), "100.0")
			}
			if a.focusedPanel != PanelMessages || a.view != ViewChannels {
				t.Errorf("focus/view = %v/%v, want messages/channels", a.focusedPanel, a.view)
			}
			if a.pendingLinkNav != nil {
				t.Errorf("pendingLinkNav = %+v, want nil", *a.pendingLinkNav)
			}
			if a.backJump != backJump {
				t.Error("the '' slot changed")
			}
			if s := a.navHistory.Stack("T1"); s != nil && s.cursor != cursor {
				t.Errorf("history cursor = %d, want %d", s.cursor, cursor)
			}
		})
	}
}

// A same-channel jump taken while the channel's fetch is still running
// lands on the cached buffer. The fetched messages then replace that
// buffer, and the jump must survive it rather than snap to the newest
// message.
func TestJumpMark_SameChannelSurvivesRunningFetch(t *testing.T) {
	app := jumpMarkTestApp(t)
	// syncedAt 0: the cache renders and a fetch is fired (tier 2).
	app.setChannelSyncedAtReaderForTest(func(ids.ChannelID) int64 { return 0 })
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C1", MessageTS: "1.0"})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})

	driveJump(app, pressJumpChord(app, 'a'))
	if sel, ok := app.messagepane.SelectedMessage(); !ok || sel.TS != "1.0" {
		t.Fatalf("precondition: jump selected %+v ok=%v, want 1.0", sel, ok)
	}

	_, cmd := app.Update(MessagesLoadedMsg{ChannelID: "C1", Messages: []messages.MessageItem{
		{TS: "1.0", Text: "one"}, {TS: "2.0", Text: "two"}, {TS: "3.0", Text: "three"},
	}})
	drainCmd(cmd)

	if sel, ok := app.messagepane.SelectedMessage(); !ok || sel.TS != "1.0" {
		t.Fatalf("after the fetch landed, selected = %+v ok=%v, want the marked 1.0", sel, ok)
	}
}
