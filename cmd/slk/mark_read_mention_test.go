package main

import (
	"context"
	"errors"
	"testing"

	"github.com/gammons/slk/internal/cache"
)

// The mention-count half of markChannelRead's contract. The read-state
// half is covered next door by TestMarkChannelRead_SuccessPersistsReadState
// and TestMarkChannelRead_FailureLeavesChannelUnread; these two assert
// that the badge follows the same rule the dot does.
//
// fakeChannelMarker lives in event_handler_marked_test.go.

// Reading a channel clears its mention badge.
func TestMarkChannelRead_ClearsMentionCount(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.UpdateChannelReadState("C1", "1.0000", true); err != nil {
		t.Fatalf("seed read state: %v", err)
	}
	if err := db.SetChannelMentionCount("C1", 3); err != nil {
		t.Fatalf("seed mention count: %v", err)
	}
	marker := &fakeChannelMarker{}

	if err := markChannelRead(context.Background(), marker, db, "C1", "1.0050"); err != nil {
		t.Fatalf("markChannelRead: %v", err)
	}

	if len(marker.calls) != 1 || marker.calls[0] != "C1/1.0050" {
		t.Fatalf("unexpected MarkChannel calls: %v", marker.calls)
	}
	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.MentionCount != 0 {
		t.Errorf("MentionCount = %d, want 0 after read", state.MentionCount)
	}
	// The dot and the badge must clear together, not one render apart.
	if state.HasUnread {
		t.Error("HasUnread = true, want false after read")
	}
	if state.LastReadTS != "1.0050" {
		t.Errorf("LastReadTS = %q, want 1.0050", state.LastReadTS)
	}
}

// A mark Slack rejected must leave the badge standing. Clearing it would
// blank a mention the user has not actually read anywhere, and unlike the
// dot there is no second signal to notice the loss by — the same
// cross-client divergence #159 is about, applied to the mention count.
//
// This assertion was impossible before conversations.mark's failure was
// surfaced and the write moved behind it.
func TestMarkChannelRead_FailureLeavesMentionCount(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.UpdateChannelReadState("C1", "", true); err != nil {
		t.Fatalf("seed read state: %v", err)
	}
	if err := db.SetChannelMentionCount("C1", 3); err != nil {
		t.Fatalf("seed mention count: %v", err)
	}
	marker := &fakeChannelMarker{err: errors.New("conversations.mark: invalid_auth")}

	if err := markChannelRead(context.Background(), marker, db, "C1", "1.0050"); err == nil {
		t.Fatal("markChannelRead: want error, got nil")
	}

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.MentionCount != 3 {
		t.Errorf("MentionCount = %d, want 3 untouched after a rejected mark", state.MentionCount)
	}
	if !state.HasUnread {
		t.Error("HasUnread must stay true so the state can be reconciled later")
	}
}

// markChannelReadAsync's two guards. Both return before the goroutine is
// spawned, so a synchronous assertion is deterministic — there is no
// racing work to wait for.
//
// The ts == "" guard is reachable in production: MarkRead passes ts
// straight through from the reducer, and only flushPendingMarks
// pre-checks it. Without the guard an empty watermark would clear a
// channel's badge while marking it read at ts "", which Slack rejects.
func TestMarkChannelReadAsync_EmptyTSDoesNotMark(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.SetChannelMentionCount("C1", 3); err != nil {
		t.Fatalf("seed mention count: %v", err)
	}
	marker := &fakeChannelMarker{}

	markChannelReadAsync(context.Background(), marker, db, nil, "C1", "")

	if len(marker.calls) != 0 {
		t.Errorf("empty ts issued a mark: %v", marker.calls)
	}
	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.MentionCount != 3 {
		t.Errorf("MentionCount = %d, want 3 untouched", state.MentionCount)
	}
}

// A nil marker means the workspace failed to construct. It must not
// reach the goroutine, where it would nil-deref on MarkChannel.
func TestMarkChannelReadAsync_NilMarkerDoesNotMark(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.SetChannelMentionCount("C1", 3); err != nil {
		t.Fatalf("seed mention count: %v", err)
	}

	markChannelReadAsync(context.Background(), nil, db, nil, "C1", "1.0050")

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.MentionCount != 3 {
		t.Errorf("nil marker wrote to the DB; MentionCount = %d, want 3 untouched", state.MentionCount)
	}
}
