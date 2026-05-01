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
