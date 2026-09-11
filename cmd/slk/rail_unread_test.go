package main

import (
	"reflect"
	"testing"

	"github.com/gammons/slk/internal/bootstrap"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/service"
	"github.com/gammons/slk/internal/slack/boot"
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
		{
			// A channel the sidebar cannot show has no dot to explain
			// this one and no keystroke to clear it, so it must not
			// light the rail. The field case was an archived channel;
			// see TestRailUnreadWorkspaces_ArchivedChannelInCache.
			name:   "unread row for a channel not in the workspace list does not light",
			unread: []cache.UnreadChannel{unreadRow("T1", "C9")},
			all: map[string]*WorkspaceContext{
				"T1": {Channels: []sidebar.ChannelItem{{ID: "C1"}}},
			},
			want: nil,
		},
		{
			name:   "unlisted row next to a listed unread still lights",
			unread: []cache.UnreadChannel{unreadRow("T1", "C1"), unreadRow("T1", "C9")},
			all: map[string]*WorkspaceContext{
				"T1": {Channels: []sidebar.ChannelItem{{ID: "C1"}}},
			},
			want: []string{"T1"},
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

// TestRailUnreadWorkspaces_ArchivedChannelInCache is the synthetic
// repro for the field case, built the way production builds it rather
// than by hand: hydrateFirstSight caches every conversation userBoot
// names, archived included; the sidebar list comes from
// bootConversations, which drops the archived one; and the counts
// snapshot then marks the archived channel unread with Slack's
// never-opened last_read sentinel. Before the membership rule the
// resulting row lit the rail with nothing visible to account for it.
func TestRailUnreadWorkspaces_ArchivedChannelInCache(t *testing.T) {
	db, err := cache.New(":memory:")
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	defer db.Close()

	res := &bootstrap.Result{
		Channels: []boot.Channel{
			{ID: "C1", Name: "general", IsChannel: true},
			{ID: "C2", Name: "amtrx-vendor-connect", IsChannel: true, IsPrivate: true, IsArchived: true},
		},
	}
	hydrateFirstSight(db, "T1", res)
	if _, err := db.GetChannel("C2"); err != nil {
		t.Fatalf("archived channel was not cached; this test no longer exercises the repro: %v", err)
	}

	wctx := &WorkspaceContext{
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{},
		BotUserIDs:        map[string]bool{},
	}
	for _, ch := range bootConversations(res) {
		item, _ := buildChannelItem(ch, wctx, config.Config{}, "T1")
		wctx.Channels = append(wctx.Channels, item)
	}
	if len(wctx.Channels) != 1 || wctx.Channels[0].ID != "C1" {
		t.Fatalf("sidebar list = %+v; want only C1 (bootConversations drops archived)", wctx.Channels)
	}

	if err := db.ReplaceWorkspaceReadState("T1", []cache.ChannelReadStateUpdate{
		{ChannelID: "C2", LastReadTS: "0000000000.000000", HasUnread: true},
	}); err != nil {
		t.Fatalf("ReplaceWorkspaceReadState: %v", err)
	}
	unread, err := db.UnreadChannels()
	if err != nil {
		t.Fatalf("UnreadChannels: %v", err)
	}
	if len(unread) != 1 || unread[0].ChannelID != "C2" {
		t.Fatalf("UnreadChannels = %+v; want the archived row alone", unread)
	}

	all := map[string]*WorkspaceContext{"T1": wctx}
	if got := railUnreadWorkspaces(unread, railLookup(all)); got != nil {
		t.Errorf("archived channel lit the rail: got %v, want none", got)
	}
}
