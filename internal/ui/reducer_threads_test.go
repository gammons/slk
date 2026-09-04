package ui

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// TestApp_WorkspaceReadyAndActivationBothEnsureSubscriptions pins where
// the subscriptions.thread.getView fetch is triggered.
//
// Boot is a trigger because the socket replays nothing: an app that
// was closed (or asleep) for days missed every thread_subscription_
// changed event, and rendering the threads view from the days-old
// SQLite snapshot is the staleness this fetch fixes. Activation stays
// a trigger as a safety net. Collapsing both to one actual network
// sweep per workspace is the main-package gate's job
// (threadSubsGate) — the reducer deliberately fires unconditionally
// and stays dumb about throttling.
func TestApp_WorkspaceReadyAndActivationBothEnsureSubscriptions(t *testing.T) {
	app := NewApp()
	ensured := make(chan string, 4)
	app.SetThreadService(NewThreadService(ThreadServiceFuncs{
		ListFetch: func(teamID ids.TeamID) tea.Msg {
			return ThreadsListLoadedMsg{TeamID: string(teamID)}
		},
		EnsureSubscriptions: func(teamID ids.TeamID) {
			ensured <- string(teamID)
		},
	}))

	_, cmd := app.Update(WorkspaceReadyMsg{TeamID: "T1", TeamName: "Test", InitialActive: true})
	for _, m := range drainBatch(cmd) {
		_ = m
	}
	select {
	case team := <-ensured:
		if team != "T1" {
			t.Errorf("workspace-ready ensured subscriptions for %q; want T1", team)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("workspace-ready did not ensure subscriptions; a workspace slk never opens the Threads view on stays stale for the whole session")
	}

	app.activeTeamID = "T1"
	_, cmd = app.Update(ThreadsViewActivatedMsg{})
	for _, m := range drainBatch(cmd) {
		_ = m
	}
	select {
	case team := <-ensured:
		if team != "T1" {
			t.Errorf("activation ensured subscriptions for %q; want T1", team)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("opening the Threads view did not ensure subscriptions")
	}
}

func TestThreadMarkedLocalMsg_SuccessAppliesCursor(t *testing.T) {
	app := NewApp()
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: true},
	})

	_, _ = app.Update(ThreadMarkedLocalMsg{ChannelID: "C1", ThreadTS: "P1", TS: "5.000000"})

	for _, s := range app.threadsView.Summaries() {
		if s.ThreadTS == "P1" && s.Unread {
			t.Error("a successful thread mark must clear the unread flag")
		}
	}
}

func TestThreadMarkedLocalMsg_FailureLeavesFlagAlone(t *testing.T) {
	app := NewApp()
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: true},
	})

	_, _ = app.Update(ThreadMarkedLocalMsg{
		ChannelID: "C1", ThreadTS: "P1", TS: "5.000000",
		Err: errors.New("subscriptions.thread.mark: thread_not_found"),
	})

	for _, s := range app.threadsView.Summaries() {
		if s.ThreadTS == "P1" && !s.Unread {
			t.Error("a rejected thread mark must leave the thread unread")
		}
	}
}

func TestThreadRepliesLoaded_ReturnsMarkCmd(t *testing.T) {
	app := NewApp()
	var got []string
	app.SetThreadService(NewThreadService(ThreadServiceFuncs{
		Mark: func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) tea.Cmd {
			got = append(got, string(channelID)+"/"+string(threadTS)+"/"+string(ts))
			return nil
		},
	}))
	app.threadVisible = true
	app.threadPanel.SetThread(messages.MessageItem{TS: "P1"}, nil, "C1", "P1")

	_, _ = app.Update(ThreadRepliesLoadedMsg{
		ThreadTS: "P1",
		Replies:  []messages.MessageItem{{TS: "R1"}, {TS: "R5"}},
	})

	if len(got) != 1 || got[0] != "C1/P1/R5" {
		t.Fatalf("Mark calls = %v, want one call marking up to the newest reply", got)
	}
}
