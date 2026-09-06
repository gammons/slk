package cache

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ThreadSubscription is one row in the thread_subscriptions table.
// Mirrors Slack's authoritative per-thread subscription state:
// whether the user is "subscribed for unread updates" on this thread,
// and the last-read timestamp inside the thread.
type ThreadSubscription struct {
	WorkspaceID string
	ChannelID   string
	ThreadTS    string
	LastRead    string
	// LatestReply is the ts of the newest reply in the thread as
	// reported by subscriptions.thread.getView (root_msg.latest_reply).
	// It is the authoritative "newest activity" watermark and lets the
	// threads view compute unread state (LatestReply > LastRead)
	// WITHOUT requiring the thread's replies to be cached locally.
	// Empty when unknown (e.g. rows created by a live thread_subscribed
	// event before the next getView reconcile); the threads query then
	// falls back to MAX(cached reply ts).
	LatestReply string
	Active      bool
	UpdatedAt   int64 // unix seconds; bumped on every upsert
}

// UpsertThreadSubscription inserts or updates a thread_subscriptions
// row. Bumps updated_at to time.Now().Unix() on every call. Use
// active=false to tombstone a row (the row is kept so its LastRead
// survives later re-subscriptions).
func (db *DB) UpsertThreadSubscription(workspaceID, channelID, threadTS, lastRead string, active bool) error {
	if workspaceID == "" || channelID == "" || threadTS == "" {
		return fmt.Errorf("UpsertThreadSubscription: workspace/channel/thread_ts required")
	}
	activeInt := 0
	if active {
		activeInt = 1
	}
	const q = `
INSERT INTO thread_subscriptions
    (workspace_id, channel_id, thread_ts, last_read, active, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(workspace_id, channel_id, thread_ts) DO UPDATE SET
    last_read  = excluded.last_read,
    active     = excluded.active,
    updated_at = excluded.updated_at
`
	_, err := db.conn.Exec(q, workspaceID, channelID, threadTS, lastRead, activeInt, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("upserting thread_subscriptions: %w", err)
	}
	return nil
}

// DeleteThreadSubscription removes a thread_subscriptions row outright
// (not a tombstone). Used by tests; production callers prefer
// UpsertThreadSubscription with active=false to preserve LastRead.
func (db *DB) DeleteThreadSubscription(workspaceID, channelID, threadTS string) error {
	const q = `DELETE FROM thread_subscriptions WHERE workspace_id=? AND channel_id=? AND thread_ts=?`
	_, err := db.conn.Exec(q, workspaceID, channelID, threadTS)
	if err != nil {
		return fmt.Errorf("deleting thread_subscriptions: %w", err)
	}
	return nil
}

// UpdateThreadLastRead advances (or rewinds) a thread's read cursor
// WITHOUT touching `active` on a row that already exists — the ON
// CONFLICT clause updates last_read and updated_at and nothing else.
// thread_marked carries a read cursor, not a subscription change, so it
// must not be able to tombstone a row or resurrect one: `active` is
// owned solely by thread_subscribed, thread_unsubscribed, and the
// getView reconcile. Creating a missing row is the one exception, and
// the paragraph below is its precondition.
//
// A missing row is inserted with active=1. That branch is ONLY valid for
// callers that can prove the user is subscribed to the thread, because
// ListSubscribedThreads filters on active=1 and would surface the
// inserted row in the Threads list. The sole such caller is
// rtmEventHandler.OnThreadMarked, and only on the branch where the
// thread_marked event's subscription block reports active=true: there
// the insert reconstructs a row the local cache merely hasn't seen yet.
// The precondition is checked by that caller, not assumed here — slk's
// own subscriptions.thread.mark echoes back as thread_marked, so the
// event by itself does not prove subscription.
//
// Callers that CANNOT prove subscription — the local mark path, which
// fires for any thread the user opens, and OnThreadMarked's
// active=false branch — must use UpdateThreadLastReadIfExists instead.
func (db *DB) UpdateThreadLastRead(workspaceID, channelID, threadTS, lastRead string) error {
	if workspaceID == "" || channelID == "" || threadTS == "" {
		return fmt.Errorf("UpdateThreadLastRead: workspace/channel/thread_ts required")
	}
	const q = `
INSERT INTO thread_subscriptions
    (workspace_id, channel_id, thread_ts, last_read, active, updated_at)
VALUES (?, ?, ?, ?, 1, ?)
ON CONFLICT(workspace_id, channel_id, thread_ts) DO UPDATE SET
    last_read  = excluded.last_read,
    updated_at = excluded.updated_at
`
	if _, err := db.conn.Exec(q, workspaceID, channelID, threadTS, lastRead, time.Now().Unix()); err != nil {
		return fmt.Errorf("updating thread last_read: %w", err)
	}
	return nil
}

// UpdateThreadLastReadIfExists advances a thread's read cursor only when
// a subscription row already exists, and never creates one. Used by the
// local mark path, which fires for ANY thread the user opens — including
// threads they were never subscribed to. Inserting there would fabricate
// an active=1 row and surface a phantom entry in the Threads list, since
// ListSubscribedThreads filters on active=1.
//
// A missing row is not an error: an unsubscribed thread has no cursor
// worth storing, and if the user later becomes subscribed the getView
// reconcile supplies last_read from Slack.
//
// It never touches `active` (so a tombstoned row stays tombstoned) or
// `latest_reply` — unlike UpdateThreadLastRead, which leaves `active`
// alone on an existing row but writes active=1 when it inserts one.
func (db *DB) UpdateThreadLastReadIfExists(workspaceID, channelID, threadTS, lastRead string) error {
	if workspaceID == "" || channelID == "" || threadTS == "" {
		return fmt.Errorf("UpdateThreadLastReadIfExists: workspace/channel/thread_ts required")
	}
	const q = `
UPDATE thread_subscriptions
SET last_read = ?, updated_at = ?
WHERE workspace_id=? AND channel_id=? AND thread_ts=?
`
	if _, err := db.conn.Exec(q, lastRead, time.Now().Unix(), workspaceID, channelID, threadTS); err != nil {
		return fmt.Errorf("updating thread last_read: %w", err)
	}
	return nil
}

// GetThreadLastRead returns a thread's read cursor, or "" when no row
// exists (which the thread panel treats as "no unread boundary").
// A missing row is not an error.
func (db *DB) GetThreadLastRead(workspaceID, channelID, threadTS string) (string, error) {
	const q = `SELECT last_read FROM thread_subscriptions
WHERE workspace_id=? AND channel_id=? AND thread_ts=?`
	var lastRead string
	err := db.conn.QueryRow(q, workspaceID, channelID, threadTS).Scan(&lastRead)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading thread last_read: %w", err)
	}
	return lastRead, nil
}

// ListActiveThreadSubscriptions returns every active subscription in
// the given workspace, in PRIMARY KEY order. Tombstoned rows
// (active=0) are filtered out.
func (db *DB) ListActiveThreadSubscriptions(workspaceID string) ([]ThreadSubscription, error) {
	const q = `
SELECT workspace_id, channel_id, thread_ts, last_read, active, updated_at
FROM thread_subscriptions
WHERE workspace_id = ? AND active = 1
ORDER BY channel_id, thread_ts
`
	rows, err := db.conn.Query(q, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing thread_subscriptions: %w", err)
	}
	defer rows.Close()
	var out []ThreadSubscription
	for rows.Next() {
		var s ThreadSubscription
		var activeInt int
		if err := rows.Scan(&s.WorkspaceID, &s.ChannelID, &s.ThreadTS,
			&s.LastRead, &activeInt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning thread_subscriptions: %w", err)
		}
		s.Active = activeInt == 1
		out = append(out, s)
	}
	return out, rows.Err()
}

// ReconcileThreadSubscriptions replaces the workspace's local
// subscription set with the given fresh list. Upserts every fresh
// entry (active=1) and tombstones (active=0) any local active row
// whose (channel_id, thread_ts) doesn't appear in the fresh list.
//
// Used by the reconnect backfill: after fetching the full server-side
// list, calling this reconciles any subscribes/unsubscribes that
// happened while the WS was disconnected. Tombstoning preserves the
// row's LastRead so a later re-subscribe doesn't lose history.
func (db *DB) ReconcileThreadSubscriptions(workspaceID string, fresh []ThreadSubscription) error {
	if workspaceID == "" {
		return fmt.Errorf("ReconcileThreadSubscriptions: workspaceID required")
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin reconcile tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().Unix()

	// Build the set of keys present in the fresh list.
	type key struct{ ch, ts string }
	freshKeys := make(map[key]struct{}, len(fresh))
	for _, s := range fresh {
		freshKeys[key{s.ChannelID, s.ThreadTS}] = struct{}{}
	}

	// 1. Upsert each fresh entry as active=1. latest_reply comes from
	// the authoritative getView snapshot and drives thread-unread
	// without needing replies cached (see ThreadSubscription docs).
	const upsertQ = `
INSERT INTO thread_subscriptions
    (workspace_id, channel_id, thread_ts, last_read, latest_reply, active, updated_at)
VALUES (?, ?, ?, ?, ?, 1, ?)
ON CONFLICT(workspace_id, channel_id, thread_ts) DO UPDATE SET
    last_read    = excluded.last_read,
    latest_reply = excluded.latest_reply,
    active       = 1,
    updated_at   = excluded.updated_at
`
	for _, s := range fresh {
		if _, err := tx.Exec(upsertQ, workspaceID, s.ChannelID, s.ThreadTS, s.LastRead, s.LatestReply, now); err != nil {
			return fmt.Errorf("upserting fresh subscription (%s/%s): %w", s.ChannelID, s.ThreadTS, err)
		}
	}

	// 2. Find currently-active rows that aren't in the fresh list and
	// tombstone them. Walk the existing active rows once; tombstone in
	// a second pass to avoid mutating during iteration.
	rows, err := tx.Query(
		`SELECT channel_id, thread_ts FROM thread_subscriptions WHERE workspace_id=? AND active=1`,
		workspaceID,
	)
	if err != nil {
		return fmt.Errorf("listing active for reconcile: %w", err)
	}
	var toTombstone []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.ch, &k.ts); err != nil {
			rows.Close()
			return fmt.Errorf("scanning active for reconcile: %w", err)
		}
		if _, ok := freshKeys[k]; !ok {
			toTombstone = append(toTombstone, k)
		}
	}
	rows.Close()

	for _, k := range toTombstone {
		if _, err := tx.Exec(
			`UPDATE thread_subscriptions SET active=0, updated_at=? WHERE workspace_id=? AND channel_id=? AND thread_ts=?`,
			now, workspaceID, k.ch, k.ts,
		); err != nil {
			return fmt.Errorf("tombstoning subscription (%s/%s): %w", k.ch, k.ts, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reconcile tx: %w", err)
	}
	return nil
}
