package main

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/debuglog"
	slackclient "github.com/gammons/slk/internal/slack"
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

// ensureThreadSubscriptions runs one subscription sync per workspace
// per session, in the background, and returns immediately.
//
// It is called from the Threads view's list fetch, which is where the
// data is first needed. It used to run at boot — before that, on every
// reconnect — and the Threads view is not what the user is looking at
// when slk starts, so the cost was paid by everyone and used by the
// few who open it.
//
// The Once is not an optimisation. The list fetch runs on activation
// AND on every ThreadsListDirtyMsg, including the one onDone sends, so
// an ungated trigger would loop. And the call is not cheap:
// subscriptions.thread.getView paginates to a 1000-item hard cap,
// measured at ~62 requests per workspace on a real account. Within a
// session the thread_subscription_changed WS events keep the table
// current, which is what makes once enough.
//
// onDone fires only on success — telling the view to re-read a cache
// that a failed fetch did not change would be a wasted round trip.
func ensureThreadSubscriptions(ctx context.Context, once *sync.Once, s *threadSubscriptionSync, onDone func()) {
	once.Do(func() {
		go func() {
			if err := s.sync(ctx); err != nil {
				return
			}
			if onDone != nil {
				onDone()
			}
		}()
	})
}
