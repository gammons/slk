package cache

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
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

// TestSubtypeMigrationOnPreExistingDB verifies that an existing
// database created before the `subtype` column was added gets the
// column added idempotently when New() is called against it.
func TestSubtypeMigrationOnPreExistingDB(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "old.db")

	// Simulate a pre-migration database: create messages table WITHOUT
	// the subtype column, then close.
	{
		conn, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatal(err)
		}
		_, err = conn.Exec(`
			CREATE TABLE messages (
				ts TEXT NOT NULL,
				channel_id TEXT NOT NULL,
				workspace_id TEXT NOT NULL,
				user_id TEXT NOT NULL DEFAULT '',
				text TEXT NOT NULL DEFAULT '',
				thread_ts TEXT NOT NULL DEFAULT '',
				reply_count INTEGER NOT NULL DEFAULT 0,
				edited_at TEXT NOT NULL DEFAULT '',
				is_deleted INTEGER NOT NULL DEFAULT 0,
				raw_json TEXT NOT NULL DEFAULT '',
				created_at INTEGER NOT NULL DEFAULT 0,
				PRIMARY KEY (ts, channel_id)
			);
			INSERT INTO messages (ts, channel_id, workspace_id, user_id, text)
				VALUES ('1.0', 'C1', 'T1', 'U1', 'old row');
		`)
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}

	// Open via cache.New — migration should add the subtype column.
	db, err := New(dsn)
	if err != nil {
		t.Fatalf("New on pre-existing db: %v", err)
	}
	defer db.Close()

	var subtype string
	if err := db.conn.QueryRow(
		`SELECT subtype FROM messages WHERE ts='1.0' AND channel_id='C1'`,
	).Scan(&subtype); err != nil {
		t.Fatalf("querying subtype after migration: %v", err)
	}
	if subtype != "" {
		t.Errorf("existing row subtype=%q, want empty default", subtype)
	}

	// Calling New again must be a no-op (idempotent).
	db.Close()
	db2, err := New(dsn)
	if err != nil {
		t.Fatalf("re-opening migrated db: %v", err)
	}
	db2.Close()
}
