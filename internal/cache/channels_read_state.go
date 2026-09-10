package cache

import (
	"database/sql"
	"fmt"
)

// ReadState captures the per-channel read-state values that drive the
// unread dot, the mention badge, and the "new messages" line. It is the
// canonical type for passing read state across package boundaries.
type ReadState struct {
	LastReadTS string
	HasUnread  bool
	// MentionCount is the number of unread direct mentions: an explicit
	// @user or an @here/@channel/@everyone broadcast. For DMs and group
	// DMs (Slack's ims and mpims), client.counts is believed to report
	// every unread message here, which is what makes the sidebar badge
	// match the official client without a client-side branch — that
	// reading is unverified against a live capture; see UnreadInfo's doc
	// in internal/slack/client.go. Rendering gates it on HasUnread.
	MentionCount int
}

// ChannelReadStateUpdate is one entry in a batched read-state write.
// LastReadTS == "" means "preserve the existing last_read_ts" (used by
// events that update has_unread only, e.g. new-message arrivals).
// MentionCount is always applied: both batch writers are fed from
// client.counts, which is authoritative.
type ChannelReadStateUpdate struct {
	ChannelID    string
	LastReadTS   string
	HasUnread    bool
	MentionCount int
}

// UpdateChannelReadState atomically updates the per-channel read state.
// If lastReadTS == "", the existing last_read_ts is preserved.
//
// Outside the batch writers below (BatchUpdateChannelReadState and
// ReplaceWorkspaceReadState, which apply authoritative snapshots on
// bootstrap and reconnect), this is the only function permitted to
// modify last_read_ts or has_unread. It never touches mention_count:
// that column belongs to SetChannelMentionCount and
// IncrementChannelMentionCount, the deliberate exception.
func (db *DB) UpdateChannelReadState(channelID, lastReadTS string, hasUnread bool) error {
	var q string
	var args []any
	if lastReadTS == "" {
		q = `UPDATE channels SET has_unread = ? WHERE id = ?`
		args = []any{boolToInt(hasUnread), channelID}
	} else {
		q = `UPDATE channels SET last_read_ts = ?, has_unread = ? WHERE id = ?`
		args = []any{lastReadTS, boolToInt(hasUnread), channelID}
	}
	if _, err := db.conn.Exec(q, args...); err != nil {
		return fmt.Errorf("updating channel read state: %w", err)
	}
	return nil
}

// SetChannelMentionCount overwrites the channel's mention count with an
// authoritative value. Callers: the *_marked WS handler (server-supplied
// count) and the read paths that clear it (markChannelReadAsync, the u-key
// mark-unread).
//
// Deliberately separate from UpdateChannelReadState rather than a
// parameter on it: "leave the mention count alone" is the common case, and
// expressing it as a parameter value would have forced ~20 existing test
// call sites to spell out a no-op.
func (db *DB) SetChannelMentionCount(channelID string, n int) error {
	if _, err := db.conn.Exec(
		`UPDATE channels SET mention_count = ? WHERE id = ?`,
		n, channelID,
	); err != nil {
		return fmt.Errorf("setting channel mention count: %w", err)
	}
	return nil
}

// IncrementChannelMentionCount adds one to the channel's mention count.
// Used by the inbound-message path, which detects mentions locally and has
// no server-supplied total to set. The addition happens in SQL so it is
// atomic and cannot lose a concurrent update. Like SetChannelMentionCount
// it writes mention_count and nothing else.
func (db *DB) IncrementChannelMentionCount(channelID string) error {
	if _, err := db.conn.Exec(
		`UPDATE channels SET mention_count = mention_count + 1 WHERE id = ?`,
		channelID,
	); err != nil {
		return fmt.Errorf("incrementing channel mention count: %w", err)
	}
	return nil
}

// BatchUpdateChannelReadState writes multiple updates in a single
// transaction. Used by bootstrap and reconnect catch-up paths.
func (db *DB) BatchUpdateChannelReadState(updates []ChannelReadStateUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin batch read-state tx: %w", err)
	}
	stmtBoth, err := tx.Prepare(`UPDATE channels SET last_read_ts = ?, has_unread = ?, mention_count = ? WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare both: %w", err)
	}
	defer stmtBoth.Close()
	stmtFlag, err := tx.Prepare(`UPDATE channels SET has_unread = ?, mention_count = ? WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare flag: %w", err)
	}
	defer stmtFlag.Close()

	for _, u := range updates {
		if u.LastReadTS == "" {
			if _, err := stmtFlag.Exec(boolToInt(u.HasUnread), u.MentionCount, u.ChannelID); err != nil {
				tx.Rollback()
				return fmt.Errorf("batch flag for %s: %w", u.ChannelID, err)
			}
		} else {
			if _, err := stmtBoth.Exec(u.LastReadTS, boolToInt(u.HasUnread), u.MentionCount, u.ChannelID); err != nil {
				tx.Rollback()
				return fmt.Errorf("batch both for %s: %w", u.ChannelID, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit batch read-state: %w", err)
	}
	return nil
}

// ReplaceWorkspaceReadState applies an authoritative full snapshot of
// unread state for a workspace. In a single transaction it first
// resets has_unread=0 and mention_count=0 for EVERY channel in the
// workspace, then applies the given updates (has_unread + last_read_ts
// + mention_count). Channels absent from updates are therefore treated
// as read.
//
// This is the boot/bootstrap path. Unlike BatchUpdateChannelReadState
// (which only touches rows named in the batch), the reset step clears
// stale unread flags for channels the user read in another client
// while slk was closed — channels that Slack's client.counts response
// omits entirely (the unread-counts endpoint need not list read
// channels). last_read_ts of an absent channel is left untouched:
// there is no fresh value to apply and has_unread=0 already suppresses
// the dot.
//
// Callers MUST only invoke this with a snapshot they trust to list
// every currently-unread channel (i.e. a successful client.counts
// call). On fetch failure, do NOT call this — a reset with no data
// would wrongly clear every dot.
func (db *DB) ReplaceWorkspaceReadState(workspaceID string, updates []ChannelReadStateUpdate) error {
	if workspaceID == "" {
		return fmt.Errorf("ReplaceWorkspaceReadState: workspaceID required")
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin replace read-state tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(
		`UPDATE channels SET has_unread = 0, mention_count = 0 WHERE workspace_id = ?`,
		workspaceID,
	); err != nil {
		return fmt.Errorf("reset workspace unread: %w", err)
	}

	stmtBoth, err := tx.Prepare(`UPDATE channels SET last_read_ts = ?, has_unread = ?, mention_count = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare both: %w", err)
	}
	defer stmtBoth.Close()
	stmtFlag, err := tx.Prepare(`UPDATE channels SET has_unread = ?, mention_count = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare flag: %w", err)
	}
	defer stmtFlag.Close()

	for _, u := range updates {
		if u.LastReadTS == "" {
			if _, err := stmtFlag.Exec(boolToInt(u.HasUnread), u.MentionCount, u.ChannelID); err != nil {
				return fmt.Errorf("replace flag for %s: %w", u.ChannelID, err)
			}
		} else {
			if _, err := stmtBoth.Exec(u.LastReadTS, boolToInt(u.HasUnread), u.MentionCount, u.ChannelID); err != nil {
				return fmt.Errorf("replace both for %s: %w", u.ChannelID, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace read-state: %w", err)
	}
	return nil
}

// GetChannelReadState returns the read state for a single channel.
// A missing row yields a zero-valued ReadState and a nil error.
func (db *DB) GetChannelReadState(channelID string) (ReadState, error) {
	var lastReadTS string
	var hasUnread, mentionCount int
	err := db.conn.QueryRow(
		`SELECT last_read_ts, has_unread, mention_count FROM channels WHERE id = ?`,
		channelID,
	).Scan(&lastReadTS, &hasUnread, &mentionCount)
	if err == sql.ErrNoRows {
		return ReadState{}, nil
	}
	if err != nil {
		return ReadState{}, fmt.Errorf("getting channel read state: %w", err)
	}
	return ReadState{
		LastReadTS:   lastReadTS,
		HasUnread:    hasUnread == 1,
		MentionCount: mentionCount,
	}, nil
}

// GetWorkspaceReadState returns channelID -> ReadState for every
// channel in the workspace. Single batched query. Called by the
// sidebar View() at render time.
func (db *DB) GetWorkspaceReadState(workspaceID string) (map[string]ReadState, error) {
	rows, err := db.conn.Query(
		`SELECT id, last_read_ts, has_unread, mention_count FROM channels WHERE workspace_id = ?`,
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("query workspace read state: %w", err)
	}
	defer rows.Close()
	out := make(map[string]ReadState)
	for rows.Next() {
		var id, lastRead string
		var hasUnread, mentionCount int
		if err := rows.Scan(&id, &lastRead, &hasUnread, &mentionCount); err != nil {
			return nil, fmt.Errorf("scan workspace read state: %w", err)
		}
		out[id] = ReadState{
			LastReadTS:   lastRead,
			HasUnread:    hasUnread == 1,
			MentionCount: mentionCount,
		}
	}
	return out, rows.Err()
}

// WorkspacesWithUnreads returns the set of workspace IDs with at least
// one has_unread=true channel. Used by the workspace rail.
func (db *DB) WorkspacesWithUnreads() ([]string, error) {
	rows, err := db.conn.Query(
		`SELECT DISTINCT workspace_id FROM channels WHERE has_unread = 1`,
	)
	if err != nil {
		return nil, fmt.Errorf("query workspaces with unreads: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan workspace id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
