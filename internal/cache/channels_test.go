package cache

import (
	"testing"
)

func setupDBWithWorkspace(t *testing.T) *DB {
	t.Helper()
	db, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.UpsertWorkspace(Workspace{ID: "T1", Name: "Test", Domain: "test"})
	return db
}

func TestUpsertAndGetChannel(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	ch := Channel{
		ID:          "C123",
		WorkspaceID: "T1",
		Name:        "general",
		Type:        "channel",
		Topic:       "General discussion",
		IsMember:    true,
	}

	if err := db.UpsertChannel(ch); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetChannel("C123")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "general" {
		t.Errorf("expected 'general', got %q", got.Name)
	}
	if !got.IsMember {
		t.Error("expected is_member true")
	}
}

func TestListChannelsByWorkspace(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})
	db.UpsertChannel(Channel{ID: "C2", WorkspaceID: "T1", Name: "random", Type: "channel", IsMember: true})
	db.UpsertChannel(Channel{ID: "C3", WorkspaceID: "T1", Name: "archived", Type: "channel", IsMember: false})

	channels, err := db.ListChannels("T1", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 2 {
		t.Errorf("expected 2 member channels, got %d", len(channels))
	}
}

func TestUpdateUnreadCount(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	if err := db.UpdateUnreadCount("C1", 5); err != nil {
		t.Fatal(err)
	}

	ch, _ := db.GetChannel("C1")
	if ch.UnreadCount != 5 {
		t.Errorf("expected unread count 5, got %d", ch.UnreadCount)
	}
}

func TestUpdateLastReadTS_RoundTrip(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	if err := db.UpdateLastReadTS("C1", "1234567890.000100"); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetLastReadTS("C1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1234567890.000100" {
		t.Errorf("expected '1234567890.000100', got %q", got)
	}

	// Update again — overwrites prior value.
	if err := db.UpdateLastReadTS("C1", "1234567890.000200"); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetLastReadTS("C1")
	if got != "1234567890.000200" {
		t.Errorf("expected '1234567890.000200' after overwrite, got %q", got)
	}

	// Roll backward — also allowed (mark-unread will need this).
	if err := db.UpdateLastReadTS("C1", "1234567890.000050"); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetLastReadTS("C1")
	if got != "1234567890.000050" {
		t.Errorf("expected backward roll to '1234567890.000050', got %q", got)
	}
}

func TestGetChannelSyncedAt_DefaultsToZero(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	if got := db.GetChannelSyncedAt("C1"); got != 0 {
		t.Errorf("default synced_at = %d, want 0", got)
	}
}

func TestGetChannelSyncedAt_MissingChannelReturnsZero(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	if got := db.GetChannelSyncedAt("C-nonexistent"); got != 0 {
		t.Errorf("missing channel synced_at = %d, want 0", got)
	}
}

func TestSetChannelSyncedAt_RoundTrip(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	if err := db.SetChannelSyncedAt("C1", 1700000000); err != nil {
		t.Fatal(err)
	}
	if got := db.GetChannelSyncedAt("C1"); got != 1700000000 {
		t.Errorf("synced_at = %d, want 1700000000", got)
	}

	// Overwrite.
	if err := db.SetChannelSyncedAt("C1", 1800000000); err != nil {
		t.Fatal(err)
	}
	if got := db.GetChannelSyncedAt("C1"); got != 1800000000 {
		t.Errorf("synced_at after overwrite = %d, want 1800000000", got)
	}
}

func TestSetAndGetChannelLatestSyncedTS(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	if err := db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	// Default: empty string.
	if got := db.GetChannelLatestSyncedTS("C1"); got != "" {
		t.Errorf("default GetChannelLatestSyncedTS = %q, want empty", got)
	}

	// After Set, returns what we set.
	if err := db.SetChannelLatestSyncedTS("C1", "1700000000.000123"); err != nil {
		t.Fatalf("SetChannelLatestSyncedTS: %v", err)
	}
	if got := db.GetChannelLatestSyncedTS("C1"); got != "1700000000.000123" {
		t.Errorf("GetChannelLatestSyncedTS = %q, want %q", got, "1700000000.000123")
	}

	// Unknown channel: returns empty without error.
	if got := db.GetChannelLatestSyncedTS("CDOESNOTEXIST"); got != "" {
		t.Errorf("GetChannelLatestSyncedTS unknown = %q, want empty", got)
	}
}

func TestAdvanceChannelLatestSyncedTS_NoRegress(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	if err := db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	// First advance from empty: writes the value.
	got, err := db.AdvanceChannelLatestSyncedTS("C1", "1700000000.000010")
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	if got != "1700000000.000010" {
		t.Errorf("first advance: got %q want %q", got, "1700000000.000010")
	}

	// Advance to a newer ts: writes.
	got, err = db.AdvanceChannelLatestSyncedTS("C1", "1700000001.000000")
	if err != nil {
		t.Fatalf("advance newer: %v", err)
	}
	if got != "1700000001.000000" {
		t.Errorf("advance newer: got %q want %q", got, "1700000001.000000")
	}

	// Advance to an older ts: NO regress; current value preserved.
	got, err = db.AdvanceChannelLatestSyncedTS("C1", "1699999999.000000")
	if err != nil {
		t.Fatalf("advance older: %v", err)
	}
	if got != "1700000001.000000" {
		t.Errorf("advance older should not regress: got %q want %q", got, "1700000001.000000")
	}

	// Advance to equal ts: no change.
	got, err = db.AdvanceChannelLatestSyncedTS("C1", "1700000001.000000")
	if err != nil {
		t.Fatalf("advance equal: %v", err)
	}
	if got != "1700000001.000000" {
		t.Errorf("advance equal: got %q want %q", got, "1700000001.000000")
	}

	// Empty ts: no-op, returns current value.
	got, err = db.AdvanceChannelLatestSyncedTS("C1", "")
	if err != nil {
		t.Fatalf("advance empty: %v", err)
	}
	if got != "1700000001.000000" {
		t.Errorf("advance empty: got %q want %q", got, "1700000001.000000")
	}
}

func TestMaxMessageTSForChannel(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	// Empty channel: empty result.
	got, err := db.MaxMessageTSForChannel("C1")
	if err != nil {
		t.Fatalf("max empty: %v", err)
	}
	if got != "" {
		t.Errorf("max empty: got %q want empty", got)
	}

	// Insert a few messages out of order.
	for _, ts := range []string{"1700000000.000010", "1700000005.000000", "1700000002.000050"} {
		if err := db.UpsertMessage(Message{
			TS: ts, ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "x",
			CreatedAt: 1700000000,
		}); err != nil {
			t.Fatalf("upsert message %s: %v", ts, err)
		}
	}

	got, err = db.MaxMessageTSForChannel("C1")
	if err != nil {
		t.Fatalf("max populated: %v", err)
	}
	if got != "1700000005.000000" {
		t.Errorf("max: got %q want %q", got, "1700000005.000000")
	}
}

func TestGetChannelWatermark_PrefersExplicitThenMaxTS(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	if err := db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	// No watermark, no messages → empty.
	if got, err := db.GetChannelWatermark("C1"); err != nil || got != "" {
		t.Errorf("empty: got %q err %v, want empty/nil", got, err)
	}

	// No explicit watermark, but cached messages → MAX(ts).
	if err := db.UpsertMessage(Message{
		TS: "1700000100.000000", ChannelID: "C1", WorkspaceID: "T1",
		UserID: "U1", Text: "msg", CreatedAt: 1700000100,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got, err := db.GetChannelWatermark("C1"); err != nil || got != "1700000100.000000" {
		t.Errorf("fallback: got %q err %v, want %q", got, err, "1700000100.000000")
	}

	// Explicit watermark takes precedence, even if behind MAX(ts).
	// (This models the "we have a gap above the watermark" scenario.)
	if err := db.SetChannelLatestSyncedTS("C1", "1700000050.000000"); err != nil {
		t.Fatalf("set explicit: %v", err)
	}
	if got, err := db.GetChannelWatermark("C1"); err != nil || got != "1700000050.000000" {
		t.Errorf("explicit precedence: got %q err %v, want %q", got, err, "1700000050.000000")
	}
}
