// internal/ui/navhistory_test.go
//
// Integration tests for the position-carrying navigation history:
//   - walkNavCmd folds the departing position into the current entry
//     BEFORE Walk moves the cursor (the ChannelSelectedMsg arm cannot
//     do it — by the time the walk's message is reduced, the cursor
//     already names the destination and UpdateCurrent's channel guard
//     no-ops).
//   - the departure position, thread positions, stale-entry handling,
//     stack growth, per-workspace isolation, and the
//     message-is-gone degradation.
package ui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// walkAndUpdate invokes the walk command, verifies it synthesizes a
// FromHistory ChannelSelectedMsg, feeds it back through Update, and
// drives any follow-up work the pending navigation dispatched (thread
// replies, surrounding-history fetch) — as the program loop would.
func walkAndUpdate(t *testing.T, app *App, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a walk command, got nil")
	}
	cs, ok := cmd().(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", cmd())
	}
	if !cs.FromHistory {
		t.Fatal("walk navigation must carry FromHistory")
	}
	_, done := app.Update(cs)
	for _, m := range drainCmd(done) {
		switch tm := m.(type) {
		case ThreadRepliesLoadedMsg, permalinkThreadResultMsg:
			app.Update(tm)
		case MessagesAroundLoadedMsg:
			app.Update(tm)
		}
	}
}

// Walking away from a channel must record the position the user was at
// in that channel. Walk back to C1, move the selection to a different
// message, walk forward, then walk back again: the second return must
// land on the message moved to, not the one stored before the first
// walk.
func TestWalkBackForwardBackRestoresMovedPosition(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		return string(channelID) + "-name", "channel", true
	})
	app.setChannelCacheReaderForTest(func(channelID ids.ChannelID) []messages.MessageItem {
		switch channelID {
		case "C1":
			return []messages.MessageItem{
				{TS: "1.0", Text: "old"},
				{TS: "2.0", Text: "new"},
			}
		case "C2":
			return []messages.MessageItem{{TS: "10.0", Text: "c2"}}
		}
		return nil
	})
	app.setChannelSyncedAtReaderForTest(func(channelID ids.ChannelID) int64 {
		return time.Now().Unix()
	})

	// Simulate startup opening C1 (as the app does): seeds the stack.
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	// C1 renders its cache with the newest message selected; scroll to
	// the older message so the departing position is unambiguous.
	app.messagepane.SelectByTS("1.0")

	// C1 -> C2: records C1@1.0 in the stack.
	app.Update(ChannelSelectedMsg{ID: "C2", Name: "two", Type: "channel"})
	app.messagepane.SelectByTS("10.0") // deterministic position on C2

	// Walk back to C1: lands on C1@1.0.
	walkAndUpdate(t, app, app.navigateBack())
	if sel, ok := app.messagepane.SelectedMessage(); !ok || sel.TS != "1.0" {
		t.Fatalf("after first walk back, selected = %+v ok=%v, want 1.0", sel, ok)
	}

	// Move the selection on C1 to a different message (2.0).
	app.messagepane.SelectByTS("2.0")

	// Walk forward to C2: folds C1@2.0 into the C1 entry, lands on C2.
	walkAndUpdate(t, app, app.navigateForward())

	// Walk back to C1 again: must land on the message moved to before
	// walking forward (2.0), not the one stored before the first walk
	// (1.0).
	walkAndUpdate(t, app, app.navigateBack())
	sel, ok := app.messagepane.SelectedMessage()
	if !ok || sel.TS != "2.0" {
		t.Fatalf("second walk back restored %+v ok=%v, want the moved-to message 2.0", sel, ok)
	}
}

// navFixtureApp wires the lookup, fresh cache, and (optionally) fetch-
// around service used by the position-restoration tests.
func navFixtureApp(t *testing.T, caches map[ids.ChannelID][]messages.MessageItem, fetchAround func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg) *App {
	t.Helper()
	app := NewApp()
	app.activeTeamID = "T1"
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		return string(channelID) + "-name", "channel", true
	})
	app.setChannelCacheReaderForTest(func(channelID ids.ChannelID) []messages.MessageItem {
		return caches[channelID]
	})
	app.setChannelSyncedAtReaderForTest(func(channelID ids.ChannelID) int64 {
		return time.Now().Unix()
	})
	if fetchAround != nil {
		setChannelFetchAroundForTest(app, fetchAround)
	}
	return app
}

// Going back after scrolling to an older message must restore THAT
// message, not the newest one in the channel.
func TestNavBackRestoresScrolledToMessage(t *testing.T) {
	app := navFixtureApp(t, map[ids.ChannelID][]messages.MessageItem{
		"C1": {{TS: "1.0", Text: "old"}, {TS: "2.0", Text: "new"}},
		"C2": {{TS: "10.0", Text: "c2"}},
	}, nil)

	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0") // scroll to the older message

	app.Update(ChannelSelectedMsg{ID: "C2", Name: "two", Type: "channel"})

	walkAndUpdate(t, app, app.navigateBack())
	sel, ok := app.messagepane.SelectedMessage()
	if !ok || sel.TS != "1.0" {
		t.Fatalf("back restored %+v ok=%v, want the scrolled-to older message 1.0, not the newest", sel, ok)
	}
}

// Departing from an open thread must record the thread AND the reply,
// and walking back must reopen that thread with the recorded reply
// selected.
func TestNavBackFromOpenThreadRecordsThreadAndReopens(t *testing.T) {
	app := navFixtureApp(t, map[ids.ChannelID][]messages.MessageItem{
		"C1": {
			{TS: "P1", ThreadTS: "P1", Text: "parent"},
			{TS: "R1", ThreadTS: "P1", Text: "reply 1"},
			{TS: "R2", ThreadTS: "P1", Text: "reply 2"},
		},
		"C2": {{TS: "10.0", Text: "c2"}},
	}, nil)
	// The thread cache supplies the replies for the reopen so the
	// reopened panel gets them without a network round-trip.
	app.SetThreadService(core.NewThreadService(core.ThreadServiceFuncs{
		CacheRead: func(channelID ids.ChannelID, threadTS ids.ThreadTS) []messages.MessageItem {
			return []messages.MessageItem{
				{TS: "P1", ThreadTS: "P1", Text: "parent"},
				{TS: "R1", ThreadTS: "P1", Text: "reply 1"},
				{TS: "R2", ThreadTS: "P1", Text: "reply 2"},
			}
		},
	}))

	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.threadPanel.SetThread(
		messages.MessageItem{TS: "P1", ThreadTS: "P1", Text: "parent"},
		[]messages.MessageItem{
			{TS: "R1", ThreadTS: "P1", Text: "reply 1"},
			{TS: "R2", ThreadTS: "P1", Text: "reply 2"},
		},
		"C1", "P1")
	app.focusedPanel = PanelThread
	app.threadPanel.SelectByIndex(0) // reading reply R1

	app.Update(ChannelSelectedMsg{ID: "C2", Name: "two", Type: "channel"})

	// The recorded entry names the thread and the reply the user was
	// reading when they left.
	stack := app.navHistory.Stack("T1")
	if stack == nil || len(stack.entries) == 0 {
		t.Fatal("expected a recorded entry for the departed channel")
	}
	entry := stack.entries[0]
	if entry.ChannelID != "C1" || entry.MessageTS != "R1" || entry.ThreadTS != "P1" {
		t.Fatalf("recorded entry = %+v, want C1 / reply R1 / thread P1", entry)
	}

	// Walking back reopens that thread with the recorded reply
	// selected (not the newest).
	walkAndUpdate(t, app, app.navigateBack())
	if !app.threadVisible {
		t.Fatal("walking back to a thread location must reopen the thread panel")
	}
	if app.threadPanel.ThreadTS() != "P1" || app.threadPanel.ChannelID() != "C1" {
		t.Fatalf("reopened thread = %s in %s, want P1 in C1", app.threadPanel.ThreadTS(), app.threadPanel.ChannelID())
	}
	if got := app.threadPanel.SelectedReply(); got == nil || got.TS != "R1" {
		t.Fatalf("reopened thread selected %+v, want the recorded reply R1", got)
	}
}

// A walk skips an entry whose channel no longer resolves, drops it,
// and continues in the same direction; the cleaned stack still walks
// forward past where the stale entry used to sit.
func TestNavWalkSkipsUnresolvableChannelAndContinues(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		if channelID == "C2" {
			return "", "", false // C2 no longer resolves
		}
		return string(channelID), "channel", true
	})

	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C2", Name: "b", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C3", Name: "c", Type: "channel"})

	cmd := app.navigateBack()
	if cmd == nil {
		t.Fatal("navigateBack returned nil cmd")
	}
	cs, ok := cmd().(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", cmd())
	}
	if cs.ID != "C1" {
		t.Fatalf("back landed on %q, want C1 (skipping unresolvable C2)", cs.ID)
	}
	stack := app.navHistory.Stack("T1")
	if got := navEntryChannelIDs(stack.entries); !reflect.DeepEqual(got, []string{"C1", "C3"}) {
		t.Fatalf("entries after skip: want [C1 C3], got %v", got)
	}
	if stack.cursor != 0 {
		t.Fatalf("cursor: want 0, got %d", stack.cursor)
	}

	cmd = app.navigateForward()
	if cmd == nil {
		t.Fatal("navigateForward returned nil cmd after stale drop")
	}
	cs, ok = cmd().(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", cmd())
	}
	if cs.ID != "C3" {
		t.Fatalf("forward landed on %q, want C3 (past the dropped C2)", cs.ID)
	}
	if stack.cursor != 1 {
		t.Fatalf("cursor after forward: want 1, got %d", stack.cursor)
	}
}

// Walking back and forward repeatedly must not grow the stack.
func TestNavWalkCyclesDoNotGrowStack(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		return string(channelID), "channel", true
	})

	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C2", Name: "b", Type: "channel"})

	stack := app.navHistory.Stack("T1")
	for i := 0; i < 5; i++ {
		walkAndUpdate(t, app, app.navigateBack())
		if len(stack.entries) != 2 {
			t.Fatalf("cycle %d back: stack grew to %d entries: %v", i+1, len(stack.entries), navEntryChannelIDs(stack.entries))
		}
		if stack.cursor != 0 {
			t.Fatalf("cycle %d back: cursor = %d, want 0", i+1, stack.cursor)
		}
		walkAndUpdate(t, app, app.navigateForward())
		if len(stack.entries) != 2 {
			t.Fatalf("cycle %d forward: stack grew to %d entries: %v", i+1, len(stack.entries), navEntryChannelIDs(stack.entries))
		}
		if stack.cursor != 1 {
			t.Fatalf("cycle %d forward: cursor = %d, want 1", i+1, stack.cursor)
		}
	}
}

// Each workspace keeps its own independent history, and walking in one
// workspace leaves the other's stack untouched.
func TestNavHistoryIsolationAcrossWorkspaces(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		return string(channelID), "channel", true
	})

	_, _ = app.Update(ChannelSelectedMsg{ID: "C1", Name: "a", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C2", Name: "b", Type: "channel"})

	app.activeTeamID = "T2"
	_, _ = app.Update(ChannelSelectedMsg{ID: "C3", Name: "c", Type: "channel"})
	_, _ = app.Update(ChannelSelectedMsg{ID: "C4", Name: "d", Type: "channel"})

	// Walk back within T2.
	cmd := app.navigateBack()
	if cmd == nil {
		t.Fatal("navigateBack returned nil cmd")
	}
	cs, ok := cmd().(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", cmd())
	}
	if cs.ID != "C3" {
		t.Fatalf("back in T2 landed on %q, want C3", cs.ID)
	}

	t2 := app.navHistory.Stack("T2")
	if t2.cursor != 0 {
		t.Fatalf("T2 cursor: want 0, got %d", t2.cursor)
	}
	t1 := app.navHistory.Stack("T1")
	if t1 == nil || t1.cursor != 1 {
		t.Fatalf("T1 cursor must be untouched, got %v", t1)
	}
	if got := navEntryChannelIDs(t1.entries); !reflect.DeepEqual(got, []string{"C1", "C2"}) {
		t.Fatalf("T1 entries must be untouched, got %v", got)
	}
}

// walkToCompletion drives a history walk through the whole pipeline as
// the program loop would: it invokes the walk command, feeds the
// synthesized FromHistory ChannelSelectedMsg through Update, drains any
// MessagesAroundLoadedMsg the pending navigation dispatches, and
// returns the resulting selection and every toast text the walk
// produced. Empty toasts means the walk completed silently.
func walkToCompletion(t *testing.T, app *App, cmd tea.Cmd) (selectedTS string, toasts []string) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a walk command, got nil")
	}
	cs, ok := cmd().(ChannelSelectedMsg)
	if !ok || !cs.FromHistory {
		t.Fatalf("walk cmd produced %T FromHistory=%v, want a FromHistory ChannelSelectedMsg", cs, ok)
	}
	_, done := app.Update(cs)
	for _, m := range drainCmd(done) {
		switch tm := m.(type) {
		case ToastMsg:
			toasts = append(toasts, tm.Text)
		case MessagesAroundLoadedMsg:
			_, nested := app.Update(tm)
			for _, m2 := range drainCmd(nested) {
				if t2, ok := m2.(ToastMsg); ok {
					toasts = append(toasts, t2.Text)
				}
			}
		}
	}
	if sel, ok := app.messagepane.SelectedMessage(); ok {
		selectedTS = sel.TS
	}
	return selectedTS, toasts
}

// A walked location whose message can no longer be found opens the
// channel, toasts "Message not found", and keeps the entry — only an
// unresolvable CHANNEL causes an entry to be dropped (Walk).
func TestWalkToMissingMessageOpensChannelToastsKeepsEntry(t *testing.T) {
	deleted := false
	app := navFixtureApp(t, nil, func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
		// The fetch-around window also lacks the deleted message.
		return MessagesAroundLoadedMsg{
			ChannelID: string(channelID),
			TargetTS:  string(ts),
			Messages:  []messages.MessageItem{{TS: "2.0", Text: "new"}, {TS: "3.0", Text: "newer"}},
		}
	})

	// Seed the stack with a departure position whose message disappears
	// before the walk restores it.
	app.setChannelCacheReaderForTest(func(channelID ids.ChannelID) []messages.MessageItem {
		switch channelID {
		case "C1":
			if deleted {
				return []messages.MessageItem{{TS: "2.0", Text: "new"}, {TS: "3.0", Text: "newer"}}
			}
			return []messages.MessageItem{{TS: "1.0", Text: "old"}, {TS: "2.0", Text: "new"}}
		case "C2":
			return []messages.MessageItem{{TS: "10.0", Text: "c2"}}
		}
		return nil
	})
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")
	deleted = true
	app.Update(ChannelSelectedMsg{ID: "C2", Name: "two", Type: "channel"})

	// Walk back to C1@1.0. The message is gone from the cache, so the
	// pending nav falls through to the surrounding-history fetch, which
	// also cannot find it.
	_, toasts := walkToCompletion(t, app, app.navigateBack())
	found := false
	for _, txt := range toasts {
		if strings.Contains(txt, "Message not found") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the not-found toast for a missing message, got toasts %v", toasts)
	}

	// The channel is open and the entry is retained in degraded form.
	if app.activeChannelID != "C1" {
		t.Fatalf("active channel = %q, want C1", app.activeChannelID)
	}
	stack := app.navHistory.Stack("T1")
	if got := navEntryChannelIDs(stack.entries); !reflect.DeepEqual(got, []string{"C1", "C2"}) {
		t.Fatalf("dead-message entry must be retained, got %v", got)
	}
	if stack.entries[0].MessageTS != "1.0" {
		t.Fatalf("retained entry must still name the missing message, got %+v", stack.entries[0])
	}
}

// The message-gone toast must fire ONLY for the message-gone case: a
// successful walk to a location whose message is present completes
// silently. (Regression guard for the twelve bogus toasts that once
// replaced every `return nil, true` in reducer_channels.go.)
func TestWalkToPresentMessageProducesNoToast(t *testing.T) {
	app := navFixtureApp(t, map[ids.ChannelID][]messages.MessageItem{
		"C1": {{TS: "1.0", Text: "old"}, {TS: "2.0", Text: "new"}},
		"C2": {{TS: "10.0", Text: "c2"}},
	}, nil)

	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")
	app.Update(ChannelSelectedMsg{ID: "C2", Name: "two", Type: "channel"})

	selected, toasts := walkToCompletion(t, app, app.navigateBack())
	if selected != "1.0" {
		t.Fatalf("walk restored %q, want 1.0", selected)
	}
	if len(toasts) != 0 {
		t.Fatalf("a successful walk must stay silent, got toasts: %v", toasts)
	}
}

// A message outside the loaded history is restored by loading the
// surrounding window — through the SAME MessagesAroundLoadedMsg arm
// that toasts when the message is gone — and that restore must stay
// silent. This pins the found path of the toast-capable arm, so the
// message-gone toast cannot come to double as a success notice.
func TestWalkToOutsideHistoryMessageRestoresSilently(t *testing.T) {
	shrunk := false
	app := navFixtureApp(t, nil, func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
		return MessagesAroundLoadedMsg{
			ChannelID: string(channelID),
			TargetTS:  string(ts),
			Messages:  []messages.MessageItem{{TS: "0.5", Text: "older"}, {TS: "1.0", Text: "old"}, {TS: "1.5", Text: "middle"}},
		}
	})
	app.setChannelCacheReaderForTest(func(channelID ids.ChannelID) []messages.MessageItem {
		switch channelID {
		case "C1":
			if shrunk {
				return []messages.MessageItem{{TS: "2.0", Text: "new"}, {TS: "3.0", Text: "newer"}}
			}
			return []messages.MessageItem{{TS: "1.0", Text: "old"}, {TS: "2.0", Text: "new"}}
		case "C2":
			return []messages.MessageItem{{TS: "10.0", Text: "c2"}}
		}
		return nil
	})

	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")
	shrunk = true // 1.0 is now outside the cached window
	app.Update(ChannelSelectedMsg{ID: "C2", Name: "two", Type: "channel"})

	selected, toasts := walkToCompletion(t, app, app.navigateBack())
	if selected != "1.0" {
		t.Fatalf("walk restored %q, want the outside-history message 1.0", selected)
	}
	if len(toasts) != 0 {
		t.Fatalf("restoring an outside-history message must stay silent, got toasts: %v", toasts)
	}
}

// Switching workspace is a departure too: the position in the
// workspace being left must be recorded, so walking its history later
// returns there rather than to the newest message.
func TestNavWorkspaceSwitchRecordsDepartingPosition(t *testing.T) {
	app := navFixtureApp(t, map[ids.ChannelID][]messages.MessageItem{
		"C1": {{TS: "1.0", Text: "old"}, {TS: "2.0", Text: "new"}},
	}, nil)
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "one", Type: "channel"})
	app.messagepane.SelectByTS("1.0")

	app.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "Beta"})

	s := app.navHistory.Stack("T1")
	if s == nil || s.cursor < 0 {
		t.Fatal("T1 has no history")
	}
	if got := s.entries[s.cursor]; got.ChannelID != "C1" || got.MessageTS != "1.0" {
		t.Fatalf("T1 current entry = %+v, want C1 at 1.0", got)
	}
}
