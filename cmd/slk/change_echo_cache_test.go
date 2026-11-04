package main

import (
	"encoding/json"
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/slack-go/slack"
)

// A message_changed echo is a partial message: no reply count, and the
// dispatcher passes an empty subtype. Caching it must change the row's
// content and keep the rest, or a channel reopened from cache shows a
// thread parent without replies and drops a broadcast from the feed.

const (
	echoChannel  = "C1"
	echoParentTS = "1700000001.000000"
	echoReplyTS  = "1700000002.000000"
)

// echoCacheApp is a handler on the active workspace whose cache holds
// what a history fetch writes for a thread parent with three replies
// and one reply broadcast to the channel, as fetchChannelMessages and
// fetchThreadReplies do: the whole slack.Message marshalled into
// raw_json. The parent first arrived over the WebSocket, so the
// history upsert overwrites a synthetic row, as in production.
func echoCacheApp(t *testing.T) (*rtmEventHandler, *cache.DB) {
	t.Helper()
	db := newCacheForTest(t)
	if err := db.UpsertChannel(cache.Channel{ID: echoChannel, WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	h := &rtmEventHandler{
		db:              db,
		workspaceID:     "T1",
		wsCtx:           &WorkspaceContext{},
		userNames:       newUserNameStore(nil),
		isActive:        func() bool { return true },
		activeChannelID: func() string { return echoChannel },
		currentUserID:   "USELF",
		channelTypes:    map[string]string{echoChannel: "channel"},
	}
	h.OnMessage(echoChannel, "U1", echoParentTS, "parent", "", "", false, false, nil, slack.Blocks{}, nil, "", "")

	history := []slack.Message{
		{Msg: slack.Msg{Type: "message", Timestamp: echoParentTS, User: "U1", Text: "parent",
			ThreadTimestamp: echoParentTS, ReplyCount: 3, ReplyUsers: []string{"U2"}}},
		{Msg: slack.Msg{Type: "message", SubType: "thread_broadcast", Timestamp: echoReplyTS, User: "U2",
			Text: "broadcast", ThreadTimestamp: echoParentTS}},
	}
	for _, m := range history {
		raw, _ := json.Marshal(m)
		if err := db.UpsertMessage(cache.Message{TS: m.Timestamp, ChannelID: echoChannel, WorkspaceID: "T1",
			UserID: m.User, Text: m.Text, ThreadTS: m.ThreadTimestamp, ReplyCount: m.ReplyCount,
			Subtype: m.SubType, RawJSON: string(raw), CreatedAt: 1}); err != nil {
			t.Fatalf("UpsertMessage: %v", err)
		}
	}
	return h, db
}

// cachedFeed reopens the channel from cache, keyed by ts.
func cachedFeed(t *testing.T, db *cache.DB) map[string]feedRow {
	t.Helper()
	out := map[string]feedRow{}
	for _, m := range loadCachedMessages(db, "USELF", echoChannel, newUserNameStore(nil), "3:04 PM", nil) {
		out[m.TS] = feedRow{m.Text, m.ReplyCount, m.Subtype, m.ThreadTS, len(m.LegacyAttachments)}
	}
	return out
}

// feedRow is what a reload shows of one message.
type feedRow struct {
	text       string
	replyCount int
	subtype    string
	threadTS   string
	previews   int
}

var unfurl = []slack.Attachment{{ID: 1, Title: "A page", TitleLink: "https://example.com", FromURL: "https://example.com"}}

func TestChangeEcho_ThreadParentKeepsReplyCount(t *testing.T) {
	for _, tc := range []struct {
		name        string
		text        string
		edited      bool
		attachments []slack.Attachment
		previews    int
		threadTS    string
	}{
		// The inner message's thread_ts is the parent's own ts once it
		// has replies; the edit case passes none, as the parent's first
		// WebSocket delivery did, to show the column does not follow it.
		{"edit", "parent, edited", true, nil, 0, ""},
		{"unfurl", "parent", false, unfurl, 1, echoParentTS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, db := echoCacheApp(t)
			// The dispatcher passes an empty subtype for message_changed.
			h.OnMessage(echoChannel, "U1", echoParentTS, tc.text, tc.threadTS, "", true, tc.edited, nil, slack.Blocks{}, tc.attachments, "", "")

			got, ok := cachedFeed(t, db)[echoParentTS]
			if !ok {
				t.Fatal("the parent is gone from the cached channel feed")
			}
			if got.text != tc.text || got.previews != tc.previews {
				t.Errorf("parent text=%q previews=%d, want %q and %d", got.text, got.previews, tc.text, tc.previews)
			}
			if got.replyCount != 3 || got.threadTS != echoParentTS {
				t.Errorf("parent reply_count=%d thread_ts=%q, want 3 and %q", got.replyCount, got.threadTS, echoParentTS)
			}
			row, _ := db.GetMessage(echoChannel, echoParentTS)
			var raw slack.Message
			if err := json.Unmarshal([]byte(row.RawJSON), &raw); err != nil {
				t.Fatalf("raw_json: %v", err)
			}
			if raw.ReplyCount != 3 || len(raw.ReplyUsers) != 1 {
				t.Errorf("raw_json lost the thread: reply_count=%d reply_users=%v", raw.ReplyCount, raw.ReplyUsers)
			}

			// The cached thread opens with its parent: the UI drops the
			// first row as the parent, so a parent missing from it would
			// cost the first reply.
			thread := loadCachedThreadReplies(db, "USELF", echoChannel, echoParentTS, newUserNameStore(nil), "3:04 PM", nil)
			if len(thread) != 2 || thread[0].TS != echoParentTS || thread[0].Text != tc.text {
				t.Errorf("cached thread = %+v, want the changed parent, then the broadcast", thread)
			}
		})
	}
}

func TestChangeEcho_BroadcastStaysInTheFeed(t *testing.T) {
	for _, tc := range []struct {
		name        string
		text        string
		edited      bool
		attachments []slack.Attachment
		previews    int
	}{
		{"edit", "broadcast, edited", true, nil, 0},
		{"unfurl", "broadcast", false, unfurl, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, db := echoCacheApp(t)
			h.OnMessage(echoChannel, "U2", echoReplyTS, tc.text, echoParentTS, "", true, tc.edited, nil, slack.Blocks{}, tc.attachments, "", "")

			got, ok := cachedFeed(t, db)[echoReplyTS]
			if !ok {
				t.Fatal("the broadcast is gone from the cached channel feed")
			}
			if got.subtype != "thread_broadcast" || got.text != tc.text || got.previews != tc.previews {
				t.Errorf("broadcast subtype=%q text=%q previews=%d, want thread_broadcast, %q, %d",
					got.subtype, got.text, got.previews, tc.text, tc.previews)
			}

			thread := loadCachedThreadReplies(db, "USELF", echoChannel, echoParentTS, newUserNameStore(nil), "3:04 PM", nil)
			if len(thread) != 2 || thread[1].TS != echoReplyTS || thread[1].Subtype != "thread_broadcast" || thread[1].Text != tc.text {
				t.Errorf("cached thread = %+v, want the parent and the changed broadcast", thread)
			}
		})
	}
}

// A change for a message the cache never saw is not written: a row
// built from the echo alone would have no reply count, no subtype and
// no bot identity. The next fetch that reaches the message caches it.
func TestChangeEcho_UncachedMessageIsNotCached(t *testing.T) {
	h, db := echoCacheApp(t)
	const oldTS = "1600000000.000000"
	h.OnMessage(echoChannel, "U1", oldTS, "edited long ago", "", "", true, true, nil, slack.Blocks{}, nil, "", "")

	if _, err := db.GetMessage(echoChannel, oldTS); err == nil {
		t.Error("a change echo for an uncached message created a row")
	}
}

// A new message still caches the whole message as it arrived.
func TestNewMessage_StillUpserts(t *testing.T) {
	h, db := echoCacheApp(t)
	const ts = "1700000009.000000"
	h.OnMessage(echoChannel, "U1", ts, "fresh", "", "", false, false, nil, slack.Blocks{}, unfurl, "", "")

	if got := cachedFeed(t, db)[ts]; got.text != "fresh" || got.previews != 1 {
		t.Errorf("new message cached as %+v, want text fresh with one preview", got)
	}
}

// A parent cached only from the WebSocket has no thread_ts: its first
// delivery had none. The echo of a change to it carries one, which
// fills the column, so the cached thread opens with the parent and the
// UI does not drop the first reply in its place.
func TestChangeEcho_FillsAnEmptyThreadTS(t *testing.T) {
	db := newCacheForTest(t)
	if err := db.UpsertChannel(cache.Channel{ID: echoChannel, WorkspaceID: "T1", Name: "general", Type: "channel"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	h := &rtmEventHandler{
		db:            db,
		workspaceID:   "T1",
		wsCtx:         &WorkspaceContext{},
		userNames:     newUserNameStore(nil),
		isActive:      func() bool { return true },
		currentUserID: "USELF",
		channelTypes:  map[string]string{echoChannel: "channel"},
	}
	h.OnMessage(echoChannel, "U1", echoParentTS, "parent", "", "", false, false, nil, slack.Blocks{}, nil, "", "")
	h.OnMessage(echoChannel, "U2", echoReplyTS, "reply", echoParentTS, "", false, false, nil, slack.Blocks{}, nil, "", "")

	h.OnMessage(echoChannel, "U1", echoParentTS, "parent, edited", echoParentTS, "", true, true, nil, slack.Blocks{}, nil, "", "")

	thread := loadCachedThreadReplies(db, "USELF", echoChannel, echoParentTS, newUserNameStore(nil), "3:04 PM", nil)
	if len(thread) != 2 || thread[0].TS != echoParentTS || thread[1].TS != echoReplyTS {
		t.Errorf("cached thread = %+v, want the parent, then the reply", thread)
	}
}

// A change is not a fetch and not a new arrival: it leaves synced_at
// and latest_synced_ts alone. Reconnect zeroes synced_at so the next
// open refetches what was missed; an edit of an old message must not
// make that channel look fresh again.
func TestChangeEcho_LeavesSyncStateAlone(t *testing.T) {
	h, db := echoCacheApp(t)
	if err := db.SetChannelSyncedAt(echoChannel, 0); err != nil {
		t.Fatalf("SetChannelSyncedAt: %v", err)
	}
	if err := db.SetChannelLatestSyncedTS(echoChannel, ""); err != nil {
		t.Fatalf("SetChannelLatestSyncedTS: %v", err)
	}

	h.OnMessage(echoChannel, "U1", echoParentTS, "parent, edited", echoParentTS, "", true, true, nil, slack.Blocks{}, nil, "", "")
	if got := db.GetChannelSyncedAt(echoChannel); got != 0 {
		t.Errorf("synced_at = %d after a change, want 0", got)
	}
	if got := db.GetChannelLatestSyncedTS(echoChannel); got != "" {
		t.Errorf("latest_synced_ts = %q after a change, want empty", got)
	}

	const ts = "1700000009.000000"
	h.OnMessage(echoChannel, "U1", ts, "fresh", "", "", false, false, nil, slack.Blocks{}, nil, "", "")
	if got := db.GetChannelSyncedAt(echoChannel); got == 0 {
		t.Error("synced_at = 0 after a new message, want it set")
	}
	if got := db.GetChannelLatestSyncedTS(echoChannel); got != ts {
		t.Errorf("latest_synced_ts = %q after a new message, want %q", got, ts)
	}
}
