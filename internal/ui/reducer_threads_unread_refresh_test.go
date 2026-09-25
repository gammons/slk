package ui

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/workspace"
)

// railRefreshApp builds an App with T1 active and T2's dot lit, then
// re-points the rail reader so T2 is dark. The title still says "+1"
// because nothing has recomputed it; a message that reaches
// notifyReadStateChanged drops the "+1", one that does not leaves it.
// That is the same observation TestWorkspaceReady_RefreshesRailAndTitle
// makes for #207's refresh sites.
func railRefreshApp(t *testing.T) *App {
	t.Helper()
	app := setupAppForTitleTest(t,
		[]sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}},
		[]workspace.WorkspaceItem{
			{ID: "T1", Name: "SWAP", Initials: "SW"},
			{ID: "T2", Name: "Other", Initials: "OT"},
		},
		map[string]cache.ReadState{},
		nil,
	)
	unreads := []string{"T2"}
	// #219 renamed App.SetWorkspaceUnreadReader to SetUnreadService; the
	// rail reader is the part this test exercises, so install it directly.
	app.workspaceRail.SetUnreadReader(func() []string { return unreads })
	app.activeTeamID = "T1"
	app.notifyReadStateChanged()
	if got, want := app.windowTitle, "slk SW +1"; got != want {
		t.Fatalf("precondition: windowTitle = %q want %q", got, want)
	}
	unreads = nil
	return app
}

// TestThreadReadState_RefreshesRail pins that every message carrying a
// thread read-state change the DB has already absorbed recomputes the
// rail. The rail's thread half reads thread_subscriptions and the
// message cache (railThreadsUnread in cmd/slk), and until this nothing
// on these paths called notifyReadStateChanged, so a thread read
// elsewhere, or a reply landing in the active workspace, left the dot
// where it was until an unrelated channel event.
func TestThreadReadState_RefreshesRail(t *testing.T) {
	reply := NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "2.0", UserID: "U2", Text: "reply", ThreadTS: "1.0"},
	}
	cases := []struct {
		name string
		msg  any
	}{
		// thread_marked from Slack: OnThreadMarked wrote last_read
		// before sending this.
		{"thread marked remotely", ThreadMarkedRemoteMsg{ChannelID: "C1", ThreadTS: "1.0", LastRead: "2.0"}},
		// slk's own mark: markThreadRead wrote last_read before
		// reporting success.
		{"thread marked locally", ThreadMarkedLocalMsg{ChannelID: "C1", ThreadTS: "1.0", TS: "2.0"}},
		// The list is read from the same rows the rail reads, and its
		// badge is what the rail must agree with.
		{"threads list loaded for the active workspace", ThreadsListLoadedMsg{TeamID: "T1"}},
		// A reply in the active workspace: the WS handler cached it
		// before sending this, and a subscribed thread with a newer
		// reply is unread.
		{"thread reply in the active workspace", reply},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := railRefreshApp(t)
			app.Update(tc.msg)
			if got, want := app.windowTitle, "slk SW"; got != want {
				t.Errorf("windowTitle = %q want %q: the rail was not recomputed", got, want)
			}
		})
	}
}

// TestThreadsListLoaded_InactiveWorkspace_RefreshesRail pins the arm
// that used to return before doing anything. The list cannot go on
// screen, and the badge must stay untouched
// (TestApp_ThreadsListLoadedIgnoredForOtherWorkspace), but the message
// is the tail of a thread change dispatched while that workspace was
// active, and a workspace switch does not recompute the rail -- so
// this is that change's last chance to reach the dot.
func TestThreadsListLoaded_InactiveWorkspace_RefreshesRail(t *testing.T) {
	app := railRefreshApp(t)
	app.Update(ThreadsListLoadedMsg{
		TeamID:    "T2",
		Summaries: []cache.ThreadSummary{{ChannelID: "C9", ThreadTS: "1.0", Unread: true}},
	})
	if got, want := app.windowTitle, "slk SW"; got != want {
		t.Errorf("windowTitle = %q want %q: the rail was not recomputed", got, want)
	}
	if n := app.sidebar.ThreadsUnreadCount(); n != 0 {
		t.Errorf("ThreadsUnreadCount = %d, want 0: an inactive workspace's list must not set the badge", n)
	}
}

// TestThreadsListDirty_InactiveWorkspace_RefreshesRail pins the other
// arm that returned before doing anything. The subscription reconcile
// (boot, reconnect, wake: ensureWorkspaceThreadSubs in cmd/slk) writes
// last_read and latest_reply for every workspace and signals only
// ThreadsListDirtyMsg, so for a workspace the user is not looking at
// this message is the only word that its rows changed -- and it lands
// after WorkspaceReadyMsg's refresh, so a thread read elsewhere while
// slk was closed kept its dot until an unrelated event. No fetch may
// be scheduled: the list is not on screen.
func TestThreadsListDirty_InactiveWorkspace_RefreshesRail(t *testing.T) {
	app := railRefreshApp(t)
	app.Update(ThreadsListDirtyMsg{TeamID: "T2"})
	if got, want := app.windowTitle, "slk SW"; got != want {
		t.Errorf("windowTitle = %q want %q: the rail was not recomputed", got, want)
	}
	if app.threadsListFetchScheduled {
		t.Error("threadsListFetchScheduled = true: an inactive workspace's dirty message must not schedule a fetch")
	}
}

// TestThreadReply_OnScreenAndFocused_DoesNotRefreshRail pins the one
// exception, mirrored from the channel arms of reduceNewMessage: a
// reply the user is looking at in the open thread panel, with the
// terminal focused, stages a thread mark that clears it before
// anything would repaint the dot, so recomputing now would only flash
// the dot on.
func TestThreadReply_OnScreenAndFocused_DoesNotRefreshRail(t *testing.T) {
	app := railRefreshApp(t)
	app.threadVisible = true
	app.threadPanel.SetThread(messages.MessageItem{TS: "1.0", UserID: "U2", Text: "parent"}, nil, "C1", "1.0")
	app.terminalFocused = true
	app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "2.0", UserID: "U2", Text: "reply", ThreadTS: "1.0"},
	})
	if got, want := app.windowTitle, "slk SW +1"; got != want {
		t.Errorf("windowTitle = %q want %q: an on-screen, focused reply must not recompute the rail", got, want)
	}
}
