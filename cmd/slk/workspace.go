package main

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/gammons/slk/internal/service"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/slack/membership"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// UnresolvedDM tracks a DM channel whose user name wasn't in the initial user list.
type UnresolvedDM struct {
	ChannelID string
	UserID    string
}

// WorkspaceContext holds all state for a single connected workspace.
type WorkspaceContext struct {
	Client *slackclient.Client
	// Edge is the edgeapi client for this workspace: the
	// conditional-revalidation and server-side-search endpoints. Nil
	// only if construction failed, and every caller nil-checks.
	Edge *edge.Client
	// EdgeHealth records whether edge resolution is working for this
	// workspace this session. bootstrap marks it degraded on a
	// wholesale failure; the user resolver reads it to skip batch
	// attempts that would resolve nothing.
	EdgeHealth *edge.Health
	ConnMgr    *slackclient.ConnectionManager
	RTMHandler *rtmEventHandler
	UserNames  map[string]string
	// AvatarURLs maps userID -> avatar image URL. Populated from the
	// local users cache at connect time (synchronous, before any
	// goroutines spin up), from conversations.view's users array via
	// applyBootUsers, and from on-demand resolveUser calls. Read by
	// the AvatarFunc closure on the UI goroutine to trigger a lazy
	// avatar Preload when an avatar slot first renders empty.
	//
	// sync.Map (not a plain map) because writes happen from background
	// goroutines (resolveUser, the unresolved-DM sweep) while reads happen on
	// the bubbletea Update goroutine. The lookup-or-trigger pattern
	// (LoadOrStore-style) doesn't apply here — we only call Load — but
	// we still need a concurrent map to avoid Go's "concurrent map
	// writes" detector. Stored values are string (avatar URL).
	AvatarURLs *sync.Map
	// UserNamesByHandle maps a user's handle (the Slack `name` field
	// without an `@`) to a display name. Used to resolve participant
	// handles in mpdm channel names like `mpdm-grant--myles--ray-1`.
	UserNamesByHandle map[string]string
	// BotUserIDs is the set of user IDs known to be Slack apps or bots.
	// Populated from the local cache on startup, from
	// conversations.view's users array via applyBootUsers, and by any
	// on-demand resolveUser calls. Used during channel construction to
	// bucket app DMs into a separate "Apps" sidebar section.
	BotUserIDs map[string]bool
	// SectionStore holds the user's Slack-native sidebar sections for
	// this workspace. Nil when use_slack_sections is disabled, the
	// REST bootstrap failed, or this workspace hasn't connected yet.
	// channelitem.go's resolver and the sectionsProviderAdapter both
	// nil-check it before use.
	SectionStore *service.SectionStore
	// MuteStore tracks which channels the user has muted (Slack stores
	// this in the user prefs blob, not on the channel objects). The
	// sidebar uses it to suppress unread dots and apply a dimmer
	// foreground for muted channels. Nil when the bootstrap fetch
	// failed or hasn't run yet — callers must nil-check before use.
	MuteStore *service.MuteStore
	// ThreadsHasUnreads is the workspace-wide threads-have-any-unread
	// signal returned by client.counts on startup. The local SQLite
	// heuristic for per-thread unread state can produce false positives
	// (the parent channel's last_read_ts is older than a thread reply
	// the user already read in another Slack client). When Slack tells
	// us the workspace has zero unread threads, we trust that and
	// suppress the heuristic-derived flags entirely.
	ThreadsHasUnreads bool
	// ThreadSubsGate throttles the workspace's
	// subscriptions.thread.getView fetches to one per
	// threadSubsSyncInterval. Fired on workspace-ready, on Threads
	// view activation, and by the reconnect/wake catch-up. See
	// ensureThreadSubscriptions.
	ThreadSubsGate threadSubsGate
	// SubscriptionsAvailable indicates whether the most recent
	// threadSubscriptionSync attempt succeeded in fetching Slack's
	// authoritative thread-subscription list. true on bootstrap
	// (optimistic — no banner before the first Threads-view open, by
	// which point nothing has been attempted) and after every
	// successful sync; false after a failed one. The UI uses it to
	// decide whether to draw the "Threads list unavailable" banner.
	SubscriptionsAvailable bool
	Channels               []sidebar.ChannelItem
	// FinderItems is the list shown in the Ctrl+P finder: the channels
	// the user has joined. Channels they have not joined are not held
	// here at all — they arrive per query from the finder's debounced
	// channels/search and live only in the finder component.
	FinderItems   []channelfinder.Item
	TeamID        string
	TeamName      string
	UserID        string
	UnresolvedDMs []UnresolvedDM
	// customEmoji holds this workspace's emoji name -> URL (or
	// "alias:target") map. Access it via CustomEmoji/SetCustomEmoji,
	// never directly.
	//
	// atomic.Pointer for the same reason as userGroups below: the
	// background emoji.list fetch writes it while the workspace-switch
	// cmd goroutine reads it. It used to be a plain map, exempted on
	// the grounds that its single background write only happened on the
	// rare conversations.history fallback; that stopped being true once
	// emoji.list was restored to the normal path.
	customEmoji atomic.Pointer[map[string]string]
	// userGroups holds this workspace's usergroup ID -> handle map.
	// Access it via UserGroups/SetUserGroups, never directly.
	//
	// atomic.Pointer (not a plain map) because the write happens on the
	// background usergroups.list fetch goroutine while reads happen on
	// the bubbletea Update/cmd goroutines (workspace switch, search) and
	// on the RTM event loop (notification body stripping).
	//
	// The stored map is published once and never mutated afterwards, so
	// readers need no further synchronization.
	userGroups atomic.Pointer[map[string]string]
	// Self presence and DND state for this workspace. Populated on connect
	// and updated by manual_presence_change / dnd_updated WS events plus
	// optimistic writes from the presence menu.
	Presence   string    // "active" or "away"; "" until first fetch
	DNDEnabled bool      // true if either snooze or admin-DND is active
	DNDEndTS   time.Time // unified end timestamp; zero if not in DND
	// LastVisitedByChannel maps channelID -> unix-second timestamp of
	// the user's most recent visit to that channel in this workspace.
	// Populated once at connect from cache.GetChannelVisits and
	// updated on every ChannelSelectedMsg via the visit recorder.
	// Used to populate channelfinder.Item.LastVisited for sort.
	LastVisitedByChannel map[string]int64
	// UserResolver dispatches background users.info lookups for
	// unknown message authors. Set in connectWorkspace once the
	// in-memory UserNames map and the *tea.Program are both available.
	// Hot-path message processors call resolveUserCached first and
	// fall back to UserResolver.Request(userID) to enqueue an async
	// fetch; the goroutine emits ui.UserResolvedMsg back into the
	// program, which patches in-history rows live.
	UserResolver *userResolver
	// PeerStatus refetches other users' custom status and DND when the
	// socket invalidates them. Nil-safe: a nil refresher drops
	// invalidations.
	PeerStatus *peerStatusRefresher
	// Membership owns per-channel member sets for this workspace:
	// SQLite-backed cache + eager fetch on channel switch + live
	// member_joined/left WS deltas + external-user resolution. Set
	// in connectWorkspace alongside UserResolver (it depends on the
	// resolver to trigger external-user lookups for newly-seen IDs).
	Membership *membership.Manager
}

// UserGroups returns this workspace's usergroup ID -> handle map, or an
// empty map before usergroups.list has returned. Safe to call from any
// goroutine; the result must be treated as read-only.
func (w *WorkspaceContext) UserGroups() map[string]string {
	if m := w.userGroups.Load(); m != nil {
		return *m
	}
	return map[string]string{}
}

// SetUserGroups publishes a usergroup ID -> handle map for this
// workspace. The caller must not mutate the map afterwards.
func (w *WorkspaceContext) SetUserGroups(groups map[string]string) {
	w.userGroups.Store(&groups)
}

// CustomEmoji returns this workspace's emoji name -> URL map, or an
// empty map before the first publish.
func (w *WorkspaceContext) CustomEmoji() map[string]string {
	if m := w.customEmoji.Load(); m != nil {
		return *m
	}
	return map[string]string{}
}

// SetCustomEmoji publishes an emoji name -> URL (or "alias:target")
// map for this workspace. The caller must not mutate the map
// afterwards.
func (w *WorkspaceContext) SetCustomEmoji(emojis map[string]string) {
	w.customEmoji.Store(&emojis)
}

// workspaceRouter is the program-wide registry of connected
// workspaces. active is the one the user is looking at:
// wireCallbacks(router) is invoked ONCE at startup, and every
// workspace-scoped callback reads router.Active() at invocation time
// so the effective workspace tracks the user's current Ctrl-N
// selection without any closure rebinding. all is every workspace
// that has connected this session, keyed by team ID.
//
// all is guarded by mu because its writers and readers are on
// different goroutines. An earlier version of this comment said the
// map "is populated only during the connect-workspaces phase (before
// p.Run)" and so needed no mutex; that was wrong. run launches one
// connect goroutine per workspace and then calls p.Run immediately,
// so each goroutine's Add lands while the program is already handling
// messages -- including the WorkspaceReadyMsg of whichever workspace
// finished first, whose callbacks (EnsureSubscriptions, the rail's
// unread reader on every read-state event, later the workspace
// switcher) call ByID, and the wake watcher's All. Two workspaces
// finishing together, or one finishing while another's ready message
// is being handled, is a concurrent map write or read/write -- a
// runtime fatal, not a data race the detector merely reports -- and
// the window is exactly the boot phase, when every one of those
// callbacks fires.
type workspaceRouter struct {
	active atomic.Pointer[WorkspaceContext]
	mu     sync.RWMutex
	all    map[string]*WorkspaceContext
}

func newWorkspaceRouter() *workspaceRouter {
	return &workspaceRouter{all: map[string]*WorkspaceContext{}}
}

func (r *workspaceRouter) Active() *WorkspaceContext  { return r.active.Load() }
func (r *workspaceRouter) Set(wctx *WorkspaceContext) { r.active.Store(wctx) }
func (r *workspaceRouter) ByID(teamID string) *WorkspaceContext {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.all[teamID]
}

// Add registers a connected workspace. Called from its connect
// goroutine; this is the write side of mu.
func (r *workspaceRouter) Add(wctx *WorkspaceContext) {
	r.mu.Lock()
	r.all[wctx.TeamID] = wctx
	r.mu.Unlock()
}

// All returns a snapshot of every connected workspace, as a slice
// rather than the map so callers can iterate without holding mu
// across whatever they do per workspace.
func (r *workspaceRouter) All() []*WorkspaceContext {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*WorkspaceContext, 0, len(r.all))
	for _, wctx := range r.all {
		out = append(out, wctx)
	}
	return out
}

// mostRecentlyVisitedChannel returns the channel ID with the latest
// last-visited timestamp, or "" when there are no recorded visits (e.g.
// the first run). Drives last-channel restoration on startup.
func mostRecentlyVisitedChannel(visits map[string]int64) string {
	var bestID string
	var bestTS int64
	for id, ts := range visits {
		if ts > bestTS {
			bestTS = ts
			bestID = id
		}
	}
	return bestID
}
