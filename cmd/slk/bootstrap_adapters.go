package main

// bootstrap_adapters.go wires slk's concrete clients onto
// internal/bootstrap's interfaces.
//
// It is a separate file from main.go for a reason that outlives the
// task that created it: internal/bootstrap deliberately does not import
// internal/slack (see that package's comment on import direction), so
// every mismatch between the two shows up here as a small named type.
// Buried in main.go those types would read as noise; gathered here they
// read as the list of translations the import rule costs, and each one
// can carry the reason it is not a pass-through.
//
// Everything in this file is deliberately trivial. Where an adapter
// does anything at all beyond forwarding — boundedMessageVersions and
// rawMessages — that is where the decisions are, and both are written
// as free functions taking narrow interfaces so a test can observe
// them without a live Slack connection or a live workspace.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gammons/slk/internal/bootstrap"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/debuglog"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/boot"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/slack-go/slack"
)

// newBootstrapDeps assembles the dependency set bootstrap.Run takes.
//
// It exists as a function rather than a struct literal inline in
// connectWorkspace so that TestBootstrapDeps_PopulatesEveryDependency
// can assert every field is populated. That test is not decoration.
// bootstrap.Run errors on a nil Boot, Counts, View, History or Store,
// but a nil Revalidate or Store is LOG-AND-SKIP inside revalidate() —
// so forgetting Revalidate in this literal produces a workspace that
// boots normally, emits one debug line, and silently never revalidates
// anything. That is the entire conditional-revalidation phase turned
// off by an omission no compiler, no linter and no runtime check sees.
//
// accessToken is the xoxc credential; it is passed separately because
// edge.New takes the token directly rather than going through
// *slackclient.Client, which does not expose it.
func newBootstrapDeps(c *slackclient.Client, db *cache.DB, accessToken, openChannelID string, health *edge.Health) bootstrap.Deps {
	return bootstrap.Deps{
		WorkspaceID: c.TeamID(),
		Boot:        bootAdapter{c},
		Counts:      countsAdapter{c},
		View:        viewAdapter{c},
		History:     historyAdapter{c},
		// c.HTTPClient(), never a fresh http.Client: edge.New's doc
		// requires the BrowserTransport-carrying client, and the plain
		// one differs only in what goes on the wire. See
		// (*slackclient.Client).HTTPClient.
		Revalidate:    edge.New(accessToken, c.TeamID(), c.HTTPClient()),
		Health:        health,
		Store:         storeAdapter{db},
		OpenChannelID: openChannelID,
		Log:           debuglog.General,
	}
}

// bootAdapter satisfies bootstrap.UserBooter.
//
// A pass-through, not a translation layer: boot.UserBoot takes a
// boot.PostFunc, which is exactly (*slackclient.Client).PostForm's
// signature.
type bootAdapter struct{ c *slackclient.Client }

func (a bootAdapter) UserBoot(ctx context.Context) (*boot.Result, error) {
	return boot.UserBoot(ctx, a.c.PostForm)
}

// viewAdapter satisfies bootstrap.Viewer. Same pass-through as
// bootAdapter; channelID may be "", which boot.ConversationsView turns
// into an absent `channel` param rather than an empty one.
type viewAdapter struct{ c *slackclient.Client }

func (a viewAdapter) ConversationsView(ctx context.Context, channelID string) (*boot.ViewResult, error) {
	return boot.ConversationsView(ctx, a.c.PostForm, channelID)
}

// countsAdapter satisfies bootstrap.CountsFetcher.
//
// ctx is accepted and discarded, because GetUnreadCounts takes none —
// it builds its request with http.NewRequest, not
// http.NewRequestWithContext. The interface keeps the parameter so that
// giving GetUnreadCounts a ctx later is a change to one line here
// rather than a change to bootstrap's interface, and so that this
// adapter does not have to pretend cancellation works when it does not.
type countsAdapter struct{ c *slackclient.Client }

func (a countsAdapter) Counts(_ context.Context) (bootstrap.Counts, error) {
	unreads, threads, err := a.c.GetUnreadCounts()
	if err != nil {
		// Zero value, not a partially-filled one: bootstrap treats a
		// counts failure as non-fatal and logs it, and handing back
		// half a snapshot alongside the error would put "everything is
		// read" into the UI as though it had been measured.
		return bootstrap.Counts{}, err
	}
	out := bootstrap.Counts{
		Unreads: make([]bootstrap.Unread, 0, len(unreads)),
		Threads: bootstrap.Threads{
			HasUnreads:   threads.HasUnreads,
			UnreadCount:  threads.UnreadCount,
			MentionCount: threads.MentionCount,
		},
	}
	for _, u := range unreads {
		out.Unreads = append(out.Unreads, bootstrap.Unread{
			ChannelID: u.ChannelID,
			Count:     u.Count,
			HasUnread: u.HasUnread,
			LastRead:  u.LastRead,
		})
	}
	return out, nil
}

// historyAdapter satisfies bootstrap.Historian.
type historyAdapter struct{ c *slackclient.Client }

// HistoryWithVersions forwards to the client and converts the result.
//
// Only Limit is left unset, which slackclient.HistoryWithVersions reads
// as defaultHistoryLimit (28) — the page size the official client was
// observed sending on all 14 captured requests. Naming a number here
// would fork that decision away from the package that measured it.
func (a historyAdapter) HistoryWithVersions(ctx context.Context, channelID string, cached map[string]string) (bootstrap.History, error) {
	res, err := a.c.HistoryWithVersions(ctx, channelID, slackclient.HistoryOpts{
		CachedVersions: cached,
	})
	if err != nil {
		return bootstrap.History{}, err
	}
	msgs, err := rawMessages(res.Messages)
	if err != nil {
		return bootstrap.History{}, fmt.Errorf("conversations.history for %s: %w", channelID, err)
	}
	return bootstrap.History{
		Messages: msgs,
		// UnchangedTS and LatestUpdates are the whole point of the
		// call. Dropping either silently degrades an incremental sync
		// into a full refetch on the next open: without LatestUpdates
		// the caller has nothing to send back as
		// cached_latest_updates, and without UnchangedTS it cannot
		// tell "the server confirms you still hold this" from "the
		// server said nothing about it".
		UnchangedTS:   res.UnchangedTS,
		LatestUpdates: res.LatestUpdates,
		HasMore:       res.HasMore,
	}, nil
}

// rawMessages re-encodes decoded messages as raw JSON.
//
// bootstrap.History.Messages is []json.RawMessage so that a
// conversations.view result and a conversations.history result can both
// land in Result.Messages without one being converted first;
// slack.HistoryResult.Messages is []slack.Message because
// slackclient.HistoryWithVersions decodes with slack-go's type. One of
// the two has to give, and it is this direction because the view path's
// bytes are the ones that must survive untouched — that path carries 17
// distinct message shapes, only 8 of whose keys appear on every message.
//
// This IS lossy: a round trip through slack.Message drops any key
// slack-go does not model and normalises the rest. That is acceptable
// only while nothing renders Result.Messages, which is the case today —
// the fallback path's output is not yet wired to the UI. Whoever wires
// it must decide whether a re-encoded slack.Message is good enough or
// whether slackclient.HistoryWithVersions should keep the raw bodies;
// do not discover this comment afterwards.
func rawMessages(msgs []slack.Message) ([]json.RawMessage, error) {
	if len(msgs) == 0 {
		// nil, not an empty slice: "the server sent no messages" and
		// "the server sent an empty list" are the same answer here,
		// and a nil slice is what the view path produces for it.
		return nil, nil
	}
	out := make([]json.RawMessage, 0, len(msgs))
	for i, m := range msgs {
		b, err := json.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf("re-encoding message %d: %w", i, err)
		}
		out = append(out, b)
	}
	return out, nil
}

// storeAdapter satisfies bootstrap.Store.
//
// *cache.DB is embedded rather than wrapped in five forwarding methods
// because bootstrap.Store's signatures ARE cache's own, verbatim, for
// every method but one — that is stated in the interface's doc comment,
// and embedding is the most direct way to encode it. The single
// override below is the single deliberate difference, and it stands out
// as one.
//
// The shadowing is load-bearing: cache.DB.MessageVersions takes three
// arguments and this one takes one, so an outer method at depth 0 wins
// over the embedded method at depth 1. Deleting the override does not
// silently fall through to the three-argument form — it fails to
// compile, because storeAdapter then satisfies nothing.
type storeAdapter struct{ *cache.DB }

// messageVersionWindow bounds how many cached messages the adapter will
// vouch for in one cached_latest_updates map.
//
// 28 is not a round number picked for comfort: it is
// slack.defaultHistoryLimit, the page size
// slackclient.HistoryWithVersions sends and the only limit the official
// client was ever observed using (14 of 14 captured requests). The
// request asks for the newest 28 messages and sends no latest/oldest
// anchor, so the server can only answer about that window — vouching
// for the 29th-newest cached message asks a question the response has
// no room to answer, while still paying for the bytes.
//
// The bound matters in the other direction too. cached_latest_updates
// is an assertion about what slk holds, and an unbounded window puts
// every versioned message in the channel into a request body: a slow
// request, and a request whose size grows with cache age rather than
// with anything the official client varies. The captures show that
// client sending {} on 11 of 14 requests and a single entry on the
// other 3, so 28 is already an over-estimate of observed behaviour —
// it is the ceiling the protocol justifies, not a target.
const messageVersionWindow = 28

// MessageVersions narrows cache.DB.MessageVersions' three-argument form
// to bootstrap.Store's one-argument form by supplying the window.
//
// The window is the adapter's job, not the cache's: it is a property of
// the request being made. See bootstrap.Store.MessageVersions.
func (a storeAdapter) MessageVersions(channelID string) (map[string]string, error) {
	return boundedMessageVersions(a.DB, channelID)
}

// messageWindowSource is the slice of *cache.DB boundedMessageVersions
// uses. It exists so the bound can be tested: with *cache.DB inlined
// there is no way to observe which oldestTS/latestTS were passed, and
// "the adapter does not send an unbounded window" is exactly the
// property that must not regress.
type messageWindowSource interface {
	GetMessages(channelID string, limit int, beforeTS string) ([]cache.Message, error)
	MessageVersions(channelID, oldestTS, latestTS string) (map[string]string, error)
}

// boundedMessageVersions returns {ts: version} for at most
// messageVersionWindow of the channel's newest cached messages.
//
// The window is derived from the rows themselves rather than from a
// timestamp arithmetic guess: GetMessages already returns the newest N
// (ascending), so its first and last ts ARE the bounds, exactly, with
// no assumption about how densely a channel is posted in. A quiet
// channel and a firehose both get 28 entries rather than "everything in
// the last hour" being 1 in one and 4000 in the other.
//
// Note the window is a ts RANGE, so a thread reply inside it that
// GetMessages itself filters out of the main feed is still included.
// That is left alone deliberately: the entry is within the window slk
// genuinely holds, the request sets ignore_replies=true so the server
// simply says nothing about it, and excluding it would mean a second
// query for no observable difference.
func boundedMessageVersions(src messageWindowSource, channelID string) (map[string]string, error) {
	recent, err := src.GetMessages(channelID, messageVersionWindow, "")
	if err != nil {
		return nil, fmt.Errorf("reading recent messages for %s: %w", channelID, err)
	}
	if len(recent) == 0 {
		// No request-shaping decision to make, and nothing to vouch
		// for. nil becomes "{}" on the wire, which is what the
		// official client sends on 11 of its 14 captured requests.
		return nil, nil
	}
	// GetMessages returns the newest `limit` rows ordered ascending, so
	// the first is the oldest of the window and the last is the newest.
	// Reading these two off the wrong ends inverts the range and
	// returns nothing at all.
	return src.MessageVersions(channelID, recent[0].TS, recent[len(recent)-1].TS)
}

// bootMutedChannels adapts a bootstrap.Result to
// service.MutedChannelsClient, so the mute store bootstraps from
// client.userBoot's prefs rather than from a second users.prefs.get
// round trip.
//
// The parsing lives here rather than in internal/bootstrap because that
// package must not import internal/slack, which owns
// ParseMutedFromAllNotificationsPrefs — hence the raw strings on
// Result.
type bootMutedChannels struct{ res *bootstrap.Result }

// GetMutedChannels merges the two prefs the same way
// slackclient.GetMutedChannels does, and in the same order: the legacy
// flat comma-separated list first, then all_notifications_prefs, which
// is where mute state actually lives today. Both are merged rather than
// either winning — the legacy key was absent from the captured response
// (all 702 prefs keys were checked) but slk still supports workspaces
// that ship it.
//
// ctx is unused: everything needed was already fetched.
func (b bootMutedChannels) GetMutedChannels(_ context.Context) ([]string, error) {
	merged := map[string]bool{}
	for _, id := range strings.Split(b.res.LegacyMutedRaw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			merged[id] = true
		}
	}
	for _, id := range slackclient.ParseMutedFromAllNotificationsPrefs(b.res.MutePrefsRaw) {
		merged[id] = true
	}
	out := make([]string, 0, len(merged))
	for id := range merged {
		out = append(out, id)
	}
	return out, nil
}

// bootUserDisplayName picks the name to show for a user
// conversations.view returned.
//
// The chain is display name, then profile real name, then the handle —
// identical to bootstrap's own userDisplayName, deliberately, so the
// name this puts in wctx.UserNames and the name edge revalidation
// writes to the cache cannot disagree and flicker between them on a
// restart. boot.User also carries a TOP-LEVEL RealName, which is a
// different key with a possibly different value; it is not consulted,
// for the same reason.
func bootUserDisplayName(u boot.User) string {
	if u.Profile.DisplayName != "" {
		return u.Profile.DisplayName
	}
	if u.Profile.RealName != "" {
		return u.Profile.RealName
	}
	return u.Name
}

// applyBootUsers copies the users conversations.view returned into the
// in-memory maps the UI reads.
//
// These are the authors of the opened channel's messages, which is a
// small set — not a workspace directory. It is nonetheless the set that
// matters, because it is exactly the names about to be rendered. Task 8
// deletes the users.list sweep that fills these maps today; this is
// what replaces it, alongside the cache seed and on-demand resolveUser.
//
// Called on the connectWorkspace goroutine before the UI is told the
// workspace is ready, which is why these maps can be written directly:
// after that point wctx.UserNames has other readers and
// Model.PatchUserName is the only safe writer.
func applyBootUsers(wctx *WorkspaceContext, res *bootstrap.Result) {
	for _, u := range res.Users {
		name := bootUserDisplayName(u)
		wctx.UserNames[u.ID] = name
		if u.Name != "" {
			wctx.UserNamesByHandle[u.Name] = name
		}
		// The union of is_bot and is_app_user, as cache.User.IsBot
		// documents: classic bots set the first, Slack apps the
		// second, and this flag decides whether a DM lands in the
		// "Apps" sidebar section.
		if u.IsBot || u.IsAppUser {
			wctx.BotUserIDs[u.ID] = true
		}
		if u.Profile.ImageOriginal != "" {
			wctx.AvatarURLs.Store(u.ID, u.Profile.ImageOriginal)
		}
	}
}

// bootConversations converts what client.userBoot returned into the
// slack.Channel values the sidebar builder consumes.
//
// It exists because users.conversations is not reliably available. On
// the Enterprise Grid org in gammons/slk#5 that call is rejected, and
// because connectWorkspace treated the failure as fatal the whole
// workspace was dropped: no channels, no threads, and no active
// workspace at all. userBoot had already returned all 217 of that
// user's conversations moments earlier, so slk was discarding a
// session over data it already held.
//
// The mapping only has to satisfy buildChannelItem, which reads ID,
// Name, Topic, IsIM, IsMpIM, IsPrivate, IsMember and User. Everything
// else on slack.Channel is left zero deliberately: a field invented
// here would read as measured downstream.
//
// IsMember is true for every channels[] entry because userBoot returns
// the user's OWN conversation list -- there is no is_member on the
// entries and none is needed. IMs are filtered to the open ones, since
// a closed DM does not belong in the sidebar; both the per-IM is_open
// flag and userBoot's top-level is_open list are consulted, because
// either alone would silently empty the DM list if that response shape
// varies.
func bootConversations(res *bootstrap.Result) []slack.Channel {
	if res == nil {
		return nil
	}
	out := make([]slack.Channel, 0, len(res.Channels)+len(res.IMs))
	for _, ch := range res.Channels {
		if ch.IsArchived {
			continue
		}
		out = append(out, slack.Channel{
			GroupConversation: slack.GroupConversation{
				Conversation: slack.Conversation{
					ID:          ch.ID,
					IsGroup:     ch.IsGroup,
					IsPrivate:   ch.IsPrivate,
					IsMpIM:      ch.IsMPIM,
					IsShared:    ch.IsShared,
					IsOrgShared: ch.IsOrgShared,
					IsExtShared: ch.IsExtShared,
				},
				Name:    ch.Name,
				Creator: ch.Creator,
				Topic:   slack.Topic{Value: ch.Topic.Value},
				Purpose: slack.Purpose{Value: ch.Purpose.Value},
			},
			IsChannel: ch.IsChannel,
			IsGeneral: ch.IsGeneral,
			IsMember:  true,
		})
	}
	open := make(map[string]bool, len(res.IsOpen))
	for _, id := range res.IsOpen {
		open[id] = true
	}
	for _, im := range res.IMs {
		if im.IsArchived {
			continue
		}
		if !im.IsOpen && !open[im.ID] {
			continue
		}
		out = append(out, slack.Channel{
			GroupConversation: slack.GroupConversation{
				Conversation: slack.Conversation{
					ID:          im.ID,
					IsIM:        true,
					User:        im.UserID,
					IsOrgShared: im.IsOrgShared,
				},
			},
			IsMember: true,
		})
	}
	return out
}

// fillIMUsers copies counterparty user IDs from userBoot's ims[] onto
// IM conversations that lack them.
//
// users.conversations is the sidebar's primary source, but its IM
// objects are not guaranteed to carry `user` — Slack's conversation
// payload sometimes omits it, and slack-go then leaves Channel.User
// empty. buildChannelItem names a DM from that field (falling back to
// the raw user ID), and an empty User means the sidebar row is blank
// and the unresolved-DM sweep has nothing to fetch. userBoot's ims[]
// always include `user`; overlaying them is how a DM gets a name when
// GetChannels succeeded. Existing User values are left alone.
func fillIMUsers(channels []slack.Channel, ims []boot.IM) {
	if len(channels) == 0 || len(ims) == 0 {
		return
	}
	byID := make(map[string]string, len(ims))
	for _, im := range ims {
		if im.UserID != "" {
			byID[im.ID] = im.UserID
		}
	}
	for i := range channels {
		uid, ok := byID[channels[i].ID]
		if !ok {
			continue
		}
		channels[i].IsIM = true
		if channels[i].User == "" {
			channels[i].User = uid
		}
	}
}

// hydrateFirstSight inserts cache rows for the conversations and users
// this boot learned about that the cache has never seen.
//
// It is needed because bootstrap's revalidation writers —
// UpdateChannelFromEdge and UpdateUserFromEdge — are UPDATE statements
// that leave a missing row alone by design: they know only the columns
// an edge response carries, so an unknown record must go through a full
// upsert that knows all of them. Without this, a cold cache stays empty
// no matter how many times edgeapi answers.
//
// Only ABSENT rows are written, which is the difference between
// first-sight hydration and a second revalidation path. A full upsert
// over an existing row would rewrite is_member, is_starred and presence
// from values userBoot does not carry, blanking all three — precisely
// the silent data loss internal/cache/edge_sync.go exists to prevent.
//
// One known cost, and it is bytes rather than correctness: the rows
// written here get version 0, because UpsertChannel/UpsertUser do not
// write the version column and userBoot's `updated` stamp is not
// verified to share edgeapi's version space. bootstrap's revalidation
// ran BEFORE this — Run does it last, internally — so on a cold cache
// its writes found no rows and the next boot re-requests those records
// in full. The records themselves are complete either way: userBoot
// carries every column channels/info would have filled.
func hydrateFirstSight(db *cache.DB, workspaceID string, res *bootstrap.Result) {
	// The workspaces row first, and it is not optional: channels.
	// workspace_id and users.workspace_id are FOREIGN KEYs onto it,
	// with enforcement on, so every insert below fails without it.
	//
	// service.WorkspaceManager.AddWorkspace writes this row too, but
	// only AFTER connectWorkspace returns — which is why slk's very
	// first boot against a workspace has always cached no channels at
	// all (upsertChannelInDB discards its error). userBoot has just
	// returned the team record, so the parent row can be written here,
	// before anything needs it.
	if _, err := db.GetWorkspace(workspaceID); err != nil {
		if err := db.UpsertWorkspace(cache.Workspace{
			ID:   workspaceID,
			Name: res.Team.Name,
		}); err != nil {
			debuglog.General("bootstrap: hydrating workspace %s: %v", workspaceID, err)
		}
	}
	for _, ch := range res.Channels {
		if _, err := db.GetChannel(ch.ID); err == nil {
			continue
		}
		chType := "channel"
		switch {
		case ch.IsMPIM:
			chType = "group_dm"
		case ch.IsPrivate:
			chType = "private"
		}
		if err := db.UpsertChannel(cache.Channel{
			ID:          ch.ID,
			WorkspaceID: workspaceID,
			Name:        ch.Name,
			Type:        chType,
			Topic:       ch.Topic.Value,
			// userBoot's channels[] IS the membership list — it is
			// what replaced the users.conversations walk — so every
			// entry here is one the user belongs to.
			IsMember: true,
		}); err != nil {
			debuglog.General("bootstrap: hydrating channel %s: %v", ch.ID, err)
		}
	}
	for _, im := range res.IMs {
		if _, err := db.GetChannel(im.ID); err == nil {
			continue
		}
		// Type "dm", never "app": a userBoot im entry says nothing
		// about whether the counterparty is a bot. connectWorkspace
		// re-derives "app" from the cached users' is_bot on every
		// boot, so the column is corrected before it is rendered —
		// same recovery revalidate.channelType relies on.
		//
		// Name is left empty rather than guessed. The DM's rendered
		// name comes from the counterparty's user record, and writing
		// a placeholder here would be a value the UI could pick up.
		if err := db.UpsertChannel(cache.Channel{
			ID:          im.ID,
			WorkspaceID: workspaceID,
			Type:        "dm",
			IsMember:    true,
		}); err != nil {
			debuglog.General("bootstrap: hydrating DM %s: %v", im.ID, err)
		}
	}
	for _, u := range res.Users {
		if _, err := db.GetUser(u.ID); err == nil {
			continue
		}
		if err := db.UpsertUser(cache.User{
			ID:          u.ID,
			WorkspaceID: workspaceID,
			Name:        u.Name,
			DisplayName: bootUserDisplayName(u),
			AvatarURL:   u.Profile.ImageOriginal,
			// "away" is what every other first-sight write in slk
			// records: a view result carries no presence, and the
			// presence subscription corrects it on the first render.
			Presence:   "away",
			IsBot:      u.IsBot || u.IsAppUser,
			IsExternal: u.TeamID != "" && u.TeamID != workspaceID,
		}); err != nil {
			debuglog.General("bootstrap: hydrating user %s: %v", u.ID, err)
		}
	}
}
