package cache

import "fmt"

// ChannelVersions returns {channelID: version} for every cached channel
// in the workspace, for use as edgeapi's updated_ids. A channel with no
// recorded version appears with 0, which is how the official client
// asks for a full record.
func (db *DB) ChannelVersions(workspaceID string) (map[string]int64, error) {
	rows, err := db.conn.Query(
		`SELECT id, version FROM channels WHERE workspace_id = ?`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing channel versions: %w", err)
	}
	defer rows.Close()

	out := make(map[string]int64, 256)
	for rows.Next() {
		var id string
		var v int64
		if err := rows.Scan(&id, &v); err != nil {
			return nil, fmt.Errorf("scanning channel version: %w", err)
		}
		out[id] = v
	}
	return out, rows.Err()
}

// SetChannelVersion records the version stamp Slack returned for a
// channel. Implemented as an UPDATE so it only touches existing rows;
// callers must have UpsertChannel'd the channel first, otherwise this
// is a silent no-op (rows-affected is not checked).
//
// The cost of that no-op is higher here than for SetChannelSyncedAt: a
// dropped synced_at buys one redundant fetch, whereas a dropped version
// leaves the row at 0 permanently, so every boot sends 0 in updated_ids
// and pulls the full record forever. Nothing ever reports the loss.
func (db *DB) SetChannelVersion(channelID string, version int64) error {
	_, err := db.conn.Exec(
		`UPDATE channels SET version = ? WHERE id = ?`, version, channelID)
	if err != nil {
		return fmt.Errorf("setting channel version: %w", err)
	}
	return nil
}

// UserVersions returns {userID: version} for every cached user in the
// workspace. Same semantics as ChannelVersions.
func (db *DB) UserVersions(workspaceID string) (map[string]int64, error) {
	rows, err := db.conn.Query(
		`SELECT id, version FROM users WHERE workspace_id = ?`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing user versions: %w", err)
	}
	defer rows.Close()

	out := make(map[string]int64, 1024)
	for rows.Next() {
		var id string
		var v int64
		if err := rows.Scan(&id, &v); err != nil {
			return nil, fmt.Errorf("scanning user version: %w", err)
		}
		out[id] = v
	}
	return out, rows.Err()
}

// SetUserVersion records the version stamp Slack returned for a user.
// Implemented as an UPDATE so it only touches existing rows; callers
// must have UpsertUser'd the user first, otherwise this is a silent
// no-op (rows-affected is not checked). Same permanent cost as
// SetChannelVersion: the row stays at 0 and is re-fetched in full on
// every boot.
func (db *DB) SetUserVersion(userID string, version int64) error {
	_, err := db.conn.Exec(
		`UPDATE users SET version = ? WHERE id = ?`, version, userID)
	if err != nil {
		return fmt.Errorf("setting user version: %w", err)
	}
	return nil
}

// MessageVersions returns {ts: version} for cached messages in the
// channel within [oldestTS, latestTS], for use as
// conversations.history's cached_latest_updates. Messages with no
// recorded version are omitted: that parameter is an assertion about
// what we hold, and we can only vouch for messages Slack has
// versioned.
func (db *DB) MessageVersions(channelID, oldestTS, latestTS string) (map[string]string, error) {
	rows, err := db.conn.Query(
		`SELECT ts, version FROM messages
		 WHERE channel_id = ? AND ts >= ? AND ts <= ? AND version != ''`,
		channelID, oldestTS, latestTS)
	if err != nil {
		return nil, fmt.Errorf("listing message versions: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string, 64)
	for rows.Next() {
		var ts, v string
		if err := rows.Scan(&ts, &v); err != nil {
			return nil, fmt.Errorf("scanning message version: %w", err)
		}
		out[ts] = v
	}
	return out, rows.Err()
}

// SetMessageVersion records the version stamp from
// conversations.history's latest_updates. Implemented as an UPDATE so
// it only touches existing rows; callers must have UpsertMessage'd the
// message first, otherwise this is a silent no-op (rows-affected is not
// checked). Same permanent cost as SetChannelVersion: the message stays
// unversioned, so MessageVersions omits it and every window that covers
// it is re-fetched in full.
//
// channelID is not optional. `messages` is keyed on (ts, channel_id)
// and Slack ts values collide across channels, so an UPDATE matching on
// ts alone stamps some other channel's message.
func (db *DB) SetMessageVersion(channelID, ts, version string) error {
	_, err := db.conn.Exec(
		`UPDATE messages SET version = ? WHERE channel_id = ? AND ts = ?`,
		version, channelID, ts)
	if err != nil {
		return fmt.Errorf("setting message version: %w", err)
	}
	return nil
}
