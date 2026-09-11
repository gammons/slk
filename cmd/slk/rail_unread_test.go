package main

import (
	"reflect"
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/service"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/workspace"
	"github.com/slack-go/slack"
)

func unreadRow(workspaceID, channelID string) cache.UnreadChannel {
	return cache.UnreadChannel{
		WorkspaceID: workspaceID,
		ChannelID:   channelID,
		State:       cache.ReadState{LastReadTS: "1.0", HasUnread: true},
	}
}

func railLookup(all map[string]*WorkspaceContext) func(string) *WorkspaceContext {
	return func(teamID string) *WorkspaceContext { return all[teamID] }
}

func TestRailUnreadWorkspaces(t *testing.T) {
	cases := []struct {
		name   string
		unread []cache.UnreadChannel
		all    map[string]*WorkspaceContext
		want   []string
	}{
		{
			name:   "muted channel does not light the rail",
			unread: []cache.UnreadChannel{unreadRow("T1", "C1")},
			all: map[string]*WorkspaceContext{
				"T1": {Channels: []sidebar.ChannelItem{{ID: "C1", IsMuted: true}}},
			},
			want: nil,
		},
		{
			// Guards against over-filtering: one muted unread must not
			// hide the unmuted one next to it.
			name:   "muted and unmuted unread together still light",
			unread: []cache.UnreadChannel{unreadRow("T1", "C1"), unreadRow("T1", "C2")},
			all: map[string]*WorkspaceContext{
				"T1": {Channels: []sidebar.ChannelItem{{ID: "C1", IsMuted: true}, {ID: "C2"}}},
			},
			want: []string{"T1"},
		},
		{
			name:   "each workspace once, in row order",
			unread: []cache.UnreadChannel{unreadRow("T1", "C1"), unreadRow("T1", "C2"), unreadRow("T2", "C3")},
			all: map[string]*WorkspaceContext{
				"T1": {Channels: []sidebar.ChannelItem{{ID: "C1"}, {ID: "C2"}}},
				"T2": {Channels: []sidebar.ChannelItem{{ID: "C3"}}},
			},
			want: []string{"T1", "T2"},
		},
		{
			// Still connecting, or failed to connect: no channel list
			// to check against, so the pre-change behaviour holds.
			name:   "workspace the router does not know keeps its dot",
			unread: []cache.UnreadChannel{unreadRow("T1", "C1")},
			all:    map[string]*WorkspaceContext{},
			want:   []string{"T1"},
		},
		{
			name:   "no unread rows",
			unread: nil,
			all:    map[string]*WorkspaceContext{"T1": {}},
			want:   nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := railUnreadWorkspaces(tc.unread, railLookup(tc.all))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("railUnreadWorkspaces = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRailUnreadWorkspaces_MuteStoreNotReady pins the conservative
// default end to end through the production item builder: an item
// built while the MuteStore has not bootstrapped carries
// IsMuted=false, so the rail lights; once the store learns the channel
// is muted and the item is refreshed (as refreshMutedForActive does on
// pref_change), the same row goes dark.
func TestRailUnreadWorkspaces_MuteStoreNotReady(t *testing.T) {
	wctx := &WorkspaceContext{
		MuteStore:         service.NewMuteStore(), // never bootstrapped: Ready() == false
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{},
		BotUserIDs:        map[string]bool{},
	}
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{ID: "C1"},
			Name:         "firehose",
		},
	}
	item, _ := buildChannelItem(ch, wctx, config.Config{}, "T1")
	wctx.Channels = []sidebar.ChannelItem{item}
	all := map[string]*WorkspaceContext{"T1": wctx}
	unread := []cache.UnreadChannel{unreadRow("T1", "C1")}

	if got := railUnreadWorkspaces(unread, railLookup(all)); !reflect.DeepEqual(got, []string{"T1"}) {
		t.Fatalf("store not ready: got %v, want [T1] (assume nothing is muted)", got)
	}

	wctx.MuteStore.ApplyPrefChange("muted_channels", "C1")
	wctx.Channels[0].IsMuted = wctx.MuteStore.IsMuted("C1")
	if got := railUnreadWorkspaces(unread, railLookup(all)); got != nil {
		t.Fatalf("store ready and C1 muted: got %v, want none", got)
	}
}

// TestRailUnreadWorkspaces_RailAndTitleAgree wires the reader's output
// into a workspace.Model the way App does and checks that the dots the
// rail lights and the "+N" OtherUnreadCount reports are the same set,
// which is the invariant OtherUnreadCount's doc comment promises.
func TestRailUnreadWorkspaces_RailAndTitleAgree(t *testing.T) {
	all := map[string]*WorkspaceContext{
		"T1": {Channels: []sidebar.ChannelItem{{ID: "C1", IsMuted: true}}},
		"T2": {Channels: []sidebar.ChannelItem{{ID: "C2"}}},
		"T3": {Channels: []sidebar.ChannelItem{{ID: "C3", IsMuted: true}, {ID: "C4"}}},
	}
	unread := []cache.UnreadChannel{
		unreadRow("T1", "C1"), unreadRow("T2", "C2"), unreadRow("T3", "C3"), unreadRow("T3", "C4"),
	}
	ids := railUnreadWorkspaces(unread, railLookup(all))

	m := workspace.New([]workspace.WorkspaceItem{{ID: "T1"}, {ID: "T2"}, {ID: "T3"}}, 1)
	m.SetUnreadReader(func() []string { return ids })
	m.RefreshUnreads()

	// Rows 1, 3, 5: see workspace.Model.ClickAt for the rail's row layout.
	wantLit := map[string]bool{"T1": false, "T2": true, "T3": true}
	for i, id := range []string{"T1", "T2", "T3"} {
		item, ok := m.ClickAt(1 + 2*i)
		if !ok || item.ID != id {
			t.Fatalf("ClickAt(%d) = %+v, %v; want item %s", 1+2*i, item, ok, id)
		}
		if item.HasUnread != wantLit[id] {
			t.Errorf("%s HasUnread = %v, want %v", id, item.HasUnread, wantLit[id])
		}
	}
	// T2 is active, so only T3 counts toward "+N".
	if got := m.OtherUnreadCount("T2"); got != 1 {
		t.Errorf("OtherUnreadCount(T2) = %d, want 1", got)
	}
}
