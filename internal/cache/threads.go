package cache

import (
	"database/sql"
	"fmt"
	"sort"

	"github.com/gammons/slk/internal/core"
)

// ThreadSummary is defined in internal/core, which the TUI shares.
type ThreadSummary = core.ThreadSummary

// ListSubscribedThreads returns the workspace's subscribed-threads
// list — the authoritative set from thread_subscriptions joined
// against cached message/channel data for display. Replaces the v1
// "involved threads" heuristic with Slack-side subscription state.
//
// Threads with no cached messages still appear; their parent
// text/user fall back to "" and LastReplyTS falls back to the
// subscription's LastRead so sort still produces a sensible order.
//
// Ordering: newest LastReplyTS first.
//
// Unread is computed from the subscription's per-thread LastRead
// (not the per-channel channels.last_read_ts): unread iff the newest
// activity is later than LastRead. "Newest activity" is the max of the
// authoritative latest_reply watermark (from subscriptions.thread.getView)
// and the newest locally-cached reply — so a subscribed thread with
// unread replies renders unread even when those replies have not been
// fetched into the cache yet (the boot case). The only suppression is
// the optimistic just-sent window: a locally-cached newest reply known
// to be authored by self does not count as unread.
func (db *DB) ListSubscribedThreads(workspaceID, selfUserID string) ([]ThreadSummary, error) {
	const q = `
SELECT
    s.channel_id,
    s.thread_ts,
    COALESCE(c.name, ''),
    COALESCE(c.type, ''),
    s.last_read,
    s.latest_reply,
    COALESCE((SELECT user_id FROM messages
              WHERE channel_id = s.channel_id AND ts = s.thread_ts AND is_deleted = 0), ''),
    COALESCE((SELECT text FROM messages
              WHERE channel_id = s.channel_id AND ts = s.thread_ts AND is_deleted = 0), ''),
    (SELECT COUNT(*) FROM messages
     WHERE channel_id = s.channel_id AND thread_ts = s.thread_ts
       AND ts != s.thread_ts AND is_deleted = 0)
        AS reply_count,
    COALESCE(
        (SELECT MAX(ts) FROM messages
         WHERE channel_id = s.channel_id AND thread_ts = s.thread_ts AND is_deleted = 0),
        ''
    ) AS cached_max_ts,
    COALESCE(
        (SELECT user_id FROM messages
         WHERE channel_id = s.channel_id AND thread_ts = s.thread_ts AND is_deleted = 0
         ORDER BY ts DESC LIMIT 1),
        ''
    ) AS last_reply_by
FROM thread_subscriptions s
LEFT JOIN channels c ON c.id = s.channel_id
WHERE s.workspace_id = ? AND s.active = 1
`
	rows, err := db.conn.Query(q, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing subscribed threads: %w", err)
	}
	defer rows.Close()

	var out []ThreadSummary
	for rows.Next() {
		var s ThreadSummary
		var lastRead, latestReply, cachedMaxTS string
		if err := rows.Scan(
			&s.ChannelID,
			&s.ThreadTS,
			&s.ChannelName,
			&s.ChannelType,
			&lastRead,
			&latestReply,
			&s.ParentUserID,
			&s.ParentText,
			&s.ReplyCount,
			&cachedMaxTS,
			&s.LastReplyBy,
		); err != nil {
			return nil, fmt.Errorf("scanning subscribed thread row: %w", err)
		}
		s.ParentTS = s.ThreadTS

		// Effective newest activity = the max of the authoritative
		// getView watermark (latest_reply) and the newest reply we've
		// cached locally, falling back to last_read so uncached/read
		// threads still sort sensibly. Comparing string ts is valid:
		// Slack ts are fixed-width "secs.micros".
		effLatest := lastRead
		if cachedMaxTS > effLatest {
			effLatest = cachedMaxTS
		}
		if latestReply > effLatest {
			effLatest = latestReply
		}
		s.LastReplyTS = effLatest

		// Unread when the newest activity is past our read watermark.
		// Suppress only when that newest activity is a locally-cached
		// reply we know was authored by self (the optimistic just-sent
		// window, before last_read catches up). When the newest activity
		// comes from the authoritative latest_reply (author unknown /
		// uncached), we trust the ts comparison — this is the boot case
		// the old cached-reply-only heuristic got wrong.
		unread := effLatest > lastRead
		if unread && cachedMaxTS == effLatest && s.LastReplyBy == selfUserID {
			unread = false
		}
		s.Unread = unread
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastReplyTS > out[j].LastReplyTS
	})
	return out, nil
}

// ThreadInvolvesUser reports whether the given thread (identified by
// workspaceID, channelID, threadTS) has any cached message authored
// by selfUserID or containing the angle-bracketed mention "<@selfUserID>".
// Used by the reconnect backfill to filter which threads warrant a
// conversations.replies catch-up call.
func (db *DB) ThreadInvolvesUser(workspaceID, channelID, threadTS, selfUserID string) (bool, error) {
	mention := "%<@" + selfUserID + ">%"
	const q = `
SELECT 1 FROM messages
WHERE workspace_id = ? AND channel_id = ? AND thread_ts = ?
  AND is_deleted = 0
  AND (user_id = ? OR text LIKE ?)
LIMIT 1
`
	var one int
	err := db.conn.QueryRow(q, workspaceID, channelID, threadTS, selfUserID, mention).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking thread involvement: %w", err)
	}
	return true, nil
}
