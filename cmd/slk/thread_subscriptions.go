package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/debuglog"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
)

// threadSubscriptionLister is the one call this file makes:
// subscriptions.thread.getView, paginated to a 1000-item hard cap.
type threadSubscriptionLister interface {
	ListThreadSubscriptions(ctx context.Context) ([]slackclient.ThreadSubscriptionView, error)
}

// threadSubscriptionSync reconciles the local thread_subscriptions
// table against the server's view, and caches each subscription's root
// message so the Threads view can render parents for threads slk has
// never seen a message from.
//
// It used to be a phase of the reconnect backfill, and that was the
// single most expensive thing slk did: measured 2026-08-01 on a
// 105-channel workspace, the subscription phase took 132 seconds
// against the channel phase's 2.7, hit its 1000-item hard cap every
// time, and ran on every reconnect — four passes in one ~3-minute
// session, roughly six minutes of work for a 90-second outage that
// produced no new messages. It is now driven from the places that
// actually need the data.
type threadSubscriptionSync struct {
	client      threadSubscriptionLister
	db          *cache.DB
	workspaceID string

	// availableCb, if non-nil, is called with the outcome: true on
	// success, false on error. Wired to wctx.SubscriptionsAvailable so
	// the Threads view's banner reflects the most recent attempt.
	availableCb func(bool)
}

// sync fetches the subscription list and writes it through.
//
// Side effects:
//  1. thread_subscriptions reflects the server's authoritative state.
//  2. availableCb is called with the outcome.
//  3. Each ThreadSubscriptionView.RootMessage is upserted into the
//     messages cache (idempotent by (ts, channel_id)).
//
// Errors from the API call are returned; per-thread message-upsert
// failures are logged and skipped, since one bad message should not
// cost the caller the whole list.
func (s *threadSubscriptionSync) sync(ctx context.Context) error {
	start := time.Now()
	views, err := s.client.ListThreadSubscriptions(ctx)
	if err != nil {
		debuglog.Backfill("team=%s subscription-sync err=%v", s.workspaceID, err)
		if s.availableCb != nil {
			s.availableCb(false)
		}
		return err
	}
	if s.availableCb != nil {
		s.availableCb(true)
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
			WorkspaceID: s.workspaceID,
			ChannelID:   v.Subscription.ChannelID,
			ThreadTS:    v.Subscription.ThreadTS,
			LastRead:    v.Subscription.LastRead,
			// Authoritative newest-reply watermark from the getView
			// root_msg. Lets the threads view compute unread state
			// without the thread's replies being cached locally.
			LatestReply: v.RootMessage.LatestReply,
			Active:      true,
		})
	}
	if err := s.db.ReconcileThreadSubscriptions(s.workspaceID, fresh); err != nil {
		debuglog.Backfill("team=%s subscription-sync reconcile err=%v", s.workspaceID, err)
		return err
	}

	// Upsert the root_msg from every view into the messages cache.
	// Skip entries where RootMessage is empty (Subscription kept but
	// RootMessage couldn't be decoded; see the ListThreadSubscriptions
	// docstring).
	upserted := 0
	for _, v := range views {
		if v.RootMessage.Timestamp == "" {
			continue
		}
		raw, _ := json.Marshal(v.RootMessage)
		if err := s.db.UpsertMessage(cache.Message{
			TS:          v.RootMessage.Timestamp,
			ChannelID:   v.Subscription.ChannelID,
			WorkspaceID: s.workspaceID,
			UserID:      v.RootMessage.User,
			Text:        v.RootMessage.Text,
			ThreadTS:    v.RootMessage.ThreadTimestamp,
			ReplyCount:  v.RootMessage.ReplyCount,
			Subtype:     v.RootMessage.SubType,
			RawJSON:     string(raw),
			CreatedAt:   time.Now().Unix(),
		}); err != nil {
			debuglog.Backfill("team=%s subscription-sync upsert root_msg %s/%s err=%v",
				s.workspaceID, v.Subscription.ChannelID, v.Subscription.ThreadTS, err)
			continue
		}
		upserted++
	}

	debuglog.Backfill("team=%s subscription-sync subs=%d root_msgs_upserted=%d dur_ms=%d",
		s.workspaceID, len(fresh), upserted, time.Since(start).Milliseconds())
	return nil
}

// threadSubsSyncInterval is the minimum gap between a workspace's
// subscriptions.thread.getView sweeps. The call paginates to a
// 1000-item hard cap, measured at ~62 requests per workspace on a real
// account, so it cannot run on every trigger: boot, Threads-view
// activation and every WS reconnect would otherwise multiply it. 30
// minutes bounds the request volume while keeping the list fresh
// across the gaps the socket cannot cover (app closed, laptop asleep
// — the socket replays nothing).
const threadSubsSyncInterval = 30 * time.Minute

// threadSubsStagger hands out startup slots across workspaces. On
// Enterprise Grid every workspace becomes ready in the same second
// (connectWorkspace runs per-workspace goroutines), and an unstaggered
// first sync would fire N paginated getView sweeps at once. Slot i
// waits i*threadSubsStaggerStep before its first sync. Re-syncs never
// wait — the interval above already thins them out.
var threadSubsStagger atomic.Int64

// threadSubsStaggerStep is a var so tests can shrink it.
var threadSubsStaggerStep = 15 * time.Second

// threadSubsGate admits at most one subscription sync per window per
// workspace, and never two concurrently. It replaces the sync.Once
// that gated the fetch when only the first Threads-view open triggered
// it: a Once cannot express "again after a long offline gap", which is
// exactly when the table goes stale.
type threadSubsGate struct {
	mu      sync.Mutex
	last    time.Time
	running bool
	window  time.Duration
}

// tryStart reports whether a sync may begin at `now`. first is true
// only for the workspace's first-ever admission, which is what the
// startup stagger keys off. last is recorded at admission, not
// completion, so a failed sync cannot be retried into a hammering
// loop against a rate-limited endpoint.
func (g *threadSubsGate) tryStart(now time.Time) (first, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.running {
		return false, false
	}
	if !g.last.IsZero() && now.Sub(g.last) < g.window {
		return false, false
	}
	first = g.last.IsZero()
	g.last = now
	g.running = true
	return first, true
}

func (g *threadSubsGate) done() {
	g.mu.Lock()
	g.running = false
	g.mu.Unlock()
}

// unburn rolls back an admission, for a failure that must not cost
// the window: a grid member's sync that found no org session yet.
// The next trigger is admitted again; the org session's arrival (or
// any other trigger) retries the fetch.
func (g *threadSubsGate) unburn() {
	g.mu.Lock()
	g.last = time.Time{}
	g.mu.Unlock()
}

// ensureWorkspaceThreadSubs is the single construction site for the
// throttled subscription sync, shared by every trigger: the
// ThreadService closure (workspace-ready boot + Threads-view
// activation) and the RTM handler's reconnect/wake catch-up.
func ensureWorkspaceThreadSubs(ctx context.Context, wctx *WorkspaceContext, db *cache.DB, send func(tea.Msg), router *workspaceRouter) {
	ensureThreadSubscriptions(ctx, &wctx.ThreadSubsGate,
		&threadSubscriptionSync{
			client:      threadSubscriptionListerFor(router, wctx),
			db:          db,
			workspaceID: wctx.TeamID,
			availableCb: func(available bool) { wctx.SubscriptionsAvailable = available },
		},
		func() { send(ui.ThreadsListDirtyMsg{TeamID: wctx.TeamID}) })
}

// errNoOrgSessionYet reports a grid member workspace's subscription
// sync that failed because no same-user org-level session was
// connected when it ran. The member token's team_is_restricted
// refusal is expected; the org session that can answer may simply not
// have finished connecting yet. Distinguishable so the sync gate does
// not treat it as a burned attempt (see threadSubsGate.unburn).
var errNoOrgSessionYet = errors.New("no same-user org session connected yet")

// orgSessionRetryStep and orgSessionRetryAttempts bound the in-
// admission retry after an errNoOrgSessionYet failure: the org
// session usually registers within seconds (workspaces connect in
// parallel), and 30 x 2s covers a slow connect without spinning on a
// workspace whose org session never arrives. The step is a var so
// tests can shrink it.
var orgSessionRetryStep = 2 * time.Second

const orgSessionRetryAttempts = 30

// threadSubscriptionListerFor picks the lister a workspace's
// subscription sync runs on: its own client, wrapped with the
// same-user org-level session's when the workspace is a grid member.
// subscriptions.thread.getView answers a member token with
// team_is_restricted, and the list it would answer is user-scoped
// across the grid anyway - the org session's list carries
// member-workspace threads too (observed on a live grid) - so the org
// session's answer is this workspace's list, the same one the org
// workspace's own sync stores. The org session resolves LAZILY, at
// list time: the wrapper is built at trigger time, when a staggered
// first sync can predate the org session's connect by seconds.
func threadSubscriptionListerFor(router *workspaceRouter, wctx *WorkspaceContext) threadSubscriptionLister {
	if wctx.Client == nil || !wctx.Client.IsGridMember() {
		return wctx.Client
	}
	return orgFallbackThreadLister{
		own: wctx.Client,
		orgFn: func() threadSubscriptionLister {
			// Return through an explicit nil check: a nil
			// *slackclient.Client returned straight as the interface
			// would be a typed nil, non-nil to == and a panic when
			// called.
			if client := sameUserOrgClient(router, wctx); client != nil {
				return client
			}
			return nil
		},
	}
}

// orgFallbackThreadLister retries a grid member workspace's failed
// subscription list through the same-user org session, resolved at
// list time. The own lister's answer still wins; only its failure
// falls through. No org session connected at list time turns the own
// failure into errNoOrgSessionYet: transient by construction, unburned
// from the sync gate and retried when the org session arrives.
type orgFallbackThreadLister struct {
	own   threadSubscriptionLister
	orgFn func() threadSubscriptionLister
}

func (l orgFallbackThreadLister) ListThreadSubscriptions(ctx context.Context) ([]slackclient.ThreadSubscriptionView, error) {
	views, err := l.own.ListThreadSubscriptions(ctx)
	if err == nil {
		return views, nil
	}
	var org threadSubscriptionLister
	if l.orgFn != nil {
		org = l.orgFn()
	}
	if org == nil {
		return nil, fmt.Errorf("%w: %v", errNoOrgSessionYet, err)
	}
	return org.ListThreadSubscriptions(ctx)
}

// ensureThreadSubscriptions starts a subscription sync in the
// background when the gate admits one, and returns immediately
// either way.
//
// Triggers: workspace-ready (boot), Threads-view activation, and the
// reconnect/wake catch-up. The gate collapses those to at most one
// sync per threadSubsSyncInterval per workspace: within a session the
// thread_subscription_changed WS events keep the table current, so
// re-fetching more often buys nothing.
//
// The workspace's first sync waits out its stagger slot first, so a
// Grid boot doesn't fire every workspace's sweep in the same second.
// A sync admitted by boot that is still in its stagger sleep when the
// user opens the Threads view is fine — the view renders from cache
// and refreshes via onDone's ThreadsListDirtyMsg.
//
// onDone fires only on success — telling the view to re-read a cache
// that a failed fetch did not change would be a wasted round trip.
func ensureThreadSubscriptions(ctx context.Context, gate *threadSubsGate, s *threadSubscriptionSync, onDone func()) {
	first, ok := gate.tryStart(time.Now())
	if !ok {
		return
	}
	go func() {
		defer gate.done()
		if first {
			slot := threadSubsStagger.Add(1) - 1
			if delay := time.Duration(slot) * threadSubsStaggerStep; delay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
			}
		}
		err := s.sync(ctx)
		if errors.Is(err, errNoOrgSessionYet) {
			// Retry within this admission: the org session typically
			// registers moments later (workspaces connect in
			// parallel), and an external re-ensure that arrives while
			// this sync still holds the gate no-ops - without the
			// retry here, the later failure would wait for the next
			// trigger. The gate stays held (running), so nothing
			// interleaves; the bound keeps a never-connecting org from
			// spinning.
			for attempt := 0; errors.Is(err, errNoOrgSessionYet) && attempt < orgSessionRetryAttempts; attempt++ {
				select {
				case <-ctx.Done():
					return
				case <-time.After(orgSessionRetryStep):
				}
				err = s.sync(ctx)
			}
			if errors.Is(err, errNoOrgSessionYet) {
				// The org session never arrived within the bound: only
				// this outcome unburns the admission, or the banner
				// would outlive the org session's eventual arrival. A
				// retry that resolved - succeeded, or failed for a real
				// reason - keeps the burn the admission already set,
				// so the next trigger honours the window like any
				// completed sync.
				gate.unburn()
			}
		}
		if err != nil {
			return
		}
		if onDone != nil {
			onDone()
		}
	}()
}

// OnThreadSubscriptionChanged persists a subscribe/unsubscribe event
// in the thread_subscriptions table. The threads-view UI refresh is
// handled by a ThreadsListDirtyMsg dispatch so a new subscription
// shows up (active=true) or an unsubscribe removes the row
// (active=false) without per-event UI logic here.
func (h *rtmEventHandler) OnThreadSubscriptionChanged(channelID, threadTS, lastRead string, active bool) {
	// Persist subscribe/unsubscribe regardless of active-workspace
	// state. Mirrors OnChannelMarked / OnMessage: dropping the DB
	// write on inactive workspaces means a thread the user just got
	// @-mentioned in (auto-subscribed) would never enter the local
	// thread_subscriptions table, and the threads view would silently
	// omit it on next workspace switch until the next reconnect's
	// ReconcileThreadSubscriptions catches up.
	if h.db != nil {
		if err := h.db.UpsertThreadSubscription(h.workspaceID, channelID, threadTS, lastRead, active); err != nil {
			debuglog.Cache("OnThreadSubscriptionChanged: UpsertThreadSubscription %s/%s: %v",
				channelID, threadTS, err)
		}
	}
	// The threads-list refresh is filtered by team in App.Update, so
	// dispatching it for an inactive workspace would only wake the UI
	// loop for a no-op; the DB write above is what the eventual switch
	// reads. The rail is different: its thread half reads the row just
	// upserted (railThreadsUnread), and an auto-subscription from an
	// @-mention may be a new unread thread to light, an unsubscribe
	// one to stop lighting. Same send as OnThreadMarked's inactive
	// branch.
	if h.isActive != nil && !h.isActive() {
		if h.program != nil {
			h.program.Send(ui.ReadStateChangedMsg{WorkspaceID: h.workspaceID})
		}
		return
	}
	if h.program != nil {
		h.program.Send(ui.ThreadsListDirtyMsg{TeamID: h.workspaceID})
	}
}
