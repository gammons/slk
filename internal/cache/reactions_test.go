package cache

import (
	"testing"
)

func TestUpsertAndGetReactions(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	err := db.UpsertReaction("1234.5678", "C123", "thumbsup", []string{"U001", "U002"}, 2)
	if err != nil {
		t.Fatalf("UpsertReaction: %v", err)
	}

	rows, err := db.GetReactions("1234.5678", "C123")
	if err != nil {
		t.Fatalf("GetReactions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(rows))
	}
	if rows[0].Emoji != "thumbsup" {
		t.Errorf("expected emoji thumbsup, got %s", rows[0].Emoji)
	}
	if rows[0].Count != 2 {
		t.Errorf("expected count 2, got %d", rows[0].Count)
	}
	if len(rows[0].UserIDs) != 2 || rows[0].UserIDs[0] != "U001" {
		t.Errorf("unexpected userIDs: %v", rows[0].UserIDs)
	}
}

func TestUpsertReactionUpdatesExisting(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	err := db.UpsertReaction("1234.5678", "C123", "thumbsup", []string{"U001"}, 1)
	if err != nil {
		t.Fatalf("UpsertReaction: %v", err)
	}

	err = db.UpsertReaction("1234.5678", "C123", "thumbsup", []string{"U001", "U002"}, 2)
	if err != nil {
		t.Fatalf("UpsertReaction update: %v", err)
	}

	rows, err := db.GetReactions("1234.5678", "C123")
	if err != nil {
		t.Fatalf("GetReactions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(rows))
	}
	if rows[0].Count != 2 {
		t.Errorf("expected count 2, got %d", rows[0].Count)
	}
}

func TestDeleteReaction(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	err := db.UpsertReaction("1234.5678", "C123", "thumbsup", []string{"U001"}, 1)
	if err != nil {
		t.Fatalf("UpsertReaction: %v", err)
	}

	err = db.DeleteReaction("1234.5678", "C123", "thumbsup")
	if err != nil {
		t.Fatalf("DeleteReaction: %v", err)
	}

	rows, err := db.GetReactions("1234.5678", "C123")
	if err != nil {
		t.Fatalf("GetReactions: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 reactions, got %d", len(rows))
	}
}

func TestGetReactionsEmpty(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	rows, err := db.GetReactions("nonexistent", "C123")
	if err != nil {
		t.Fatalf("GetReactions: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 reactions, got %d", len(rows))
	}
}

func TestMultipleReactionsOnMessage(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	if err := db.UpsertReaction("1234.5678", "C123", "thumbsup", []string{"U001"}, 1); err != nil {
		t.Fatalf("UpsertReaction thumbsup: %v", err)
	}
	if err := db.UpsertReaction("1234.5678", "C123", "rocket", []string{"U002", "U003"}, 2); err != nil {
		t.Fatalf("UpsertReaction rocket: %v", err)
	}
	if err := db.UpsertReaction("1234.5678", "C123", "heart", []string{"U001"}, 1); err != nil {
		t.Fatalf("UpsertReaction heart: %v", err)
	}

	rows, err := db.GetReactions("1234.5678", "C123")
	if err != nil {
		t.Fatalf("GetReactions: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 reactions, got %d", len(rows))
	}
}
