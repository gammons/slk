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

// noThreadsUnread is the thread half for the channel-only cases: no
// workspace has an unread subscribed thread.
func noThreadsUnread(_, _ string) bool { return false }

// seedSubscribedThread writes one active subscription for T1 the way
// the getView reconcile does (last_read and latest_reply both set),
// then caches the given replies on top of it, so a test can build
// either shape ListSubscribedThreads' predicate distinguishes: the
// authoritative watermark (latest_reply > last_read) or a cached
// reply newer than both.
func seedSubscribedThread(t *testing.T, db *cache.DB, lastRead, latestReply string, replies ...cache.Message) {
	t.Helper()
	const channelID, threadTS = "D1", "1700000100.000000"
	if err := db.ReconcileThreadSubscriptions("T1", []cache.ThreadSubscription{{
		WorkspaceID: "T1", ChannelID: channelID, ThreadTS: threadTS,
		LastRead: lastRead, LatestReply: latestReply, Active: true,
	}}); err != nil {
		t.Fatalf("ReconcileThreadSubscriptions: %v", err)
	}
	for _, r := range replies {
		r.WorkspaceID, r.ChannelID, r.ThreadTS = "T1", channelID, threadTS
		if err := db.UpsertMessage(r); err != nil {
			t.Fatalf("UpsertMessage %s: %v", r.TS, err)
		}
	}
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
			got := railUnreadWorkspaces(tc.unread, nil, railLookup(tc.all), noThreadsUnread)
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

	if got := railUnreadWorkspaces(unread, nil, railLookup(all), noThreadsUnread); !reflect.DeepEqual(got, []string{"T1"}) {
		t.Fatalf("store not ready: got %v, want [T1] (assume nothing is muted)", got)
	}

	wctx.MuteStore.ApplyPrefChange("muted_channels", "C1")
	wctx.Channels[0].IsMuted = wctx.MuteStore.IsMuted("C1")
	if got := railUnreadWorkspaces(unread, nil, railLookup(all), noThreadsUnread); got != nil {
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
		"T4": {Channels: []sidebar.ChannelItem{{ID: "C5"}}},
	}
	unread := []cache.UnreadChannel{
		unreadRow("T1", "C1"), unreadRow("T2", "C2"), unreadRow("T3", "C3"), unreadRow("T3", "C4"),
	}
	// T4 has nothing unread in its channel list; only a thread.
	threadsUnread := func(teamID, _ string) bool { return teamID == "T4" }
	teamIDs := []string{"T1", "T2", "T3", "T4"}
	ids := railUnreadWorkspaces(unread, teamIDs, railLookup(all), threadsUnread)

	m := workspace.New([]workspace.WorkspaceItem{{ID: "T1"}, {ID: "T2"}, {ID: "T3"}, {ID: "T4"}}, 1)
	m.SetUnreadReader(func() []string { return ids })
	m.RefreshUnreads()

	// Rows 1, 3, 5, 7: see workspace.Model.ClickAt for the rail's row layout.
	wantLit := map[string]bool{"T1": false, "T2": true, "T3": true, "T4": true}
	for i, id := range teamIDs {
		item, ok := m.ClickAt(1 + 2*i)
		if !ok || item.ID != id {
			t.Fatalf("ClickAt(%d) = %+v, %v; want item %s", 1+2*i, item, ok, id)
		}
		if item.HasUnread != wantLit[id] {
			t.Errorf("%s HasUnread = %v, want %v", id, item.HasUnread, wantLit[id])
		}
	}
	// T2 is active, so T3 (channel) and T4 (thread) count toward "+N".
	if got := m.OtherUnreadCount("T2"); got != 2 {
		t.Errorf("OtherUnreadCount(T2) = %d, want 2", got)
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
	if got := railUnreadWorkspaces(unread, nil, railLookup(all), noThreadsUnread); got != nil {
		t.Errorf("archived channel lit the rail: got %v, want none", got)
	}
}

// TestRailUnreadWorkspaces_Threads pins the thread half of the rail
// predicate against the real cache: a workspace with nothing visibly
// unread in its channel list still lights when one of its subscribed
// threads is unread by ListSubscribedThreads' rule, and stays dark
// when that rule says read. The self-authored case is the one
// suppression the rule has; the cached-reply case is the field shape
// (two Betterleave DM threads whose latest_reply equalled last_read
// while a newer reply sat in the cache), which a plain
// latest_reply > last_read query misses.
func TestRailUnreadWorkspaces_Threads(t *testing.T) {
	const self = "USELF"
	reply := func(ts, user string) cache.Message {
		return cache.Message{TS: ts, UserID: user, Text: "reply"}
	}
	cases := []struct {
		name        string
		lastRead    string
		latestReply string
		replies     []cache.Message
		unread      []cache.UnreadChannel
		channels    []sidebar.ChannelItem
		want        []string
	}{
		{
			name:        "unread thread by the getView watermark, no unread channels",
			lastRead:    "1700000150.000000",
			latestReply: "1700000200.000000",
			want:        []string{"T1"},
		},
		{
			name:        "unread thread by a cached reply newer than the watermark",
			lastRead:    "1700000150.000000",
			latestReply: "1700000150.000000",
			replies:     []cache.Message{reply("1700000200.000000", "UOTHER")},
			want:        []string{"T1"},
		},
		{
			name:        "newest cached reply is self-authored: read",
			lastRead:    "1700000150.000000",
			latestReply: "1700000150.000000",
			replies:     []cache.Message{reply("1700000200.000000", self)},
			want:        nil,
		},
		{
			name:        "thread fully read",
			lastRead:    "1700000200.000000",
			latestReply: "1700000200.000000",
			replies:     []cache.Message{reply("1700000200.000000", "UOTHER")},
			want:        nil,
		},
		{
			// #207's membership rule is for channel rows: a channel the
			// sidebar cannot show has no dot to explain. It does not
			// apply to threads, because the Threads badge does not
			// filter on wctx.Channels either; the thread is on screen
			// in the Threads view whether or not its channel is listed.
			name:        "thread in a channel absent from the workspace list still lights",
			lastRead:    "1700000150.000000",
			latestReply: "1700000200.000000",
			channels:    []sidebar.ChannelItem{{ID: "C1"}},
			want:        []string{"T1"},
		},
		{
			// Threads are not subject to channel mute: the muted
			// channel keeps the channel half dark, the thread lights.
			name:        "muted unread channel plus unread thread",
			lastRead:    "1700000150.000000",
			latestReply: "1700000200.000000",
			unread:      []cache.UnreadChannel{unreadRow("T1", "C1")},
			channels:    []sidebar.ChannelItem{{ID: "C1", IsMuted: true}},
			want:        []string{"T1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			seedSubscribedThread(t, db, tc.lastRead, tc.latestReply, tc.replies...)
			all := map[string]*WorkspaceContext{"T1": {UserID: self, Channels: tc.channels}}
			got := railUnreadWorkspaces(tc.unread, []string{"T1"}, railLookup(all), railThreadsUnread(db))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("railUnreadWorkspaces = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRailUnreadWorkspaces_ThreadsUnknownWorkspace pins the nil-wctx
// default for the thread half: while a workspace is still connecting
// its self user ID is unknown, so the self-authored suppression cannot
// apply and any unread subscribed thread lights it, the same
// "unknown, so light it" choice the channel half makes.
func TestRailUnreadWorkspaces_ThreadsUnknownWorkspace(t *testing.T) {
	db := newTestDB(t)
	seedSubscribedThread(t, db, "1700000150.000000", "1700000200.000000")
	got := railUnreadWorkspaces(nil, []string{"T1"}, railLookup(nil), railThreadsUnread(db))
	if !reflect.DeepEqual(got, []string{"T1"}) {
		t.Errorf("railUnreadWorkspaces = %v, want [T1]", got)
	}
}
