package cache

import (
	"fmt"
	"sort"
)

// ThreadSummary is one row in the Threads view: a thread the user is
// involved in (authored, replied to, or @-mentioned in). Computed from
// the local cache; v1 has no Slack-side authoritative data.
type ThreadSummary struct {
	ChannelID    string
	ChannelName  string
	ChannelType  string // "channel" | "private" | "dm" | "group_dm"
	ThreadTS     string
	ParentUserID string
	ParentText   string
	ParentTS     string
	ReplyCount   int // number of replies (does not count the parent)
	LastReplyTS  string
	LastReplyBy  string
	Unread       bool
}

// ListInvolvedThreads returns threads in the given workspace where the user
// (selfUserID) authored the parent, posted a reply, or was @-mentioned
// (`<@UID>`) anywhere in the thread.
//
// Ordering: unread first, then newest LastReplyTS first.
//
// Unread heuristic: LastReplyTS > channel.last_read_ts AND LastReplyBy != self.
// This is approximate; v2 will replace it with subscriptions.thread state.
func (db *DB) ListInvolvedThreads(workspaceID, selfUserID string) ([]ThreadSummary, error) {
	mention := "%<@" + selfUserID + ">%"

	const q = `
WITH involved AS (
  SELECT DISTINCT thread_ts, channel_id
  FROM messages
  WHERE workspace_id = ?
    AND thread_ts != ''
    AND is_deleted = 0
    AND (user_id = ? OR text LIKE ?)
)
SELECT
  m.channel_id,
  m.thread_ts,
  COALESCE(c.name, ''),
  COALESCE(c.type, ''),
  COALESCE(c.last_read_ts, ''),
  COALESCE((SELECT user_id FROM messages
              WHERE channel_id = m.channel_id AND ts = m.thread_ts AND is_deleted = 0), '')
    AS parent_user,
  COALESCE((SELECT text FROM messages
              WHERE channel_id = m.channel_id AND ts = m.thread_ts AND is_deleted = 0), '')
    AS parent_text,
  -- reply count excludes the parent (rows where ts == thread_ts)
  SUM(CASE WHEN m.ts != m.thread_ts THEN 1 ELSE 0 END) AS reply_count,
  MAX(m.ts) AS last_ts,
  (SELECT user_id FROM messages
     WHERE channel_id = m.channel_id AND thread_ts = m.thread_ts AND is_deleted = 0
     ORDER BY ts DESC LIMIT 1) AS last_by
FROM messages m
JOIN involved i ON i.thread_ts = m.thread_ts AND i.channel_id = m.channel_id
LEFT JOIN channels c ON c.id = m.channel_id
WHERE m.is_deleted = 0
GROUP BY m.channel_id, m.thread_ts
`

	rows, err := db.conn.Query(q, workspaceID, selfUserID, mention)
	if err != nil {
		return nil, fmt.Errorf("listing involved threads: %w", err)
	}
	defer rows.Close()

	var out []ThreadSummary
	for rows.Next() {
		var s ThreadSummary
		var lastRead string
		if err := rows.Scan(
			&s.ChannelID,
			&s.ThreadTS,
			&s.ChannelName,
			&s.ChannelType,
			&lastRead,
			&s.ParentUserID,
			&s.ParentText,
			&s.ReplyCount,
			&s.LastReplyTS,
			&s.LastReplyBy,
		); err != nil {
			return nil, fmt.Errorf("scanning thread summary: %w", err)
		}
		s.ParentTS = s.ThreadTS
		s.Unread = s.LastReplyTS > lastRead && s.LastReplyBy != selfUserID
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Order: unread DESC, last_reply_ts DESC.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Unread != out[j].Unread {
			return out[i].Unread
		}
		return out[i].LastReplyTS > out[j].LastReplyTS
	})
	return out, nil
}
