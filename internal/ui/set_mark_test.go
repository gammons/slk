// internal/ui/set_mark_test.go
//
// Tests for the m chord: arming, the mark letter, silent
// cancel on non-letters, the preview snapshot, and the no-selection /
// failed-persist toasts.
package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

func setMarkTestApp(t *testing.T) *App {
	t.Helper()
	app := NewApp()
	app.activeTeamID = "T1"
	app.activeChannelID = "C1"
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		return "general", "channel", true
	})
	return app
}

// pressMarkChord presses m and then the mark letter through
// App.Update, returning the cmd the letter produced (nil for a
// successful record).
func pressMarkChord(app *App, letter rune) tea.Cmd {
	app.Update(keyPress('m'))
	_, cmd := app.Update(keyPress(letter))
	return cmd
}

func TestSetMark_ChannelMessageRecordsLocationAndSnapshot(t *testing.T) {
	app := setMarkTestApp(t)
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", Text: "hello world"},
		{TS: "2.0", UserName: "bob", Text: "second"},
	})
	app.messagepane.SelectByTS("1.0")

	if cmd := pressMarkChord(app, 'a'); cmd != nil {
		t.Fatalf("marking returned a cmd: %v", cmd)
	}

	m, ok := app.marks.Load("T1", "a")
	if !ok {
		t.Fatal("mark a not recorded")
	}
	if m.ChannelID != "C1" || m.MessageTS != "1.0" || m.ThreadTS != "" {
		t.Fatalf("mark location = %+v, want C1 / message 1.0", m)
	}
	// The preview snapshot is captured at mark time.
	if m.ChannelName != "general" || m.AuthorName != "alice" || m.Excerpt != "hello world" {
		t.Fatalf("mark snapshot = %+v, want general / alice / hello world", m)
	}
	if app.pendingMark {
		t.Fatal("pendingMark must be cleared after the chord")
	}
}

func TestSetMark_ThreadReplyRecordsThreadAndReply(t *testing.T) {
	app := setMarkTestApp(t)
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "P1", ThreadTS: "P1", Text: "parent"}})
	app.threadPanel.SetThread(
		messages.MessageItem{TS: "P1", ThreadTS: "P1", Text: "parent"},
		[]messages.MessageItem{
			{TS: "R1", ThreadTS: "P1", UserName: "carol", Text: "a reply"},
			{TS: "R2", ThreadTS: "P1", Text: "later"},
		},
		"C1", "P1")
	app.focusedPanel = PanelThread
	app.threadPanel.SelectByIndex(0) // reading reply R1

	if cmd := pressMarkChord(app, 'b'); cmd != nil {
		t.Fatalf("marking returned a cmd: %v", cmd)
	}

	m, ok := app.marks.Load("T1", "b")
	if !ok {
		t.Fatal("mark b not recorded")
	}
	if m.ChannelID != "C1" || m.MessageTS != "R1" || m.ThreadTS != "P1" {
		t.Fatalf("mark location = %+v, want C1 / reply R1 / thread P1", m)
	}
	if m.AuthorName != "carol" || m.Excerpt != "a reply" {
		t.Fatalf("mark snapshot = %+v, want carol / a reply", m)
	}
}

func TestSetMark_OverwriteReplaces(t *testing.T) {
	app := setMarkTestApp(t)
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "first"},
		{TS: "2.0", Text: "second"},
	})
	app.messagepane.SelectByTS("1.0")
	pressMarkChord(app, 'a')

	app.messagepane.SelectByTS("2.0")
	pressMarkChord(app, 'a')

	m, ok := app.marks.Load("T1", "a")
	if !ok || m.MessageTS != "2.0" {
		t.Fatalf("mark after overwrite = %+v ok=%v, want 2.0", m, ok)
	}
	if marks := app.marks.List("T1"); len(marks) != 1 {
		t.Fatalf("list after overwrite = %+v, want exactly one entry", marks)
	}
}

func TestSetMark_NonLetterCancelsSilently(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{"digit", tea.KeyPressMsg{Code: '5', Text: "5"}},
		{"escape", tea.KeyPressMsg{Code: tea.KeyEscape}},
		{"slash", tea.KeyPressMsg{Code: '/', Text: "/"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := setMarkTestApp(t)
			app.messagepane.SetMessages([]messages.MessageItem{{TS: "1.0", Text: "hello"}})
			app.messagepane.SelectByTS("1.0")

			app.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
			if _, cmd := app.Update(tc.msg); cmd != nil {
				t.Fatalf("cancel produced a cmd: %v", cmd)
			}
			if app.pendingMark {
				t.Fatal("pendingMark must be cleared by the cancel")
			}
			if marks := app.marks.List("T1"); len(marks) != 0 {
				t.Fatalf("cancel recorded marks: %+v", marks)
			}
		})
	}
}

func TestSetMark_NoSelectionToastsAndRecordsNothing(t *testing.T) {
	app := setMarkTestApp(t) // no messages loaded

	app.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if cmd == nil {
		t.Fatal("expected a toast cmd for the no-selection decline")
	}
	if !strings.Contains(app.statusbar.View(80), "No message selected") {
		t.Fatalf("expected no-selection toast, got %q", app.statusbar.View(80))
	}
	if _, ok := app.marks.Load("T1", "a"); ok {
		t.Fatal("no-selection must not record a mark")
	}
}

func TestSetMark_FailedPersistToasts(t *testing.T) {
	app := setMarkTestApp(t)
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "1.0", UserName: "alice", Text: "hello"}})
	app.messagepane.SelectByTS("1.0")
	app.SetMarksPersistStore(NewMarksPersistStore(MarksPersistStoreFuncs{
		Upsert: func(teamID, letter string, m Mark) error {
			return errors.New("disk full")
		},
	}))

	// Uppercase: no session copy, so a lost write means the mark does
	// not exist — the failure must be visible, not silently swallowed.
	app.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	if cmd == nil {
		t.Fatal("expected a toast cmd for the failed persist")
	}
	if !strings.Contains(app.statusbar.View(80), "Failed to save mark") {
		t.Fatalf("expected persist-failure toast, got %q", app.statusbar.View(80))
	}
	if _, ok := app.marks.Load("T1", "A"); ok {
		t.Fatal("the failed uppercase mark must not exist")
	}
}

// Under persist_all the user has explicitly asked lowercase marks to
// survive a restart, so a failed write there is not harmless: the mark
// works this session and silently disappears at the next start. Report
// it, unlike the default case where lowercase is session-only by design.
func TestSetMark_FailedPersistUnderPersistAllToasts(t *testing.T) {
	app := setMarkTestApp(t)
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "1.0", UserName: "alice", Text: "hello"}})
	app.messagepane.SelectByTS("1.0")
	app.SetMarksPersistStore(NewMarksPersistStore(MarksPersistStoreFuncs{
		Upsert: func(teamID, letter string, m Mark) error {
			return errors.New("disk full")
		},
	}))
	app.SetMarksPersistAll(true)

	app.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if cmd == nil {
		t.Fatal("expected a toast cmd for the failed lowercase persist under persist_all")
	}
	if !strings.Contains(app.statusbar.View(80), "Failed to save mark") {
		t.Fatalf("expected persist-failure toast, got %q", app.statusbar.View(80))
	}
	// The session copy still exists — the mark works for this session,
	// it just will not survive the restart the user asked for.
	if _, ok := app.marks.Load("T1", "a"); !ok {
		t.Fatal("the lowercase mark should still be usable this session")
	}
}

// Without persist_all a lowercase mark is session-only by design, so a
// persist failure is not reachable and nothing is reported.
func TestSetMark_LowercaseWithoutPersistAllIsSilent(t *testing.T) {
	app := setMarkTestApp(t)
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "1.0", UserName: "alice", Text: "hello"}})
	app.messagepane.SelectByTS("1.0")
	app.SetMarksPersistStore(NewMarksPersistStore(MarksPersistStoreFuncs{
		Upsert: func(teamID, letter string, m Mark) error {
			return errors.New("disk full")
		},
	}))

	app.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	app.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if strings.Contains(app.statusbar.View(80), "Failed to save mark") {
		t.Fatal("a session-only lowercase mark must not report a persist failure")
	}
	if _, ok := app.marks.Load("T1", "a"); !ok {
		t.Fatal("the lowercase mark should exist in the session tier")
	}
}

// A pending m chord must not survive a mode change.
//
// Ctrl+C is intercepted in handleKey BEFORE mode dispatch, so it never
// reaches handleMarkChord to be cancelled there. Left armed, the next
// letter key in normal mode is swallowed as a mark name instead of
// doing its own job — the same hazard the pendingWinCmd disarm in
// SetMode exists to prevent.
func TestSetMark_ModeChangeDisarmsPendingChord(t *testing.T) {
	app := setMarkTestApp(t)
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "1.0", Text: "hello"}})
	app.messagepane.SelectByTS("1.0")

	// Arm the chord.
	app.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if !app.pendingMark {
		t.Fatal("pressing m should arm the mark chord")
	}

	// A global intercept changes mode without the chord seeing a key.
	app.SetMode(ModeConfirm)
	if app.pendingMark {
		t.Fatal("a mode change must disarm the pending mark chord")
	}

	// Back in normal mode, a letter must do its own job, not name a mark.
	app.SetMode(ModeNormal)
	app.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if _, ok := app.marks.Load("T1", "j"); ok {
		t.Fatal("j was swallowed as a mark letter after the mode change")
	}
}

// A mark set on a thread reply must record the THREAD's channel, not
// whatever activeChannelID happens to be. In the Threads view the panel
// shows a thread from any channel while activeChannelID still names the
// last channel opened, so recording the active channel stored a thread
// ts against a channel that has no such thread — jumping then opened an
// empty panel reading "0 replies".
func TestSetMark_ThreadReplyRecordsTheThreadsOwnChannel(t *testing.T) {
	app := setMarkTestApp(t)
	app.activeChannelID = "C1" // last channel opened
	app.threadPanel.SetThread(
		messages.MessageItem{TS: "P1", ThreadTS: "P1", Text: "parent"},
		[]messages.MessageItem{{TS: "R1", ThreadTS: "P1", Text: "reply", UserName: "alice"}},
		"C-OTHER", // the thread belongs to a different channel
		"P1")
	app.focusedPanel = PanelThread
	app.threadPanel.SelectByIndex(1)

	app.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	app.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})

	m, ok := app.marks.Load("T1", "a")
	if !ok {
		t.Fatal("mark a should exist")
	}
	if string(m.ChannelID) != "C-OTHER" {
		t.Errorf("ChannelID = %q, want C-OTHER (the thread's channel, not the active one)", m.ChannelID)
	}
	if string(m.ThreadTS) != "P1" || string(m.MessageTS) != "R1" {
		t.Errorf("thread/reply = %q/%q, want P1/R1", m.ThreadTS, m.MessageTS)
	}
}
