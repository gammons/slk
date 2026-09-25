package main

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/debuglog"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

// peerStatusWindow is how long invalidations coalesce before one
// refetch. Measured on a real workspace, user_invalidated arrived 328
// times across 65 users in 12 hours, so the window exists to fold
// bursts into one request, not to hold down a steady flood.
const peerStatusWindow = 2 * time.Second

// Slack documents dnd.teamInfo as accepting at most 50 users.
const dndTeamInfoBatchSize = 50

// huddleRecheckInterval is how often users last seen in a huddle are
// refetched. Measured live, Slack sends user_invalidated when a user
// joins a huddle but not reliably when it ends: a user whose huddle
// ended got no event at all, while the huddle_state_expiration_ts on
// their record was still ~22 minutes out. Without a recheck the icon
// stayed up that long. One batched users/info per interval, and only
// while someone is in a huddle.
const huddleRecheckInterval = time.Minute

// dndRefreshTimeout bounds one DND refresh, so a slow dnd.teamInfo cannot
// hold up the caller for long.
const dndRefreshTimeout = 2 * time.Second

// peerStatusRefresher turns Slack's ID-only invalidation events into
// fresh custom status and DND state for other users.
//
// The browser-protocol socket does not push other users' user_change or
// dnd_updated_user. It sends user_invalidated and dnd_invalidated, whose
// whole payload is the user's ID, and leaves the client to refetch.
// Invalidations coalesce for window into one users/info batch and
// dnd.teamInfo batches of at most 50 users. Only users in the local
// cache are refetched here. If an unknown user later appears in a
// rendered message, the first-sight resolver emits their full status.
//
// The authenticated user is skipped. Their own changes arrive as
// user_change and dnd_updated, which carry the new state.
type peerStatusRefresher struct {
	teamID string
	selfID string
	window time.Duration

	// known reports whether a user is in the local cache.
	known func(userID string) bool
	// resolveNow refetches users in full through one edge users/info
	// batch and writes them to the cache. Each returned user has
	// already been persisted and emitted by the resolver itself.
	resolveNow func(ids []string) []edge.User
	// getUserProfile is the per-user users.info fallback for ids
	// resolveNow didn't return.
	getUserProfile func(userID string) (*slack.User, error)
	// db persists the fallback path's status/huddle fields.
	db *cache.DB
	// dndTeamInfo is dnd.teamInfo.
	dndTeamInfo func(ctx context.Context, ids []string) (map[string]slack.DNDStatus, error)
	send        func(tea.Msg)
	now         func() time.Time

	mu    sync.Mutex
	users map[string]struct{}
	dnd   map[string]struct{}
	timer *time.Timer

	// huddlers are the users last seen in a huddle; see
	// huddleRecheckInterval. huddleTimer runs only while it is non-empty.
	huddlers       map[string]struct{}
	huddleInterval time.Duration
	huddleTimer    *time.Timer
}

func newPeerStatusRefresher(
	teamID string,
	selfID string,
	known func(userID string) bool,
	resolveNow func(ids []string) []edge.User,
	dndTeamInfo func(ctx context.Context, ids []string) (map[string]slack.DNDStatus, error),
	getUserProfile func(userID string) (*slack.User, error),
	db *cache.DB,
	send func(tea.Msg),
) *peerStatusRefresher {
	return &peerStatusRefresher{
		teamID:         teamID,
		selfID:         selfID,
		window:         peerStatusWindow,
		known:          known,
		resolveNow:     resolveNow,
		getUserProfile: getUserProfile,
		db:             db,
		dndTeamInfo:    dndTeamInfo,
		send:           send,
		now:            time.Now,
		users:          make(map[string]struct{}),
		dnd:            make(map[string]struct{}),

		huddlers:       make(map[string]struct{}),
		huddleInterval: huddleRecheckInterval,
	}
}

// InvalidateUser queues a user_invalidated for the next flush.
func (p *peerStatusRefresher) InvalidateUser(userID string) { p.enqueue(userID, false) }

// InvalidateDND queues a dnd_invalidated for the next flush.
func (p *peerStatusRefresher) InvalidateDND(userID string) { p.enqueue(userID, true) }

func (p *peerStatusRefresher) enqueue(userID string, dnd bool) {
	if p == nil || userID == "" || userID == p.selfID {
		return
	}
	if p.known != nil && !p.known(userID) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if dnd {
		p.dnd[userID] = struct{}{}
	} else {
		p.users[userID] = struct{}{}
	}
	if p.timer == nil {
		p.timer = time.AfterFunc(p.window, p.flush)
	}
}

// flush refetches everything queued since the window opened.
func (p *peerStatusRefresher) flush() {
	p.mu.Lock()
	users := slices.Collect(maps.Keys(p.users))
	dnd := slices.Collect(maps.Keys(p.dnd))
	clear(p.users)
	clear(p.dnd)
	p.timer = nil
	p.mu.Unlock()

	// DND first: the per-user fallback below can wait on a slow
	// users.info call.
	if len(dnd) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), dndRefreshTimeout)
		p.RefreshDND(ctx, dnd)
		cancel()
	}
	if len(users) > 0 {
		now := p.now()
		resolved := make(map[string]struct{}, len(users))
		if p.resolveNow != nil {
			for _, u := range p.resolveNow(users) {
				// resolveNow already persisted and emitted u; only
				// huddle tracking is flush's own concern.
				resolved[u.ID] = struct{}{}
				logHuddleState("users/info refetch", u.Profile.HuddleState, u.Profile.HuddleStateExpirationTS, now)
				p.noteHuddle(u.ID, u.Profile.HuddleState)
			}
		}
		// Ids resolveNow didn't return fall back to a per-user call.
		for _, id := range users {
			if _, ok := resolved[id]; ok {
				continue
			}
			p.fallbackResolve(id, now)
		}
	}
}

// fallbackResolve refetches one user's profile over the Web API when
// flush's edge batch did not return them.
func (p *peerStatusRefresher) fallbackResolve(userID string, now time.Time) {
	if p.getUserProfile == nil {
		return
	}
	u, err := p.getUserProfile(userID)
	if err != nil {
		debuglog.General("peer status: users.info fallback for %s: %v", userID, err)
		return
	}
	logHuddleState("users.info fallback", u.Profile.HuddleState, int64(u.Profile.HuddleStateExpirationTS), now)
	p.noteHuddle(userID, u.Profile.HuddleState)
	applyProfileStatus(p.teamID, userID, u.Profile, p.db, p.send)
}

// applyProfileStatus persists a users.info profile's custom status and
// huddle state via db.UpdateUserStatus, and emits the resulting
// UserStatusChangeMsg.
func applyProfileStatus(teamID, userID string, profile slack.UserProfile, db *cache.DB, send func(tea.Msg)) {
	if db != nil {
		_ = db.UpdateUserStatus(userID, profile.StatusEmoji, profile.StatusText,
			int64(profile.StatusExpiration), profile.HuddleState, int64(profile.HuddleStateExpirationTS))
	}
	if send != nil {
		send(ui.UserStatusChangeMsg{
			TeamID:        teamID,
			UserID:        userID,
			Emoji:         profile.StatusEmoji,
			Text:          profile.StatusText,
			Expires:       statusExpiry(int64(profile.StatusExpiration)),
			Huddle:        profile.HuddleState,
			HuddleExpires: statusExpiry(int64(profile.HuddleStateExpirationTS)),
		})
	}
}

// SeedHuddles starts rechecking the users statuses holds as in a huddle,
// for a workspace whose statuses were just read from the cache. A cached
// huddle may have ended while slk was not running.
func (p *peerStatusRefresher) SeedHuddles(statuses map[string]peerstatus.Status) {
	for id, st := range statuses {
		if st.Huddle == peerstatus.HuddleActive {
			p.noteHuddle(id, st.Huddle)
		}
	}
}

// noteHuddle records whether userID was last seen in a huddle and keeps
// the recheck running while anyone is.
func (p *peerStatusRefresher) noteHuddle(userID, state string) {
	if p == nil || userID == "" || userID == p.selfID {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if state == peerstatus.HuddleActive {
		p.huddlers[userID] = struct{}{}
	} else {
		delete(p.huddlers, userID)
	}
	p.armHuddleRecheckLocked()
}

func (p *peerStatusRefresher) armHuddleRecheckLocked() {
	if len(p.huddlers) > 0 && p.huddleTimer == nil {
		p.huddleTimer = time.AfterFunc(p.huddleInterval, p.recheckHuddles)
	}
}

// recheckHuddles queues every user last seen in a huddle for the normal
// batched refetch. The refetch result updates huddlers, so a user whose
// huddle ended drops out and the timer stops once nobody is left.
func (p *peerStatusRefresher) recheckHuddles() {
	p.mu.Lock()
	ids := slices.Collect(maps.Keys(p.huddlers))
	p.huddleTimer = nil
	p.mu.Unlock()
	for _, id := range ids {
		p.InvalidateUser(id)
	}
	p.mu.Lock()
	p.armHuddleRecheckLocked()
	p.mu.Unlock()
}

// RefreshDND fetches DND state for ids in one dnd.teamInfo call and
// reports each user's. It is also called at connect for the DM peers,
// whose DND slk would otherwise not learn until it next changed.
func (p *peerStatusRefresher) RefreshDND(ctx context.Context, ids []string) {
	for _, msg := range p.FetchDND(ctx, ids) {
		p.emit(msg)
	}
}

// FetchDND is RefreshDND without reporting: the DND state for ids, keyed
// by user ID. Requests are split at Slack's documented 50-user limit;
// a user in a batch that fails is simply absent from the result. Nil
// only when there is nobody to ask about; otherwise non-nil, empty when
// every batch failed.
func (p *peerStatusRefresher) FetchDND(ctx context.Context, ids []string) map[string]ui.UserDNDChangeMsg {
	if p == nil || p.dndTeamInfo == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	ids = slices.DeleteFunc(slices.Clone(ids), func(id string) bool {
		if id == "" || id == p.selfID {
			return true
		}
		if _, ok := seen[id]; ok {
			return true
		}
		seen[id] = struct{}{}
		return false
	})
	if len(ids) == 0 {
		return nil
	}

	states := make(map[string]slack.DNDStatus, len(ids))
	for start := 0; start < len(ids); start += dndTeamInfoBatchSize {
		end := min(start+dndTeamInfoBatchSize, len(ids))
		batch, err := p.dndTeamInfo(ctx, ids[start:end])
		if err != nil {
			// This batch's users stay unknown; other batches are kept.
			debuglog.General("peer status: dnd.teamInfo for %d users: %v", end-start, err)
			continue
		}
		for id, st := range batch {
			states[id] = st
		}
	}

	now := p.now().Unix()
	out := make(map[string]ui.UserDNDChangeMsg, len(states))
	for id, st := range states {
		on, end := slackclient.DNDStateFromStatus(st, now)
		msg := ui.UserDNDChangeMsg{TeamID: p.teamID, UserID: id, Enabled: on}
		if end > 0 {
			msg.EndTS = time.Unix(end, 0)
		}
		out[id] = msg
	}
	return out
}

// cachedPeerStatuses is every cached custom status in teamID, for a
// workspace becoming active. Never nil, so callers can add to it.
func cachedPeerStatuses(db *cache.DB, teamID string) map[string]peerstatus.Status {
	out := map[string]peerstatus.Status{}
	if db == nil {
		return out
	}
	users, err := db.ListUserStatuses(teamID)
	if err != nil {
		debuglog.Cache("peer status: listing cached statuses team=%s: %v", teamID, err)
		return out
	}
	for _, u := range users {
		out[u.ID] = peerstatus.Status{}.
			WithStatus(u.StatusEmoji, u.StatusText, statusExpiry(u.StatusExpiration)).
			WithHuddle(u.HuddleState, statusExpiry(u.HuddleExpiration))
	}
	return out
}

// withPeerStatuses returns a copy of channels with each DM's Status
// taken from statuses, clearing any status a DM had that statuses no
// longer holds. Does not modify its input, which is shared with the
// goroutine that built it.
func withPeerStatuses(channels []sidebar.ChannelItem, statuses map[string]peerstatus.Status) []sidebar.ChannelItem {
	outChannels := slices.Clone(channels)
	for i := range outChannels {
		if uid := outChannels[i].DMUserID; uid != "" {
			outChannels[i].Status = statuses[uid]
		}
	}
	return outChannels
}

func (p *peerStatusRefresher) emit(msg tea.Msg) {
	if p.send != nil {
		p.send(msg)
	}
}

// statusExpiry converts Slack's status_expiration, where 0 means the
// status never expires, into the zero-means-never time the UI uses.
func statusExpiry(unix int64) time.Time {
	if unix <= 0 {
		return time.Time{}
	}
	return time.Unix(unix, 0)
}

// logHuddleState writes a huddle_state other than the unset default to
// the debug log, without the user's ID, with how far ahead its expiry
// is. It confirmed "in_a_huddle" with an expiry about 22 minutes after
// joining; it stays to show whether Slack renews that expiry during a
// long huddle and whether any other value appears.
func logHuddleState(source, state string, expiration int64, now time.Time) {
	if state == "" || state == "default_unset" {
		return
	}
	var expiresIn int64
	if expiration > 0 {
		expiresIn = expiration - now.Unix()
	}
	debuglog.General("peer status: %s huddle_state=%q expiration_set=%v expires_in=%ds", source, state, expiration > 0, expiresIn)
}

// OnUserStatusChange applies a user_change or user_status_changed. Those
// carry the full record, so no refetch is needed.
func (h *rtmEventHandler) OnUserStatusChange(userID string, st slackclient.UserStatus) {
	logHuddleState("user_change", st.HuddleState, st.HuddleExpiration, time.Now())
	if h.db != nil {
		_ = h.db.UpdateUserStatus(userID, st.Emoji, st.Text, st.Expiration, st.HuddleState, st.HuddleExpiration)
	}
	if h.program == nil {
		return
	}
	h.program.Send(ui.UserStatusChangeMsg{
		TeamID:        h.workspaceID,
		UserID:        userID,
		Emoji:         st.Emoji,
		Text:          st.Text,
		Expires:       statusExpiry(st.Expiration),
		Huddle:        st.HuddleState,
		HuddleExpires: statusExpiry(st.HuddleExpiration),
	})
}

func (h *rtmEventHandler) OnUserInvalidated(userID string) {
	if h.wsCtx != nil {
		h.wsCtx.PeerStatus.InvalidateUser(userID)
	}
}

func (h *rtmEventHandler) OnDNDInvalidated(userID string) {
	if h.wsCtx != nil {
		h.wsCtx.PeerStatus.InvalidateDND(userID)
	}
}

func (h *rtmEventHandler) OnUserDNDChange(userID string, enabled bool, endUnix int64) {
	if h.program == nil {
		return
	}
	msg := ui.UserDNDChangeMsg{TeamID: h.workspaceID, UserID: userID, Enabled: enabled}
	if endUnix > 0 {
		msg.EndTS = time.Unix(endUnix, 0)
	}
	h.program.Send(msg)
}
