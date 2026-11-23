package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// TestWorkspaceReadyRestoresLastChannel is the headline of the
// restore-last-channel feature: on the InitialActive workspace, startup
// opens the persisted most-recently-visited channel (LastChannelID) rather
// than always snapping to the sidebar's first entry, falling back to the
// first channel when there's no recorded visit or it no longer exists.
func TestWorkspaceReadyRestoresLastChannel(t *testing.T) {
	selectOnReady := func(lastID string) (ChannelSelectedMsg, bool) {
		a := NewApp()
		_, _ = a.Update(tea.WindowSizeMsg{Width: 200, Height: 60})
		_, cmd := a.Update(WorkspaceReadyMsg{
			TeamID:        "T1",
			InitialActive: true,
			Channels: []sidebar.ChannelItem{
				{ID: "C1", Name: "general", Type: "channel"},
				{ID: "C2", Name: "random", Type: "channel"},
			},
			LastChannelID: lastID,
		})
		if cmd == nil {
			return ChannelSelectedMsg{}, false
		}
		return findChannelSelected(cmd())
	}

	// Persisted last channel is restored.
	if sel, ok := selectOnReady("C2"); !ok || sel.ID != "C2" {
		t.Errorf("LastChannelID=C2: want C2 opened, got ok=%v id=%q", ok, sel.ID)
	}
	// No recorded visit (e.g. first run) -> first channel.
	if sel, ok := selectOnReady(""); !ok || sel.ID != "C1" {
		t.Errorf("no LastChannelID: want C1, got ok=%v id=%q", ok, sel.ID)
	}
	// Channel was archived/left since the last visit -> first channel.
	if sel, ok := selectOnReady("CGONE"); !ok || sel.ID != "C1" {
		t.Errorf("stale LastChannelID: want C1 fallback, got ok=%v id=%q", ok, sel.ID)
	}
}

// TestWorkspaceReadyOpensDefaultChannel: a configured default_channel
// wins over the persisted last-visited channel on startup, matches
// channels and private channels by name only (never a DM's display
// name), and falls back to the last-visited channel when nothing matches.
func TestWorkspaceReadyOpensDefaultChannel(t *testing.T) {
	chans := []sidebar.ChannelItem{
		{ID: "C1", Name: "rocketeers", Type: "channel"},
		{ID: "C2", Name: "engineering", Type: "private"},
		{ID: "D1", Name: "alice", Type: "dm"},
		{ID: "C3", Name: "random", Type: "channel"},
	}
	selectOnReady := func(defaultName string) string {
		a := newTestApp(t, withWindowSize(200, 60))
		_, cmd := a.Update(WorkspaceReadyMsg{
			TeamID:         "T1",
			InitialActive:  true,
			Channels:       chans,
			FinderItems:    finderItemsOf(chans),
			LastChannelID:  "C3",
			DefaultChannel: defaultName,
		})
		if cmd == nil {
			return ""
		}
		sel, _ := findChannelSelected(cmd())
		return sel.ID
	}
	for _, tc := range []struct{ name, want string }{
		{"engineering", "C2"},  // private channel, beats LastChannelID
		{"#Engineering", "C2"}, // "#" and case are ignored, as in /open
		{"", "C3"},             // unset: last visited
		{"missing", "C3"},      // no such channel: last visited
		{"alice", "C3"},        // DMs never match
	} {
		if got := selectOnReady(tc.name); got != tc.want {
			t.Errorf("DefaultChannel=%q: opened %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestWorkspaceSwitchedDefaultChannelOnFirstVisit: the first switch to a
// workspace in a session opens its default_channel; switching back later
// restores the channel last viewed there instead, and the default again
// once that channel is gone from the sidebar.
func TestWorkspaceSwitchedDefaultChannelOnFirstVisit(t *testing.T) {
	t2 := []sidebar.ChannelItem{
		{ID: "C1", Name: "rocketeers", Type: "channel"},
		{ID: "C2", Name: "engineering", Type: "channel"},
	}
	switchTo := func(a *App, team string, chans []sidebar.ChannelItem, def string) string {
		_, cmd := a.Update(WorkspaceSwitchedMsg{TeamID: team, Channels: chans, FinderItems: finderItemsOf(chans), DefaultChannel: def})
		if cmd == nil {
			return ""
		}
		sel, ok := findChannelSelected(cmd())
		if !ok {
			return ""
		}
		_, _ = a.Update(sel) // make it the active channel, as the runtime does
		return sel.ID
	}
	a := newTestApp(t, withWindowSize(200, 60))
	t1 := []sidebar.ChannelItem{{ID: "CA", Name: "general", Type: "channel"}}
	switchTo(a, "T1", t1, "")

	if got := switchTo(a, "T2", t2, "engineering"); got != "C2" {
		t.Fatalf("first switch to T2: opened %q, want default C2", got)
	}
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "rocketeers", Type: "channel"})
	switchTo(a, "T1", t1, "")
	if got := switchTo(a, "T2", t2, "engineering"); got != "C1" {
		t.Errorf("switch back to T2: opened %q, want last viewed C1", got)
	}

	// The channel last viewed there is gone (left elsewhere): the
	// default, not the first sidebar row.
	switchTo(a, "T1", t1, "")
	if got := switchTo(a, "T2", t2[1:], "engineering"); got != "C2" {
		t.Errorf("saved channel gone: opened %q, want default C2", got)
	}
}

// A default_channel that names no sidebar channel changes nothing: the
// first switch opens the first sidebar row, as without the setting.
func TestWorkspaceSwitchedUnknownDefaultChannel(t *testing.T) {
	a := newTestApp(t, withWindowSize(200, 60))
	chans := []sidebar.ChannelItem{
		{ID: "C1", Name: "rocketeers", Type: "channel"},
		{ID: "C2", Name: "engineering", Type: "channel"},
	}
	_, cmd := a.Update(WorkspaceSwitchedMsg{TeamID: "T2", Channels: chans, FinderItems: finderItemsOf(chans), DefaultChannel: "missing"})
	if cmd == nil {
		t.Fatal("switch returned no command")
	}
	if sel, ok := findChannelSelected(cmd()); !ok || sel.ID != "C1" {
		t.Errorf("unknown default: opened ok=%v id=%q, want C1", ok, sel.ID)
	}
}

// finderItemsOf is the finder rows the runtime sends beside a sidebar
// list: one per conversation, same ID, name and type.
func finderItemsOf(chans []sidebar.ChannelItem) []core.ChannelFinderItem {
	items := make([]core.ChannelFinderItem, len(chans))
	for i, ch := range chans {
		items[i] = core.ChannelFinderItem{ID: ch.ID, Name: ch.Name, Type: ch.Type, Joined: true}
	}
	return items
}
