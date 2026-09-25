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
