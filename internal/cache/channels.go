package cache

import (
	"database/sql"
	"fmt"
)

type Channel struct {
	ID          string
	WorkspaceID string
	Name        string
	Type        string // channel, dm, group_dm, private
	Topic       string
	IsMember    bool
	IsStarred   bool
	LastReadTS  string
	UnreadCount int
	UpdatedAt   int64
}

func (db *DB) UpsertChannel(ch Channel) error {
	_, err := db.conn.Exec(`
		INSERT INTO channels (id, workspace_id, name, type, topic, is_member, is_starred, last_read_ts, unread_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			type=excluded.type,
			topic=excluded.topic,
			is_member=excluded.is_member,
			is_starred=excluded.is_starred,
			last_read_ts=excluded.last_read_ts,
			unread_count=excluded.unread_count,
			updated_at=excluded.updated_at
	`, ch.ID, ch.WorkspaceID, ch.Name, ch.Type, ch.Topic,
		boolToInt(ch.IsMember), boolToInt(ch.IsStarred),
		ch.LastReadTS, ch.UnreadCount, ch.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upserting channel: %w", err)
	}
	return nil
}

func (db *DB) GetChannel(id string) (Channel, error) {
	var ch Channel
	var isMember, isStarred int
	err := db.conn.QueryRow(`
		SELECT id, workspace_id, name, type, topic, is_member, is_starred, last_read_ts, unread_count, updated_at
		FROM channels WHERE id = ?
	`, id).Scan(&ch.ID, &ch.WorkspaceID, &ch.Name, &ch.Type, &ch.Topic,
		&isMember, &isStarred, &ch.LastReadTS, &ch.UnreadCount, &ch.UpdatedAt)
	if err != nil {
		return ch, fmt.Errorf("getting channel: %w", err)
	}
	ch.IsMember = isMember == 1
	ch.IsStarred = isStarred == 1
	return ch, nil
}

func (db *DB) ListChannels(workspaceID string, membersOnly bool) ([]Channel, error) {
	query := `
		SELECT id, workspace_id, name, type, topic, is_member, is_starred, last_read_ts, unread_count, updated_at
		FROM channels WHERE workspace_id = ?`
	args := []any{workspaceID}

	if membersOnly {
		query += " AND is_member = 1"
	}
	query += " ORDER BY name"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing channels: %w", err)
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
		var isMember, isStarred int
		if err := rows.Scan(&ch.ID, &ch.WorkspaceID, &ch.Name, &ch.Type, &ch.Topic,
			&isMember, &isStarred, &ch.LastReadTS, &ch.UnreadCount, &ch.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning channel: %w", err)
		}
		ch.IsMember = isMember == 1
		ch.IsStarred = isStarred == 1
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (db *DB) UpdateUnreadCount(channelID string, count int) error {
	_, err := db.conn.Exec(`UPDATE channels SET unread_count = ? WHERE id = ?`, count, channelID)
	if err != nil {
		return fmt.Errorf("updating unread count: %w", err)
	}
	return nil
}

// UpdateLastReadTS sets the last read timestamp for a channel.
func (db *DB) UpdateLastReadTS(channelID, ts string) error {
	_, err := db.conn.Exec(
		`UPDATE channels SET last_read_ts = ? WHERE id = ?`,
		ts, channelID,
	)
	if err != nil {
		return fmt.Errorf("updating last_read_ts: %w", err)
	}
	return nil
}

// GetLastReadTS returns the last read timestamp for a channel.
func (db *DB) GetLastReadTS(channelID string) (string, error) {
	var ts string
	err := db.conn.QueryRow(
		`SELECT last_read_ts FROM channels WHERE id = ?`,
		channelID,
	).Scan(&ts)
	if err != nil {
		return "", err
	}
	return ts, nil
}

// SetChannelSyncedAt stores the unix timestamp (seconds) at which the
// channel's message cache was last authoritatively replaced from the
// network. Implemented as an UPDATE so it only touches existing rows;
// callers must have UpsertChannel'd the channel first, otherwise this
// is a silent no-op (rows-affected is not checked).
func (db *DB) SetChannelSyncedAt(channelID string, unixSec int64) error {
	_, err := db.conn.Exec(`UPDATE channels SET synced_at = ? WHERE id = ?`, unixSec, channelID)
	if err != nil {
		return fmt.Errorf("setting channel synced_at: %w", err)
	}
	return nil
}

// GetChannelSyncedAt returns the unix timestamp recorded by
// SetChannelSyncedAt, or 0 if the channel row is missing or the column
// was never set. The zero return doubles as the "never synced" signal
// the UI layer uses to fall into the spinner-only display tier.
func (db *DB) GetChannelSyncedAt(channelID string) int64 {
	var syncedAt int64
	err := db.conn.QueryRow(`SELECT synced_at FROM channels WHERE id = ?`, channelID).Scan(&syncedAt)
	if err != nil {
		return 0
	}
	return syncedAt
}

// SetChannelLatestSyncedTS stores a Slack message timestamp (string
// form, e.g. "1700000000.000123") that represents the watermark of
// "we have no gaps in this channel above this ts." Unlike synced_at,
// this is a Slack-domain value, not a wall-clock timestamp.
//
// Implemented as an UPDATE so it only touches existing rows; callers
// must have UpsertChannel'd the channel first. Rows-affected is not
// checked — a missing row is a silent no-op, matching the established
// pattern in SetChannelSyncedAt.
func (db *DB) SetChannelLatestSyncedTS(channelID, ts string) error {
	_, err := db.conn.Exec(
		`UPDATE channels SET latest_synced_ts = ? WHERE id = ?`,
		ts, channelID,
	)
	if err != nil {
		return fmt.Errorf("setting channel latest_synced_ts: %w", err)
	}
	return nil
}

// GetChannelLatestSyncedTS returns the watermark set by
// SetChannelLatestSyncedTS, or "" if the channel row is missing or the
// column was never written. The empty return doubles as the
// "no prior sync" signal that backfill uses to decide between
// pagination from a known cursor vs. a one-shot "fetch latest page"
// request.
func (db *DB) GetChannelLatestSyncedTS(channelID string) string {
	var ts string
	err := db.conn.QueryRow(
		`SELECT latest_synced_ts FROM channels WHERE id = ?`,
		channelID,
	).Scan(&ts)
	if err != nil {
		return ""
	}
	return ts
}

// AdvanceChannelLatestSyncedTS sets latest_synced_ts to ts only if ts
// is lexicographically greater than the current value. Slack ts
// strings (e.g., "1700000000.123456") sort lexicographically the same
// way they sort numerically when both are normalized to the same
// decimal width, which Slack always emits, so string compare is safe.
//
// This is the operation real-time WS message handlers should use: a
// WS-delivered message is always newer than any prior WS-delivered
// message on the same connection, but during reconnect or a race we
// may briefly process an event with ts older than our recorded
// watermark (e.g., a delayed thread reply that we already backfilled).
// In that case we must NOT regress the watermark.
//
// Returns the resulting watermark (either the new ts or the
// pre-existing value) and any error from the database. A missing row
// behaves as if the watermark was empty: the new ts is written.
func (db *DB) AdvanceChannelLatestSyncedTS(channelID, ts string) (string, error) {
	if ts == "" {
		return db.GetChannelLatestSyncedTS(channelID), nil
	}
	// UPDATE ... WHERE ts > existing returns rows-affected=0 if the
	// proposed ts is not strictly greater; that is the no-regress case.
	_, err := db.conn.Exec(
		`UPDATE channels
		 SET latest_synced_ts = ?
		 WHERE id = ? AND ? > latest_synced_ts`,
		ts, channelID, ts,
	)
	if err != nil {
		return "", fmt.Errorf("advancing channel latest_synced_ts: %w", err)
	}
	return db.GetChannelLatestSyncedTS(channelID), nil
}

// MaxMessageTSForChannel returns the highest message ts stored for
// the given channel, or "" if the channel has no cached messages.
// Used by GetChannelWatermark as the fallback when latest_synced_ts
// has never been written (i.e., this is a pre-migration channel whose
// cache predates the latest_synced_ts column).
//
// The query uses idx_messages_channel(channel_id, ts) and is a
// straight index lookup — it does not scan the table.
func (db *DB) MaxMessageTSForChannel(channelID string) (string, error) {
	var ts sql.NullString
	err := db.conn.QueryRow(
		`SELECT MAX(ts) FROM messages WHERE channel_id = ?`,
		channelID,
	).Scan(&ts)
	if err != nil {
		return "", fmt.Errorf("max message ts: %w", err)
	}
	if !ts.Valid {
		return "", nil
	}
	return ts.String, nil
}

// GetChannelWatermark returns the per-channel sync watermark used by
// the reconnect backfill. It prefers the explicit latest_synced_ts
// column (set by real-time WS handlers and completed backfill batches)
// and falls back to MAX(ts) FROM messages for channels whose cache
// predates the latest_synced_ts migration. Returns "" only if both
// sources are empty, which means "no prior sync — fetch the latest
// page only."
func (db *DB) GetChannelWatermark(channelID string) (string, error) {
	if v := db.GetChannelLatestSyncedTS(channelID); v != "" {
		return v, nil
	}
	return db.MaxMessageTSForChannel(channelID)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
