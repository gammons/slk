package main

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
)

func TestOnChannelMarked_WritesReadState(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	// Pre-seed unread to verify the marked event clears it.
	if err := db.UpdateChannelReadState("C1", "1.0000", true); err != nil {
		t.Fatalf("seed: %v", err)
	}

	wctx := &WorkspaceContext{}
	h := &rtmEventHandler{
		db:       db,
		wsCtx:    wctx,
		isActive: func() bool { return true },
		program:  nil, // exercise the no-program path
	}

	h.OnChannelMarked("C1", "1.0050", 0, 0)

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.LastReadTS != "1.0050" {
		t.Errorf("LastReadTS = %q, want %q", state.LastReadTS, "1.0050")
	}
	if state.HasUnread {
		t.Errorf("HasUnread should be false after channel_marked")
	}
}

func TestOnChannelMarked_InactiveWorkspace_StillWritesDB(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.UpdateChannelReadState("C1", "1.0000", true); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := &rtmEventHandler{
		db:          db,
		wsCtx:       &WorkspaceContext{},
		isActive:    func() bool { return false }, // inactive workspace
		program:     nil,
		workspaceID: "T1",
	}
	h.OnChannelMarked("C1", "1.0050", 0, 0)
	s, _ := db.GetChannelReadState("C1")
	if s.HasUnread {
		t.Errorf("HasUnread should be false even for inactive-workspace channel_marked")
	}
	if s.LastReadTS != "1.0050" {
		t.Errorf("LastReadTS = %q, want %q", s.LastReadTS, "1.0050")
	}
}

func TestOnChannelMarked_RemoteMarkUnread_SetsHasUnread(t *testing.T) {
	// When the user marks a message unread on another client (phone,
	// official desktop client), Slack sends `channel_marked` with the
	// new (older) last_read AND unread_count_display>0. Our handler
	// must use the count to set has_unread=true; previously it hardcoded
	// false, silently swallowing every remote mark-unread.
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	// Channel currently read in slk's cache.
	if err := db.UpdateChannelReadState("C1", "1.0100", false); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := &rtmEventHandler{
		db:          db,
		wsCtx:       &WorkspaceContext{},
		isActive:    func() bool { return true },
		program:     nil,
		workspaceID: "T1",
	}
	// User marks message at ts=1.0050 unread on phone. Slack rolls the
	// last_read back to 1.0050 and reports unread_count=1.
	h.OnChannelMarked("C1", "1.0050", 1, 0)

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if !state.HasUnread {
		t.Errorf("HasUnread = false after remote mark-unread; want true (unread_count was 1)")
	}
	if state.LastReadTS != "1.0050" {
		t.Errorf("LastReadTS = %q, want %q", state.LastReadTS, "1.0050")
	}
}

func TestOnChannelMarked_ZeroUnreadCount_ClearsHasUnread(t *testing.T) {
	// Companion to RemoteMarkUnread: when unread_count is 0 (the normal
	// "read" case), has_unread must clear. This pins down the
	// unread_count > 0 contract.
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.UpdateChannelReadState("C1", "1.0000", true); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h := &rtmEventHandler{
		db:          db,
		wsCtx:       &WorkspaceContext{},
		isActive:    func() bool { return true },
		program:     nil,
		workspaceID: "T1",
	}
	h.OnChannelMarked("C1", "1.0050", 0, 0)

	state, _ := db.GetChannelReadState("C1")
	if state.HasUnread {
		t.Errorf("HasUnread = true after channel_marked with unread_count=0; want false")
	}
}

func TestMarkChannelReadAsync_UpdatesReadState(t *testing.T) {
	// markChannelReadAsync runs its work in a goroutine and calls
	// client.MarkChannel on a *slackclient.Client, which requires real
	// HTTP/Slack wiring (or a fake) to construct. The function body is
	// otherwise a thin wrapper over db.UpdateChannelReadState (covered by
	// cache-level tests) plus a tea.Program send. Wiring a fake Client
	// would require introducing an interface seam we don't otherwise need.
	// The reconnect-backfill integration test in Task 20 exercises this
	// path end-to-end.
	t.Skip("markChannelReadAsync requires a real *slackclient.Client; covered by Task 20 integration test")
}

// The event's mention_count is authoritative: it replaces whatever the
// local increment path accumulated. This is the mechanism that corrects
// @usergroup undercounting without a poll.
func TestOnChannelMarked_SetsMentionCountFromEvent(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.UpdateChannelReadState("C1", "1.0000", true); err != nil {
		t.Fatalf("seed read state: %v", err)
	}
	// Local detection had accumulated 5; the server says 2.
	if err := db.SetChannelMentionCount("C1", 5); err != nil {
		t.Fatalf("seed mention count: %v", err)
	}

	h := &rtmEventHandler{
		db:       db,
		wsCtx:    &WorkspaceContext{},
		isActive: func() bool { return true },
		program:  nil,
	}

	h.OnChannelMarked("C1", "1.0050", 9, 2)

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.MentionCount != 2 {
		t.Errorf("MentionCount = %d, want 2 (server value replaces local)", state.MentionCount)
	}
}

// Reading a channel in another client pushes unread_count_display=0 and
// mention_count=0, which must clear the badge as well as the dot.
func TestOnChannelMarked_ReadClearsMentionCount(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.UpdateChannelReadState("C1", "1.0000", true); err != nil {
		t.Fatalf("seed read state: %v", err)
	}
	if err := db.SetChannelMentionCount("C1", 4); err != nil {
		t.Fatalf("seed mention count: %v", err)
	}

	h := &rtmEventHandler{
		db:       db,
		wsCtx:    &WorkspaceContext{},
		isActive: func() bool { return true },
		program:  nil,
	}

	h.OnChannelMarked("C1", "1.0050", 0, 0)

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.HasUnread {
		t.Error("HasUnread = true, want false after read")
	}
	if state.MentionCount != 0 {
		t.Errorf("MentionCount = %d, want 0 after read", state.MentionCount)
	}
}
