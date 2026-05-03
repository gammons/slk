package cache

import "fmt"

type Message struct {
	TS          string
	ChannelID   string
	WorkspaceID string
	UserID      string
	Text        string
	ThreadTS    string
	ReplyCount  int
	EditedAt    string
	IsDeleted   bool
	RawJSON     string
	CreatedAt   int64
	// Subtype mirrors Slack's `subtype` field. We persist it so that
	// thread_broadcast messages (thread replies that the author also
	// posted to the channel) survive a restart and can be labeled in
	// the main channel feed.
	Subtype string
}

func (db *DB) UpsertMessage(m Message) error {
	_, err := db.conn.Exec(`
		INSERT INTO messages (ts, channel_id, workspace_id, user_id, text, thread_ts, reply_count, edited_at, is_deleted, raw_json, created_at, subtype)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(ts, channel_id) DO UPDATE SET
			user_id=excluded.user_id,
			text=excluded.text,
			thread_ts=excluded.thread_ts,
			reply_count=excluded.reply_count,
			edited_at=excluded.edited_at,
			is_deleted=excluded.is_deleted,
			raw_json=excluded.raw_json,
			subtype=excluded.subtype
	`, m.TS, m.ChannelID, m.WorkspaceID, m.UserID, m.Text, m.ThreadTS,
		m.ReplyCount, m.EditedAt, boolToInt(m.IsDeleted), m.RawJSON, m.CreatedAt, m.Subtype)
	if err != nil {
		return fmt.Errorf("upserting message: %w", err)
	}
	return nil
}

// GetMessages returns the NEWEST `limit` messages for a channel,
// ordered by ts ascending in the result. If beforeTS is non-empty,
// only considers messages with ts < beforeTS (for pagination).
//
// We pick newest-first inside a subquery and re-sort ascending in the
// outer query so callers (UI render path, history backfill) get the
// recency-anchored window they want without having to reverse it
// themselves. A naive `ORDER BY ts ASC LIMIT N` would return the
// OLDEST N rows, which is wrong for any cache that keeps growing as
// new messages arrive.
func (db *DB) GetMessages(channelID string, limit int, beforeTS string) ([]Message, error) {
	// Main channel feed = top-level messages (thread_ts empty) plus
	// thread broadcasts (thread_ts set, but subtype=thread_broadcast).
	inner := `
		SELECT ts, channel_id, workspace_id, user_id, text, thread_ts, reply_count, edited_at, is_deleted, raw_json, created_at, subtype
		FROM messages
		WHERE channel_id = ? AND is_deleted = 0
		  AND (thread_ts = '' OR subtype = 'thread_broadcast')`
	args := []any{channelID}

	if beforeTS != "" {
		inner += " AND ts < ?"
		args = append(args, beforeTS)
	}

	inner += " ORDER BY ts DESC LIMIT ?"
	args = append(args, limit)

	query := "SELECT * FROM (" + inner + ") ORDER BY ts ASC"

	return db.queryMessages(query, args...)
}

func (db *DB) GetThreadReplies(channelID, threadTS string) ([]Message, error) {
	query := `
		SELECT ts, channel_id, workspace_id, user_id, text, thread_ts, reply_count, edited_at, is_deleted, raw_json, created_at, subtype
		FROM messages
		WHERE channel_id = ? AND thread_ts = ? AND is_deleted = 0
		ORDER BY ts ASC`

	return db.queryMessages(query, channelID, threadTS)
}

func (db *DB) DeleteMessage(channelID, ts string) error {
	_, err := db.conn.Exec(`UPDATE messages SET is_deleted = 1 WHERE channel_id = ? AND ts = ?`, channelID, ts)
	if err != nil {
		return fmt.Errorf("deleting message: %w", err)
	}
	return nil
}

func (db *DB) queryMessages(query string, args ...any) ([]Message, error) {
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		var isDeleted int
		if err := rows.Scan(&m.TS, &m.ChannelID, &m.WorkspaceID, &m.UserID, &m.Text,
			&m.ThreadTS, &m.ReplyCount, &m.EditedAt, &isDeleted, &m.RawJSON, &m.CreatedAt, &m.Subtype); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		m.IsDeleted = isDeleted == 1
		messages = append(messages, m)
	}
	return messages, rows.Err()
}
