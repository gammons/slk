package cache

import (
	"testing"
)

func TestUpsertAndGetUser(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	u := User{
		ID:          "U123",
		WorkspaceID: "T1",
		Name:        "alice",
		DisplayName: "Alice Smith",
		Presence:    "active",
	}

	if err := db.UpsertUser(u); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetUser("U123")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Alice Smith" {
		t.Errorf("expected 'Alice Smith', got %q", got.DisplayName)
	}
	if got.Presence != "active" {
		t.Errorf("expected 'active', got %q", got.Presence)
	}
}

func TestUpdatePresence(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	db.UpsertUser(User{ID: "U1", WorkspaceID: "T1", Name: "alice", Presence: "active"})

	if err := db.UpdatePresence("U1", "away"); err != nil {
		t.Fatal(err)
	}

	got, _ := db.GetUser("U1")
	if got.Presence != "away" {
		t.Errorf("expected 'away', got %q", got.Presence)
	}
}
