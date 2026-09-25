package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gammons/slk/internal/avatar"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/debuglog"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/slack-go/slack"
)

func fetchOlderMessages(client *slackclient.Client, channelID, latestTS string, db *cache.DB, userNames map[string]string, tsFormat string, router *workspaceRouter) []messages.MessageItem {
	ctx := context.Background()
	debuglog.Cache("fetchOlderMessages: channel=%s latest_ts=%s entry", channelID, latestTS)
	start := time.Now()
	history, err := client.GetOlderHistory(ctx, channelID, 50, latestTS)
	if err != nil {
		debuglog.Cache("fetchOlderMessages: GetOlderHistory %s: %v dur_ms=%d (returning nil → keep cache)",
			channelID, err, time.Since(start).Milliseconds())
		return nil
	}

	msgItems := convertAndCacheHistory(client, channelID, history, db, userNames, tsFormat, router)

	debuglog.Cache("fetchOlderMessages: channel=%s latest_ts=%s result %s dur_ms=%d (older history backfill)",
		channelID, latestTS, summarizeMessages(msgItems), time.Since(start).Milliseconds())
	return msgItems
}

// fetchMessagesAround fetches a history window centered on targetTS
// for jump-to-message navigation. Mirrors fetchOlderMessages: upserts
// into the cache, converts to MessageItems, returns ascending by TS.
// Returns nil on network failure AND when the fetch succeeds but the
// window is empty — callers cannot distinguish the two from the
// return value alone (the FetchAround closure treats both as
// failure).
func fetchMessagesAround(client *slackclient.Client, channelID, targetTS string, db *cache.DB, userNames map[string]string, tsFormat string, router *workspaceRouter) []messages.MessageItem {
	ctx := context.Background()
	debuglog.Cache("fetchMessagesAround: channel=%s target_ts=%s entry", channelID, targetTS)
	start := time.Now()
	history, err := client.GetHistoryAround(ctx, channelID, targetTS, 25)
	if err != nil {
		debuglog.Cache("fetchMessagesAround: GetHistoryAround %s @ %s: %v dur_ms=%d (returning nil)",
			channelID, targetTS, err, time.Since(start).Milliseconds())
		return nil
	}

	msgItems := convertAndCacheHistory(client, channelID, history, db, userNames, tsFormat, router)

	debuglog.Cache("fetchMessagesAround: channel=%s target_ts=%s result %s dur_ms=%d (jump-to-message window)",
		channelID, targetTS, summarizeMessages(msgItems), time.Since(start).Milliseconds())
	return msgItems
}

// convertAndCacheHistory is the shared tail of fetchOlderMessages and
// fetchMessagesAround: upserts each fetched message (and its
// reactions) into the cache, resolves user names, converts to
// messages.MessageItem, and reverses the slice from Slack's
// newest-first order to the ascending-by-TS convention used
// throughout slk.
func convertAndCacheHistory(client *slackclient.Client, channelID string, history []slack.Message, db *cache.DB, userNames map[string]string, tsFormat string, router *workspaceRouter) []messages.MessageItem {
	var msgItems []messages.MessageItem
	for _, m := range history {
		rawBytes, _ := json.Marshal(m)
		debuglog.Cache("convertAndCacheHistory: upsert channel=%s ts=%s subtype=%q reply_count=%d files=%d",
			channelID, m.Timestamp, m.SubType, m.ReplyCount, len(m.Files))
		authorID, userName := messageAuthor(m, userNames, db, router)
		db.UpsertMessage(cache.Message{
			TS:          m.Timestamp,
			ChannelID:   channelID,
			WorkspaceID: client.TeamID(),
			UserID:      authorID,
			Text:        m.Text,
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			Subtype:     m.SubType,
			RawJSON:     string(rawBytes),
			CreatedAt:   time.Now().Unix(),
		})

		// Convert reactions
		var reactions []messages.ReactionItem
		for _, r := range m.Reactions {
			hasReacted := false
			for _, uid := range r.Users {
				if uid == client.UserID() {
					hasReacted = true
					break
				}
			}
			reactions = append(reactions, messages.ReactionItem{
				Emoji:      r.Name,
				Count:      r.Count,
				HasReacted: hasReacted,
				UserIDs:    r.Users,
			})
			_ = db.UpsertReaction(m.Timestamp, channelID, r.Name, r.Users, r.Count)
		}

		msgItems = append(msgItems, messages.MessageItem{
			TS:                m.Timestamp,
			UserID:            authorID,
			UserName:          userName,
			Text:              m.Text,
			Timestamp:         formatTimestamp(m.Timestamp, tsFormat),
			ThreadTS:          m.ThreadTimestamp,
			ReplyCount:        m.ReplyCount,
			Subtype:           m.SubType,
			Reactions:         reactions,
			Attachments:       extractAttachments(m.Files),
			Blocks:            extractBlocks(m.Blocks),
			LegacyAttachments: extractLegacyAttachments(m.Attachments),
		})
	}

	// Reverse: Slack returns newest first
	for i, j := 0, len(msgItems)-1; i < j; i, j = i+1, j-1 {
		msgItems[i], msgItems[j] = msgItems[j], msgItems[i]
	}

	return msgItems
}

// summarizeMessages collapses a slice of messages.MessageItem into a
// compact "count=N oldest=<ts> newest=<ts>" string for [cache] log
// lines. Empty/nil slices return "count=0" with no ts fields. Assumes
// the slice is sorted ascending by TS (the convention everywhere in
// slk's cache and fetch paths).
func summarizeMessages(items []messages.MessageItem) string {
	if len(items) == 0 {
		return "count=0"
	}
	return fmt.Sprintf("count=%d oldest=%s newest=%s",
		len(items), items[0].TS, items[len(items)-1].TS)
}

// summarizeCachedRows is summarizeMessages's twin for raw cache.Message
// rows (used by loadCachedMessages / loadCachedThreadReplies).
func summarizeCachedRows(rows []cache.Message) string {
	if len(rows) == 0 {
		return "count=0"
	}
	return fmt.Sprintf("count=%d oldest=%s newest=%s",
		len(rows), rows[0].TS, rows[len(rows)-1].TS)
}

// loadCachedMessages reads up to 50 cached messages for a channel from
// SQLite and reconstructs []messages.MessageItem with the same fidelity
// as fetchChannelMessages — including reactions and (when raw_json is
// present) files / blocks / legacy attachments.
//
// Returns nil on cache miss (no rows for the channel) or any DB error;
// callers treat nil as "fall through to the network fetch path".
//
// selfUserID is used to compute ReactionItem.HasReacted; it is NOT used
// to drive any network call. Cache reads must remain offline-capable —
// unknown user IDs render with their userID as a fallback rather than
// triggering a fresh GetUserProfile RPC. Resolving them on-demand would
// defeat the cache-first goal (and is what fetchChannelMessages already
// does on the network path, populating userNames for next time).
//
// raw_json unmarshal failures on a single row degrade gracefully: that
// row renders as text-only (no attachments / blocks / legacy
// attachments) without aborting the rest of the load.
// enrichPerfStats accumulates per-sub-call timing across one
// loadCachedMessages invocation so the [perf] log can attribute the
// channel-open hot path's cost across the three known N+1 sources.
// Allocated only when debuglog.Enabled(); enrichCachedRow checks for
// nil and skips the time.Now() / time.Since() calls otherwise.
type enrichPerfStats struct {
	getUserCalls   int
	getUserTotal   time.Duration
	getReactCalls  int
	getReactTotal  time.Duration
	unmarshalCalls int
	unmarshalTotal time.Duration
}

func loadCachedMessages(
	db *cache.DB,
	selfUserID string,
	channelID string,
	userNames map[string]string,
	tsFormat string,
	router *workspaceRouter,
) []messages.MessageItem {
	if db == nil {
		debuglog.Cache("loadCachedMessages: channel=%s db=nil", channelID)
		return nil
	}
	debuglog.Cache("loadCachedMessages: channel=%s entry", channelID)

	// Perf instrumentation: wall-clock the whole call and attribute the
	// loop cost across GetReactions / GetUser / json.Unmarshal so we can
	// tell which N+1 dominates the channel-open hot path. Gated on
	// debuglog.Enabled() (atomic.Bool load -> zero cost when SLK_DEBUG
	// is unset). The TODO at the GetReactions callsite below predicts
	// GetReactions as the dominant cost; this trace will confirm or
	// refute it.
	var perfStart time.Time
	var stats *enrichPerfStats
	if debuglog.Enabled() {
		perfStart = time.Now()
		stats = &enrichPerfStats{}
	}

	getMsgsStart := time.Now()
	rows, err := db.GetMessages(channelID, 50, "")
	getMsgsDur := time.Since(getMsgsStart)
	if err != nil {
		debuglog.Cache("loadCachedMessages: GetMessages %s: %v", channelID, err)
		return nil
	}
	if len(rows) == 0 {
		debuglog.Cache("loadCachedMessages: channel=%s result count=0 (no cached rows)", channelID)
		return nil
	}

	out := make([]messages.MessageItem, 0, len(rows))
	for _, m := range rows {
		out = append(out, enrichCachedRow(db, selfUserID, channelID, m, userNames, tsFormat, "loadCachedMessages", router, stats))
	}
	debuglog.Cache("loadCachedMessages: channel=%s result %s", channelID, summarizeMessages(out))
	if stats != nil {
		debuglog.Perf("loadCachedMessages channel=%s N=%d total=%s GetMessages=%s GetReactions(n=%d)=%s GetUser(n=%d)=%s json.Unmarshal(n=%d)=%s",
			channelID, len(rows), time.Since(perfStart), getMsgsDur,
			stats.getReactCalls, stats.getReactTotal,
			stats.getUserCalls, stats.getUserTotal,
			stats.unmarshalCalls, stats.unmarshalTotal)
	}
	return out
}

// enrichCachedRow reconstructs a single messages.MessageItem from a
// cache.Message row using the same fidelity as the network fetchers:
// 3-tier username fallback, per-row reactions, and raw_json
// reconstruction of files / blocks / legacy attachments.
//
// userNames may be nil — username resolution still works via the
// cached users table or the userID fallback, but no memoization
// occurs.
//
// raw_json unmarshal failures degrade the row to text-only without
// failing the caller. logPrefix tags the per-row log lines so callers
// (loadCachedMessages vs loadCachedThreadReplies) remain
// distinguishable in logs.
func enrichCachedRow(
	db *cache.DB,
	selfUserID string,
	channelID string,
	m cache.Message,
	userNames map[string]string,
	tsFormat string,
	logPrefix string,
	router *workspaceRouter,
	stats *enrichPerfStats,
) messages.MessageItem {
	// Bot rows cached before the bot-identity fix have an empty UserID but
	// carry bot_id + username in raw_json. Re-key them on the bot_id and
	// resolve the avatar/name via bots.info so cached bot messages render
	// like freshly-fetched ones (without this they stay blank until a
	// live re-fetch, which never reaches messages older than the latest 50).
	effUserID := m.UserID
	botUsername := ""
	if effUserID == "" && m.RawJSON != "" {
		var rawBot struct {
			BotID    string `json:"bot_id"`
			Username string `json:"username"`
		}
		if json.Unmarshal([]byte(m.RawJSON), &rawBot) == nil && rawBot.BotID != "" {
			effUserID = rawBot.BotID
			botUsername = rawBot.Username
			if router != nil {
				if wctx := router.Active(); wctx != nil && wctx.UserResolver != nil {
					wctx.UserResolver.RequestBot(rawBot.BotID, rawBot.Username)
				}
			}
		}
	}

	// Resolve username from the in-memory map first; fall back to
	// the cached users table; finally fall back to the bot username /
	// user ID so the row still renders something readable.
	var userName string
	if userNames != nil {
		userName = userNames[effUserID]
	}
	if userName == "" && effUserID != "" {
		var t0 time.Time
		if stats != nil {
			t0 = time.Now()
		}
		u, err := db.GetUser(effUserID)
		if stats != nil {
			stats.getUserCalls++
			stats.getUserTotal += time.Since(t0)
		}
		if err == nil {
			if u.DisplayName != "" {
				userName = u.DisplayName
			} else if u.Name != "" {
				userName = u.Name
			}
			if userName != "" && userNames != nil {
				userNames[effUserID] = userName
			}
		}
	}
	if userName == "" {
		if botUsername != "" {
			userName = botUsername
		} else {
			userName = effUserID
			// Cache had no entry for this user. Enqueue an async resolver
			// fetch so the next render after UserResolvedMsg lands shows
			// the real display name instead of the raw user ID. Guarded on
			// m.UserID (the original) so bot rows — already handled via
			// RequestBot above — don't hit the users.info path.
			if router != nil && m.UserID != "" {
				if wctx := router.Active(); wctx != nil && wctx.UserResolver != nil {
					wctx.UserResolver.Request(m.UserID)
				}
			}
		}
	}

	// Reactions for this message.
	var reactions []messages.ReactionItem
	// TODO(perf): N+1 query — for 50 messages this is 50 SQLite calls on the
	// channel-open hot path. If this becomes a bottleneck, add a batched
	// db.GetReactionsForMessages([]ts) map[ts][]ReactionRow to the cache layer.
	var reactT0 time.Time
	if stats != nil {
		reactT0 = time.Now()
	}
	rs, reactErr := db.GetReactions(m.TS, channelID)
	if stats != nil {
		stats.getReactCalls++
		stats.getReactTotal += time.Since(reactT0)
	}
	if reactErr == nil {
		for _, r := range rs {
			hasReacted := false
			for _, uid := range r.UserIDs {
				if uid == selfUserID {
					hasReacted = true
					break
				}
			}
			reactions = append(reactions, messages.ReactionItem{
				Emoji:      r.Emoji,
				Count:      r.Count,
				HasReacted: hasReacted,
				UserIDs:    r.UserIDs,
			})
		}
	} else {
		debuglog.Cache("%s: GetReactions %s/%s: %v", logPrefix, channelID, m.TS, reactErr)
	}

	// Attachments / blocks / legacy attachments come from
	// raw_json. Pre-Task-2 rows have an empty raw_json; for
	// those we render text-only.
	var attachments []messages.Attachment
	var blocks []blockkit.Block
	var legacy []blockkit.LegacyAttachment
	if m.RawJSON != "" {
		var raw slack.Message
		var unmarshalT0 time.Time
		if stats != nil {
			unmarshalT0 = time.Now()
		}
		err := json.Unmarshal([]byte(m.RawJSON), &raw)
		if stats != nil {
			stats.unmarshalCalls++
			stats.unmarshalTotal += time.Since(unmarshalT0)
		}
		if err != nil {
			debuglog.Cache("%s: raw_json unmarshal for %s/%s: %v",
				logPrefix, channelID, m.TS, err)
		} else {
			attachments = extractAttachments(raw.Files)
			blocks = extractBlocks(raw.Blocks)
			legacy = extractLegacyAttachments(raw.Attachments)
		}
	}

	return messages.MessageItem{
		TS:                m.TS,
		UserID:            effUserID,
		UserName:          userName,
		Text:              m.Text,
		Timestamp:         formatTimestamp(m.TS, tsFormat),
		ThreadTS:          m.ThreadTS,
		ReplyCount:        m.ReplyCount,
		Subtype:           m.Subtype,
		Reactions:         reactions,
		Attachments:       attachments,
		Blocks:            blocks,
		LegacyAttachments: legacy,
	}
}

// loadCachedThreadReplies reads cached parent + replies for a thread
// from SQLite and reconstructs []messages.MessageItem with the same
// fidelity as fetchThreadReplies. Offline-pure (no network).
//
// The returned slice includes the parent message at index 0 followed
// by replies in chronological order, matching db.GetThreadReplies'
// ordering. Callers that pass the slice into
// ui.ThreadRepliesLoadedMsg.Replies must strip the parent
// (slice[1:]) since the reducer expects replies-only.
//
// Returns nil when no rows are cached or on DB error.
func loadCachedThreadReplies(
	db *cache.DB,
	selfUserID string,
	channelID, threadTS string,
	userNames map[string]string,
	tsFormat string,
	router *workspaceRouter,
) []messages.MessageItem {
	if db == nil {
		debuglog.Cache("loadCachedThreadReplies: channel=%s thread_ts=%s db=nil", channelID, threadTS)
		return nil
	}
	debuglog.Cache("loadCachedThreadReplies: channel=%s thread_ts=%s entry", channelID, threadTS)
	rows, err := db.GetThreadReplies(channelID, threadTS)
	if err != nil {
		debuglog.Cache("loadCachedThreadReplies: GetThreadReplies %s/%s: %v", channelID, threadTS, err)
		return nil
	}
	if len(rows) == 0 {
		debuglog.Cache("loadCachedThreadReplies: channel=%s thread_ts=%s result count=0", channelID, threadTS)
		return nil
	}

	out := make([]messages.MessageItem, 0, len(rows))
	for _, m := range rows {
		out = append(out, enrichCachedRow(db, selfUserID, channelID, m, userNames, tsFormat, "loadCachedThreadReplies", router, nil))
	}
	debuglog.Cache("loadCachedThreadReplies: channel=%s thread_ts=%s result %s",
		channelID, threadTS, summarizeMessages(out))
	return out
}

// fetchChannelMessages returns the channel's recent messages from the
// network, with cache write-through. The return-value contract:
//
//	nil   - the network call FAILED (transient error, auth issue, etc.)
//	[]    - the channel is genuinely empty
//	[...] - normal case
//
// The MessagesLoadedMsg handler distinguishes nil from empty so a
// failed background refresh doesn't wipe a successfully-rendered
// cache view. Do NOT change nil to mean "empty channel".
func fetchChannelMessages(client *slackclient.Client, channelID string, db *cache.DB, userNames map[string]string, tsFormat string, avatarCache *avatar.Cache, router *workspaceRouter) []messages.MessageItem {
	ctx := context.Background()
	debuglog.Cache("fetchChannelMessages: channel=%s entry", channelID)
	start := time.Now()
	history, err := client.GetHistory(ctx, channelID, 50, "")
	if err != nil {
		debuglog.Cache("fetchChannelMessages: GetHistory %s: %v dur_ms=%d (returning nil → keep cache)",
			channelID, err, time.Since(start).Milliseconds())
		return nil
	}

	msgItems := make([]messages.MessageItem, 0, len(history))
	for _, m := range history {
		rawBytes, _ := json.Marshal(m)
		debuglog.Cache("fetchChannelMessages: upsert channel=%s ts=%s subtype=%q reply_count=%d files=%d",
			channelID, m.Timestamp, m.SubType, m.ReplyCount, len(m.Files))
		authorID, userName := messageAuthor(m, userNames, db, router)
		db.UpsertMessage(cache.Message{
			TS:          m.Timestamp,
			ChannelID:   channelID,
			WorkspaceID: client.TeamID(),
			UserID:      authorID,
			Text:        m.Text,
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			Subtype:     m.SubType,
			RawJSON:     string(rawBytes),
			CreatedAt:   time.Now().Unix(),
		})

		// Convert reactions
		var reactions []messages.ReactionItem
		for _, r := range m.Reactions {
			hasReacted := false
			for _, uid := range r.Users {
				if uid == client.UserID() {
					hasReacted = true
					break
				}
			}
			reactions = append(reactions, messages.ReactionItem{
				Emoji:      r.Name,
				Count:      r.Count,
				HasReacted: hasReacted,
				UserIDs:    r.Users,
			})
			_ = db.UpsertReaction(m.Timestamp, channelID, r.Name, r.Users, r.Count)
		}

		msgItems = append(msgItems, messages.MessageItem{
			TS:                m.Timestamp,
			UserID:            authorID,
			UserName:          userName,
			Text:              m.Text,
			Timestamp:         formatTimestamp(m.Timestamp, tsFormat),
			ThreadTS:          m.ThreadTimestamp,
			ReplyCount:        m.ReplyCount,
			Subtype:           m.SubType,
			Reactions:         reactions,
			Attachments:       extractAttachments(m.Files),
			Blocks:            extractBlocks(m.Blocks),
			LegacyAttachments: extractLegacyAttachments(m.Attachments),
		})
	}

	// Reverse: Slack returns newest first
	for i, j := 0, len(msgItems)-1; i < j; i, j = i+1, j-1 {
		msgItems[i], msgItems[j] = msgItems[j], msgItems[i]
	}

	debuglog.Cache("fetchChannelMessages: channel=%s result %s dur_ms=%d (authoritative replace)",
		channelID, summarizeMessages(msgItems), time.Since(start).Milliseconds())
	if err := db.SetChannelSyncedAt(channelID, time.Now().Unix()); err != nil {
		debuglog.Cache("fetchChannelMessages: SetChannelSyncedAt %s: %v", channelID, err)
	}
	return msgItems
}

// fetchThreadReplies returns network thread replies (parent stripped),
// with cache write-through. Same nil-vs-empty contract as
// fetchChannelMessages: nil signals failure, [] signals "no replies",
// so the ThreadRepliesLoadedMsg consumer can decide whether to clobber
// an already-rendered cached view.
func fetchThreadReplies(client *slackclient.Client, channelID, threadTS string, db *cache.DB, userNames map[string]string, tsFormat string, avatarCache *avatar.Cache, router *workspaceRouter) []messages.MessageItem {
	ctx := context.Background()
	debuglog.Cache("fetchThreadReplies: channel=%s thread_ts=%s entry", channelID, threadTS)
	start := time.Now()
	history, err := client.GetReplies(ctx, channelID, threadTS)
	if err != nil {
		debuglog.Cache("fetchThreadReplies: GetReplies %s/%s: %v dur_ms=%d (returning nil → keep cache)",
			channelID, threadTS, err, time.Since(start).Milliseconds())
		return nil
	}

	msgItems := make([]messages.MessageItem, 0, len(history))
	for _, m := range history {
		rawBytes, _ := json.Marshal(m)
		debuglog.Cache("fetchThreadReplies: upsert channel=%s ts=%s subtype=%q reply_count=%d files=%d",
			channelID, m.Timestamp, m.SubType, m.ReplyCount, len(m.Files))
		authorID, userName := messageAuthor(m, userNames, db, router)
		db.UpsertMessage(cache.Message{
			TS:          m.Timestamp,
			ChannelID:   channelID,
			WorkspaceID: client.TeamID(),
			UserID:      authorID,
			Text:        m.Text,
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			Subtype:     m.SubType,
			RawJSON:     string(rawBytes),
			CreatedAt:   time.Now().Unix(),
		})

		// Convert reactions
		var reactions []messages.ReactionItem
		for _, r := range m.Reactions {
			hasReacted := false
			for _, uid := range r.Users {
				if uid == client.UserID() {
					hasReacted = true
					break
				}
			}
			reactions = append(reactions, messages.ReactionItem{
				Emoji:      r.Name,
				Count:      r.Count,
				HasReacted: hasReacted,
				UserIDs:    r.Users,
			})
			_ = db.UpsertReaction(m.Timestamp, channelID, r.Name, r.Users, r.Count)
		}

		msgItems = append(msgItems, messages.MessageItem{
			TS:                m.Timestamp,
			UserID:            authorID,
			UserName:          userName,
			Text:              m.Text,
			Timestamp:         formatTimestamp(m.Timestamp, tsFormat),
			ThreadTS:          m.ThreadTimestamp,
			ReplyCount:        m.ReplyCount,
			Subtype:           m.SubType,
			Reactions:         reactions,
			Attachments:       extractAttachments(m.Files),
			Blocks:            extractBlocks(m.Blocks),
			LegacyAttachments: extractLegacyAttachments(m.Attachments),
		})
	}

	// First message from GetConversationReplies is the parent -- skip it for the replies list.
	// Return non-nil empty on success-no-replies so the consumer can distinguish from the
	// error path (which returns nil above).
	var out []messages.MessageItem
	if len(msgItems) > 1 {
		out = msgItems[1:]
	} else {
		out = []messages.MessageItem{}
	}
	debuglog.Cache("fetchThreadReplies: channel=%s thread_ts=%s result %s dur_ms=%d (authoritative replace)",
		channelID, threadTS, summarizeMessages(out), time.Since(start).Milliseconds())
	return out
}

func formatTimestamp(ts, format string) string {
	// Slack ts is like "1700000001.000000" -- split on "." and parse the seconds
	parts := strings.SplitN(ts, ".", 2)
	if len(parts) == 0 {
		return ts
	}
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return ts
	}
	t := time.Unix(sec, 0)
	return t.Format(format)
}
