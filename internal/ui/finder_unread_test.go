package ui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// Ctrl+P lists the conversations the sidebar shows as unread before the
// read ones, and a muted unread channel (no sidebar dot) stays in
// recency order.
func TestChannelFinderListsUnreadFirst(t *testing.T) {
	a := newTestApp(t,
		withActiveTeam("T1"),
		withChannels(
			sidebar.ChannelItem{ID: "C1", Name: "alpha", Type: "channel"},
			sidebar.ChannelItem{ID: "C2", Name: "beta", Type: "channel"},
			sidebar.ChannelItem{ID: "C3", Name: "gamma", Type: "channel", IsMuted: true},
			sidebar.ChannelItem{ID: "C4", Name: "delta", Type: "channel"},
		),
	)
	a.channelFinder.SetItems([]core.ChannelFinderItem{
		{ID: "C1", Name: "alpha", Type: "channel", Joined: true, LastVisited: 400},
		{ID: "C2", Name: "beta", Type: "channel", Joined: true, LastVisited: 300},
		{ID: "C3", Name: "gamma", Type: "channel", Joined: true, LastVisited: 200},
		{ID: "C4", Name: "delta", Type: "channel", Joined: true, LastVisited: 100},
	})
	a.setReadStateReaderForTest(func() map[string]core.ReadState {
		return map[string]core.ReadState{
			"C3": {HasUnread: true},
			"C4": {HasUnread: true},
		}
	})

	pressKey(t, a, keyMod('p', tea.ModCtrl))
	if a.mode != ModeChannelFinder {
		t.Fatalf("mode = %v, want ModeChannelFinder", a.mode)
	}
	if got, want := finderIDs(a), []string{"C4", "C1", "C2", "C3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("empty query order = %v, want %v", got, want)
	}
}

func finderIDs(a *App) []string {
	var ids []string
	for _, it := range a.channelFinder.FilteredItems() {
		if !it.Synthetic {
			ids = append(ids, it.ID)
		}
	}
	return ids
}
