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

func TestMigration_AddsHasUnreadColumn(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	rows, err := db.conn.Query("PRAGMA table_info(channels)")
	if err != nil {
		t.Fatalf("PRAGMA: %v", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "has_unread" {
			found = true
			if ctype != "INTEGER" {
				t.Errorf("has_unread type = %q, want INTEGER", ctype)
			}
			if notnull != 1 {
				t.Errorf("has_unread NOT NULL = %d, want 1", notnull)
			}
			if !dflt.Valid || dflt.String != "0" {
				t.Errorf("has_unread default = %v, want 0", dflt)
			}
		}
	}
	if !found {
		t.Fatal("has_unread column not added")
	}
}

// seedUsersTable opens a fresh sqlite file at path and hand-creates
// just enough pre-migration schema for the migrate() run that follows
// to see it: a workspaces table, and a users table built from exactly
// columnsDDL (the caller lists every column, including id/
// workspace_id/name/version) with one seeded row.
func seedUsersTable(t *testing.T, path, columnsDDL, rowValues string) {
	t.Helper()
	seed, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening seed db: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE workspaces (id TEXT PRIMARY KEY, name TEXT NOT NULL, domain TEXT NOT NULL DEFAULT '', icon_url TEXT NOT NULL DEFAULT '', last_synced_at INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatalf("seeding workspaces table: %v", err)
	}
	if _, err := seed.Exec("CREATE TABLE users (" + columnsDDL + ")"); err != nil {
		t.Fatalf("seeding partial users schema: %v", err)
	}
	if _, err := seed.Exec(`INSERT INTO workspaces (id, name) VALUES ('T1', 'T1')`); err != nil {
		t.Fatalf("seeding workspace: %v", err)
	}
	if _, err := seed.Exec("INSERT INTO users VALUES (" + rowValues + ")"); err != nil {
		t.Fatalf("seeding user: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("closing seed db: %v", err)
	}
}

// assertUserVersion opens path (running migrate() via New, the real
// entry point) and checks U1's version against want.
func assertUserVersion(t *testing.T, path string, want int64, msg string) {
	t.Helper()
	db, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()
	var got int64
	if err := db.conn.QueryRow(`SELECT version FROM users WHERE id = 'U1'`).Scan(&got); err != nil {
		t.Fatalf("reading back version: %v", err)
	}
	if got != want {
		t.Errorf("version = %d; want %d -- %s", got, want, msg)
	}
}

// TestMigration_StatusColumnGroupResetsVersionEvenIfOneColumnAlreadyExisted:
// status_emoji present but status_text/status_expiration missing still
// resets the version (huddle columns seeded complete, to isolate this
// from the huddle group).
func TestMigration_StatusColumnGroupResetsVersionEvenIfOneColumnAlreadyExisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	seedUsersTable(t, path,
		"id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, name TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 0, "+
			"status_emoji TEXT NOT NULL DEFAULT '', huddle_state TEXT NOT NULL DEFAULT '', huddle_expiration INTEGER NOT NULL DEFAULT 0",
		"'U1', 'T1', 'alice', 42, '', 'default_unset', 0")

	assertUserVersion(t, path, 0,
		"status_text/status_expiration were missing even though status_emoji was present")
}

// TestMigration_HuddleColumnGroupResetsVersionEvenIfOneColumnAlreadyExisted
// is the huddle-pair mirror: huddle_state present but huddle_expiration
// missing still resets the version (status columns seeded complete).
func TestMigration_HuddleColumnGroupResetsVersionEvenIfOneColumnAlreadyExisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	seedUsersTable(t, path,
		"id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, name TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 0, "+
			"status_emoji TEXT NOT NULL DEFAULT '', status_text TEXT NOT NULL DEFAULT '', status_expiration INTEGER NOT NULL DEFAULT 0, huddle_state TEXT NOT NULL DEFAULT ''",
		"'U1', 'T1', 'alice', 42, '', '', 0, 'default_unset'")

	assertUserVersion(t, path, 0,
		"huddle_expiration was missing even though huddle_state was present")
}

// TestMigration_StatusColumnGroupLeavesVersionAloneWhenAlreadyComplete
// is the flip side: a database that already has every column in the
// status group must not have its version reset again on a later open.
func TestMigration_StatusColumnGroupLeavesVersionAloneWhenAlreadyComplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	db, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := db.conn.Exec(`INSERT INTO workspaces (id, name) VALUES ('T1', 'T1')`); err != nil {
		t.Fatalf("seeding workspace: %v", err)
	}
	if _, err := db.conn.Exec(`INSERT INTO users (id, workspace_id, name, version) VALUES ('U1', 'T1', 'alice', 7)`); err != nil {
		t.Fatalf("seeding user: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("closing db: %v", err)
	}

	// Reopening an already-fully-migrated database must be a no-op for
	// the version column: every status/huddle column is already there.
	db2, err := New(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer db2.Close()

	var version int64
	if err := db2.conn.QueryRow(`SELECT version FROM users WHERE id = 'U1'`).Scan(&version); err != nil {
		t.Fatalf("reading back version: %v", err)
	}
	if version != 7 {
		t.Errorf("version = %d; want left at 7, an already-complete database must not reset it again", version)
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

func TestMembershipSchema(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Tables exist.
	for _, table := range []string{"channel_members", "channel_membership_meta"} {
		var name string
		err := db.conn.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}

	// is_external column present on users.
	rows, err := db.conn.Query(`PRAGMA table_info(users)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "is_external" {
			found = true
		}
	}
	if !found {
		t.Error("users.is_external column missing")
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
	const wantCols = 7 // workspace_id, channel_id, thread_ts, last_read, latest_reply, active, updated_at
	if count != wantCols {
		t.Fatalf("thread_subscriptions: want %d cols, got %d", wantCols, count)
	}
}

// countColumn reports how many columns named `column` exist on `table`.
// Expected values are 0 (missing) or 1 (present).
func countColumn(t *testing.T, db *DB, table, column string) int {
	t.Helper()
	var count int
	err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`,
		table, column,
	).Scan(&count)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	return count
}

// columnInfo reports the declared type, NOT NULL flag and DEFAULT
// literal of `table`.`column`, plus whether the column exists at all.
// dflt carries dflt_value verbatim as it appears in the DDL, so a
// string default arrives still quoted: an empty-string default reads
// back as a two-character value (a pair of single quotes), not as the
// empty string.
func columnInfo(t *testing.T, db *DB, table, column string) (ctype string, notnull int, dflt sql.NullString, found bool) {
	t.Helper()
	err := db.conn.QueryRow(
		`SELECT type, "notnull", dflt_value FROM pragma_table_info(?) WHERE name = ?`,
		table, column,
	).Scan(&ctype, &notnull, &dflt)
	if err == sql.ErrNoRows {
		return "", 0, sql.NullString{}, false
	}
	if err != nil {
		t.Fatalf("pragma_table_info(%s).%s: %v", table, column, err)
	}
	return ctype, notnull, dflt, true
}

// TestMigrate_AddsVersionColumns pins the *shape* of each version
// column, not just its presence. The type split is the substance of
// this migration: channels/users carry numeric versions, messages
// carries Slack's opaque version string ("1783024685.163100").
// SQLite type affinity is advisory, so declaring messages.version
// INTEGER would not fail loudly — it would silently coerce that
// string and corrupt the revalidation key, surfacing much later as
// Slack returning full records where empty ones were expected.
// Asserting type/notnull/default is what makes such a slip fail here.
func TestMigrate_AddsVersionColumns(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	for _, tc := range []struct {
		table, column string
		wantType      string
		wantNotNull   int
		wantDefault   string // dflt_value verbatim: quoted for string defaults
	}{
		{"channels", "version", "INTEGER", 1, "0"},
		{"users", "version", "INTEGER", 1, "0"},
		{"messages", "version", "TEXT", 1, "''"},
	} {
		t.Run(tc.table+"."+tc.column, func(t *testing.T) {
			ctype, notnull, dflt, found := columnInfo(t, db, tc.table, tc.column)
			if !found {
				t.Fatalf("column %s.%s missing after migrate()", tc.table, tc.column)
			}
			if ctype != tc.wantType {
				t.Errorf("%s.%s type = %q, want %q", tc.table, tc.column, ctype, tc.wantType)
			}
			if notnull != tc.wantNotNull {
				t.Errorf("%s.%s NOT NULL = %d, want %d", tc.table, tc.column, notnull, tc.wantNotNull)
			}
			if !dflt.Valid || dflt.String != tc.wantDefault {
				t.Errorf("%s.%s default = %v, want %q", tc.table, tc.column, dflt, tc.wantDefault)
			}
		})
	}
}

func TestMigrate_VersionColumnsAreIdempotent(t *testing.T) {
	// migrate() runs on every Open, so a second run must not error:
	// ALTER TABLE ADD COLUMN fails on a duplicate name, so an unguarded
	// add would break every Open after the first. The t.Fatalf below is
	// the load-bearing assertion. The count check that follows cannot
	// see a duplicate (sqlite would have errored above); it catches the
	// opposite failure, a guard that skips the add entirely and leaves
	// the column absent.
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	if err := db.migrate(); err != nil {
		t.Fatalf("second migrate(): %v", err)
	}

	for _, tc := range []struct{ table, column string }{
		{"channels", "version"},
		{"users", "version"},
		{"messages", "version"},
	} {
		if got := countColumn(t, db, tc.table, tc.column); got != 1 {
			t.Errorf("after second migrate(), %s.%s count = %d, want 1", tc.table, tc.column, got)
		}
	}
}

func TestMigration_AddsMentionCountColumn(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	rows, err := db.conn.Query("PRAGMA table_info(channels)")
	if err != nil {
		t.Fatalf("PRAGMA: %v", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "mention_count" {
			found = true
			if ctype != "INTEGER" {
				t.Errorf("mention_count type = %q, want INTEGER", ctype)
			}
			if notnull != 1 {
				t.Errorf("mention_count NOT NULL = %d, want 1", notnull)
			}
			if !dflt.Valid || dflt.String != "0" {
				t.Errorf("mention_count default = %v, want 0", dflt)
			}
		}
	}
	if !found {
		t.Fatal("mention_count column not added")
	}
}

// unread_count was declared in the original schema and never read or
// written. It was removed when mention_count landed, because two
// similarly-named count columns invite a future author to pick the wrong
// one. Fresh databases must not have it. Pre-existing databases keep the
// vestigial column harmlessly; no destructive migration is performed.
func TestSchema_FreshDBHasNoUnreadCountColumn(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	rows, err := db.conn.Query("PRAGMA table_info(channels)")
	if err != nil {
		t.Fatalf("PRAGMA: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "unread_count" {
			t.Error("unread_count column still present in fresh schema")
		}
	}
}
