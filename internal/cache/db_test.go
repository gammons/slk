package cache

import (
	"testing"
)

func TestNewDB(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal("failed to create db:", err)
	}
	defer db.Close()

	// Verify tables exist by querying them
	tables := []string{"workspaces", "users", "channels", "messages", "reactions", "files"}
	for _, table := range tables {
		var count int
		err := db.conn.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count)
		if err != nil {
			t.Errorf("table %q does not exist: %v", table, err)
		}
	}
}

func TestNewDBCreatesIndexes(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal("failed to create db:", err)
	}
	defer db.Close()

	// Check that key indexes exist
	var count int
	err = db.conn.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='index' AND name='idx_messages_channel'
	`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Error("expected idx_messages_channel index to exist")
	}
}
