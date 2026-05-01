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
