package main

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui"
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

func TestMarkChannelReadAndNotify_FailureDoesNotNotify(t *testing.T) {
	// The UI must not optimistically clear an unread state that Slack
	// rejected: a ChannelMarkedReadMsg would blank the sidebar's unread
	// badge for a channel Slack still considers unread, which is the
	// cross-client divergence #159 is about.
	db := newTestDB(t)
	_ = db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	if err := db.UpdateChannelReadState("C1", "", true); err != nil {
		t.Fatalf("seed has_unread: %v", err)
	}
	marker := &fakeChannelMarker{err: errors.New("conversations.mark: invalid_auth")}
	var sent []tea.Msg

	markChannelReadAndNotify(context.Background(), marker, db, func(m tea.Msg) {
		sent = append(sent, m)
	}, "C1", "1700000000.000100")

	if len(sent) != 0 {
		t.Errorf("notify called %d time(s) with %v; want no notification after a rejected mark", len(sent), sent)
	}
	s, _ := db.GetChannelReadState("C1")
	if s.LastReadTS != "" || !s.HasUnread {
		t.Errorf("read state = %+v, want untouched after a rejected mark", s)
	}
}

func TestMarkChannelReadAndNotify_SuccessNotifiesOnce(t *testing.T) {
	db := newTestDB(t)
	_ = db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	if err := db.UpdateChannelReadState("C1", "", true); err != nil {
		t.Fatalf("seed has_unread: %v", err)
	}
	marker := &fakeChannelMarker{}
	var sent []tea.Msg

	markChannelReadAndNotify(context.Background(), marker, db, func(m tea.Msg) {
		sent = append(sent, m)
	}, "C1", "1700000000.000100")

	if len(sent) != 1 {
		t.Fatalf("notify called %d time(s), want exactly 1", len(sent))
	}
	got, ok := sent[0].(ui.ChannelMarkedReadMsg)
	if !ok {
		t.Fatalf("notified with %T, want ui.ChannelMarkedReadMsg", sent[0])
	}
	if got != (ui.ChannelMarkedReadMsg{ChannelID: "C1"}) {
		t.Errorf("notified with %+v, want ChannelID C1", got)
	}
}

type fakeThreadMarker struct {
	err   error
	calls []string // "channelID/threadTS/ts" per call
}

func (f *fakeThreadMarker) MarkThread(_ context.Context, channelID, threadTS, ts string) error {
	f.calls = append(f.calls, channelID+"/"+threadTS+"/"+ts)
	return f.err
}

func TestMarkThreadRead_SuccessAdvancesTheCursor(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertThreadSubscription("T1", "C1", "1700000000.000100", "1700000000.000100", true); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	marker := &fakeThreadMarker{}

	if err := markThreadRead(context.Background(), marker, db, "T1", "C1", "1700000000.000100", "1700000000.000500"); err != nil {
		t.Fatalf("markThreadRead: %v", err)
	}

	if len(marker.calls) != 1 || marker.calls[0] != "C1/1700000000.000100/1700000000.000500" {
		t.Fatalf("unexpected MarkThread calls: %v", marker.calls)
	}
	got, err := db.GetThreadLastRead("T1", "C1", "1700000000.000100")
	if err != nil {
		t.Fatalf("GetThreadLastRead: %v", err)
	}
	if got != "1700000000.000500" {
		t.Errorf("last_read = %q, want it advanced to 1700000000.000500; the WS echo may never arrive", got)
	}
}

func TestMarkThreadRead_FailureLeavesTheCursorAlone(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertThreadSubscription("T1", "C1", "1700000000.000100", "1700000000.000100", true); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	marker := &fakeThreadMarker{err: errors.New("subscriptions.thread.mark: thread_not_found")}

	if err := markThreadRead(context.Background(), marker, db, "T1", "C1", "1700000000.000100", "1700000000.000500"); err == nil {
		t.Fatal("markThreadRead: want error, got nil")
	}

	got, err := db.GetThreadLastRead("T1", "C1", "1700000000.000100")
	if err != nil {
		t.Fatalf("GetThreadLastRead: %v", err)
	}
	if got != "1700000000.000100" {
		t.Errorf("last_read = %q, want it unchanged after a rejected mark", got)
	}
}
