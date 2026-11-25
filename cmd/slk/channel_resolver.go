package main

import (
	"context"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui"
	"github.com/slack-go/slack"
)

const channelResolverRetryDelay = 30 * time.Second

// channelResolver learns names for bare channel mentions without joining a
// conversation or adding sidebar/finder rows. Each resolver belongs to the
// source workspace; cached channel names must belong to that workspace too.
// Failures leave the mention unresolved, including inaccessible private channels.
type channelResolver struct {
	teamID   string
	db       *cache.DB
	lookup   func(context.Context, string) (*slack.Channel, error)
	inflight sync.Map // channel ID -> struct{}
	// retryAfter holds per-channel deadlines after failed lookups. Later
	// failures prune expired records, including IDs never requested again.
	// Known names and cache hits bypass and clear the cooldown.
	retryAfter sync.Map // channel ID -> time.Time
	// Reuse userNameStore for channel IDs: its locking, UI-owned snapshots
	// and versioned NotifyFrom handoff apply to any ID -> name mapping.
	// Only uiChannelNameNotifier differs; no user IDs are stored here.
	names *userNameStore
	// As with userResolver, acquiring a slot happens in the worker so
	// history readers, UI cache reads and WS callbacks never wait on HTTP.
	sem chan struct{}
}

func newChannelResolver(teamID string, db *cache.DB, lookup func(context.Context, string) (*slack.Channel, error)) *channelResolver {
	r := &channelResolver{
		teamID: teamID,
		db:     db,
		lookup: lookup,
		names:  newUserNameStore(nil),
		sem:    make(chan struct{}, userResolverConcurrency),
	}
	if db != nil {
		channels, _ := db.ListChannels(teamID, false)
		for _, ch := range channels {
			if ch.Name != "" && (ch.Type == "channel" || ch.Type == "private") {
				r.names.Set(ch.ID, ch.Name)
			}
		}
	}
	return r
}

// nameStore returns the resolver's names, or nil when resolution is disabled.
// userNameStore already makes reads, writes, snapshots and notifications nil-safe.
func (r *channelResolver) nameStore() *userNameStore {
	if r == nil {
		return nil
	}
	return r.names
}

// Request uses known names and the local cache first, then schedules a
// concurrency-limited conversations.info lookup. Failed lookups cool down before
// another Request can retry; no automatic retries are scheduled. A nil resolver
// disables optional resolution, as in cache-only callers and tests.
func (r *channelResolver) Request(channelID string) {
	if r == nil || channelID == "" {
		return
	}
	if _, exists := r.inflight.LoadOrStore(channelID, struct{}{}); exists {
		return
	}
	if name, ok := r.names.Get(channelID); ok && name != "" {
		r.retryAfter.Delete(channelID)
		r.inflight.Delete(channelID)
		return
	}
	if r.db != nil {
		if ch, err := r.db.GetChannel(channelID); err == nil && ch.WorkspaceID == r.teamID && ch.Name != "" && (ch.Type == "channel" || ch.Type == "private") {
			r.retryAfter.Delete(channelID)
			r.names.Set(channelID, ch.Name)
			r.inflight.Delete(channelID)
			return
		}
	}
	if r.lookup == nil {
		r.inflight.Delete(channelID)
		return
	}
	if retryAt, ok := r.retryAfter.Load(channelID); ok {
		if time.Now().Before(retryAt.(time.Time)) {
			r.inflight.Delete(channelID)
			return
		}
		r.retryAfter.Delete(channelID)
	}
	go r.resolve(channelID)
}

func (r *channelResolver) resolve(channelID string) {
	defer r.inflight.Delete(channelID)
	r.sem <- struct{}{}
	defer func() { <-r.sem }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := r.lookup(ctx, channelID)
	if err != nil || ch == nil || ch.ID != channelID || ch.Name == "" || ch.IsIM || ch.IsMpIM || !(ch.IsChannel || ch.IsGroup || ch.IsPrivate) {
		now := time.Now()
		r.retryAfter.Range(func(id, retryAt any) bool {
			if !now.Before(retryAt.(time.Time)) {
				r.retryAfter.CompareAndDelete(id, retryAt)
			}
			return true
		})
		r.retryAfter.Store(channelID, now.Add(channelResolverRetryDelay))
		return
	}
	r.retryAfter.Delete(channelID)

	chType := "channel"
	if ch.IsPrivate || ch.IsGroup {
		chType = "private"
	}
	if r.db != nil {
		_ = r.db.UpsertChannelMetadata(cache.Channel{
			ID:          channelID,
			WorkspaceID: r.teamID,
			Name:        ch.Name,
			Type:        chType,
			Topic:       ch.Topic.Value,
			IsMember:    ch.IsMember,
			UpdatedAt:   time.Now().Unix(),
		})
	}
	r.names.Set(channelID, ch.Name)
}

// uiChannelNameNotifier mirrors uiNameNotifier: Program.Send must run off the
// caller because synchronous UI cache reads can Set names inside Update.
func uiChannelNameNotifier(teamID string, send func(tea.Msg)) func(channelID, name string) {
	return func(channelID, name string) {
		go send(ui.ChannelResolvedMsg{TeamID: teamID, ChannelID: channelID, Name: name})
	}
}
