package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/debuglog"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
	"github.com/slack-go/slack"
)

// historyFetcher is the subset of the Slack client surface the
// backfiller needs. Defined here rather than in internal/slack so
// the dependency runs from the caller (cmd/slk) toward the package
// being depended on (internal/slack), keeping the abstraction owned
// by its sole consumer. *slackclient.Client implicitly satisfies
// this interface.
type historyFetcher interface {
	GetHistorySince(ctx context.Context, channelID, oldest string, maxTotal int) (slackclient.HistorySinceResult, error)
	GetReplies(ctx context.Context, channelID, threadTS string) ([]slack.Message, error)
	ListThreadSubscriptions(ctx context.Context) ([]slackclient.ThreadSubscriptionView, error)
	GetUnreadCounts() ([]slackclient.UnreadInfo, slackclient.ThreadsAggregate, error)
}

// teaSender is the subset of *tea.Program the backfiller uses to
// dispatch a refresh into the UI loop. *tea.Program satisfies it
// implicitly; tests pass a captureSender.
type teaSender interface {
	Send(msg tea.Msg)
}

// backfiller orchestrates a single reconnect backfill pass for one
// workspace. Holds all per-pass state so the dedupe in OnConnect only
// needs to track timestamps, not in-flight work.
//
// Two-phase: runChannelPhase fetches conversations.history for every
// channel that has cached messages, and runThreadPhase fetches
// conversations.replies for any threads with new activity that the
// user is involved in. run() executes both phases and dispatches a
// ThreadsListDirtyMsg.
type backfiller struct {
	client        historyFetcher
	db            *cache.DB
	workspaceID   string
	selfUserID    string
	program       teaSender // nil in tests; used to dispatch ThreadsListDirtyMsg
	concurrency   int
	perChannelCap int

	// Threads discovered while iterating channel-phase results.
	// Populated during runChannelPhase; consumed by runThreadPhase.
	// Stored as a set of (channelID, threadTS) pairs.
	mu                sync.Mutex
	discoveredThreads map[threadKey]struct{}

	// availableCb, if non-nil, is called with the outcome of the
	// subscription-phase API call: true on success, false on error.
	// The OnConnect site wires this to wctx.SubscriptionsAvailable so
	// the UI banner reflects the most recent attempt.
	availableCb func(bool)
}

// threadKey is the composite identifier of a thread in the cache:
// the channel it lives in and the thread's parent timestamp.
type threadKey struct {
	ChannelID string
	ThreadTS  string
}

// newBackfiller constructs a backfiller. concurrency caps simultaneous
// HTTP calls (use 4 in production); perChannelCap is the maxTotal
// passed to GetHistorySince (use 500).
func newBackfiller(client historyFetcher, db *cache.DB, workspaceID, selfUserID string, program teaSender, concurrency, perChannelCap int, availableCb func(bool)) *backfiller {
	if concurrency < 1 {
		concurrency = 1
	}
	if perChannelCap < 1 {
		perChannelCap = 500
	}
	return &backfiller{
		client:            client,
		db:                db,
		workspaceID:       workspaceID,
		selfUserID:        selfUserID,
		program:           program,
		concurrency:       concurrency,
		perChannelCap:     perChannelCap,
		discoveredThreads: map[threadKey]struct{}{},
		availableCb:       availableCb,
	}
}

// runChannelPhase fetches conversations.history(oldest=synced_at) for
// every channel in the workspace that has cached messages. Upserts
// all returned messages and bumps synced_at on success. Records any
// thread_ts seen in the results into b.discoveredThreads for
// runThreadPhase to consume.
//
// One channel's failure does not abort the pass; failures are logged
// and the goroutine moves on.
func (b *backfiller) runChannelPhase(ctx context.Context) error {
	// Fetch the server's unread map first so we can include channels
	// the user has never opened in slk (e.g., a brand-new DM that
	// arrived during an offline window). Failures are non-fatal: we
	// fall back to the cached-channels-only set.
	var unreadIDs []string
	if unreads, _, err := b.client.GetUnreadCounts(); err != nil {
		debuglog.Backfill("team=%s GetUnreadCounts err=%v (falling back to cached-only)", b.workspaceID, err)
	} else {
		for _, u := range unreads {
			if u.HasUnread {
				unreadIDs = append(unreadIDs, u.ChannelID)
			}
		}
	}

	channels, err := b.db.BackfillCandidates(b.workspaceID, unreadIDs)
	if err != nil {
		return err
	}
	debuglog.Backfill("team=%s trigger=reconnect channels=%d unread_only=%d start",
		b.workspaceID, len(channels), len(unreadIDs))
	start := time.Now()

	sem := make(chan struct{}, b.concurrency)
	var wg sync.WaitGroup
	var totalMu sync.Mutex
	totalMsgs := 0

	for _, row := range channels {
		wg.Add(1)
		go func(row cache.ChannelSyncRow) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			n, err := b.backfillOneChannel(ctx, row)
			if err != nil {
				debuglog.Backfill("team=%s channel=%s err=%v", b.workspaceID, row.ChannelID, err)
				return
			}
			totalMu.Lock()
			totalMsgs += n
			totalMu.Unlock()
		}(row)
	}
	wg.Wait()

	debuglog.Backfill("team=%s channel-phase done total_msgs=%d dur_ms=%d",
		b.workspaceID, totalMsgs, time.Since(start).Milliseconds())
	return nil
}

// backfillOneChannel fetches missed history for a single channel and
// upserts every returned message. Returns the count of upserted
// messages. Records thread_ts of any returned thread-reply messages
// into b.discoveredThreads.
//
// Watermark advancement rule: latest_synced_ts is advanced to the
// highest ts in the fetched batch ONLY when the batch was not capped
// (i.e., we know we got every message in (oldest, now]). If the cap
// was hit, the watermark is left untouched so the next reconnect
// re-fetches the missed range. The wall-clock synced_at is bumped
// either way for UI cache-freshness display.
func (b *backfiller) backfillOneChannel(ctx context.Context, row cache.ChannelSyncRow) (int, error) {
	// Resolve the watermark: explicit latest_synced_ts wins over
	// MAX(ts) fallback. Empty string means "first sync — just fetch
	// the latest page."
	oldest, err := b.db.GetChannelWatermark(row.ChannelID)
	if err != nil {
		return 0, fmt.Errorf("watermark for %s: %w", row.ChannelID, err)
	}
	start := time.Now()

	res, err := b.client.GetHistorySince(ctx, row.ChannelID, oldest, b.perChannelCap)
	if err != nil {
		return 0, err
	}

	var maxTS string
	for _, m := range res.Messages {
		raw, _ := json.Marshal(m)
		if err := b.db.UpsertMessage(cache.Message{
			TS:          m.Timestamp,
			ChannelID:   row.ChannelID,
			WorkspaceID: b.workspaceID,
			UserID:      m.User,
			Text:        m.Text,
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			Subtype:     m.SubType,
			RawJSON:     string(raw),
			CreatedAt:   time.Now().Unix(),
		}); err != nil {
			return 0, fmt.Errorf("upsert channel %s ts %s: %w", row.ChannelID, m.Timestamp, err)
		}
		if m.Timestamp > maxTS {
			maxTS = m.Timestamp
		}
		if m.ThreadTimestamp != "" {
			b.mu.Lock()
			b.discoveredThreads[threadKey{ChannelID: row.ChannelID, ThreadTS: m.ThreadTimestamp}] = struct{}{}
			b.mu.Unlock()
		}
	}

	// Wall-clock freshness bump (unchanged behavior). The UI uses
	// this to decide whether to spin or show the cache.
	b.db.SetChannelSyncedAt(row.ChannelID, time.Now().Unix())

	// Watermark advance: only when we know we got everything. A
	// capped batch means there are still messages in (maxTS, now)
	// the API didn't give us; advancing past oldest would skip them
	// on the next reconnect.
	if !res.Capped && maxTS != "" {
		if _, err := b.db.AdvanceChannelLatestSyncedTS(row.ChannelID, maxTS); err != nil {
			debuglog.Backfill("team=%s channel=%s advance watermark err=%v", b.workspaceID, row.ChannelID, err)
		}
	}

	capStr := ""
	if res.Capped {
		capStr = " capped=true"
	}
	debuglog.Backfill("team=%s channel=%s oldest=%s count=%d max_ts=%s dur_ms=%d%s",
		b.workspaceID, row.ChannelID, oldest, len(res.Messages), maxTS, time.Since(start).Milliseconds(), capStr)
	return len(res.Messages), nil
}

// runThreadPhase iterates b.discoveredThreads, filters to threads
// where the user is involved per the cache, and fetches replies for
// each through a bounded worker pool. Failures are logged and
// skipped — one bad thread does not abort the pass.
func (b *backfiller) runThreadPhase(ctx context.Context) error {
	b.mu.Lock()
	threads := make([]threadKey, 0, len(b.discoveredThreads))
	for k := range b.discoveredThreads {
		threads = append(threads, k)
	}
	b.mu.Unlock()

	start := time.Now()
	// Filter to involved threads using the cache (cheap, no network).
	involved := make([]threadKey, 0, len(threads))
	for _, k := range threads {
		ok, err := b.db.ThreadInvolvesUser(b.workspaceID, k.ChannelID, k.ThreadTS, b.selfUserID)
		if err != nil {
			debuglog.Backfill("team=%s thread-filter err channel=%s thread_ts=%s err=%v", b.workspaceID, k.ChannelID, k.ThreadTS, err)
			continue
		}
		if ok {
			involved = append(involved, k)
		}
	}

	sem := make(chan struct{}, b.concurrency)
	var wg sync.WaitGroup
	for _, k := range involved {
		wg.Add(1)
		go func(k threadKey) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := b.backfillOneThread(ctx, k); err != nil {
				debuglog.Backfill("team=%s thread channel=%s thread_ts=%s err=%v", b.workspaceID, k.ChannelID, k.ThreadTS, err)
			}
		}(k)
	}
	wg.Wait()

	debuglog.Backfill("team=%s thread-phase threads_involved=%d done dur_ms=%d",
		b.workspaceID, len(involved), time.Since(start).Milliseconds())
	return nil
}

// backfillOneThread fetches the full reply list for a thread and
// upserts every returned message. The Slack response includes the
// parent at index 0 followed by replies; UpsertMessage is idempotent
// by (ts, channel_id), so re-upserting the parent is safe.
func (b *backfiller) backfillOneThread(ctx context.Context, k threadKey) error {
	replies, err := b.client.GetReplies(ctx, k.ChannelID, k.ThreadTS)
	if err != nil {
		return err
	}
	for _, m := range replies {
		raw, _ := json.Marshal(m)
		b.db.UpsertMessage(cache.Message{
			TS:          m.Timestamp,
			ChannelID:   k.ChannelID,
			WorkspaceID: b.workspaceID,
			UserID:      m.User,
			Text:        m.Text,
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			Subtype:     m.SubType,
			RawJSON:     string(raw),
			CreatedAt:   time.Now().Unix(),
		})
	}
	return nil
}

// runSubscriptionPhase fetches the workspace's full thread-subscription
// list via subscriptions.thread.getView, reconciles the local
// thread_subscriptions table against it, and upserts every returned
// root_msg into the messages cache so the threads-view can render
// parents even for threads slk has never seen a message from.
//
// Side effects:
//  1. thread_subscriptions reflects the server's authoritative state.
//  2. b.availableCb is called with true/false so the UI banner can
//     reflect the outcome.
//  3. Each ThreadSubscriptionView.RootMessage is upserted into the
//     messages cache (idempotent by (ts, channel_id)).
//
// Errors from the API call are returned to the caller; per-thread
// message-upsert failures are logged and skipped (one bad message
// does not abort the pass).
func (b *backfiller) runSubscriptionPhase(ctx context.Context) error {
	start := time.Now()
	views, err := b.client.ListThreadSubscriptions(ctx)
	if err != nil {
		debuglog.Backfill("team=%s subscription-phase err=%v", b.workspaceID, err)
		if b.availableCb != nil {
			b.availableCb(false)
		}
		return err
	}
	if b.availableCb != nil {
		b.availableCb(true)
	}

	// Adapt slack-client view rows into cache.ThreadSubscription. The
	// API method already filters out subscribed=false items, so the
	// list is conservative: every item here is currently active.
	fresh := make([]cache.ThreadSubscription, 0, len(views))
	for _, v := range views {
		if !v.Subscription.Active {
			continue
		}
		fresh = append(fresh, cache.ThreadSubscription{
			WorkspaceID: b.workspaceID,
			ChannelID:   v.Subscription.ChannelID,
			ThreadTS:    v.Subscription.ThreadTS,
			LastRead:    v.Subscription.LastRead,
			Active:      true,
		})
	}
	if err := b.db.ReconcileThreadSubscriptions(b.workspaceID, fresh); err != nil {
		debuglog.Backfill("team=%s reconcile err=%v", b.workspaceID, err)
		return err
	}

	// Upsert the root_msg from every view into the messages cache.
	// Mirrors the upsert pattern in backfillOneThread (the parent
	// payload comes from a different endpoint here, but the cache
	// row shape is the same). Skip entries where RootMessage is
	// empty (Subscription kept but RootMessage couldn't be decoded;
	// see the ListThreadSubscriptions docstring).
	upserted := 0
	for _, v := range views {
		if v.RootMessage.Timestamp == "" {
			continue
		}
		raw, _ := json.Marshal(v.RootMessage)
		if err := b.db.UpsertMessage(cache.Message{
			TS:          v.RootMessage.Timestamp,
			ChannelID:   v.Subscription.ChannelID,
			WorkspaceID: b.workspaceID,
			UserID:      v.RootMessage.User,
			Text:        v.RootMessage.Text,
			ThreadTS:    v.RootMessage.ThreadTimestamp,
			ReplyCount:  v.RootMessage.ReplyCount,
			Subtype:     v.RootMessage.SubType,
			RawJSON:     string(raw),
			CreatedAt:   time.Now().Unix(),
		}); err != nil {
			debuglog.Backfill("team=%s subscription-phase upsert root_msg %s/%s err=%v",
				b.workspaceID, v.Subscription.ChannelID, v.Subscription.ThreadTS, err)
			continue
		}
		upserted++
	}

	debuglog.Backfill("team=%s subscription-phase subs=%d root_msgs_upserted=%d dur_ms=%d",
		b.workspaceID, len(fresh), upserted, time.Since(start).Milliseconds())
	return nil
}

// run executes the full backfill pass: channel phase, thread phase,
// subscription phase, then a ThreadsListDirtyMsg dispatch so the UI
// re-queries the threads view from the now-current cache. Phase
// errors are logged but do not abort subsequent work — best-effort
// overall.
func (b *backfiller) run(ctx context.Context) error {
	start := time.Now()
	if err := b.runChannelPhase(ctx); err != nil {
		debuglog.Backfill("team=%s channel-phase err=%v", b.workspaceID, err)
	}
	if err := b.runThreadPhase(ctx); err != nil {
		debuglog.Backfill("team=%s thread-phase err=%v", b.workspaceID, err)
	}
	if err := b.runSubscriptionPhase(ctx); err != nil {
		debuglog.Backfill("team=%s subscription-phase err=%v", b.workspaceID, err)
	}
	if b.program != nil {
		b.program.Send(ui.ThreadsListDirtyMsg{TeamID: b.workspaceID})
	}
	debuglog.Backfill("team=%s trigger=reconnect total_dur_ms=%d status=ok",
		b.workspaceID, time.Since(start).Milliseconds())
	return nil
}

// dedupeGate enforces a minimum interval between backfill passes.
// Used by OnConnect so a rapid disconnect/reconnect flap doesn't
// trigger thundering backfills. Safe for concurrent calls.
type dedupeGate struct {
	mu     sync.Mutex
	last   time.Time
	window time.Duration
}

// tryStart reports whether a new backfill pass may begin at `now`. If
// the previous pass started less than `window` ago, returns false and
// leaves `last` unchanged. Otherwise records `last = now` and returns
// true.
func (g *dedupeGate) tryStart(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.last.IsZero() && now.Sub(g.last) < g.window {
		return false
	}
	g.last = now
	return true
}
