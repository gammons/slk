package ui

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/workspace"
)

// TestWorkspaceReady_RefreshesRailAndTitle pins that a workspace
// finishing its connect recomputes the rail and the title. The rail
// reader keeps a workspace's cached dot until the router knows the
// workspace; once connected the answer can change (the cached dot was
// held up by a muted or unlisted channel), and before this nothing
// recomputed it until an unrelated read-state event.
func TestWorkspaceReady_RefreshesRailAndTitle(t *testing.T) {
	app := setupAppForTitleTest(t,
		[]sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}},
		[]workspace.WorkspaceItem{
			{ID: "T1", Name: "SWAP", Initials: "SW"},
			{ID: "T2", Name: "Other", Initials: "OT"},
		},
		map[string]cache.ReadState{"C1": {HasUnread: true}},
		nil,
	)
	workspaceUnreads := []string{"T1", "T2"}
	app.SetWorkspaceUnreadReader(func() []string { return workspaceUnreads })
	app.activeTeamID = "T1"
	app.notifyReadStateChanged()
	if got, want := app.windowTitle, "slk SW (1) +1"; got != want {
		t.Fatalf("before T2 connects: windowTitle = %q want %q", got, want)
	}

	// T2 connects. With its channel list known, the reader no longer
	// counts it: its only unread channel turns out to be muted.
	workspaceUnreads = []string{"T1"}
	app.Update(WorkspaceReadyMsg{TeamID: "T2", TeamName: "Other"})

	if got, want := app.windowTitle, "slk SW (1)"; got != want {
		t.Errorf("after T2 connects: windowTitle = %q want %q", got, want)
	}
	// Rail rows are 1, 3, 5, ...; see workspace.Model.ClickAt.
	if item, ok := app.workspaceRail.ClickAt(3); !ok || item.ID != "T2" || item.HasUnread {
		t.Errorf("rail T2 after ready = %+v, %v; want dark", item, ok)
	}
}

// TestSectionsRefreshed_ActiveWorkspace_RefreshesTitle pins the active
// twin of muteRefreshMsg's inactive case: a SectionsRefreshedMsg that
// mutes an unread channel must drop it from "(N)" and $SLK_UNREAD, not
// only from the sidebar's dots.
func TestSectionsRefreshed_ActiveWorkspace_RefreshesTitle(t *testing.T) {
	app := setupAppForTitleTest(t,
		[]sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}},
		[]workspace.WorkspaceItem{{ID: "T1", Name: "SWAP", Initials: "SW"}},
		map[string]cache.ReadState{"C1": {HasUnread: true}},
		[]string{"T1"},
	)
	app.activeTeamID = "T1"
	app.notifyReadStateChanged()
	if got, want := app.windowTitle, "slk SW (1)"; got != want {
		t.Fatalf("before mute: windowTitle = %q want %q", got, want)
	}

	app.Update(SectionsRefreshedMsg{
		TeamID:   "T1",
		Channels: []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel", IsMuted: true}},
	})

	if got, want := app.windowTitle, "slk SW"; got != want {
		t.Errorf("after mute: windowTitle = %q want %q", got, want)
	}
}
