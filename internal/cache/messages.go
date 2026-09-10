package cache

import (
	"fmt"

	"github.com/gammons/slk/internal/debuglog"
)

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
		debuglog.Cache("UpsertMessage: channel=%s ts=%s ERR=%v", m.ChannelID, m.TS, err)
		return fmt.Errorf("upserting message: %w", err)
	}
	debuglog.Cache("UpsertMessage: channel=%s ts=%s thread_ts=%s subtype=%q deleted=%v edited=%q",
		m.ChannelID, m.TS, m.ThreadTS, m.Subtype, m.IsDeleted, m.EditedAt)
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
	// Main channel feed includes three row shapes:
	//   1. Plain top-level messages: thread_ts = ''.
	//   2. Thread parents: thread_ts = ts (Slack's conversations.history
	//      sets a parent's thread_ts to its own ts once it has replies;
	//      WS events for the parent itself arrive with thread_ts = '',
	//      so the cache holds the empty form until a GetHistory fetch
	//      upserts the parent form).
	//   3. Thread broadcasts: thread_ts != '' but subtype = 'thread_broadcast'.
	// Plain replies (thread_ts != '' && thread_ts != ts && subtype != broadcast)
	// belong to the thread panel, not the main feed.
	inner := `
		SELECT ts, channel_id, workspace_id, user_id, text, thread_ts, reply_count, edited_at, is_deleted, raw_json, created_at, subtype
		FROM messages
		WHERE channel_id = ? AND is_deleted = 0
		  AND (thread_ts = '' OR thread_ts = ts OR subtype = 'thread_broadcast')`
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

// GetMessagesSince returns the cached main-feed messages at or after
// sinceTS, oldest first. The row-shape filter matches GetMessages: plain
// top-level messages, thread parents, and thread broadcasts, but not
// plain thread replies, which belong to the thread panel.
//
// Inclusive of sinceTS, because its caller is mark-unread: the message
// the user selected is itself unread.
//
// This reads the cache, not Slack, so it can only see messages slk has
// already fetched. For the case it exists to serve — counting mentions
// below a boundary the user just picked off their own screen — those
// messages are cached by construction. Marking something unread far
// enough back can undercount; client.counts corrects it on the next
// reconnect.
func (db *DB) GetMessagesSince(channelID, sinceTS string) ([]Message, error) {
	if sinceTS == "" {
		return nil, nil
	}
	return db.queryMessages(`
		SELECT ts, channel_id, workspace_id, user_id, text, thread_ts, reply_count, edited_at, is_deleted, raw_json, created_at, subtype
		FROM messages
		WHERE channel_id = ? AND is_deleted = 0 AND ts >= ?
		  AND (thread_ts = '' OR thread_ts = ts OR subtype = 'thread_broadcast')
		ORDER BY ts ASC`, channelID, sinceTS)
}

// GetMessage returns the message with the given (channel_id, ts) primary
// key. Returns sql.ErrNoRows if no such message exists.
func (db *DB) GetMessage(channelID, ts string) (Message, error) {
	var m Message
	var isDeleted int
	err := db.conn.QueryRow(`
		SELECT ts, channel_id, workspace_id, user_id, text, thread_ts, reply_count, edited_at, is_deleted, raw_json, created_at, subtype
		FROM messages
		WHERE channel_id = ? AND ts = ?
	`, channelID, ts).Scan(&m.TS, &m.ChannelID, &m.WorkspaceID, &m.UserID, &m.Text,
		&m.ThreadTS, &m.ReplyCount, &m.EditedAt, &isDeleted, &m.RawJSON, &m.CreatedAt, &m.Subtype)
	if err != nil {
		return m, err
	}
	m.IsDeleted = isDeleted == 1
	return m, nil
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
		debuglog.Cache("DeleteMessage: channel=%s ts=%s ERR=%v", channelID, ts, err)
		return fmt.Errorf("deleting message: %w", err)
	}
	debuglog.Cache("DeleteMessage: channel=%s ts=%s", channelID, ts)
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
