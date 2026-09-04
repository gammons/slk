package main

import (
	"context"
	"errors"
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

	h.OnChannelMarked("C1", "1.0050", 0)

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
	h.OnChannelMarked("C1", "1.0050", 0)
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
	h.OnChannelMarked("C1", "1.0050", 1)

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
	h.OnChannelMarked("C1", "1.0050", 0)

	state, _ := db.GetChannelReadState("C1")
	if state.HasUnread {
		t.Errorf("HasUnread = true after channel_marked with unread_count=0; want false")
	}
}

type fakeChannelMarker struct {
	err   error
	calls []string // "channelID/ts" per call
}

func (f *fakeChannelMarker) MarkChannel(_ context.Context, channelID, ts string) error {
	f.calls = append(f.calls, channelID+"/"+ts)
	return f.err
}

func TestMarkChannelRead_SuccessPersistsReadState(t *testing.T) {
	db := newTestDB(t)
	_ = db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	if err := db.UpdateChannelReadState("C1", "", true); err != nil {
		t.Fatalf("seed has_unread: %v", err)
	}
	marker := &fakeChannelMarker{}

	if err := markChannelRead(context.Background(), marker, db, "C1", "1700000000.000100"); err != nil {
		t.Fatalf("markChannelRead: %v", err)
	}

	if len(marker.calls) != 1 || marker.calls[0] != "C1/1700000000.000100" {
		t.Fatalf("unexpected MarkChannel calls: %v", marker.calls)
	}
	s, _ := db.GetChannelReadState("C1")
	if s.LastReadTS != "1700000000.000100" || s.HasUnread {
		t.Errorf("read state = %+v, want LastReadTS advanced and HasUnread=false", s)
	}
}

func TestMarkChannelRead_FailureLeavesChannelUnread(t *testing.T) {
	db := newTestDB(t)
	_ = db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	if err := db.UpdateChannelReadState("C1", "", true); err != nil {
		t.Fatalf("seed has_unread: %v", err)
	}
	marker := &fakeChannelMarker{err: errors.New("conversations.mark: invalid_auth")}

	if err := markChannelRead(context.Background(), marker, db, "C1", "1700000000.000100"); err == nil {
		t.Fatal("markChannelRead: want error, got nil")
	}

	s, _ := db.GetChannelReadState("C1")
	if s.LastReadTS != "" {
		t.Errorf("LastReadTS = %q, want unchanged after a rejected mark", s.LastReadTS)
	}
	if !s.HasUnread {
		t.Error("HasUnread must stay true so the state can be reconciled later")
	}
}
