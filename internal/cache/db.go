package cache

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

func New(dsn string) (*DB, error) {
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}

	// Enable foreign keys
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS workspaces (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		domain TEXT NOT NULL DEFAULT '',
		icon_url TEXT NOT NULL DEFAULT '',
		last_synced_at INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		workspace_id TEXT NOT NULL,
		name TEXT NOT NULL,
		display_name TEXT NOT NULL DEFAULT '',
		avatar_url TEXT NOT NULL DEFAULT '',
		presence TEXT NOT NULL DEFAULT 'away',
		updated_at INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (workspace_id) REFERENCES workspaces(id)
	);

	CREATE TABLE IF NOT EXISTS channels (
		id TEXT PRIMARY KEY,
		workspace_id TEXT NOT NULL,
		name TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'channel',
		topic TEXT NOT NULL DEFAULT '',
		is_member INTEGER NOT NULL DEFAULT 0,
		is_starred INTEGER NOT NULL DEFAULT 0,
		last_read_ts TEXT NOT NULL DEFAULT '',
		unread_count INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (workspace_id) REFERENCES workspaces(id)
	);

	CREATE TABLE IF NOT EXISTS messages (
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
		subtype TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (ts, channel_id)
	);

	CREATE TABLE IF NOT EXISTS reactions (
		message_ts TEXT NOT NULL,
		channel_id TEXT NOT NULL,
		emoji TEXT NOT NULL,
		user_ids TEXT NOT NULL DEFAULT '[]',
		count INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (message_ts, channel_id, emoji)
	);

	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		message_ts TEXT NOT NULL DEFAULT '',
		channel_id TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL DEFAULT '',
		mimetype TEXT NOT NULL DEFAULT '',
		size INTEGER NOT NULL DEFAULT 0,
		url_private TEXT NOT NULL DEFAULT '',
		local_path TEXT NOT NULL DEFAULT '',
		thumbnail_path TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS frecent_emoji (
		emoji TEXT PRIMARY KEY,
		use_count INTEGER NOT NULL DEFAULT 0,
		last_used INTEGER NOT NULL DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_messages_channel ON messages(channel_id, ts);
	CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages(thread_ts, channel_id);
	CREATE INDEX IF NOT EXISTS idx_channels_workspace ON channels(workspace_id);
	CREATE INDEX IF NOT EXISTS idx_users_workspace ON users(workspace_id);
	`

	if _, err := db.conn.Exec(schema); err != nil {
		return err
	}

	// Idempotent column-level migrations for existing databases.
	// SQLite's ADD COLUMN has no IF NOT EXISTS, so we probe first.
	if err := db.addColumnIfMissing("messages", "subtype",
		"ALTER TABLE messages ADD COLUMN subtype TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	return nil
}

// addColumnIfMissing runs the given DDL only if the column isn't
// already present on the table. Used for additive schema migrations on
// pre-existing databases.
func (db *DB) addColumnIfMissing(table, column, ddl string) error {
	rows, err := db.conn.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("inspecting %s columns: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scanning %s columns: %w", table, err)
		}
		if name == column {
			return nil // already present
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := db.conn.Exec(ddl); err != nil {
		return fmt.Errorf("adding %s.%s: %w", table, column, err)
	}
	return nil
}
