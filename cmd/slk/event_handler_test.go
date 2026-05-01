package main

import (
	"testing"

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
// an already-known channel updates the descriptive fields (Name) but preserves
// live unread state (UnreadCount, LastReadTS). Same-ID Slack events arrive
// duplicated in practice (e.g. im_open followed by im_created on first DM).
func TestOnConversationOpened_DedupesByID(t *testing.T) {
	wctx := &WorkspaceContext{
		BotUserIDs:        map[string]bool{},
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{"alice": "Alice", "bob": "Bob"},
		Channels: []sidebar.ChannelItem{
			{ID: "G1", Name: "old", Type: "group_dm", UnreadCount: 5, LastReadTS: "1700000000.000000"},
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
	if wctx.Channels[0].UnreadCount != 5 {
		t.Errorf("UnreadCount = %d, want 5 (preserved across update)", wctx.Channels[0].UnreadCount)
	}
	if wctx.Channels[0].LastReadTS != "1700000000.000000" {
		t.Errorf("LastReadTS = %q, want preserved", wctx.Channels[0].LastReadTS)
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

func TestOnMessage_InactiveWorkspace_BumpsChannelUnreadCount(t *testing.T) {
	wctx := &WorkspaceContext{
		BotUserIDs: map[string]bool{},
		UserNames:  map[string]string{},
		Channels: []sidebar.ChannelItem{
			{ID: "D1", Name: "alice", Type: "dm", UnreadCount: 0},
			{ID: "C1", Name: "general", Type: "channel"},
		},
	}
	h := &rtmEventHandler{
		wsCtx:        wctx,
		workspaceID:  "T1",
		channelNames: map[string]string{"D1": "alice", "C1": "general"},
		channelTypes: map[string]string{"D1": "dm", "C1": "channel"},
		isActive:     func() bool { return false },
		// db, program, notifier left nil — handler must guard.
	}
	h.OnMessage("D1", "U2", "1700000001.000000", "hi", "", "", false, nil, slack.Blocks{}, nil)

	for _, ch := range wctx.Channels {
		if ch.ID == "D1" && ch.UnreadCount != 1 {
			t.Errorf("D1 UnreadCount = %d, want 1", ch.UnreadCount)
		}
	}
}

func TestOnMessage_InactiveWorkspace_ThreadReplyDoesNotBumpChannel(t *testing.T) {
	wctx := &WorkspaceContext{
		Channels: []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}},
	}
	h := &rtmEventHandler{
		wsCtx:        wctx,
		workspaceID:  "T1",
		channelNames: map[string]string{"C1": "general"},
		channelTypes: map[string]string{"C1": "channel"},
		isActive:     func() bool { return false },
	}
	// thread_ts != ts and subtype != "thread_broadcast" → reply, must not bump.
	h.OnMessage("C1", "U2", "1700000002.000000", "reply", "1700000001.000000", "", false, nil, slack.Blocks{}, nil)

	if wctx.Channels[0].UnreadCount != 0 {
		t.Errorf("thread reply bumped channel unread; got %d", wctx.Channels[0].UnreadCount)
	}
}

func TestOnMessage_InactiveWorkspace_ThreadBroadcastBumpsChannel(t *testing.T) {
	wctx := &WorkspaceContext{
		Channels: []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}},
	}
	h := &rtmEventHandler{
		wsCtx:        wctx,
		workspaceID:  "T1",
		channelNames: map[string]string{"C1": "general"},
		channelTypes: map[string]string{"C1": "channel"},
		isActive:     func() bool { return false },
	}
	// thread_broadcast bumps the channel even though it's a reply.
	h.OnMessage("C1", "U2", "1700000002.000000", "broadcast", "1700000001.000000", "thread_broadcast", false, nil, slack.Blocks{}, nil)

	if wctx.Channels[0].UnreadCount != 1 {
		t.Errorf("thread_broadcast did not bump channel unread; got %d", wctx.Channels[0].UnreadCount)
	}
}
