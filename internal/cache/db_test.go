package cache

import (
	"context"
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
	tables := []string{"workspaces", "users", "channels", "messages", "reactions", "files", "channel_visits"}
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

func TestMigrateAddsChannelsSyncedAtColumn(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Probe PRAGMA table_info for the synced_at column on channels.
	rows, err := db.conn.Query("PRAGMA table_info(channels)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var found bool
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "synced_at" {
			if ctype != "INTEGER" {
				t.Errorf("synced_at type = %q, want INTEGER", ctype)
			}
			if notnull != 1 {
				t.Error("synced_at should be NOT NULL")
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("channels table missing synced_at column")
	}
}

// TestNew_SetsBusyTimeoutOnAllPoolConnections is a regression test
// for issue #9. Without a busy_timeout pragma on every connection in
// the pool, two goroutines in WAL mode that try to write at the same
// time will fail the second writer with SQLITE_BUSY immediately
// instead of waiting. The reconnect backfill (cmd/slk/reconnect_backfill.go)
// fans out N goroutines across the shared *sql.DB and silently
// dropped messages on systems where the lock window was long enough
// to collide.
//
// We force the pool to open multiple connections and assert that
// each one has a non-zero busy_timeout. PRAGMA busy_timeout is
// per-connection in sqlite, so the only way to ensure every pooled
// connection has it is to set it in the DSN (so it runs as part of
// the per-connection init), not via a one-off conn.Exec.
func TestNew_SetsBusyTimeoutOnAllPoolConnections(t *testing.T) {
	db, err := New(filepath.Join(t.TempDir(), "busy.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	// Hold N concurrent conns so the pool actually opens N distinct
	// underlying sqlite connections. If we just queried PRAGMA on
	// db.conn N times, the pool would hand back the same connection.
	const N = 4
	conns := make([]*sql.Conn, 0, N)
	for i := 0; i < N; i++ {
		c, err := db.conn.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn[%d]: %v", i, err)
		}
		conns = append(conns, c)
	}
	for i, c := range conns {
		var bt int
		if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&bt); err != nil {
			t.Fatalf("conn %d PRAGMA busy_timeout: %v", i, err)
		}
		if bt < 1000 {
			t.Errorf("conn %d busy_timeout = %d ms, want >= 1000 (writers must wait, not return SQLITE_BUSY immediately)", i, bt)
		}
		c.Close()
	}
}

func TestMigrate_CreatesThreadSubscriptionsTable(t *testing.T) {
	db := setupDBWithWorkspace(t)
	// PRAGMA table_info returns one row per column on an existing
	// table, zero rows if the table doesn't exist.
	rows, err := db.conn.Query("PRAGMA table_info(thread_subscriptions)")
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if count == 0 {
		t.Fatalf("thread_subscriptions table missing after migrate()")
	}
	const wantCols = 6 // workspace_id, channel_id, thread_ts, last_read, active, updated_at
	if count != wantCols {
		t.Fatalf("thread_subscriptions: want %d cols, got %d", wantCols, count)
	}
}
