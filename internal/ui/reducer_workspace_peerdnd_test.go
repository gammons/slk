package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/sidebar"
)

// TestWorkspaceSwitched_RefreshesPeerDNDAfterSwitchApplies pins that
// reduceWorkspaceSwitched appends WorkspaceSwitchedMsg.RefreshPeerDND to
// the returned batch, and that a UserDNDChangeMsg it later delivers
// reaches the DM row.
func TestWorkspaceSwitched_RefreshesPeerDNDAfterSwitchApplies(t *testing.T) {
	app := NewApp()
	_, cmd := app.Update(WorkspaceSwitchedMsg{
		TeamID:   "T2",
		Channels: []sidebar.ChannelItem{{ID: "D1", Name: "alice", Type: "dm", DMUserID: "U1"}},
		RefreshPeerDND: func() tea.Msg {
			return UserDNDChangeMsg{TeamID: "T2", UserID: "U1", Enabled: true}
		},
	})

	// Find the DND result among the batch's leaf messages (which also
	// include ChannelSelectedMsg / ThreadsListLoadedMsg) and deliver it
	// the way bubbletea would once the cmd resolves.
	var dnd UserDNDChangeMsg
	found := false
	for _, m := range drainBatch(cmd) {
		if d, ok := m.(UserDNDChangeMsg); ok {
			dnd, found = d, true
		}
	}
	if !found {
		t.Fatal("switch cmd did not include RefreshPeerDND's message")
	}
	app.Update(dnd)

	for _, it := range app.sidebar.Items() {
		if it.ID == "D1" {
			if !it.Status.DND {
				t.Fatalf("DM row DND not applied after switch: %+v", it.Status)
			}
			return
		}
	}
	t.Fatal("D1 not found in sidebar after switch")
}

// TestWorkspaceSwitched_RunsAfterSwitchOnceActive: cmd/slk starts
// reporting the workspace's newly learned names from AfterSwitch, and
// the reducer drops UserResolvedMsg for any team but the active one. So
// AfterSwitch must be in the returned batch (not run inline), and a
// name it reports must land, i.e. activeTeamID is already the new team
// when it runs.
func TestWorkspaceSwitched_RunsAfterSwitchOnceActive(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	ranInline := false
	_, cmd := app.Update(WorkspaceSwitchedMsg{
		TeamID:    "T2",
		UserNames: map[string]string{},
		AfterSwitch: func() tea.Msg {
			ranInline = true
			return UserResolvedMsg{TeamID: "T2", UserID: "U9", DisplayName: "Nina"}
		},
	})
	if ranInline {
		t.Fatal("AfterSwitch ran inside Update; it must be returned as a cmd")
	}

	var resolved *UserResolvedMsg
	for _, m := range drainBatch(cmd) {
		if r, ok := m.(UserResolvedMsg); ok {
			resolved = &r
		}
	}
	if resolved == nil {
		t.Fatal("switch cmd did not include AfterSwitch's message")
	}
	app.Update(*resolved)
	if got := app.threadPanel.UserNames()["U9"]; got != "Nina" {
		t.Errorf("thread panel name for U9 = %q after AfterSwitch's UserResolvedMsg; want Nina (dropped as another team's?)", got)
	}
}
