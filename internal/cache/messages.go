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
}

func (db *DB) UpsertMessage(m Message) error {
	_, err := db.conn.Exec(`
		INSERT INTO messages (ts, channel_id, workspace_id, user_id, text, thread_ts, reply_count, edited_at, is_deleted, raw_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(ts, channel_id) DO UPDATE SET
			user_id=excluded.user_id,
			text=excluded.text,
			thread_ts=excluded.thread_ts,
			reply_count=excluded.reply_count,
			edited_at=excluded.edited_at,
			is_deleted=excluded.is_deleted,
			raw_json=excluded.raw_json
	`, m.TS, m.ChannelID, m.WorkspaceID, m.UserID, m.Text, m.ThreadTS,
		m.ReplyCount, m.EditedAt, boolToInt(m.IsDeleted), m.RawJSON, m.CreatedAt)
	if err != nil {
		return fmt.Errorf("upserting message: %w", err)
	}
	return nil
}

// GetMessages returns messages for a channel, ordered by ts ascending.
// If beforeTS is non-empty, only returns messages with ts < beforeTS (for pagination).
func (db *DB) GetMessages(channelID string, limit int, beforeTS string) ([]Message, error) {
	query := `
		SELECT ts, channel_id, workspace_id, user_id, text, thread_ts, reply_count, edited_at, is_deleted, raw_json, created_at
		FROM messages
		WHERE channel_id = ? AND is_deleted = 0 AND thread_ts = ''`
	args := []any{channelID}

	if beforeTS != "" {
		query += " AND ts < ?"
		args = append(args, beforeTS)
	}

	query += " ORDER BY ts ASC LIMIT ?"
	args = append(args, limit)

	return db.queryMessages(query, args...)
}

func (db *DB) GetThreadReplies(channelID, threadTS string) ([]Message, error) {
	query := `
		SELECT ts, channel_id, workspace_id, user_id, text, thread_ts, reply_count, edited_at, is_deleted, raw_json, created_at
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
			&m.ThreadTS, &m.ReplyCount, &m.EditedAt, &isDeleted, &m.RawJSON, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		m.IsDeleted = isDeleted == 1
		messages = append(messages, m)
	}
	return messages, rows.Err()
}
