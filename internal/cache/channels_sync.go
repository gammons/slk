package cache

import (
	"fmt"
	"sort"
)

// ChannelSyncRow is a (channelID, synced_at) pair used by the
// reconnect backfill to drive per-channel conversations.history calls.
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

// BackfillCandidates returns the union of (channels with at least one
// cached message) and (channels listed in `unreadChannelIDs`),
// de-duplicated and stable-sorted by channel ID. SyncedAt on the
// returned rows is the channel's wall-clock synced_at (0 for channels
// not in the channels table). The caller resolves the ts watermark
// per-row via GetChannelWatermark.
//
// This is the reconnect-backfill driver's source of truth: the cached
// branch covers the steady-state "catch up on rooms I read regularly,"
// the unread branch covers "I was offline and got a DM in a room I've
// never opened."
func (db *DB) BackfillCandidates(workspaceID string, unreadChannelIDs []string) ([]ChannelSyncRow, error) {
	seen := make(map[string]int64, 32) // channelID -> synced_at (0 if not in channels table)

	cached, err := db.ChannelsWithMessages(workspaceID)
	if err != nil {
		return nil, err
	}
	for _, r := range cached {
		seen[r.ChannelID] = r.SyncedAt
	}

	for _, id := range unreadChannelIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		// Look up synced_at if the channel row exists. A miss leaves
		// SyncedAt at 0, which GetChannelWatermark handles correctly.
		var sa int64
		_ = db.conn.QueryRow(
			`SELECT synced_at FROM channels WHERE id = ?`, id,
		).Scan(&sa)
		seen[id] = sa
	}

	out := make([]ChannelSyncRow, 0, len(seen))
	for id, sa := range seen {
		out = append(out, ChannelSyncRow{ChannelID: id, SyncedAt: sa})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ChannelID < out[j].ChannelID })
	return out, nil
}
