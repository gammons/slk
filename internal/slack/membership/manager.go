// Package membership manages per-channel member sets: SQLite-backed
// persistence, eager fetch on channel switch, live event deltas, and
// external (Slack Connect) user resolution.
package membership

import (
	"context"
	"sync"
	"time"

	"github.com/gammons/slk/internal/cache"
)

// TTL bounds how stale cached membership can be before EnsureFresh
// triggers a background re-fetch. Spec: 24h.
const TTL = 24 * time.Hour

// FailureBackoff bounds how soon a FAILED fetch may be retried. A
// failed fetch never bumps last_full_fetch_at, so without this every
// EnsureFresh re-issues conversations.members — and OnConnect's
// ForceStale+EnsureFresh pair fires on every websocket reconnect.
// Measured live: the on-screen channel was one the workspace's token
// could not see (channel_not_found), the socket flapped, and a
// 25-second session started 42 conversations.members requests. 5
// minutes throttles that loop without wedging a transiently failing
// channel for the full TTL.
const FailureBackoff = 5 * time.Minute

// ConversationMemberAPI is the slack-client subset Manager needs.
// Decoupled from *slackclient.Client for testability.
type ConversationMemberAPI interface {
	GetUsersInConversation(ctx context.Context, channelID string) ([]string, error)
}

// UserResolver is the userResolver subset Manager invokes to trigger
// external-user resolution for unknown IDs. nil-safe in early tasks;
// wired in Task 12.
type UserResolver interface {
	Request(userID string)
}

// PushFunc is invoked by Manager whenever a channel's membership has
// new data to surface to the UI. Wired in main.go to program.Send
// with a ui.ChannelMembershipMsg.
type PushFunc func(channelID string, memberIDs []string)

// Manager owns per-channel member state for one workspace.
type Manager struct {
	workspaceID string
	api         ConversationMemberAPI
	db          *cache.DB
	push        PushFunc
	resolver    UserResolver

	mu          sync.Mutex
	members     map[string]map[string]struct{} // channelID -> member set
	fetching    map[string]struct{}            // in-flight sentinel by channelID
	lastFetched map[string]time.Time           // last successful full-fetch (in-memory, for dedup)
	lastFailed  map[string]time.Time           // last failed fetch; backs off retries
}

// New constructs a Manager bound to one workspace.
func New(workspaceID string, api ConversationMemberAPI, db *cache.DB, push PushFunc, resolver UserResolver) *Manager {
	return &Manager{
		workspaceID: workspaceID,
		api:         api,
		db:          db,
		push:        push,
		resolver:    resolver,
		members:     map[string]map[string]struct{}{},
		fetching:    map[string]struct{}{},
		lastFetched: map[string]time.Time{},
		lastFailed:  map[string]time.Time{},
	}
}

// EnsureFresh loads (if needed) cached membership for a channel,
// pushes it to the UI, and triggers a background full-fetch if the
// cache is missing or older than TTL. The background fetch is
// asynchronous, but the initial cache load and PushFunc invocation
// run synchronously on the caller's goroutine.
//
// IMPORTANT: EnsureFresh calls PushFunc synchronously via pushSnapshot.
// If PushFunc invokes bubbletea's Program.Send (which uses an unbuffered
// channel in bubbletea v2), DO NOT call EnsureFresh from inside a
// bubbletea Update handler — Send would deadlock waiting for the
// Update goroutine to receive. Call EnsureFresh from a separate
// goroutine in that case. Calling from a WebSocket-reader goroutine
// or any background goroutine is safe.
func (m *Manager) EnsureFresh(ctx context.Context, channelID string) {
	m.loadIntoMemory(channelID)
	m.pushSnapshot(channelID)

	fetchedAt, ok, err := m.db.GetChannelMembershipMeta(m.workspaceID, channelID)
	if err != nil {
		// Treat read error as stale — better to refetch than to wedge.
		ok = false
	}
	fresh := ok && time.Since(time.Unix(fetchedAt, 0)) < TTL
	if fresh {
		return
	}
	go m.backgroundFetch(ctx, channelID)
}

// loadIntoMemory reads the cached member set into the in-memory map
// if not already present. Safe to call repeatedly.
func (m *Manager) loadIntoMemory(channelID string) {
	m.mu.Lock()
	_, have := m.members[channelID]
	m.mu.Unlock()
	if have {
		return
	}
	ids, err := m.db.ListChannelMembers(m.workspaceID, channelID)
	if err != nil {
		return
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	m.mu.Lock()
	if _, raced := m.members[channelID]; !raced {
		m.members[channelID] = set
	}
	m.mu.Unlock()
}

// pushSnapshot calls the push callback with the current in-memory
// member set for a channel.
func (m *Manager) pushSnapshot(channelID string) {
	m.mu.Lock()
	set := m.members[channelID]
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	if m.push != nil {
		m.push(channelID, ids)
	}
}

// backgroundFetch performs a single GetUsersInConversation call,
// persists the result via cache.ReplaceChannelMembers (which bumps
// last_full_fetch_at), updates in-memory state, and pushes to UI.
// Deduped by per-channel in-flight sentinel.
func (m *Manager) backgroundFetch(ctx context.Context, channelID string) {
	m.mu.Lock()
	if _, busy := m.fetching[channelID]; busy {
		m.mu.Unlock()
		return
	}
	// Also dedup against an in-memory recent-fetch timestamp: a prior
	// goroutine may have already completed and released the sentinel
	// before this one arrived. The DB-backed TTL check in EnsureFresh
	// can't catch this because a flurry of concurrent EnsureFresh calls
	// all read the (empty) meta before any one of them persists.
	if last, ok := m.lastFetched[channelID]; ok && time.Since(last) < TTL {
		m.mu.Unlock()
		return
	}
	// A recent failure suppresses the retry. ForceStale clears
	// lastFetched but deliberately NOT lastFailed: ForceStale runs on
	// every websocket reconnect, and a channel whose fetch keeps
	// failing (e.g. one this workspace's token cannot see) would
	// otherwise be re-fetched on every flap.
	if last, ok := m.lastFailed[channelID]; ok && time.Since(last) < FailureBackoff {
		m.mu.Unlock()
		return
	}
	m.fetching[channelID] = struct{}{}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.fetching, channelID)
		m.mu.Unlock()
	}()

	ids, err := m.api.GetUsersInConversation(ctx, channelID)
	if err != nil {
		m.mu.Lock()
		m.lastFailed[channelID] = time.Now()
		m.mu.Unlock()
		return
	}
	// Deliberately no resolution pass over ids.
	//
	// This used to call m.resolver.Request for every member, which is
	// one users.info request per member on any id the cache has not
	// seen. Measured on a cold cache: a 35-second boot started 40,523
	// of them, one per distinct row in channel_members. The resolver
	// short-circuits on a cache hit, so the users.list sweep used to
	// hide it by filling the cache first; deleting that sweep exposed
	// it.
	//
	// It is also a question the official client never asks — zero
	// /api/users.info and zero /api/conversations.members across all 8
	// captures. It fetches one channel's first page of members through
	// edgeapi's users/list (count 30, present first), which returns
	// full user records with no resolution step at all. Moving to that
	// is the next step; removing the fan-out does not have to wait for
	// it.
	//
	// The ids themselves are still fetched and cached below: that is
	// one bounded call per channel, and it is what the mention
	// picker's in-channel ordering reads. Names for members slk has
	// not met come from the cache, the boot response, and on-demand
	// resolution when a row is actually rendered.
	now := time.Now().Unix()
	if err := m.db.ReplaceChannelMembers(m.workspaceID, channelID, ids, now); err != nil {
		return
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	m.mu.Lock()
	m.members[channelID] = set
	m.lastFetched[channelID] = time.Now()
	delete(m.lastFailed, channelID)
	m.mu.Unlock()
	m.pushSnapshot(channelID)
}

// ApplyJoin records a single membership addition (from a
// member_joined_channel event). Single-row upsert; does NOT bump
// last_full_fetch_at — that timestamp tracks full-fetch freshness only.
// If a UserResolver is configured, ApplyJoin invokes it for the user
// (resolver dedupes via its inflight map; redundant calls are cheap).
func (m *Manager) ApplyJoin(channelID, userID string) {
	now := time.Now().Unix()
	if err := m.db.UpsertChannelMember(m.workspaceID, channelID, userID, now); err != nil {
		return
	}
	m.mu.Lock()
	set := m.members[channelID]
	if set == nil {
		set = map[string]struct{}{}
		m.members[channelID] = set
	}
	set[userID] = struct{}{}
	m.mu.Unlock()

	if m.resolver != nil {
		m.resolver.Request(userID)
	}
	m.pushSnapshot(channelID)
}

// ApplyLeave records a single membership removal.
func (m *Manager) ApplyLeave(channelID, userID string) {
	if err := m.db.DeleteChannelMember(m.workspaceID, channelID, userID); err != nil {
		return
	}
	m.mu.Lock()
	if set := m.members[channelID]; set != nil {
		delete(set, userID)
	}
	m.mu.Unlock()
	m.pushSnapshot(channelID)
}

// ForceStale invalidates the freshness timestamp for a channel so the
// next EnsureFresh will trigger a re-fetch. Called from the websocket
// reconnect hook for the currently active channel. Preserves both the
// in-memory and persisted member list — only the meta timestamp is
// zeroed.
func (m *Manager) ForceStale(channelID string) {
	if err := m.db.ZeroChannelMembershipMeta(m.workspaceID, channelID); err != nil {
		return
	}
	// Also clear the in-memory dedup window so EnsureFresh actually
	// re-fetches even within a recent successful-fetch interval.
	m.mu.Lock()
	delete(m.lastFetched, channelID)
	m.mu.Unlock()
}
