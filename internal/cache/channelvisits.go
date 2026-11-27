package cache

import (
	"fmt"
	"time"
)

// RecordChannelVisit upserts the (workspace_id, channel_id) row to the
// current unix timestamp. Used by the App when the user navigates to a
// channel so the Ctrl+T finder can order entries by recency.
func (db *DB) RecordChannelVisit(workspaceID, channelID string) error {
	now := time.Now().Unix()
	_, err := db.conn.Exec(`
		INSERT INTO channel_visits (workspace_id, channel_id, last_visited)
		VALUES (?, ?, ?)
		ON CONFLICT(workspace_id, channel_id)
		DO UPDATE SET last_visited = excluded.last_visited`,
		workspaceID, channelID, now,
	)
	if err != nil {
		return fmt.Errorf("recording channel visit: %w", err)
	}
	return nil
}

// GetChannelVisits returns a map of channel_id -> last_visited (unix
// seconds) for the given workspace. Used at workspace-connect time to
// seed the in-memory map that the channel finder consults for sorting.
func (db *DB) GetChannelVisits(workspaceID string) (map[string]int64, error) {
	// A Grid shared DM has one channels row but can be visited from more
	// than one member workspace. Include the DM's visit regardless of
	// which workspace recorded it, taking the latest; channels stay
	// workspace-local.
	rows, err := db.conn.Query(`
		SELECT v.channel_id, MAX(v.last_visited)
		FROM channel_visits v
		LEFT JOIN channels c ON c.id = v.channel_id
		WHERE v.workspace_id = ? OR c.type IN ('dm', 'group_dm')
		GROUP BY v.channel_id`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying channel visits: %w", err)
	}
	defer rows.Close()

	out := make(map[string]int64)
	for rows.Next() {
		var channelID string
		var lastVisited int64
		if err := rows.Scan(&channelID, &lastVisited); err != nil {
			return nil, fmt.Errorf("scanning channel visit: %w", err)
		}
		out[channelID] = lastVisited
	}
	return out, rows.Err()
}
