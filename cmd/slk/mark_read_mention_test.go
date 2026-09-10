package main

import (
	"context"
	"testing"
	"time"

	"github.com/gammons/slk/internal/cache"
)

// waitForMentionCount polls until the channel's mention count reaches
// want, or fails. markChannelReadAsync does its work in a goroutine and,
// with a nil *tea.Program, emits no completion signal — so poll rather
// than sleeping a fixed duration.
func waitForMentionCount(t *testing.T, db *cache.DB, channelID string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	last := -1
	for time.Now().Before(deadline) {
		state, err := db.GetChannelReadState(channelID)
		if err != nil {
			t.Fatalf("GetChannelReadState: %v", err)
		}
		last = state.MentionCount
		if last == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("mention count for %s = %d after 2s, want %d", channelID, last, want)
}

// Entering a channel clears its mention badge. This drives the real
// markChannelReadAsync against a fake Slack so the production path is
// what gets exercised, not a hand-rolled pair of DB writes.
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

	srv := newFakeSlack(t, map[string]string{"/api/conversations.mark": `{"ok":true}`})
	wctx := &WorkspaceContext{Client: newTestClient(t, srv.Server)}

	markChannelReadAsync(context.Background(), wctx, db, nil, "C1", "1.0050")
	waitForMentionCount(t, db, "C1", 0)

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.HasUnread {
		t.Error("HasUnread = true, want false after read")
	}
	if state.LastReadTS != "1.0050" {
		t.Errorf("LastReadTS = %q, want 1.0050", state.LastReadTS)
	}
	// The DB write is unconditional (markChannelReadAsync discards
	// MarkChannel's error), so assert the API call really happened —
	// otherwise this test would pass with no network path at all.
	if got := srv.requestTo(t, "/api/conversations.mark").form.Get("channel"); got != "C1" {
		t.Errorf("conversations.mark channel = %q, want C1", got)
	}
}

// The nil-workspace guard must short-circuit before any write, so a
// workspace that failed to construct cannot silently clear badges.
// No goroutine starts in this path, so the check is immediate.
func TestMarkChannelRead_NilWorkspaceDoesNotWrite(t *testing.T) {
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
		t.Errorf("nil wctx wrote to the DB; MentionCount = %d, want 3 untouched", state.MentionCount)
	}
}

// An empty ts is the other guard: there is no watermark to advance, so
// nothing should be cleared.
func TestMarkChannelRead_EmptyTSDoesNotWrite(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.SetChannelMentionCount("C1", 3); err != nil {
		t.Fatalf("seed mention count: %v", err)
	}

	srv := newFakeSlack(t, map[string]string{"/api/conversations.mark": `{"ok":true}`})
	wctx := &WorkspaceContext{Client: newTestClient(t, srv.Server)}

	markChannelReadAsync(context.Background(), wctx, db, nil, "C1", "")

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.MentionCount != 3 {
		t.Errorf("empty ts wrote to the DB; MentionCount = %d, want 3 untouched", state.MentionCount)
	}
}
