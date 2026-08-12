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

func TestMarkChannelsStale_ZeroesEveryChannelExceptTheOneKept(t *testing.T) {
	// The reconnect handler refreshes exactly one channel over the
	// network — the one the user is looking at — and marks the rest
	// stale so each revalidates when it is next opened. That is what
	// makes reconnect O(1) instead of O(channels).
	db := setupDBWithWorkspace(t)
	defer db.Close()

	for _, id := range []string{"C1", "C2", "C3"} {
		if err := db.UpsertChannel(Channel{ID: id, WorkspaceID: "T1", Name: id, Type: "channel"}); err != nil {
			t.Fatalf("UpsertChannel(%s): %v", id, err)
		}
		if err := db.SetChannelSyncedAt(id, 1700000000); err != nil {
			t.Fatalf("SetChannelSyncedAt(%s): %v", id, err)
		}
	}

	if err := db.MarkChannelsStale("T1", "C2"); err != nil {
		t.Fatalf("MarkChannelsStale: %v", err)
	}

	if got := db.GetChannelSyncedAt("C1"); got != 0 {
		t.Errorf("C1 synced_at = %d; want 0 — an unrefreshed channel must look stale so its next open refetches", got)
	}
	if got := db.GetChannelSyncedAt("C3"); got != 0 {
		t.Errorf("C3 synced_at = %d; want 0", got)
	}
	if got := db.GetChannelSyncedAt("C2"); got != 1700000000 {
		t.Errorf("C2 synced_at = %d; want 1700000000 preserved — it is the channel the handler just refreshed, and staling it would make the very next render refetch what it already has", got)
	}
}

func TestMarkChannelsStale_LeavesOtherWorkspacesAlone(t *testing.T) {
	// One workspace's socket flapping says nothing about another's.
	db := setupDBWithWorkspace(t)
	defer db.Close()
	if err := db.UpsertWorkspace(Workspace{ID: "T2", Name: "Other"}); err != nil {
		t.Fatalf("UpsertWorkspace: %v", err)
	}
	if err := db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "here", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if err := db.UpsertChannel(Channel{ID: "C9", WorkspaceID: "T2", Name: "elsewhere", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	db.SetChannelSyncedAt("C1", 1700000000)
	db.SetChannelSyncedAt("C9", 1700000000)

	if err := db.MarkChannelsStale("T1", ""); err != nil {
		t.Fatalf("MarkChannelsStale: %v", err)
	}

	if got := db.GetChannelSyncedAt("C1"); got != 0 {
		t.Errorf("C1 synced_at = %d; want 0", got)
	}
	if got := db.GetChannelSyncedAt("C9"); got != 1700000000 {
		t.Errorf("C9 synced_at = %d; want it untouched — it belongs to another workspace", got)
	}
}

func TestMarkChannelsStale_EmptyKeepStalesEverything(t *testing.T) {
	// There is no active channel on a workspace the user has not
	// looked at yet. "" must not be read as "keep the channel whose
	// id is the empty string" in a way that skips the sweep.
	db := setupDBWithWorkspace(t)
	defer db.Close()
	if err := db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "a", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	db.SetChannelSyncedAt("C1", 1700000000)

	if err := db.MarkChannelsStale("T1", ""); err != nil {
		t.Fatalf("MarkChannelsStale: %v", err)
	}
	if got := db.GetChannelSyncedAt("C1"); got != 0 {
		t.Errorf("C1 synced_at = %d; want 0", got)
	}
}
