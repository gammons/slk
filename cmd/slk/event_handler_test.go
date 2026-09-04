package main

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

// TestOnConversationOpened_AppendsAndSends verifies that a new mpdm event
// appends a sidebar.ChannelItem + finder Item to the workspace context and
// mirrors the channelTypes/channelNames maps. db, program, and isActive are
// nil — the handler must guard all three.
func TestOnConversationOpened_AppendsAndSends(t *testing.T) {
	wctx := &WorkspaceContext{
		BotUserIDs:        map[string]bool{},
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{},
		Channels:          []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}},
		FinderItems:       []channelfinder.Item{{ID: "C1", Name: "general", Type: "channel", Joined: true}},
	}
	h := &rtmEventHandler{
		wsCtx:        wctx,
		workspaceID:  "T1",
		cfg:          config.Config{},
		channelNames: map[string]string{},
		channelTypes: map[string]string{},
		// db, program, isActive left nil — handler must guard against all three.
	}
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{ID: "G1", IsMpIM: true},
			Name:         "mpdm-alice--bob-1",
		},
	}
	h.OnConversationOpened(ch)

	if len(wctx.Channels) != 2 {
		t.Errorf("len(Channels) = %d, want 2", len(wctx.Channels))
	}
	if h.channelTypes["G1"] != "group_dm" {
		t.Errorf("channelTypes[G1] = %q, want group_dm", h.channelTypes["G1"])
	}
	if len(wctx.FinderItems) != 2 {
		t.Errorf("len(FinderItems) = %d, want 2", len(wctx.FinderItems))
	}
}

// TestOnConversationOpened_DedupesByID verifies that a re-delivered event for
// an already-known channel updates the descriptive fields (Name) in place
// instead of double-adding the row. Same-ID Slack events arrive duplicated
// in practice (e.g. im_open followed by im_created on first DM).
//
// Read state used to be mirrored on ChannelItem (UnreadCount, LastReadTS)
// and required explicit preservation across this upsert. Those fields are
// gone -- the read-state DB is the single source of truth -- so there's
// nothing left to assert on that axis here. The descriptive-field
// overwrite and FinderItems dedupe are the only behaviors that remain.
func TestOnConversationOpened_DedupesByID(t *testing.T) {
	wctx := &WorkspaceContext{
		BotUserIDs:        map[string]bool{},
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{"alice": "Alice", "bob": "Bob"},
		Channels: []sidebar.ChannelItem{
			{ID: "G1", Name: "old", Type: "group_dm"},
		},
		// Seed FinderItems so we can assert dedupe doesn't double-add.
		FinderItems: []channelfinder.Item{
			{ID: "G1", Name: "old", Type: "group_dm", Joined: true},
		},
	}
	h := &rtmEventHandler{
		wsCtx:        wctx,
		workspaceID:  "T1",
		cfg:          config.Config{},
		channelNames: map[string]string{},
		channelTypes: map[string]string{},
	}
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{ID: "G1", IsMpIM: true},
			Name:         "mpdm-alice--bob-1",
		},
	}
	h.OnConversationOpened(ch)

	if len(wctx.Channels) != 1 {
		t.Errorf("len(Channels) = %d, want 1 (deduped on ID)", len(wctx.Channels))
	}
	if wctx.Channels[0].Name != "Alice, Bob" {
		t.Errorf("Name = %q, want %q (updated descriptive field)", wctx.Channels[0].Name, "Alice, Bob")
	}
	if len(wctx.FinderItems) != 1 {
		t.Errorf("len(FinderItems) = %d, want 1 (dedupe must not double-add)", len(wctx.FinderItems))
	}
}

// TestOnConversationOpened_InactiveWorkspace_PersistsContext verifies that
// when the workspace is inactive, the handler still mutates wsCtx.Channels
// (so a workspace switch later picks up the new conversation) and does not
// panic. The program-send half of the gate is verified by code inspection
// of the isActive check; injecting a fake tea.Program for end-to-end
// assertion would require widening the rtmEventHandler abstraction
// beyond the scope of this task.
func TestOnConversationOpened_InactiveWorkspace_PersistsContext(t *testing.T) {
	wctx := &WorkspaceContext{
		BotUserIDs:        map[string]bool{},
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{},
	}
	h := &rtmEventHandler{
		wsCtx:        wctx,
		workspaceID:  "T1",
		cfg:          config.Config{},
		channelNames: map[string]string{},
		channelTypes: map[string]string{},
		isActive:     func() bool { return false },
		// program left nil; isActive=false should not be reached as a
		// panic path even when program is non-nil because the gate
		// returns before Send.
	}
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{ID: "G1", IsMpIM: true},
			Name:         "mpdm-alice--bob-1",
		},
	}
	h.OnConversationOpened(ch)

	if len(wctx.Channels) != 1 || wctx.Channels[0].ID != "G1" {
		t.Errorf("inactive workspace context not updated: %+v", wctx.Channels)
	}
	if h.channelTypes["G1"] != "group_dm" {
		t.Errorf("channelTypes not mirrored on inactive workspace: %q", h.channelTypes["G1"])
	}
}

// The OnMessage read-state matrix below replaces the pre-Task-10 trio
// (InactiveWorkspace_BumpsChannelUnreadCount, ...ThreadReplyDoesNotBumpChannel,
// ...ThreadBroadcastBumpsChannel), which asserted against the now-removed
// wctx.Channels[i].UnreadCount in-memory bump. The DB-backed assertions
// here exercise the actual contract (UpdateChannelReadState writes) and
// also cover the new active-channel suppression dimension.

func TestOnMessage_InactiveChannel_SetsHasUnread(t *testing.T) {
	db := newTestDB(t)
	_ = db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	h := &rtmEventHandler{
		db:              db,
		wsCtx:           &WorkspaceContext{},
		isActive:        func() bool { return true },
		activeChannelID: func() string { return "C2" }, // viewing a different channel
	}
	h.OnMessage("C1", "U1", "1.001", "hi", "", "", false, nil, slack.Blocks{}, nil, "", "")

	s, _ := db.GetChannelReadState("C1")
	if !s.HasUnread {
		t.Errorf("HasUnread = false, want true for inactive-channel message")
	}
}

func TestOnMessage_ActiveChannel_DoesNotSetHasUnread(t *testing.T) {
	db := newTestDB(t)
	_ = db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	h := &rtmEventHandler{
		db:              db,
		wsCtx:           &WorkspaceContext{},
		isActive:        func() bool { return true },
		activeChannelID: func() string { return "C1" }, // viewing the same channel
	}
	h.OnMessage("C1", "U1", "1.001", "hi", "", "", false, nil, slack.Blocks{}, nil, "", "")

	s, _ := db.GetChannelReadState("C1")
	if s.HasUnread {
		t.Errorf("HasUnread = true, want false (active-channel suppression)")
	}
}

func TestOnMessage_InactiveWorkspace_StillSetsHasUnread(t *testing.T) {
	db := newTestDB(t)
	_ = db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	h := &rtmEventHandler{
		db:              db,
		wsCtx:           &WorkspaceContext{},
		isActive:        func() bool { return false },
		activeChannelID: func() string { return "" },
		workspaceID:     "T1",
	}
	h.OnMessage("C1", "U1", "1.001", "hi", "", "", false, nil, slack.Blocks{}, nil, "", "")

	s, _ := db.GetChannelReadState("C1")
	if !s.HasUnread {
		t.Errorf("HasUnread = false, want true (inactive workspace)")
	}
}

func TestOnMessage_ThreadReply_DoesNotSetHasUnread(t *testing.T) {
	db := newTestDB(t)
	_ = db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	h := &rtmEventHandler{
		db:              db,
		wsCtx:           &WorkspaceContext{},
		isActive:        func() bool { return true },
		activeChannelID: func() string { return "C2" },
	}
	// threadTS != ts and not a broadcast subtype = non-broadcast thread reply.
	h.OnMessage("C1", "U1", "1.002", "reply", "1.001", "", false, nil, slack.Blocks{}, nil, "", "")

	s, _ := db.GetChannelReadState("C1")
	if s.HasUnread {
		t.Errorf("HasUnread = true, want false (non-broadcast thread reply)")
	}
}

func TestOnMessage_ThreadBroadcast_SetsHasUnread(t *testing.T) {
	db := newTestDB(t)
	_ = db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	h := &rtmEventHandler{
		db:              db,
		wsCtx:           &WorkspaceContext{},
		isActive:        func() bool { return true },
		activeChannelID: func() string { return "C2" },
	}
	h.OnMessage("C1", "U1", "1.002", "ALL", "1.001", "thread_broadcast", false, nil, slack.Blocks{}, nil, "", "")

	s, _ := db.GetChannelReadState("C1")
	if !s.HasUnread {
		t.Errorf("HasUnread = false, want true (thread_broadcast bumps channel)")
	}
}

func TestOnThreadMarked_AdvancesCursorWithoutTombstoning(t *testing.T) {
	db := newTestDB(t)
	h := &rtmEventHandler{
		db:          db,
		workspaceID: "T1",
		isActive:    func() bool { return true },
	}

	h.OnThreadMarked("C1", "1700000100.000000", "1700000150.000000")

	got, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 active sub after thread_marked, got %d", len(got))
	}
	if got[0].ChannelID != "C1" || got[0].ThreadTS != "1700000100.000000" ||
		got[0].LastRead != "1700000150.000000" || !got[0].Active {
		t.Fatalf("subscription row mismatch: %+v", got[0])
	}

	// A later cursor move must advance last_read and leave the row
	// active. Writing `active` here used to tombstone the row, making
	// the thread disappear from the Threads list.
	h.OnThreadMarked("C1", "1700000100.000000", "1700000200.000000")
	got, err = db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("thread must stay in the list after being read, got %d active", len(got))
	}
	if got[0].LastRead != "1700000200.000000" {
		t.Errorf("LastRead = %q, want 1700000200.000000", got[0].LastRead)
	}
}

// An empty last_read would erase the read cursor and make the whole
// thread render unread. UpdateThreadLastRead does not reject it, so the
// handler must drop the event outright rather than persist or dispatch.
func TestOnThreadMarked_EmptyLastReadIsIgnored(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpdateThreadLastRead("T1", "C1", "1700000100.000000", "1700000150.000000"); err != nil {
		t.Fatalf("UpdateThreadLastRead: %v", err)
	}
	h := &rtmEventHandler{
		db:          db,
		workspaceID: "T1",
		isActive:    func() bool { return true },
		// program intentionally nil: a UI dispatch here would panic,
		// pinning that the handler returns before touching it.
	}

	h.OnThreadMarked("C1", "1700000100.000000", "")

	lastRead, err := db.GetThreadLastRead("T1", "C1", "1700000100.000000")
	if err != nil {
		t.Fatalf("GetThreadLastRead: %v", err)
	}
	if lastRead != "1700000150.000000" {
		t.Errorf("LastRead = %q, want the cursor left untouched at 1700000150.000000", lastRead)
	}
}

func TestOnThreadSubscriptionChanged_UpsertsActive(t *testing.T) {
	db := newTestDB(t)
	h := &rtmEventHandler{
		db:          db,
		workspaceID: "T1",
		isActive:    func() bool { return true },
	}

	h.OnThreadSubscriptionChanged("C1", "1700000100.000000", "1700000150.000000", true)

	got, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(got) != 1 || got[0].ChannelID != "C1" || !got[0].Active {
		t.Fatalf("subscription row mismatch: %+v", got)
	}
}

func TestOnThreadSubscriptionChanged_TombstonesOnUnsubscribe(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertThreadSubscription("T1", "C1", "1700000100.000000", "1700000150.000000", true); err != nil {
		t.Fatalf("UpsertThreadSubscription: %v", err)
	}
	h := &rtmEventHandler{
		db:          db,
		workspaceID: "T1",
		isActive:    func() bool { return true },
	}

	h.OnThreadSubscriptionChanged("C1", "1700000100.000000", "1700000150.000000", false)

	got, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 active after unsubscribe, got %d: %+v", len(got), got)
	}
}

// TestOnThreadMarked_PersistsOnInactiveWorkspace guards against the
// "missing unread thread" bug: when slk is connected to multiple
// workspaces and the user is focused on another one, an incoming
// thread_marked event on the inactive workspace must STILL be
// persisted to the local thread_subscriptions cache. Without this,
// switching to the inactive workspace later would show stale read
// state (and, for newly-unread threads, no unread indicator at all
// until the next reconnect-driven reconcile).
//
// This mirrors OnMessage / OnChannelMarked which both persist to the
// cache regardless of active-workspace state and only gate the UI
// dispatch on isActive.
func TestOnThreadMarked_PersistsOnInactiveWorkspace(t *testing.T) {
	db := newTestDB(t)
	h := &rtmEventHandler{
		db:          db,
		workspaceID: "T1",
		isActive:    func() bool { return false }, // inactive workspace
		// program intentionally nil: the handler must not need it to
		// persist the DB row.
	}

	h.OnThreadMarked("C1", "1700000100.000000", "1700000150.000000")

	got, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("inactive workspace must still persist thread_marked; got %d active rows, want 1", len(got))
	}
	if got[0].ChannelID != "C1" || got[0].ThreadTS != "1700000100.000000" ||
		got[0].LastRead != "1700000150.000000" || !got[0].Active {
		t.Fatalf("subscription row mismatch on inactive workspace: %+v", got[0])
	}
}

// TestOnThreadSubscriptionChanged_PersistsOnInactiveWorkspace guards
// against the same class of bug for thread_subscribed /
// thread_unsubscribed events. Without this, a thread the user just
// got @-mentioned in on an inactive workspace would never make it
// into the local thread_subscriptions table, and the threads view
// would silently omit it on next workspace switch.
func TestOnThreadSubscriptionChanged_PersistsOnInactiveWorkspace(t *testing.T) {
	db := newTestDB(t)
	h := &rtmEventHandler{
		db:          db,
		workspaceID: "T1",
		isActive:    func() bool { return false }, // inactive workspace
	}

	h.OnThreadSubscriptionChanged("C1", "1700000100.000000", "1700000150.000000", true)

	got, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(got) != 1 || got[0].ChannelID != "C1" || !got[0].Active {
		t.Fatalf("inactive workspace must still persist thread_subscribed; got %+v, want 1 active row", got)
	}
}
