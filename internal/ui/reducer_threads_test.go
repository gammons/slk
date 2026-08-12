package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/ids"
)

// TestApp_OnlyThreadsViewActivationEnsuresSubscriptions pins where the
// subscriptions.thread.getView fetch is allowed to happen.
//
// The list fetcher runs on workspace-ready too, because the sidebar
// shows a Threads unread badge before the user ever opens the view —
// but that read is cache-only. Hanging the network sync off it puts a
// ~62-request paginated call back into every boot, which is what this
// task removed.
func TestApp_OnlyThreadsViewActivationEnsuresSubscriptions(t *testing.T) {
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
		t.Fatalf("workspace-ready ensured subscriptions for %s; that fetch belongs to the first Threads-view open", team)
	case <-time.After(50 * time.Millisecond):
	}

	app.activeTeamID = "T1"
	_, cmd = app.Update(ThreadsViewActivatedMsg{})
	for _, m := range drainBatch(cmd) {
		_ = m
	}
	select {
	case team := <-ensured:
		if team != "T1" {
			t.Errorf("ensured subscriptions for %q; want T1", team)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("opening the Threads view did not ensure subscriptions; the list would render from a cache nothing ever fills")
	}
}
