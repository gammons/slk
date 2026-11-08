package cache

import (
	"database/sql"
	"errors"
	"fmt"
)

// CustomEmoji returns a workspace's cached emoji name -> value map,
// empty (never nil) when nothing is cached. Values are verbatim from
// emoji.list: a URL or "alias:target".
func (db *DB) CustomEmoji(workspaceID string) (map[string]string, error) {
	rows, err := db.conn.Query(`
		SELECT name, value FROM custom_emoji WHERE workspace_id = ?
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing custom emoji: %w", err)
	}
	defer rows.Close()

	emojis := make(map[string]string)
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return nil, fmt.Errorf("scanning custom emoji: %w", err)
		}
		emojis[name] = value
	}
	return emojis, rows.Err()
}

// ReplaceCustomEmoji atomically replaces a workspace's cached set and
// records when it was fetched. fetchedAt is a Unix second timestamp.
func (db *DB) ReplaceCustomEmoji(workspaceID string, emojis map[string]string, fetchedAt int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin replace custom emoji: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // committed below on success

	if _, err := tx.Exec(`DELETE FROM custom_emoji WHERE workspace_id = ?`, workspaceID); err != nil {
		return fmt.Errorf("clearing custom emoji: %w", err)
	}

	stmt, err := tx.Prepare(`INSERT INTO custom_emoji (workspace_id, name, value) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()
	for name, value := range emojis {
		if _, err := stmt.Exec(workspaceID, name, value); err != nil {
			return fmt.Errorf("insert custom emoji %s: %w", name, err)
		}
	}

	if _, err := tx.Exec(`
		INSERT INTO custom_emoji_meta (workspace_id, fetched_at)
		VALUES (?, ?)
		ON CONFLICT(workspace_id) DO UPDATE SET fetched_at=excluded.fetched_at
	`, workspaceID, fetchedAt); err != nil {
		return fmt.Errorf("upserting custom emoji meta: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace custom emoji: %w", err)
	}
	return nil
}

// UpsertCustomEmoji inserts or updates one emoji. Used by emoji_changed
// deltas, which leave fetched_at alone: only a full emoji.list resets
// the TTL.
func (db *DB) UpsertCustomEmoji(workspaceID, name, value string) error {
	_, err := db.conn.Exec(`
		INSERT INTO custom_emoji (workspace_id, name, value)
		VALUES (?, ?, ?)
		ON CONFLICT(workspace_id, name) DO UPDATE SET value=excluded.value
	`, workspaceID, name, value)
	if err != nil {
		return fmt.Errorf("upserting custom emoji: %w", err)
	}
	return nil
}

// DeleteCustomEmoji removes the named emoji. Names that aren't cached
// are ignored.
func (db *DB) DeleteCustomEmoji(workspaceID string, names []string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin delete custom emoji: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // committed below on success

	for _, name := range names {
		if _, err := tx.Exec(`
			DELETE FROM custom_emoji WHERE workspace_id = ? AND name = ?
		`, workspaceID, name); err != nil {
			return fmt.Errorf("deleting custom emoji %s: %w", name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete custom emoji: %w", err)
	}
	return nil
}

// ZeroCustomEmojiFetchedAt marks a workspace's cache stale without
// dropping its rows, so the next launch still paints from it and then
// refetches. No-op (no error) if the workspace was never fetched.
func (db *DB) ZeroCustomEmojiFetchedAt(workspaceID string) error {
	_, err := db.conn.Exec(`
		UPDATE custom_emoji_meta SET fetched_at = 0 WHERE workspace_id = ?
	`, workspaceID)
	if err != nil {
		return fmt.Errorf("zeroing custom emoji meta: %w", err)
	}
	return nil
}

// CustomEmojiFetchedAt returns when emoji.list last filled the cache.
// ok=false if it never has, which is distinct from a workspace that was
// fetched and has no custom emoji.
func (db *DB) CustomEmojiFetchedAt(workspaceID string) (int64, bool, error) {
	var ts int64
	err := db.conn.QueryRow(`
		SELECT fetched_at FROM custom_emoji_meta WHERE workspace_id = ?
	`, workspaceID).Scan(&ts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("getting custom emoji meta: %w", err)
	}
	return ts, true, nil
}
