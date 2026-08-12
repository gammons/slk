package cache

import "fmt"

// ChannelSyncRow is a (channelID, synced_at) pair describing what the
// cache holds for a channel.
//
// It used to drive the reconnect backfill's per-channel
// conversations.history fan-out; that caller is gone (see
// DB.MarkChannelsStale for what replaced it) and this is now a plain
// query with no fan-out semantics attached.
//
// SyncedAt is the unix-second timestamp recorded by
// SetChannelSyncedAt; 0 means the channel row is missing or the
// column was never set (treat as "no prior sync — fetch latest page
// only" upstream).
type ChannelSyncRow struct {
	ChannelID string
	SyncedAt  int64
}

// ChannelsWithMessages returns one ChannelSyncRow per distinct
// channel_id in the messages table for the given workspace. Channels
// without any cached messages are excluded — they were either never
// visited in slk or never received a WS message event, so there is
// nothing to "catch up on" via reconnect backfill.
//
// The LEFT JOIN against channels means messages whose channel row
// was never UpsertChannel'd still appear (with SyncedAt=0). This
// happens when WS pushes a message for a channel slk hadn't
// discovered via conversations.list yet.
func (db *DB) ChannelsWithMessages(workspaceID string) ([]ChannelSyncRow, error) {
	const q = `
SELECT DISTINCT m.channel_id, COALESCE(c.synced_at, 0) AS synced_at
FROM messages m
LEFT JOIN channels c ON c.id = m.channel_id
WHERE m.workspace_id = ?
ORDER BY m.channel_id
`
	rows, err := db.conn.Query(q, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing channels with messages: %w", err)
	}
	defer rows.Close()

	var out []ChannelSyncRow
	for rows.Next() {
		var r ChannelSyncRow
		if err := rows.Scan(&r.ChannelID, &r.SyncedAt); err != nil {
			return nil, fmt.Errorf("scanning channels_sync row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
