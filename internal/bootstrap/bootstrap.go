// Package bootstrap owns the sequence of API calls slk makes when it
// connects to a workspace.
//
// It exists as a package, rather than as more of cmd/slk/main.go's
// connectWorkspace, for one reason: this sequence is what gets slk's
// Enterprise Grid users signed out for "data scraping", and inside
// connectWorkspace no test could reach it. connectWorkspace builds a
// live *slack.Client and calls Connect, so there is no seam without a
// live Slack connection. Everything here takes an interface.
//
// The call budget is the point. Across 8 captures of the official web
// client, a boot issues ~70 API requests and NEVER enumerates: zero
// users.list, zero conversations.list, zero per-channel
// conversations.history. slk previously issued roughly 400 and did all
// three. TestRun_NeverEnumerates is the regression guard.
//
// # Import direction
//
// This package must NOT import internal/slack. Phase 2b makes
// internal/slack import internal/slack/boot, and cmd/slk wires slack
// and bootstrap together; keeping the dependency pointing one way is
// what lets boot and edge stay stdlib-only parsers. The visible cost is
// that Result carries the RAW all_notifications_prefs string rather
// than a parsed mute list — the caller parses it with
// slack.ParseMutedFromAllNotificationsPrefs — and that Counts restates
// slack.UnreadInfo and slack.ThreadsAggregate rather than reusing them.
//
// internal/cache IS imported, and that is not a hole in the rule: the
// dependency runs inward, since cache imports neither bootstrap nor
// slack. What it buys is cache.MembershipSnapshot and the
// EdgeXFromEdge update structs travelling through the Store interface
// with their own semantics intact — see Store.
package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/slack/boot"
	"github.com/gammons/slk/internal/slack/edge"
)

// UserBooter fetches and parses client.userBoot.
type UserBooter interface {
	UserBoot(ctx context.Context) (*boot.Result, error)
}

// CountsFetcher fetches client.counts, slk's unread source of truth.
//
// Named CountsFetcher and not Counter, which is what the plan called
// it. slackhttp.Counter is an unrelated concrete type in this same
// phase — it tallies outbound requests by endpoint — and the two would
// sit side by side in cmd/slk's wiring, where "Counter" would name a
// request tally in one line and an unread-state fetcher in the next.
// The agent-noun form also matches UserBooter, Viewer, Historian and
// Revalidator below, none of which is named for the noun it returns.
type CountsFetcher interface {
	Counts(ctx context.Context) (Counts, error)
}

// Viewer fetches conversations.view for one channel. channelID may be
// "", reproducing the captured request, which sent no channel param
// and got back the last-viewed conversation.
type Viewer interface {
	ConversationsView(ctx context.Context, channelID string) (*boot.ViewResult, error)
}

// Historian is the verified fallback for Viewer: conversations.history
// with limit=28 and cached_latest_updates.
type Historian interface {
	HistoryWithVersions(ctx context.Context, channelID string, cached map[string]string) (History, error)
}

// Revalidator is the edgeapi conditional-revalidation pair. This is
// what replaces enumeration.
type Revalidator interface {
	// ChannelsInfo revalidates conversations against the edge cache,
	// scoped to teamID — on Enterprise Grid the owning team, which is
	// not necessarily the workspace's own. See edge.ChannelsInfo.
	ChannelsInfo(ctx context.Context, teamID string, updatedIDs map[string]int64) (edge.ChannelsInfoResult, error)
	UsersInfo(ctx context.Context, updatedIDs map[string]int64) ([]edge.User, error)
}

// Store is the cache surface bootstrap writes through. Deliberately
// the narrow revalidation writers from internal/cache/edge_sync.go,
// not the full upserts: a full upsert would blank is_member,
// is_starred, avatar_url and presence, none of which any edge response
// carries.
//
// The signatures are cache's own, verbatim, for every method except
// MessageVersions. That is deliberate. An earlier draft of this
// interface took ApplyMembership's membership as a []string and left
// the adapter to reconstruct the snapshot; cache.MembershipSnapshot
// exists precisely because encoding/json cannot distinguish an absent
// member_channels from a literal [], and those are opposite answers —
// "the server said nothing, keep what you have" versus "the server
// looked and named nobody, clear them all". A []string cannot carry
// that distinction, so the []string version pushed a heuristic into
// the adapter, which is exactly where the bug would have lived. It is
// resolved here instead, from edge.ChannelsInfoResult.MembershipQueried,
// which records the ids whose batch actually answered.
//
// *cache.DB satisfies everything here but MessageVersions, whose
// three-argument form is narrowed below.
type Store interface {
	ChannelVersions(workspaceID string) (map[string]int64, error)
	UserVersions(workspaceID string) (map[string]int64, error)
	ApplyMembership(workspaceID string, queriedIDs []string, snap cache.MembershipSnapshot) error

	// UpdateChannelFromEdge and UpdateUserFromEdge are the PARTIAL
	// writers, and taking them rather than UpsertChannel/UpsertUser is
	// the whole reason this interface is narrow. The full upserts
	// overwrite a fixed column list, and an edge result covers a
	// different subset of it: no is_member (0 of 36 observed
	// channels/info results carry it — membership arrives separately,
	// via ApplyMembership), no is_starred, no presence. Revalidating
	// through the full upserts would blank all three on every pass,
	// silently, surfacing as UI bugs long afterwards.
	UpdateChannelFromEdge(u cache.EdgeChannelUpdate) error
	UpdateUserFromEdge(u cache.EdgeUserUpdate) error

	// MessageVersions returns {ts: version} for the messages slk
	// already holds in one channel — the cached_latest_updates the
	// conversations.history fallback sends so the server can return
	// only what changed.
	//
	// ONE argument, deliberately, where cache.MessageVersions takes
	// (channelID, oldestTS, latestTS). The window is the Task 7
	// adapter's job because it is a property of the request being
	// made, not of the cache: an unbounded window puts an arbitrarily
	// large map into a request body, which is both a slow request and
	// exactly the sort of outlier Grid's anomaly detection scores.
	// Widening this to three arguments would push that choice out to
	// every caller and invite "" / "9".
	MessageVersions(channelID string) (map[string]string, error)
}

// Unread is one channel's unread state from client.counts.
//
// A restatement of slack.UnreadInfo rather than a reuse of it: see the
// package comment on import direction. The field set is identical, so
// the Task 7 adapter's conversion is mechanical.
type Unread struct {
	ChannelID string
	Count     int
	HasUnread bool
	LastRead  string
}

// Threads is client.counts' workspace-wide thread rollup — a
// restatement of slack.ThreadsAggregate.
//
// HasUnreads is the authoritative answer to "does the user have unread
// thread activity", and slk needs it because the local cache holds no
// per-thread read state and its heuristic produces false positives.
type Threads struct {
	HasUnreads   bool
	UnreadCount  int
	MentionCount int
}

// Counts is everything one client.counts call learned.
type Counts struct {
	Unreads []Unread
	Threads Threads
}

// History is what a conversations.history fallback returned — a
// restatement of slack.HistoryResult, again to keep internal/slack out
// of this package's imports.
//
// Messages is []json.RawMessage rather than []slack.Message so that a
// view result and a history result can both land in Result.Messages
// without one of them being converted first. boot.History.Messages is
// already raw for its own reasons (the shape varies: 17 distinct keys
// across 56 captured messages, only 8 on all of them), so raw is the
// type the two paths already share.
type History struct {
	// Messages are the bodies the server actually sent. With a
	// populated cached map this is only what CHANGED.
	Messages []json.RawMessage
	// UnchangedTS lists the timestamps from the request's
	// cached_latest_updates the server confirms the caller still holds.
	UnchangedTS []string
	// LatestUpdates is {ts: version} for the returned messages, to be
	// fed back as the cached map next time. The versions are opaque
	// and are only ever echoed, never parsed or compared.
	LatestUpdates map[string]string
	// HasMore reports whether the requested window was truncated.
	HasMore bool
}

// Result is everything the boot sequence learned, in the shape
// connectWorkspace consumes.
//
// The boot types are reused rather than restated — boot.Self,
// boot.Team, boot.Channel, boot.IM and boot.DND all appear verbatim.
// They are stdlib-only parse targets with no behaviour, and copying
// them here would create two shapes to keep in agreement forever.
type Result struct {
	Self boot.Self
	Team boot.Team

	// Channels are the conversations the user belongs to, DMs
	// excluded; IMs are the DMs. userBoot splits them, and so does
	// this.
	Channels []boot.Channel
	IMs      []boot.IM

	// IsOpen holds the conversation ids currently shown in the
	// sidebar, channels and DMs mixed.
	IsOpen []string

	DND boot.DND

	// ChannelsPriority is Slack's per-channel affinity score.
	ChannelsPriority map[string]float64

	// EmojiCacheTS is a 17-character cache token to be echoed back
	// verbatim. It looks numeric and is not.
	EmojiCacheTS string

	// MutePrefsRaw is the RAW all_notifications_prefs value: a
	// JSON-encoded string whose contents are JSON. It is not parsed
	// here because slack.ParseMutedFromAllNotificationsPrefs already
	// decodes exactly this and calling it would mean importing
	// internal/slack. Callers parse.
	MutePrefsRaw string

	// LegacyMutedRaw is the legacy flat comma-separated muted_channels
	// list. It was absent from the captured response — all 702 prefs
	// keys were checked — but slk's existing GetMutedChannels still
	// merges it for workspaces that do ship it, so it is carried
	// through rather than dropped.
	LegacyMutedRaw string

	// Counts is the unread state. It is the zero value when
	// client.counts failed, which on its own is not distinguishable
	// from a workspace with nothing unread — hence CountsOK.
	Counts Counts

	// CountsOK reports whether the client.counts call succeeded.
	//
	// The distinction is not cosmetic for anyone applying Counts as a
	// full snapshot. slk resets every channel in the workspace to read
	// and then marks the ones counts reported unread, so an empty
	// Unreads slice is either "everything is read", which must be
	// applied, or "we never found out", which must not be — the second
	// one silently wipes every unread dot with nothing to restore them
	// from. Callers that only read Counts.Unreads additively can
	// ignore this.
	CountsOK bool

	// OpenedChannelID is the conversation Messages belongs to. It is
	// always the channel that was ASKED for (Deps.OpenChannelID) and
	// never the id the server answered with — see openChannel: when
	// those two disagree the response is discarded, so reporting the
	// server's value would name a conversation whose messages are not
	// here.
	//
	// Empty means no channel was REQUESTED. It is set whenever one
	// was, including when loading it failed outright and Messages is
	// therefore empty — the caller should open this conversation
	// either way, since the alternative is silently reopening
	// whatever it had before.
	OpenedChannelID string

	// Messages is the opened channel's history, newest-window-first as
	// Slack sends it. Raw for the reasons boot.History.Messages
	// documents; both the conversations.view and the
	// conversations.history path land here.
	//
	// Empty alongside a non-empty OpenedChannelID means the load
	// failed on both paths: the channel exists and was asked for, and
	// slk has no scrollback for it. That is logged, not returned as an
	// error — see Run's "What is fatal".
	Messages []json.RawMessage

	// HasMore reports whether the returned window was truncated, i.e.
	// whether there is older scrollback to page to.
	HasMore bool

	// UnchangedTS and LatestUpdates are the incremental-sync
	// bookkeeping from the conversations.history fallback:
	// UnchangedTS lists the cached timestamps the server confirms are
	// still current, LatestUpdates is the {ts: version} map to send
	// back as cached_latest_updates next time.
	//
	// Both are empty on the conversations.view path, which sends no
	// cached_latest_updates and so returns no verdict on them.
	UnchangedTS   []string
	LatestUpdates map[string]string

	// Users, ViewChannels and Emojis are the sections
	// conversations.view returns ALONGSIDE the history: the message
	// authors (replacing a per-author users.info fan-out), the
	// conversations those messages mention, and the custom emoji they
	// use (replacing emoji.list).
	//
	// All three are empty on the conversations.history fallback, which
	// returns messages and nothing else. That is the real cost of the
	// fallback and the reason conversations.view is tried first: on
	// that path the caller has to resolve authors and emoji itself.
	Users        []boot.User
	ViewChannels []boot.ViewChannelEntry
	Emojis       map[string]string
}

// Deps is everything Run needs. Every field is required unless its
// comment says otherwise.
type Deps struct {
	WorkspaceID string

	Boot       UserBooter
	Counts     CountsFetcher
	View       Viewer
	History    Historian
	Revalidate Revalidator
	Store      Store

	// Health, when non-nil, is marked degraded when the largest
	// context-team group fails wholesale (every non-IM id unresolved)
	// and holds at least half of all revalidated ids; the remaining
	// groups are then aborted. Nil disables the check. See
	// revalidateChannels for the reasoning.
	Health *edge.Health

	// OpenChannelID is the conversation to open — the restored last
	// channel, or the configured default. Empty means "whatever Slack
	// considers last-viewed", which is what the capture did.
	OpenChannelID string

	// Log is optional; nil discards.
	Log func(format string, args ...any)
}

// Run performs the boot sequence and returns everything the UI needs.
//
// On any error the returned *Result is nil. That is load-bearing
// rather than tidiness, and it is the same rule boot.UserBoot follows:
// a caller handed both a Result and an error can use the Result, and a
// workspace assembled from a failed boot renders like a real one.
//
// # What is fatal
//
// Only two things: a nil required dependency, and client.userBoot.
// userBoot is fatal because every step below it is keyed by what it
// returned — there is no workspace without a self, a team and a
// conversation list, so there is nothing to degrade to.
//
// Everything else degrades and is logged:
//
//   - client.counts fails: Result.Counts stays zero, badges are
//     missing, the workspace works.
//   - opening the channel fails on BOTH conversations.view and the
//     conversations.history fallback: Result.Messages stays empty,
//     OpenedChannelID is still the channel that was asked for, the
//     workspace works. See openChannel.
//
// The channel open was fatal until 2026-08-01 and is deliberately no
// longer. The argument for fatal was that an empty Messages renders an
// empty channel, indistinguishable from a quiet one. That is true and
// it is the smaller harm: one conversation's scrollback is independent
// of every other conversation, the sidebar, unread state and the
// user's identity, and refusing to connect throws all of them away to
// avoid one ambiguous message pane. It matters most on Enterprise
// Grid, where conversations.view has never been captured at all, so an
// unknown_method there is a plausible steady state rather than an
// outage — and "this channel looks empty" is recoverable in a way
// "slk will not connect" is not.
func Run(ctx context.Context, deps Deps) (*Result, error) {
	logf := deps.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}

	// Deps is assembled field by field at a call site in
	// cmd/slk/main.go, so a forgotten dependency arrives as a nil
	// interface. Calling through it panics, which in a Bubble Tea
	// program means a stack trace over a torn-down terminal instead of
	// a message. Boot and Counts are checked because Boot and Counts
	// are what Run uses; later tasks check what they add.
	if deps.Boot == nil {
		return nil, errors.New("bootstrap: Deps.Boot is required")
	}
	if deps.Counts == nil {
		return nil, errors.New("bootstrap: Deps.Counts is required")
	}

	bootRes, err := deps.Boot.UserBoot(ctx)
	if err != nil {
		// Fatal: every step below is keyed by what this returned.
		return nil, fmt.Errorf("bootstrap: userBoot: %w", err)
	}

	out := &Result{
		Self:             bootRes.Self,
		Team:             bootRes.Team,
		Channels:         bootRes.Channels,
		IMs:              bootRes.IMs,
		IsOpen:           bootRes.IsOpen,
		DND:              bootRes.DND,
		ChannelsPriority: bootRes.ChannelsPriority,
		EmojiCacheTS:     bootRes.EmojiCacheTS,
		MutePrefsRaw:     bootRes.Prefs.AllNotificationsPrefs,
		LegacyMutedRaw:   bootRes.Prefs.MutedChannels,
	}

	// Unread state. Non-fatal: badges are cosmetic and a workspace
	// that boots without them beats one that does not boot.
	if counts, err := deps.Counts.Counts(ctx); err != nil {
		logf("bootstrap: counts: %v (continuing without unread state)", err)
	} else {
		out.Counts = counts
		out.CountsOK = true
	}

	// The first channel. Counts comes first because it is what tells
	// the UI this channel has unreads, and the history that lands
	// below is what gets rendered against that state.
	if deps.OpenChannelID != "" {
		if err := checkOpenChannelDeps(deps); err != nil {
			return nil, err
		}
		// Set from what was ASKED for, before the call, so that no
		// path can report a channel other than the one requested.
		out.OpenedChannelID = deps.OpenChannelID
		if err := openChannel(ctx, deps, out, logf); err != nil {
			// Non-fatal: both paths to the channel failed, and the
			// cost of that is one empty message pane. Failing the
			// boot instead would cost the whole workspace — see Run's
			// "What is fatal". Messages stays empty and
			// OpenedChannelID stays what was asked for, so the caller
			// opens the right conversation with no scrollback rather
			// than silently reopening a different one.
			logf("bootstrap: opening %s: %v (continuing with an empty channel)", deps.OpenChannelID, err)
		}
	}

	// Last, and it has to be last: the users this revalidates are the
	// ones conversations.view just returned, so running it any earlier
	// would scope the request to the open DMs alone and leave every
	// author in the opened channel stale. See revalidate.
	revalidate(ctx, deps, out, logf)

	return out, nil
}

// checkOpenChannelDeps rejects the nil interfaces opening a channel
// needs, for the same reason Run checks Boot and Counts: Deps is built
// field by field at a call site, and a forgotten field is a nil
// interface whose first method call panics.
//
// History and Store are required even though the conversations.view
// success path never touches them. A boot that works only while the
// UNVERIFIED primary path keeps working is the failure this whole task
// is about: the fallback has to be wired before it is needed, not
// discovered missing at the moment Slack ignores the channel param.
func checkOpenChannelDeps(deps Deps) error {
	switch {
	case deps.View == nil:
		return errors.New("bootstrap: Deps.View is required to open a channel")
	case deps.History == nil:
		return errors.New("bootstrap: Deps.History is required to open a channel")
	case deps.Store == nil:
		return errors.New("bootstrap: Deps.Store is required to open a channel")
	}
	return nil
}

// openChannel loads the first channel's history, preferring
// conversations.view and falling back to conversations.history.
//
// An error from here is NOT fatal to the boot — see Run's "What is
// fatal". It means the opened channel has no scrollback, not that the
// workspace is unusable. When it returns an error, out is left
// untouched: no half-populated Messages, no LatestUpdates vouching for
// versions slk does not hold.
//
// # The `channel` param on conversations.view
//
// Status: VERIFIED on 2026-08-01 against two live non-Grid
// workspaces. Both honoured it — Channel.ID came back equal to the
// requested id, and neither boot logged the "falling back" line. Prior
// to that observation no captured request carried a `channel` param at
// all and the client got back whatever it had last viewed, so this was
// a pure assumption.
//
// Still UNVERIFIED on Enterprise Grid, which is the environment this
// entire phase exists for and the one where conversations.view has
// never been captured under any parameters.
//
// So the probe-and-compare below STAYS, and is not downgraded to an
// assertion or removed. Two non-Grid observations are not the
// contract, the failure they would miss is silent (a 200 full of
// another conversation's messages, rendered under this channel's
// name), and since the double failure became non-fatal the fallback
// costs one extra request rather than a workspace.
//
// The fallback is conversations.history with cached_latest_updates,
// which IS fully verified (14 of 14 captured requests) — not a plain
// history fetch, which would re-download scrollback slk already holds.
// On Enterprise Grid the fallback may well be the ordinary path.
func openChannel(ctx context.Context, deps Deps, out *Result, logf func(string, ...any)) error {
	want := deps.OpenChannelID

	view, err := deps.View.ConversationsView(ctx, want)
	switch {
	case err != nil:
		logf("bootstrap: conversations.view failed (%v); falling back to conversations.history", err)
	case view == nil:
		// Not something Slack can send — boot.ConversationsView
		// returns nil only alongside an error — but it is what a
		// mis-written adapter returns, and dereferencing it takes the
		// whole TUI down.
		logf("bootstrap: conversations.view returned no result; falling back to conversations.history")
	case view.Channel.ID != want: // ViewChannel embeds boot.Channel, so .ID resolves through it
		logf("bootstrap: conversations.view ignored the channel param (asked %s, got %s); falling back to conversations.history",
			want, view.Channel.ID)
	default:
		out.Messages = view.History.Messages
		out.Users = view.Users
		out.ViewChannels = view.Channels
		out.Emojis = view.Emojis
		out.HasMore = view.History.HasMore
		return nil
	}

	cached, err := deps.Store.MessageVersions(want)
	if err != nil {
		// Not fatal: an empty map means "we vouch for nothing", which
		// is the shape the client sends when it holds nothing. The
		// value returned next to the error is discarded — vouching for
		// versions slk does not hold makes the server withhold
		// messages slk never received.
		logf("bootstrap: reading cached message versions for %s: %v", want, err)
		cached = nil
	}
	hist, err := deps.History.HistoryWithVersions(ctx, want, cached)
	if err != nil {
		return err
	}
	out.Messages = hist.Messages
	out.HasMore = hist.HasMore
	out.UnchangedTS = hist.UnchangedTS
	out.LatestUpdates = hist.LatestUpdates
	return nil
}
