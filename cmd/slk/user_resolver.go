package main

import (
	"context"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/avatar"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/debuglog"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/ui"
	"github.com/slack-go/slack"
)

// userResolverConcurrency caps how many users.info round trips the
// resolver has open at once.
//
// The number is a rate bound, not a throughput target. Before it,
// Request spawned one goroutine per unresolved user with nothing
// between it and the transport, and a cold cache turned that into a
// 40,000-request burst -- one per distinct channel member -- all
// entering RoundTrip within moments of each other. The membership
// fan-out that produced that particular burst is gone, but Request is
// still reachable from the render path, the unresolved-DM sweep and
// inbound messages, so the bound stays. Eight is well under what a
// browser opens to one host and far more than a person generates.
const userResolverConcurrency = 8

// userResolverBatchWindow is how long Request waits for more misses
// to coalesce before flushing them as one edge users/info call. The
// render path resolves a channel's unknown authors in a single
// burst, so a short window turns a channel open from N requests into
// one; 200 ms is below what a person perceives as resolution lag.
const userResolverBatchWindow = 200 * time.Millisecond

// userBatcher is the edge.UsersInfo subset the resolver batches
// misses through. *edge.Client satisfies it structurally; nil means
// no batching and every miss takes the per-user users.info path.
type userBatcher interface {
	UsersInfo(ctx context.Context, updatedIDs map[string]int64) ([]edge.User, error)
}

// userResolver resolves unknown message authors in the background.
// Misses coalesce into batched edge users/info calls (see flush); ids
// edge cannot resolve, a failed batch, and degraded workspaces fall
// back to the per-user Web API users.info path (see resolveOne).
// Deduplicates concurrent requests for the same userID; failures are
// silent (the row stays rendered as its user ID). Bound to a single
// workspace because user IDs are workspace-scoped.
type userResolver struct {
	teamID   string
	client   *slackclient.Client
	db       *cache.DB
	avatars  *avatar.Cache
	send     func(tea.Msg)
	inflight sync.Map // userID -> struct{}
	// sem bounds concurrent round trips on the per-user users.info
	// path (resolveOne). Buffered, so acquiring it happens inside the
	// request goroutine and Request itself never blocks -- it is
	// called from the render path and from WS event handlers, neither
	// of which may wait on the network. The batch path needs no
	// semaphore: it is bounded by the window itself, at one edge
	// users/info call per userResolverBatchWindow (plus that call's
	// internal 80-id splitting, sequential within the call).
	sem chan struct{}

	batcher  userBatcher
	degraded func() bool // nil: never degraded

	pendingMu  sync.Mutex
	pending    map[string]struct{}
	flushTimer *time.Timer
}

func newUserResolver(
	teamID string,
	client *slackclient.Client,
	db *cache.DB,
	avatars *avatar.Cache,
	send func(tea.Msg),
	batcher userBatcher,
	degraded func() bool,
) *userResolver {
	return &userResolver{
		teamID:   teamID,
		client:   client,
		db:       db,
		avatars:  avatars,
		send:     send,
		sem:      make(chan struct{}, userResolverConcurrency),
		batcher:  batcher,
		degraded: degraded,
		pending:  map[string]struct{}{},
	}
}

// Request enqueues a users.info fetch for userID. Returns immediately.
// On success, emits a ui.UserResolvedMsg via the resolver's send
// callback so the App can patch in-history display names live.
func (r *userResolver) Request(userID string) {
	if r == nil || userID == "" {
		return
	}
	if _, exists := r.inflight.LoadOrStore(userID, struct{}{}); exists {
		return
	}
	// Skip if already resolved (in cache.User). This is the hot path
	// for membership.Manager which calls Request for every channel
	// member returned by conversations.members; without this, every
	// channel-switch refetches users.info for each member, which is
	// O(channel-size) API calls per switch (a 1000-member shared
	// channel = 1000 calls). Stale-data refresh is the responsibility
	// of an explicit re-resolution path (not implemented here); this
	// gate is for "first time we see this user".
	//
	// Order matters: inflight.LoadOrStore first (claim the slot),
	// THEN the cache check (bail if cached), with inflight.Delete on
	// the bail path. This avoids a race where two concurrent Requests
	// both miss the cache, both then LoadOrStore (one wins, the loser
	// silently returns). Store-first + check-second means at most one
	// goroutine ever passes the cache check.
	if _, err := r.db.GetUser(userID); err == nil {
		r.inflight.Delete(userID)
		return
	}
	if r.batcher == nil || (r.degraded != nil && r.degraded()) {
		go r.resolveOne(userID)
		return
	}
	r.pendingMu.Lock()
	r.pending[userID] = struct{}{}
	if r.flushTimer == nil {
		r.flushTimer = time.AfterFunc(userResolverBatchWindow, r.flush)
	}
	r.pendingMu.Unlock()
}

// resolveOne resolves a single user through the Web API users.info
// path: the pre-batch behaviour, now the fallback for ids edge did
// not return, for a failed edge call, and for workspaces whose edge
// is degraded. Callers run it on its own goroutine.
func (r *userResolver) resolveOne(userID string) {
	defer r.inflight.Delete(userID)
	if r.sem != nil {
		r.sem <- struct{}{}
		defer func() { <-r.sem }()
	}
	u, err := r.client.GetUserProfile(userID)
	if err != nil {
		debuglog.Cache("userResolver: GetUserProfile team=%s user=%s err=%v",
			r.teamID, userID, err)
		return
	}
	name := u.Profile.DisplayName
	if name == "" {
		name = u.RealName
	}
	if name == "" {
		name = u.Name
	}
	isBot := u.IsBot || u.IsAppUser
	// r.teamID is the workspace's home TeamID; u.TeamID is the
	// user's home TeamID. If they differ (and u.TeamID is set),
	// the user is a Slack Connect / shared-channel guest. Treat
	// an empty u.TeamID as internal — better to under-detect than
	// to falsely flag.
	isExternal := u.TeamID != "" && u.TeamID != r.teamID
	// Persist to the cache DB (its own goroutine-safe SQLite
	// connection) and the avatar cache (internal RWMutex), but
	// do NOT write r.userNames[userID] from this goroutine —
	// userNames is a plain map shared with the UI goroutine and
	// other code paths, and a direct write here trips Go's
	// "concurrent map writes" detector under load (two parallel
	// Request goroutines for different userIDs is enough). The
	// UserResolvedMsg below is delivered to the bubbletea Update
	// loop, which calls Model.PatchUserName on the UI goroutine
	// — that is the single safe writer for in-history rows.
	// Subsequent resolveUserCached misses fall back to the DB
	// row we just upserted, so we don't re-fetch on every miss
	// in the small window before UserResolvedMsg lands.
	r.avatars.Preload(userID, u.Profile.Image32)
	_ = r.db.UpsertUser(cache.User{
		ID:               userID,
		WorkspaceID:      r.teamID,
		Name:             u.Name,
		DisplayName:      name,
		AvatarURL:        u.Profile.Image32,
		Presence:         "away",
		IsBot:            isBot,
		IsExternal:       isExternal,
		StatusEmoji:      u.Profile.StatusEmoji,
		StatusText:       u.Profile.StatusText,
		StatusExpiration: int64(u.Profile.StatusExpiration),
		HuddleState:      u.Profile.HuddleState,
		HuddleExpiration: int64(u.Profile.HuddleStateExpirationTS),
	})
	if r.send != nil {
		// Status before UserResolvedMsg, which callers treat as the
		// end of this user's resolution.
		r.send(ui.UserStatusChangeMsg{
			TeamID:        r.teamID,
			UserID:        userID,
			Emoji:         u.Profile.StatusEmoji,
			Text:          u.Profile.StatusText,
			Expires:       statusExpiry(int64(u.Profile.StatusExpiration)),
			Huddle:        u.Profile.HuddleState,
			HuddleExpires: statusExpiry(int64(u.Profile.HuddleStateExpirationTS)),
		})
		r.send(ui.UserResolvedMsg{
			TeamID:      r.teamID,
			UserID:      userID,
			DisplayName: name,
			IsBot:       isBot,
		})
	}
	if isExternal && r.send != nil {
		r.send(ui.UserExternalMsg{UserID: userID, IsExternal: true})
	}
}

// flush resolves everything queued since the window opened, as one
// edge users/info batch. Anything the batch does not return falls
// back to the per-user path: absence from the batch means "could not
// resolve", and an errored batch resolves nothing at all.
func (r *userResolver) flush() {
	r.pendingMu.Lock()
	ids := make([]string, 0, len(r.pending))
	for id := range r.pending {
		ids = append(ids, id)
	}
	clear(r.pending)
	r.flushTimer = nil
	r.pendingMu.Unlock()
	if len(ids) == 0 {
		return
	}
	// Re-check at flush time: boot may have marked the workspace
	// degraded while the window was open.
	if r.degraded != nil && r.degraded() {
		for _, id := range ids {
			go r.resolveOne(id)
		}
		return
	}
	updated := make(map[string]int64, len(ids))
	for _, id := range ids {
		// 0 is the conditional protocol's "never seen, send the full
		// record" — the resolver only ever queues cache misses.
		updated[id] = 0
	}
	users, err := r.batcher.UsersInfo(context.Background(), updated)
	if err != nil {
		debuglog.Cache("userResolver: edge users/info for %d users team=%s: %v (falling back to per-user users.info)", len(ids), r.teamID, err)
		for _, id := range ids {
			go r.resolveOne(id)
		}
		return
	}
	returned := make(map[string]struct{}, len(users))
	for _, u := range users {
		name := u.Profile.DisplayName
		if name == "" {
			name = u.Profile.RealName
		}
		if name == "" {
			name = u.Name
		}
		if name == "" {
			// Unobserved, but an empty record would otherwise cache
			// an empty name and blank a rendered one.
			continue
		}
		returned[u.ID] = struct{}{}
		r.applyEdgeUser(u)
	}
	for _, id := range ids {
		if _, ok := returned[id]; !ok {
			go r.resolveOne(id)
		}
	}
}

// ResolveNow resolves ids immediately through one edge users/info
// batch and returns the records edge resolved. Unlike Request it
// blocks the caller, so it is for background goroutines that need the
// results — the unresolved-DM sweep maps them to channel ids and
// cannot use the fire-and-forget queue. Ids edge does not resolve are
// simply absent from the result; the caller falls back per-user.
// Nil means "resolve everything per-user": no edge client, a degraded
// workspace, a failed call, or nothing worth sending.
//
// Note applyEdgeUser's deferred inflight.Delete is NOT always a
// no-op here: a Request(id) racing a ResolveNow for the same id can
// claim the inflight slot after ResolveNow's batch returned but
// before its applyEdgeUser runs, and the deferred Delete then drops
// that claim. The duplicate work this enables is bounded: the Delete
// runs after the cache upsert, so any Request arriving later bails
// on the cache check; the realistic duplicate is a Request that
// already queued a pending entry before the upsert landed, which
// flush then re-fetches once. The upserts are idempotent, so no
// guard is taken.
func (r *userResolver) ResolveNow(ids []string) []edge.User {
	if r == nil || r.batcher == nil || len(ids) == 0 {
		return nil
	}
	if r.degraded != nil && r.degraded() {
		return nil
	}
	updated := make(map[string]int64, len(ids))
	for _, id := range ids {
		if id != "" {
			updated[id] = 0
		}
	}
	if len(updated) == 0 {
		return nil
	}
	users, err := r.batcher.UsersInfo(context.Background(), updated)
	if err != nil {
		debuglog.Cache("userResolver: ResolveNow edge users/info for %d users team=%s: %v (caller falls back per-user)", len(updated), r.teamID, err)
		return nil
	}
	for _, u := range users {
		r.applyEdgeUser(u)
	}
	return users
}

// applyEdgeUser records one user the edge batch returned: cache row
// (created — these are misses), avatar preload, and the same resolved,
// status and external messages the per-user path emits.
func (r *userResolver) applyEdgeUser(u edge.User) {
	defer r.inflight.Delete(u.ID)
	name := u.Profile.DisplayName
	if name == "" {
		name = u.Profile.RealName
	}
	if name == "" {
		name = u.Name
	}
	isExternal := u.TeamID != "" && u.TeamID != r.teamID
	r.avatars.Preload(u.ID, u.Profile.ImageOriginal)
	_ = r.db.UpsertUserFromEdge(r.teamID, cache.EdgeUserUpdate{
		ID:               u.ID,
		Name:             u.Name,
		DisplayName:      name,
		AvatarURL:        u.Profile.ImageOriginal,
		IsBot:            u.IsBot,
		IsExternal:       isExternal,
		StatusEmoji:      u.Profile.StatusEmoji,
		StatusText:       u.Profile.StatusText,
		StatusExpiration: u.Profile.StatusExpiration,
		HuddleState:      u.Profile.HuddleState,
		HuddleExpiration: u.Profile.HuddleStateExpirationTS,
		Version:          u.Version,
	})
	if r.send != nil {
		// Status before UserResolvedMsg, as in resolveOne.
		r.send(ui.UserStatusChangeMsg{
			TeamID:        r.teamID,
			UserID:        u.ID,
			Emoji:         u.Profile.StatusEmoji,
			Text:          u.Profile.StatusText,
			Expires:       statusExpiry(u.Profile.StatusExpiration),
			Huddle:        u.Profile.HuddleState,
			HuddleExpires: statusExpiry(u.Profile.HuddleStateExpirationTS),
		})
		r.send(ui.UserResolvedMsg{
			TeamID:      r.teamID,
			UserID:      u.ID,
			DisplayName: name,
			IsBot:       u.IsBot,
		})
	}
	if isExternal && r.send != nil {
		r.send(ui.UserExternalMsg{UserID: u.ID, IsExternal: true})
	}
}

// RequestBot enqueues a bots.info fetch for a bot author (bot_message has
// no `user`, only a `bot_id`). Keyed by botID so the resolved name +
// avatar attach to messages whose UserID was set to the bot_id. username
// is the name carried on the message (used as a fallback / immediate
// value); bots.info supplies the icon (and a name if the message had
// none). Mirrors Request: inflight dedup, cache-skip, async fetch,
// Preload + UpsertUser + UserResolvedMsg (which AvatarReadyMsg follows).
func (r *userResolver) RequestBot(botID, username string) {
	if r == nil || botID == "" {
		return
	}
	if _, exists := r.inflight.LoadOrStore(botID, struct{}{}); exists {
		return
	}
	if _, err := r.db.GetUser(botID); err == nil {
		r.inflight.Delete(botID)
		return
	}
	go func() {
		defer r.inflight.Delete(botID)
		bot, err := r.client.GetBotInfo(context.Background(), botID)
		if err != nil {
			debuglog.Cache("userResolver: GetBotInfo team=%s bot=%s err=%v", r.teamID, botID, err)
			return
		}
		name := username
		if name == "" {
			name = bot.Name
		}
		if name == "" {
			name = botID
		}
		iconURL := bestBotIcon(bot.Icons)
		r.avatars.Preload(botID, iconURL)
		_ = r.db.UpsertUser(cache.User{
			ID:          botID,
			WorkspaceID: r.teamID,
			Name:        name,
			DisplayName: name,
			AvatarURL:   iconURL,
			Presence:    "away",
			IsBot:       true,
		})
		if r.send != nil {
			r.send(ui.UserResolvedMsg{
				TeamID:      r.teamID,
				UserID:      botID,
				DisplayName: name,
				IsBot:       true,
			})
		}
	}()
}

// bestBotIcon returns the best bot icon URL for the avatar grid. The
// avatar fetcher downscales to the avatar cell size, so we prefer the
// 72px icon — large enough to render sharp yet almost always present —
// then fall back through the other sizes by availability rather than
// strictly by pixel size.
func bestBotIcon(ic slack.Icons) string {
	for _, u := range []string{ic.Image72, ic.Image48, ic.Image132, ic.Image36, ic.Image230} {
		if u != "" {
			return u
		}
	}
	return ""
}
