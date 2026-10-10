package cache

import "fmt"

// Mark is one row in the marks table: the location a mark names plus
// the preview snapshot taken at mark time (channel name, author
// display name, message excerpt). The snapshot exists so the marks
// overlay can render offline and immediately after a restart, with no
// network round trip; the jump resolves live and may land on newer
// content than the snapshot.
type Mark struct {
	WorkspaceID string
	Letter      string
	ChannelID   string
	MessageTS   string
	ThreadTS    string
	ChannelName string
	AuthorName  string
	Excerpt     string
}

// UpsertMark inserts or replaces the (workspace_id, letter) row in
// place. There is no history and no eviction: 52 letters per workspace
// is the bound, and overwriting a letter replaces the previous
// location without confirmation (vim semantics).
func (db *DB) UpsertMark(workspaceID, letter string, m Mark) error {
	if workspaceID == "" || letter == "" {
		return fmt.Errorf("UpsertMark: workspace_id/letter required")
	}
	const q = `
INSERT INTO marks
    (workspace_id, letter, channel_id, message_ts, thread_ts, channel_name, author_name, excerpt)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(workspace_id, letter) DO UPDATE SET
    channel_id   = excluded.channel_id,
    message_ts   = excluded.message_ts,
    thread_ts    = excluded.thread_ts,
    channel_name = excluded.channel_name,
    author_name  = excluded.author_name,
    excerpt      = excluded.excerpt
`
	_, err := db.conn.Exec(q, workspaceID, letter,
		m.ChannelID, m.MessageTS, m.ThreadTS, m.ChannelName, m.AuthorName, m.Excerpt)
	if err != nil {
		return fmt.Errorf("upserting mark %s/%s: %w", workspaceID, letter, err)
	}
	return nil
}

// ListMarks returns every mark row for the workspace, in letter order.
func (db *DB) ListMarks(workspaceID string) ([]Mark, error) {
	const q = `
SELECT workspace_id, letter, channel_id, message_ts, thread_ts, channel_name, author_name, excerpt
FROM marks
WHERE workspace_id = ?
ORDER BY letter
`
	rows, err := db.conn.Query(q, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing marks: %w", err)
	}
	defer rows.Close()

	var out []Mark
	for rows.Next() {
		var m Mark
		if err := rows.Scan(&m.WorkspaceID, &m.Letter, &m.ChannelID, &m.MessageTS,
			&m.ThreadTS, &m.ChannelName, &m.AuthorName, &m.Excerpt); err != nil {
			return nil, fmt.Errorf("scanning mark: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteMark removes the (workspace_id, letter) row. Deleting a letter
// that has no mark is a harmless no-op.
func (db *DB) DeleteMark(workspaceID, letter string) error {
	const q = `DELETE FROM marks WHERE workspace_id=? AND letter=?`
	if _, err := db.conn.Exec(q, workspaceID, letter); err != nil {
		return fmt.Errorf("deleting mark %s/%s: %w", workspaceID, letter, err)
	}
	return nil
}
