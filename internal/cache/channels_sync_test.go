package cache

import (
	"testing"
)

func TestChannelsWithMessages_EmptyWorkspace(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	got, err := db.ChannelsWithMessages("T1")
	if err != nil {
		t.Fatalf("ChannelsWithMessages: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %d rows: %+v", len(got), got)
	}
}

func TestChannelsWithMessages_ReturnsChannelsWithAnyMessage(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	db.UpsertChannel(Channel{ID: "C2", WorkspaceID: "T1", Name: "random", Type: "channel"})
	db.UpsertChannel(Channel{ID: "C3", WorkspaceID: "T1", Name: "empty", Type: "channel"})
	db.SetChannelSyncedAt("C1", 1700000000)
	db.SetChannelSyncedAt("C2", 1700001000)

	db.UpsertMessage(Message{TS: "1.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "hi"})
	db.UpsertMessage(Message{TS: "2.000000", ChannelID: "C2", WorkspaceID: "T1", UserID: "U1", Text: "yo"})

	got, err := db.ChannelsWithMessages("T1")
	if err != nil {
		t.Fatalf("ChannelsWithMessages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(got), got)
	}
	byID := map[string]ChannelSyncRow{}
	for _, r := range got {
		byID[r.ChannelID] = r
	}
	if byID["C1"].SyncedAt != 1700000000 {
		t.Errorf("C1 synced_at = %d, want 1700000000", byID["C1"].SyncedAt)
	}
	if byID["C2"].SyncedAt != 1700001000 {
		t.Errorf("C2 synced_at = %d, want 1700001000", byID["C2"].SyncedAt)
	}
	if _, present := byID["C3"]; present {
		t.Errorf("C3 (no messages) should not be in result")
	}
}

func TestChannelsWithMessages_ChannelRowMissing(t *testing.T) {
	// A message can land via WS for a channel never UpsertChannel'd
	// (the OnMessage handler only upserts the message, not the channel).
	// In that case synced_at is 0.
	db := setupDBWithWorkspace(t)
	defer db.Close()

	db.UpsertMessage(Message{TS: "1.000000", ChannelID: "C99", WorkspaceID: "T1", UserID: "U1", Text: "orphan"})

	got, err := db.ChannelsWithMessages("T1")
	if err != nil {
		t.Fatalf("ChannelsWithMessages: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if got[0].ChannelID != "C99" || got[0].SyncedAt != 0 {
		t.Errorf("got %+v, want {C99, 0}", got[0])
	}
}

func TestChannelsWithMessages_WorkspaceIsolation(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertWorkspace(Workspace{ID: "T2", Name: "Other"})

	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	db.UpsertChannel(Channel{ID: "C2", WorkspaceID: "T2", Name: "general", Type: "channel"})
	db.UpsertMessage(Message{TS: "1.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "a"})
	db.UpsertMessage(Message{TS: "2.000000", ChannelID: "C2", WorkspaceID: "T2", UserID: "U1", Text: "b"})

	got, err := db.ChannelsWithMessages("T1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ChannelID != "C1" {
		t.Errorf("expected only C1, got %+v", got)
	}
}

func TestBackfillCandidates_UnionOfCachedAndUnread(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	// C1: cached, no unread. Will appear via ChannelsWithMessages.
	if err := db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("upsert C1: %v", err)
	}
	if err := db.SetChannelSyncedAt("C1", 1700000050); err != nil {
		t.Fatalf("synced_at C1: %v", err)
	}
	if err := db.UpsertMessage(Message{
		TS: "1700000010.000000", ChannelID: "C1", WorkspaceID: "T1",
		UserID: "U1", Text: "x", CreatedAt: 1700000010,
	}); err != nil {
		t.Fatalf("upsert message C1: %v", err)
	}

	// D1: unread DM, never opened in slk. Will appear via unread set.
	// We do NOT pre-insert the channel row to simulate the
	// "completely new" case.

	// C2: cached AND unread (no double-count expected).
	if err := db.UpsertChannel(Channel{ID: "C2", WorkspaceID: "T1", Name: "random", Type: "channel"}); err != nil {
		t.Fatalf("upsert C2: %v", err)
	}
	if err := db.UpsertMessage(Message{
		TS: "1700000020.000000", ChannelID: "C2", WorkspaceID: "T1",
		UserID: "U1", Text: "x", CreatedAt: 1700000020,
	}); err != nil {
		t.Fatalf("upsert message C2: %v", err)
	}

	got, err := db.BackfillCandidates("T1", []string{"D1", "C2"})
	if err != nil {
		t.Fatalf("BackfillCandidates: %v", err)
	}

	want := map[string]bool{"C1": true, "C2": true, "D1": true}
	if len(got) != 3 {
		t.Fatalf("len got = %d, want 3 (rows=%+v)", len(got), got)
	}
	for _, r := range got {
		if !want[r.ChannelID] {
			t.Errorf("unexpected channel %q in candidates", r.ChannelID)
		}
		delete(want, r.ChannelID)
	}
	for missing := range want {
		t.Errorf("expected channel %q in candidates, missing", missing)
	}
}

func TestBackfillCandidates_EmptyUnreadList(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	if err := db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.UpsertMessage(Message{
		TS: "1700000000.000000", ChannelID: "C1", WorkspaceID: "T1",
		UserID: "U1", Text: "x", CreatedAt: 1700000000,
	}); err != nil {
		t.Fatalf("upsert msg: %v", err)
	}

	got, err := db.BackfillCandidates("T1", nil)
	if err != nil {
		t.Fatalf("BackfillCandidates: %v", err)
	}
	if len(got) != 1 || got[0].ChannelID != "C1" {
		t.Errorf("unexpected: %+v", got)
	}
}
