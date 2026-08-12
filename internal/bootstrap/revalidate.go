package bootstrap

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/slack/edge"
)

// revalidate refreshes the cache against edgeapi instead of
// enumerating it.
//
// This is the function that replaces slk's ~50-page users.list sweep.
// The official client issues zero users.list and zero
// conversations.list calls across all 8 captures; it sends
// {id: version} for what it holds and gets back only what moved. A
// fully current cache costs one ~290-byte response per batch.
//
// The id sets are deliberately SCOPED rather than "everything cached":
//
//   - channels: userBoot's channels + ims, i.e. what the sidebar will
//     actually render.
//   - users: the authors conversations.view returned, plus open-DM
//     counterparties.
//
// Everything else is left stale and revalidated when first needed. A
// fixed batch size over an unbounded id set emits a long run of
// identically-sized requests — 125 consecutive exactly-80s on a
// 10k-user workspace — which is a cleaner distributional signature
// than the client's own ragged 1-80 spread. Scoping is the fix; jitter
// would be inventing a shape no capture shows.
//
// Errors are non-fatal. A stale cache renders; a workspace that failed
// to boot does not.
func revalidate(ctx context.Context, deps Deps, out *Result, logf func(string, ...any)) {
	// Nil dependencies are logged and skipped rather than returned as
	// errors, which is the opposite of what Run does for Boot, Counts,
	// View, History and Store-when-opening-a-channel.
	//
	// The rule is the same in both places — a wiring bug must not
	// panic — but the consequence differs. A missing Viewer means no
	// channel opens; a missing Revalidator means the cache stays as
	// stale as it already was, which is exactly the outcome of a
	// revalidation request that fails, and that outcome is documented
	// non-fatal. Refusing to boot over it would be strictly worse than
	// the failure it is reporting. The log line is the only signal, so
	// it says what was lost.
	if deps.Revalidate == nil {
		logf("bootstrap: revalidation skipped: Deps.Revalidate is nil; the cache will render stale")
		return
	}
	if deps.Store == nil {
		logf("bootstrap: revalidation skipped: Deps.Store is nil; the cache will render stale")
		return
	}

	// Two independent steps, not one early-returning sequence: a
	// failed channels/info says nothing about users/info, and losing
	// the user pass to it would send slk back to resolving every
	// author one users.info call at a time — the fan-out this phase
	// exists to delete.
	revalidateChannels(ctx, deps, out, logf)
	revalidateUsers(ctx, deps, out, logf)
}

// revalidateChannels conditionally refreshes the conversations the
// sidebar will render: userBoot's channels plus its ims, and nothing
// else in the cache.
//
// The id set is partitioned by each conversation's context_team_id,
// and each team gets its own channels/info call. On Enterprise Grid a
// user's conversations are owned by many teams within the org and the
// edge cache keys records under the owning team: a single call scoped
// to the auth.test team resolved zero of one Grid user's 217
// conversations (gammons/slk#5). On a non-Grid workspace every
// context team is expected to be the workspace team, so the partition
// is a single group and the request shape is identical to before. An
// empty context team groups under the workspace team, preserving the
// old behaviour for anything the field is missing on.
//
// The ims are included on purpose even though channels/info cannot
// resolve them. Measured across the captures: of 193 ids the official
// client sent to this endpoint, 22 were IM ids, and **all 22 came back
// in failed_ids** — none appeared in results, none in member_channels.
// So the official client sends them and they always fail, and matching
// that is the point of this package.
//
// Two consequences worth knowing before anyone "optimises" ims out of
// this set, which would be a divergence from the client:
//
//   - No IM is ever written by UpdateChannelFromEdge, because IMs never
//     come back as results. A DM's cached name and type cannot be
//     corrupted from here.
//   - Every IM lands in FailedIDs, and ApplyMembership preserves failed
//     ids rather than clearing them. That is the only reason DMs keep
//     is_member across a boot. Removing the failed-id exclusion would
//     mark every DM a non-membership on the next revalidation.
func revalidateChannels(ctx context.Context, deps Deps, out *Result, logf func(string, ...any)) {
	teamOf := make(map[string]string, len(out.Channels)+len(out.IMs))
	ids := make([]string, 0, len(out.Channels)+len(out.IMs))
	for _, ch := range out.Channels {
		ids = append(ids, ch.ID)
		teamOf[ch.ID] = ch.ContextTeamID
	}
	for _, im := range out.IMs {
		ids = append(ids, im.ID)
		teamOf[im.ID] = im.ContextTeamID
	}

	cached, err := deps.Store.ChannelVersions(deps.WorkspaceID)
	if err != nil {
		// Degrade to sending 0 for everything, which asks for full
		// records: more bytes back, but correct. The map returned
		// beside the error is discarded — a version slk cannot vouch
		// for makes the server withhold a record slk does not have.
		logf("bootstrap: reading cached channel versions: %v (revalidating everything from scratch)", err)
		cached = nil
	}

	updated := conditionalVersions(ids, cached)
	if len(updated) == 0 {
		// No request at all. An updated_ids-less revalidation is a
		// round trip that can only return nothing, and a stream of
		// them is a shape the official client never produces.
		return
	}

	// noteIM tracks which ids are IMs: they ALWAYS land in failed_ids
	// (22 of 22 across the captures, healthy workspaces included), so
	// the wholesale-failure check below must not count them, or every
	// healthy workspace would trip it.
	noteIM := make(map[string]bool, len(out.IMs))
	for _, im := range out.IMs {
		noteIM[im.ID] = true
	}

	groups := make(map[string]map[string]int64)
	for id, version := range updated {
		team := teamOf[id]
		if team == "" {
			team = deps.WorkspaceID
		}
		if groups[team] == nil {
			groups[team] = make(map[string]int64)
		}
		groups[team][id] = version
	}
	// The majority base is NON-IM ids, on both sides. IMs always fail
	// (above), so wholesaleFailure excludes them — and counting them
	// here would let a group that is mostly IMs claim the majority
	// and get edge marked degraded over a single channel's failure.
	nonIMTotal := nonIMCount(updated, noteIM)
	// Largest group first, alphabetical on ties. On a Grid org the
	// enterprise-id group holds the overwhelming majority of a user's
	// conversations (measured: 218 of 277), and judging it first means
	// a wholesale edge failure is diagnosed before the remaining
	// groups spend their requests. Deterministic order also keeps the
	// debug log readable and tests able to rely on call order.
	teams := slices.Collect(maps.Keys(groups))
	slices.SortFunc(teams, func(a, b string) int {
		if d := len(groups[b]) - len(groups[a]); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	for i, team := range teams {
		outcome := revalidateChannelTeam(ctx, deps, team, groups[team], logf)
		// Only the first (largest) group can trip the wholesale
		// check: later groups are smaller, so if the largest holds
		// under half the non-IM ids no group reaches the majority
		// threshold. On an exact 50/50 tie the second group is never
		// evaluated — half is not a majority — and the alpha-first
		// tiebreak makes which half goes first arbitrary but
		// deterministic.
		if i == 0 && deps.Health != nil && nonIMTotal > 0 && nonIMCount(groups[team], noteIM)*2 >= nonIMTotal && wholesaleFailure(outcome, groups[team], noteIM) {
			deps.Health.MarkDegraded()
			logf("bootstrap: channels/info for team %s failed wholesale (%d of %d ids); marking edge degraded for this session and skipping the remaining %d team group(s)",
				team, len(groups[team]), len(updated), len(teams)-1)
			return
		}
	}
}

// teamOutcome is what one team's revalidation learned, for the
// wholesale-failure check in revalidateChannels. callErr covers both
// transport and ok:false failures (the body's ids are all
// unresolved either way); failed is the failed_ids set.
type teamOutcome struct {
	callErr bool
	failed  map[string]struct{}
}

// wholesaleFailure reports whether a team's call resolved NOTHING it
// was asked about: an errored call, or every non-IM id in failed_ids.
// IMs are excluded — they always fail (see revalidateChannels), so
// counting them would trip the check on healthy workspaces. A group
// whose response is empty because everything was already current is
// NOT a wholesale failure: nothing in failed_ids means nothing
// failed. A group with no non-IM ids can only fail wholesale via
// callErr.
func wholesaleFailure(oc teamOutcome, group map[string]int64, noteIM map[string]bool) bool {
	if oc.callErr {
		return true
	}
	nonIM := 0
	for id := range group {
		if noteIM[id] {
			continue
		}
		nonIM++
		if _, failed := oc.failed[id]; !failed {
			return false
		}
	}
	return nonIM > 0
}

// nonIMCount is the number of ids in group that are not IMs — the
// base the majority check and wholesaleFailure both reason over,
// since IMs always land in failed_ids and would otherwise inflate
// both.
func nonIMCount(group map[string]int64, noteIM map[string]bool) int {
	n := 0
	for id := range group {
		if !noteIM[id] {
			n++
		}
	}
	return n
}

// revalidateChannelTeam runs the channels/info call and its
// write-through for one owning team. Team failures are independent:
// one team's error is logged and its ids are left stale, and the
// remaining teams still run — the same independence the channels and
// users passes have from each other, one level down.
func revalidateChannelTeam(ctx context.Context, deps Deps, team string, updated map[string]int64, logf func(string, ...any)) teamOutcome {
	res, err := deps.Revalidate.ChannelsInfo(ctx, team, updated)
	if err != nil {
		// Everything below is discarded along with it. Slack answers
		// ok:false with a populated body, so "err != nil and the
		// value looks fine" is the normal shape of a failure here.
		logf("bootstrap: channels/info for team %s: %d conversations: %v (leaving them stale)", team, len(updated), err)
		return teamOutcome{callErr: true}
	}

	for _, ch := range res.Channels {
		if err := deps.Store.UpdateChannelFromEdge(cache.EdgeChannelUpdate{
			ID:      ch.ID,
			Name:    ch.Name,
			Type:    channelType(ch),
			Topic:   ch.Topic.Value,
			Version: ch.Version,
		}); err != nil {
			// One bad row must not cost the rest of the batch: the
			// call is already spent, and abandoning the remaining
			// updates would leave versions unadvanced for channels the
			// server did answer about.
			logf("bootstrap: caching revalidated channel %s: %v", ch.ID, err)
		}
	}

	// Membership, and the queried set is res.MembershipQueried — the
	// ids sent in batches whose response actually carried
	// member_channels — never res.MemberChannels.
	//
	// The difference is silent data loss. member_channels is a
	// snapshot over the ids ASKED about: one absent from it is a
	// genuine non-member, so passing the returned members as the
	// queried set would tell ApplyMembership that every id it was not
	// handed is a non-member, and clear is_member for the whole rest
	// of the batch.
	//
	// Empty means no batch reported, which is the COMMON case —
	// member_channels is absent from 13 of the 18 observed responses, all
	// of which requested it — and it means "no information", so
	// nothing is written and no call is made. Note res.FailedIDs
	// accumulates across all batches while MembershipQueried covers
	// only the reporting ones; that is harmless, since ApplyMembership
	// touches nothing outside the queried set.
	//
	// Applying per team is safe because the queried set is exactly
	// this team's ids: membership answers never cross the partition.
	if len(res.MembershipQueried) > 0 {
		if err := deps.Store.ApplyMembership(deps.WorkspaceID, res.MembershipQueried,
			cache.MembershipReported(res.MemberChannels, res.FailedIDs)); err != nil {
			logf("bootstrap: applying membership for %d conversations: %v", len(res.MembershipQueried), err)
		}
	}

	// Failed ids are left exactly as they were, versions included.
	//
	// This is a correctness hazard rather than a lost nicety. Absence
	// from res.Channels otherwise means "unchanged, still fresh", so
	// stamping a failed id as current would keep its stale record
	// forever — its version never advances, so it never comes back.
	// Leaving the version where it is means the next boot asks again.
	//
	// The team is named: on Grid this line is the whole diagnosis.
	if len(res.FailedIDs) > 0 {
		logf("bootstrap: channels/info for team %s could not resolve %d ids (%v); leaving them stale to be retried", team, len(res.FailedIDs), res.FailedIDs)
	}

	failed := make(map[string]struct{}, len(res.FailedIDs))
	for _, id := range res.FailedIDs {
		failed[id] = struct{}{}
	}
	return teamOutcome{failed: failed}
}

// revalidateUsers conditionally refreshes the people the opened
// channel actually references — the authors conversations.view
// returned — plus the counterparties of the open DMs. Nothing else at
// boot; users met later are resolved on demand.
func revalidateUsers(ctx context.Context, deps Deps, out *Result, logf func(string, ...any)) {
	ids := make([]string, 0, len(out.Users)+len(out.IMs))
	for _, u := range out.Users {
		ids = append(ids, u.ID)
	}
	for _, im := range out.IMs {
		// UserID, tagged `json:"user"`, is the DM's counterparty —
		// the person on the other side. im.ID is the conversation, and
		// belongs to the channel pass above.
		ids = append(ids, im.UserID)
	}

	cached, err := deps.Store.UserVersions(deps.WorkspaceID)
	if err != nil {
		logf("bootstrap: reading cached user versions: %v (revalidating everything from scratch)", err)
		cached = nil
	}

	updated := conditionalVersions(ids, cached)
	if len(updated) == 0 {
		return
	}

	users, err := deps.Revalidate.UsersInfo(ctx, updated)
	if err != nil {
		logf("bootstrap: users/info for %d users: %v (leaving them stale)", len(updated), err)
		return
	}

	for _, u := range users {
		if err := deps.Store.UpdateUserFromEdge(cache.EdgeUserUpdate{
			ID:          u.ID,
			Name:        u.Name,
			DisplayName: userDisplayName(u),
			// ImageOriginal is present on 255 of 291 observed
			// results; the 36 without it are users with no custom
			// image, and UpdateUserFromEdge reads an empty value as
			// "preserve the stored avatar" for exactly that reason.
			AvatarURL:  u.Profile.ImageOriginal,
			IsBot:      u.IsBot,
			IsExternal: isExternal(u, deps.WorkspaceID),
			Version:    u.Version,
		}); err != nil {
			logf("bootstrap: caching revalidated user %s: %v", u.ID, err)
		}
	}
}

// conditionalVersions builds the {id: version} map a cache endpoint
// takes, pairing each id with the version the cache holds.
//
// An id the cache has never seen is sent with 0 rather than omitted.
// That is how the protocol asks for a full record — the captures show
// the client doing exactly that, {"C6M7U8DFF":0} — and omitting it
// would leave the row permanently unhydrated, since a record that is
// never asked for is never returned.
//
// Empty ids are dropped. They cannot identify anything, and an
// updated_ids map containing "" is a request shape nothing observed
// produces.
func conditionalVersions(ids []string, cached map[string]int64) map[string]int64 {
	updated := make(map[string]int64, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		// A map lookup, not a comma-ok: absent is 0, which is the
		// value this endpoint wants for an unknown id anyway.
		updated[id] = cached[id]
	}
	return updated
}

// channelType maps an edge result onto the cache's `type` column,
// whose values are "channel", "private", "dm" and "group_dm".
//
// The order is load-bearing and mirrors cmd/slk's own classification
// in buildChannelItem: an MPDM is private too, and a DM is neither, so
// testing is_private first would file every group DM under "private".
//
// cmd/slk has a fifth value, "app", for a DM whose counterparty is a
// bot. It is not reachable from here — a channels/info result says
// nothing about who is on the other end — so a bot DM revalidates to
// "dm". That is recoverable rather than lost: connectWorkspace
// re-derives "app" from the cached users' is_bot on every boot
// (main.go:1941), so the column is corrected before it is rendered.
func channelType(ch edge.Channel) string {
	switch {
	case ch.IsIM:
		return "dm"
	case ch.IsMPIM:
		return "group_dm"
	case ch.IsPrivate:
		return "private"
	default:
		return "channel"
	}
}

// userDisplayName picks the name to show, mirroring the fallback chain
// resolveUser already uses (main.go:2432): display name, then real
// name, then the handle.
//
// The fallback matters more here than there, because
// UpdateUserFromEdge writes display_name unconditionally — a user with
// no display_name set would otherwise blank whatever the cache had.
func userDisplayName(u edge.User) string {
	if u.Profile.DisplayName != "" {
		return u.Profile.DisplayName
	}
	if u.Profile.RealName != "" {
		return u.Profile.RealName
	}
	return u.Name
}

// isExternal reports whether a user's home team differs from this
// workspace's — a Slack Connect or shared-channel guest. Same test
// resolveUser applies (main.go:2440), including the empty guard: a
// result with no team_id is unknown, not foreign.
func isExternal(u edge.User, workspaceID string) bool {
	return u.TeamID != "" && u.TeamID != workspaceID
}
