package cache

import (
	"testing"
)

func newRSChannel(t *testing.T, db *DB, id, workspaceID string) {
	t.Helper()
	if err := db.UpsertWorkspace(Workspace{ID: workspaceID, Name: "ws"}); err != nil {
		t.Fatalf("UpsertWorkspace: %v", err)
	}
	if err := db.UpsertChannel(Channel{ID: id, WorkspaceID: workspaceID, Name: id, Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
}

func TestUpdateChannelReadState_WritesBothColumns(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	if err := db.UpdateChannelReadState("C1", "1700000000.000001", true); err != nil {
		t.Fatalf("UpdateChannelReadState: %v", err)
	}
	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.LastReadTS != "1700000000.000001" {
		t.Errorf("LastReadTS = %q, want %q", state.LastReadTS, "1700000000.000001")
	}
	if !state.HasUnread {
		t.Errorf("HasUnread = false, want true")
	}
}

func TestUpdateChannelReadState_EmptyTSPreservesExisting(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	if err := db.UpdateChannelReadState("C1", "1700000000.000001", false); err != nil {
		t.Fatalf("first update: %v", err)
	}
	if err := db.UpdateChannelReadState("C1", "", true); err != nil {
		t.Fatalf("second update: %v", err)
	}
	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.LastReadTS != "1700000000.000001" {
		t.Errorf("LastReadTS = %q, want preserved %q", state.LastReadTS, "1700000000.000001")
	}
	if !state.HasUnread {
		t.Errorf("HasUnread = false, want true")
	}
}

func TestUpdateChannelReadState_Idempotent(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	for i := 0; i < 3; i++ {
		if err := db.UpdateChannelReadState("C1", "1700000000.000001", true); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	state, _ := db.GetChannelReadState("C1")
	if state.LastReadTS != "1700000000.000001" || !state.HasUnread {
		t.Errorf("state = %+v after 3 writes", state)
	}
}

func TestBatchUpdateChannelReadState_WritesAll(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T1")
	newRSChannel(t, db, "C3", "T1")

	updates := []ChannelReadStateUpdate{
		{ChannelID: "C1", LastReadTS: "1.0001", HasUnread: true},
		{ChannelID: "C2", LastReadTS: "1.0002", HasUnread: false},
		{ChannelID: "C3", LastReadTS: "", HasUnread: true},
	}
	if err := db.BatchUpdateChannelReadState(updates); err != nil {
		t.Fatalf("BatchUpdateChannelReadState: %v", err)
	}
	s1, _ := db.GetChannelReadState("C1")
	if s1.LastReadTS != "1.0001" || !s1.HasUnread {
		t.Errorf("C1 = %+v", s1)
	}
	s2, _ := db.GetChannelReadState("C2")
	if s2.LastReadTS != "1.0002" || s2.HasUnread {
		t.Errorf("C2 = %+v", s2)
	}
	s3, _ := db.GetChannelReadState("C3")
	if s3.LastReadTS != "" || !s3.HasUnread {
		t.Errorf("C3 = %+v (LastReadTS should be preserved empty)", s3)
	}
}

func TestBatchUpdateChannelReadState_Transactional(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	// Seed C1
	if err := db.UpdateChannelReadState("C1", "1.0", false); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Empty batch is a no-op and returns nil.
	if err := db.BatchUpdateChannelReadState(nil); err != nil {
		t.Errorf("nil batch: %v", err)
	}
	if err := db.BatchUpdateChannelReadState([]ChannelReadStateUpdate{}); err != nil {
		t.Errorf("empty batch: %v", err)
	}
	// Original state untouched
	s, _ := db.GetChannelReadState("C1")
	if s.LastReadTS != "1.0" || s.HasUnread {
		t.Errorf("after empty batch C1 = %+v", s)
	}
}

func TestGetWorkspaceReadState_ReturnsAllChannels(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T1")
	newRSChannel(t, db, "C3", "T1")
	newRSChannel(t, db, "C4", "T2") // different workspace

	_ = db.UpdateChannelReadState("C1", "1.0001", true)
	_ = db.UpdateChannelReadState("C2", "1.0002", false)
	// C3 untouched — defaults

	got, err := db.GetWorkspaceReadState("T1")
	if err != nil {
		t.Fatalf("GetWorkspaceReadState: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3 (C3 must be included with defaults): %+v", len(got), got)
	}
	if got["C1"].LastReadTS != "1.0001" || !got["C1"].HasUnread {
		t.Errorf("C1 = %+v", got["C1"])
	}
	if got["C2"].LastReadTS != "1.0002" || got["C2"].HasUnread {
		t.Errorf("C2 = %+v", got["C2"])
	}
	if got["C3"].LastReadTS != "" || got["C3"].HasUnread {
		t.Errorf("C3 default = %+v", got["C3"])
	}
	if _, ok := got["C4"]; ok {
		t.Errorf("C4 from other workspace should not be returned")
	}
}

func TestWorkspacesWithUnreads(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T2")
	newRSChannel(t, db, "C3", "T3")

	_ = db.UpdateChannelReadState("C1", "1.0", true)
	_ = db.UpdateChannelReadState("C3", "1.0", true)
	// C2/T2 has no unreads

	got, err := db.WorkspacesWithUnreads()
	if err != nil {
		t.Fatalf("WorkspacesWithUnreads: %v", err)
	}
	want := map[string]bool{"T1": true, "T3": true}
	if len(got) != 2 {
		t.Fatalf("got %d ids, want 2: %v", len(got), got)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected workspace %q", id)
		}
	}
}

func TestReplaceWorkspaceReadState_ClearsChannelsAbsentFromSnapshot(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T1")
	newRSChannel(t, db, "C3", "T1")
	newRSChannel(t, db, "D1", "T2") // different workspace

	// Prior-session state: C1, C2 unread; D1 (other workspace) unread.
	_ = db.UpdateChannelReadState("C1", "1.0001", true)
	_ = db.UpdateChannelReadState("C2", "1.0002", true)
	_ = db.UpdateChannelReadState("D1", "1.0003", true)

	// Fresh authoritative snapshot for T1: only C3 is unread now.
	// C1 was read in the official client while slk was closed and is
	// therefore ABSENT from the snapshot; it must be cleared. C2 is
	// explicitly reported read. C3 becomes unread.
	updates := []ChannelReadStateUpdate{
		{ChannelID: "C2", LastReadTS: "1.0100", HasUnread: false},
		{ChannelID: "C3", LastReadTS: "1.0101", HasUnread: true},
	}
	if err := db.ReplaceWorkspaceReadState("T1", updates); err != nil {
		t.Fatalf("ReplaceWorkspaceReadState: %v", err)
	}

	s1, _ := db.GetChannelReadState("C1")
	if s1.HasUnread {
		t.Errorf("C1 HasUnread = true; absent-from-snapshot channel must be cleared (Symptom 2)")
	}
	// last_read_ts of an absent channel is left untouched (no fresh value to apply).
	if s1.LastReadTS != "1.0001" {
		t.Errorf("C1 LastReadTS = %q, want preserved %q", s1.LastReadTS, "1.0001")
	}
	s2, _ := db.GetChannelReadState("C2")
	if s2.HasUnread || s2.LastReadTS != "1.0100" {
		t.Errorf("C2 = %+v, want {1.0100 false}", s2)
	}
	s3, _ := db.GetChannelReadState("C3")
	if !s3.HasUnread || s3.LastReadTS != "1.0101" {
		t.Errorf("C3 = %+v, want {1.0101 true}", s3)
	}

	// Other workspace is untouched by a T1 replace.
	d1, _ := db.GetChannelReadState("D1")
	if !d1.HasUnread || d1.LastReadTS != "1.0003" {
		t.Errorf("D1 (other workspace) = %+v, want {1.0003 true} untouched", d1)
	}
}

func TestReplaceWorkspaceReadState_EmptySnapshotClearsAll(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T1")
	_ = db.UpdateChannelReadState("C1", "1.0", true)
	_ = db.UpdateChannelReadState("C2", "1.0", true)

	// A successful client.counts call that reports zero unreads means
	// everything is read.
	if err := db.ReplaceWorkspaceReadState("T1", nil); err != nil {
		t.Fatalf("ReplaceWorkspaceReadState: %v", err)
	}
	for _, id := range []string{"C1", "C2"} {
		s, _ := db.GetChannelReadState(id)
		if s.HasUnread {
			t.Errorf("%s HasUnread = true after empty snapshot; want cleared", id)
		}
	}
}

func TestUpsertChannel_DoesNotClobberReadState(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	// Set read state.
	if err := db.UpdateChannelReadState("C1", "1700000000.000001", true); err != nil {
		t.Fatalf("UpdateChannelReadState: %v", err)
	}

	// Re-upsert the channel with zero-value LastReadTS/UnreadCount.
	// This mirrors what bootstrap (upsertChannelInDB) does today.
	if err := db.UpsertChannel(Channel{
		ID:          "C1",
		WorkspaceID: "T1",
		Name:        "renamed",
		Type:        "channel",
	}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.LastReadTS != "1700000000.000001" {
		t.Errorf("LastReadTS = %q, want preserved %q (clobber regression!)", state.LastReadTS, "1700000000.000001")
	}
	if !state.HasUnread {
		t.Errorf("HasUnread = false after upsert (clobber regression!)")
	}
}

func TestSetChannelMentionCount(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	if err := db.SetChannelMentionCount("C1", 4); err != nil {
		t.Fatalf("SetChannelMentionCount: %v", err)
	}
	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.MentionCount != 4 {
		t.Errorf("MentionCount = %d, want 4", state.MentionCount)
	}

	// Overwrite, including back down to zero — the read paths rely on
	// this to clear a badge.
	if err := db.SetChannelMentionCount("C1", 0); err != nil {
		t.Fatalf("SetChannelMentionCount(0): %v", err)
	}
	state, err = db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.MentionCount != 0 {
		t.Errorf("MentionCount = %d, want 0 after reset", state.MentionCount)
	}
}

func TestIncrementChannelMentionCount_AccumulatesFromZero(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	// A freshly upserted channel has mention_count = 0 by column
	// default, so the first increment must yield 1 rather than error or
	// leave NULL.
	for i := 1; i <= 3; i++ {
		if err := db.IncrementChannelMentionCount("C1"); err != nil {
			t.Fatalf("increment %d: %v", i, err)
		}
		state, err := db.GetChannelReadState("C1")
		if err != nil {
			t.Fatalf("GetChannelReadState: %v", err)
		}
		if state.MentionCount != i {
			t.Errorf("after %d increments MentionCount = %d, want %d", i, state.MentionCount, i)
		}
	}
}

// The mention setters must not disturb the columns that drive the unread
// dot and the "new messages" divider. An earlier design threaded mention
// count through UpdateChannelReadState; this test pins the separation.
func TestMentionSetters_DoNotDisturbReadState(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	if err := db.UpdateChannelReadState("C1", "1700000000.000009", true); err != nil {
		t.Fatalf("UpdateChannelReadState: %v", err)
	}
	if err := db.SetChannelMentionCount("C1", 2); err != nil {
		t.Fatalf("SetChannelMentionCount: %v", err)
	}
	if err := db.IncrementChannelMentionCount("C1"); err != nil {
		t.Fatalf("IncrementChannelMentionCount: %v", err)
	}

	state, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState: %v", err)
	}
	if state.LastReadTS != "1700000000.000009" {
		t.Errorf("LastReadTS = %q, want preserved 1700000000.000009", state.LastReadTS)
	}
	if !state.HasUnread {
		t.Error("HasUnread = false, want preserved true")
	}
	if state.MentionCount != 3 {
		t.Errorf("MentionCount = %d, want 3", state.MentionCount)
	}
}

func TestBatchUpdateChannelReadState_WritesMentionCount(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T1")

	// C1 exercises the "both columns" statement (non-empty LastReadTS);
	// C2 exercises the flag-only statement (empty LastReadTS).
	updates := []ChannelReadStateUpdate{
		{ChannelID: "C1", LastReadTS: "1.0001", HasUnread: true, MentionCount: 7},
		{ChannelID: "C2", LastReadTS: "", HasUnread: true, MentionCount: 2},
	}
	if err := db.BatchUpdateChannelReadState(updates); err != nil {
		t.Fatalf("BatchUpdateChannelReadState: %v", err)
	}

	for _, want := range []struct {
		id    string
		count int
	}{{"C1", 7}, {"C2", 2}} {
		state, err := db.GetChannelReadState(want.id)
		if err != nil {
			t.Fatalf("GetChannelReadState(%s): %v", want.id, err)
		}
		if state.MentionCount != want.count {
			t.Errorf("%s MentionCount = %d, want %d", want.id, state.MentionCount, want.count)
		}
	}
}

// Boot applies an authoritative full snapshot. A channel the user read in
// another client while slk was closed is simply absent from
// client.counts, so the workspace-wide reset must clear its mention count
// as well as its unread flag — otherwise a stale badge survives forever.
func TestReplaceWorkspaceReadState_ZeroesAbsentChannelMentionCount(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")
	newRSChannel(t, db, "C2", "T1")

	if err := db.SetChannelMentionCount("C1", 5); err != nil {
		t.Fatalf("seed C1: %v", err)
	}
	if err := db.SetChannelMentionCount("C2", 9); err != nil {
		t.Fatalf("seed C2: %v", err)
	}

	// Snapshot mentions C1 only. C2 must be reset.
	if err := db.ReplaceWorkspaceReadState("T1", []ChannelReadStateUpdate{
		{ChannelID: "C1", LastReadTS: "1.0", HasUnread: true, MentionCount: 3},
	}); err != nil {
		t.Fatalf("ReplaceWorkspaceReadState: %v", err)
	}

	c1, err := db.GetChannelReadState("C1")
	if err != nil {
		t.Fatalf("GetChannelReadState(C1): %v", err)
	}
	if c1.MentionCount != 3 {
		t.Errorf("C1 MentionCount = %d, want 3", c1.MentionCount)
	}
	c2, err := db.GetChannelReadState("C2")
	if err != nil {
		t.Fatalf("GetChannelReadState(C2): %v", err)
	}
	if c2.MentionCount != 0 {
		t.Errorf("C2 MentionCount = %d, want 0 (absent from snapshot)", c2.MentionCount)
	}
}

func TestGetWorkspaceReadState_IncludesMentionCount(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	newRSChannel(t, db, "C1", "T1")

	if err := db.SetChannelMentionCount("C1", 6); err != nil {
		t.Fatalf("SetChannelMentionCount: %v", err)
	}
	m, err := db.GetWorkspaceReadState("T1")
	if err != nil {
		t.Fatalf("GetWorkspaceReadState: %v", err)
	}
	if m["C1"].MentionCount != 6 {
		t.Errorf("C1 MentionCount = %d, want 6", m["C1"].MentionCount)
	}
}
