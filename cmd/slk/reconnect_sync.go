package main

import (
	"context"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/debuglog"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
)

// reconnectClient is the entire client surface the reconnect path is
// allowed to touch, and the narrowness is the point.
//
// What used to live here was an interface with GetHistorySince,
// GetReplies and ListThreadSubscriptions on it, driving a sweep over
// every channel slk had ever cached plus the workspace's whole
// thread-subscription list. Measured over one ~3-minute session on a
// 105-channel workspace:
//
//	per-channel conversations.history calls:  288
//	  returned 0 messages:                    250  (86%)
//	channel phase:        total_msgs=0   dur_ms=2711
//	subscription phase:   subs=1000      dur_ms=132248
//	ListThreadSubscriptions: hit hard cap 1000, stopping   (x4)
//
// and it ran from OnConnect, which fires on first connect and on every
// reconnect — so every laptop sleep, wifi change and VPN flap replayed
// all of it. One method is what "O(1) reconnect" means in code;
// TestReconnect_ClientSurfaceCannotEnumerate fails if it grows.
type reconnectClient interface {
	// GetUnreadCounts is client.counts: one request that returns
	// unread state for every conversation in the workspace.
	GetUnreadCounts() ([]slackclient.UnreadInfo, slackclient.ThreadsAggregate, error)
}

// teaSender is the subset of *tea.Program the reconnect path uses to
// dispatch a refresh into the UI loop. *tea.Program satisfies it
// implicitly; tests pass a captureSender.
type teaSender interface {
	Send(msg tea.Msg)
}

// reconnectSync is slk's catch-up after the WebSocket comes back.
//
// It exists because the socket does not replay what was missed.
// Measured 2026-08-01 on a real two-workspace setup: after a
// 90-second outage with a message posted and marked unread from
// another client, the reconnected socket delivered ~160
// presence_change events and nothing else — no message, no
// channel_marked. slk's OnConnect never called client.counts (it is
// boot-only), which is exactly why that unread never appeared. So this
// is a user-visible fix as well as a fingerprint change.
//
// Three steps, and the cost of all three is independent of workspace
// size:
//
//  1. client.counts — one request, unread state for everything.
//  2. Mark every other channel stale, so it revalidates when opened.
//  3. Refresh the channel actually on screen, through the same path a
//     channel switch uses.
type reconnectSync struct {
	client      reconnectClient
	db          *cache.DB
	workspaceID string
	program     teaSender

	// activeChannel returns the channel currently on screen, or "" if
	// this workspace has none (it is not the active workspace, or the
	// user has not opened anything yet).
	activeChannel func() string

	// refreshChannel reloads one channel from the server through the
	// normal open path and pushes the result into the UI. Required;
	// tests substitute a recorder.
	refreshChannel func(ctx context.Context, channelID string)
}

// run performs one catch-up pass.
//
// Every step is best-effort: a workspace that comes back with stale
// badges is better than one that returns an error nobody surfaces.
// The error return is reserved for the cache write that would leave
// staleness unrecorded, since that is the one failure a later open
// cannot compensate for.
func (r *reconnectSync) run(ctx context.Context) error {
	start := time.Now()
	active := ""
	if r.activeChannel != nil {
		active = r.activeChannel()
	}

	r.refreshUnreadState()

	// Everything not refreshed below is marked stale rather than
	// fetched. synced_at = 0 is the same value the cache reports for a
	// channel it has never seen, so the UI's freshness tiers already
	// know what to do with it: render the cache immediately, then
	// refetch. The work moves to the moment a channel is looked at,
	// where it is one request the user is waiting for.
	if err := r.db.MarkChannelsStale(r.workspaceID, active); err != nil {
		return err
	}

	if active != "" && r.refreshChannel != nil {
		r.refreshChannel(ctx, active)
	}

	debuglog.Backfill("team=%s reconnect-sync active=%q dur_ms=%d",
		r.workspaceID, active, time.Since(start).Milliseconds())
	return nil
}

// refreshUnreadState pulls client.counts and writes it through to the
// cache, then tells the UI to re-render read state.
//
// Failure is logged and swallowed: unread badges are cosmetic next to
// the messages themselves, and the next boot refreshes them anyway.
func (r *reconnectSync) refreshUnreadState() {
	unreads, _, err := r.client.GetUnreadCounts()
	if err != nil {
		debuglog.Backfill("team=%s reconnect-sync counts err=%v (unread badges stay stale)", r.workspaceID, err)
		return
	}
	if len(unreads) == 0 {
		return
	}
	updates := make([]cache.ChannelReadStateUpdate, 0, len(unreads))
	for _, u := range unreads {
		updates = append(updates, cache.ChannelReadStateUpdate{
			ChannelID:  u.ChannelID,
			LastReadTS: u.LastRead,
			HasUnread:  u.HasUnread,
		})
	}
	if err := r.db.BatchUpdateChannelReadState(updates); err != nil {
		debuglog.Backfill("team=%s reconnect-sync BatchUpdateChannelReadState err=%v", r.workspaceID, err)
		return
	}
	if r.program != nil {
		r.program.Send(ui.ReadStateChangedMsg{WorkspaceID: r.workspaceID})
	}
}

// dedupeGate enforces a minimum interval between reconnect passes.
// Used by OnConnect so a rapid disconnect/reconnect flap doesn't
// trigger overlapping catch-ups. Safe for concurrent calls.
type dedupeGate struct {
	mu     sync.Mutex
	last   time.Time
	window time.Duration
}

// tryStart reports whether a new pass may begin at `now`. If the
// previous pass started less than `window` ago, returns false and
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
