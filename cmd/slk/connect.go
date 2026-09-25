package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/avatar"
	"github.com/gammons/slk/internal/bootstrap"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/service"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/slack/membership"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/statusbar"
)

// shouldReloadTimeout bounds the background _x_version_ts refresh.
// Matches slackclient.MintToken's 15s — the only other bounded Slack
// HTTP call in slk — rather than inventing a second number for the
// same job. Nothing waits on this refresh, so a generous bound costs
// nothing, while a tight one would turn a merely slow proxy into a
// lost refresh and a stale build timestamp.
const shouldReloadTimeout = 15 * time.Second

func connectWorkspace(ctx context.Context, token slackclient.Token, db *cache.DB, cfg config.Config, avatarCache *avatar.Cache, p *tea.Program, configPath string) (*WorkspaceContext, error) {
	client := slackclient.NewClient(token.AccessToken, token.Cookie)

	// Seed the build timestamp from the last run so the very first
	// request of this session already carries a current _x_version_ts
	// instead of the compiled-in fallback.
	seedVersionTS(client.Envelope(), cfg, token.TeamID)

	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connecting %s: %w", token.TeamName, err)
	}

	// Refresh the build timestamp in the background. Failure is
	// non-fatal: the seeded or compiled-in value stays in use.
	go func() {
		// The API client sets no http.Client.Timeout, and
		// http.DefaultTransport bounds only the dial and the TLS
		// handshake — not the response headers or body. A server that
		// accepts the connection and then never answers (captive
		// portal, wedged corporate proxy — see #111) would otherwise
		// pin this goroutine and its connection for the whole life of
		// the process, because ctx here is the app root context.
		rctx, cancel := context.WithTimeout(ctx, shouldReloadTimeout)
		defer cancel()
		ts, err := client.ShouldReload(rctx)
		if err != nil {
			debuglog.General("shouldReload: %v", err)
			return
		}
		if env := client.Envelope(); env != nil {
			env.SetVersionTS(ts)
		}
		tomlKey := workspaceTOMLKey(cfg, client.TeamID())
		if err := saveWorkspaceVersionTS(configPath, tomlKey, client.TeamID(), token.TeamName, ts); err != nil {
			debuglog.General("saving version_ts: %v", err)
		}
	}()

	wctx := &WorkspaceContext{
		Client: client,
		// Same construction as newBootstrapDeps makes for
		// revalidation, and for the same reason it must be
		// client.HTTPClient(): edge.New needs the
		// BrowserTransport-carrying client, and a plain one differs
		// only in what goes on the wire.
		Edge:                 edge.New(token.AccessToken, client.TeamID(), client.HTTPClient()),
		EdgeHealth:           edge.NewHealth(),
		TeamID:               client.TeamID(),
		TeamName:             token.TeamName,
		UserID:               client.UserID(),
		UserNames:            make(map[string]string),
		AvatarURLs:           &sync.Map{},
		UserNamesByHandle:    make(map[string]string),
		BotUserIDs:           make(map[string]bool),
		LastVisitedByChannel: make(map[string]int64),
		ThreadSubsGate:       threadSubsGate{window: threadSubsSyncInterval},
	}
	wctx.SubscriptionsAvailable = true

	// Seed user names + bot flags from cache (fast, local). The bot
	// flag is what lets channel construction below classify app DMs
	// into "app" vs "dm" without waiting for the network fetch.
	cachedUsers, _ := db.ListUsers(client.TeamID())
	for _, u := range cachedUsers {
		name := u.DisplayName
		if name == "" {
			name = u.Name
		}
		wctx.UserNames[u.ID] = name
		if u.Name != "" {
			wctx.UserNamesByHandle[u.Name] = name
		}
		if u.IsBot {
			wctx.BotUserIDs[u.ID] = true
		}
		// Record the avatar URL for lazy fetch on first render.
		//
		// We intentionally do NOT bulk-Preload every cached user here.
		// A typical Slack workspace has tens of thousands of cached
		// users, virtually none of whom are visible on first paint. The
		// old eager-Preload spawned one goroutine per cached user, and
		// for each one rendered into a kitty graphics APC upload that
		// was synchronously written to os.Stdout. On kitty the terminal
		// applies flow control while decoding the upload PNGs, which
		// blocked the bubbletea View() goroutine's stdout writes and
		// presented as a multi-minute startup hang with idle CPU. The
		// lazy AvatarFunc path (see SetAvatarFunc) triggers a single
		// Preload per userID on first render demand, deduped by
		// avatar.Cache's inflight set.
		if u.AvatarURL != "" {
			wctx.AvatarURLs.Store(u.ID, u.AvatarURL)
		}
	}

	// Construct the per-workspace async user resolver. It writes
	// resolved display names to the cache DB and emits
	// UserResolvedMsg back into the bubbletea program; the UI's
	// Update handler patches the in-memory userNames map on the
	// UI goroutine via Model.PatchUserName (the single safe writer
	// for that shared map). p may be nil in tests, in which case
	// the resolver's send callback is a no-op.
	wctx.UserResolver = newUserResolver(
		wctx.TeamID,
		wctx.Client,
		db,
		avatarCache,
		func(msg tea.Msg) {
			if p != nil {
				p.Send(msg)
			}
		},
		wctx.Edge, wctx.EdgeHealth.Degraded,
	)

	// Refetches other users' custom status and DND on the socket's
	// ID-only user_invalidated / dnd_invalidated events. known reads the
	// cache on the WS goroutine, the same read Request makes there.
	wctx.PeerStatus = newPeerStatusRefresher(
		wctx.TeamID,
		wctx.UserID,
		func(userID string) bool {
			_, err := db.GetUser(userID)
			return err == nil
		},
		wctx.UserResolver.ResolveNow,
		wctx.Client.GetDNDTeamInfo,
		wctx.Client.GetUserProfile,
		db,
		func(msg tea.Msg) {
			if p != nil {
				p.Send(msg)
			}
		},
	)

	// Per-workspace channel-membership manager. *slackclient.Client
	// structurally satisfies membership.ConversationMemberAPI; the
	// user resolver satisfies membership.UserResolver. The push
	// callback funnels member-set snapshots into the App via
	// ui.ChannelMembershipMsg for picker hydration.
	wctx.Membership = membership.New(
		wctx.TeamID,
		wctx.Client,
		db,
		func(channelID string, memberIDs []string) {
			if p != nil {
				p.Send(ui.ChannelMembershipMsg{ChannelID: channelID, MemberIDs: memberIDs})
			}
		},
		wctx.UserResolver,
	)

	// Seed last-visited timestamps for the channel finder's recency
	// sort. Best-effort: failure is logged and the map stays empty,
	// which means the finder uses its default order until the user
	// starts visiting channels.
	if visits, err := db.GetChannelVisits(client.TeamID()); err != nil {
		log.Printf("warning: loading channel visits for %s: %v", token.TeamName, err)
	} else {
		wctx.LastVisitedByChannel = visits
	}

	// The boot sequence: client.userBoot, client.counts,
	// conversations.view for the restored channel (falling back to
	// conversations.history), and conditional revalidation of the
	// cache against edgeapi. See internal/bootstrap.
	//
	// This runs AFTER the channel-visits load because
	// mostRecentlyVisitedChannel reads wctx.LastVisitedByChannel,
	// which that load fills. It is the same expression the UI is
	// handed as WorkspaceReadyMsg.LastChannelID, so the channel
	// bootstrap opens is the channel the sidebar restores rather than
	// a second, differently-chosen one. Empty is legal and means "open
	// nothing" — a fresh profile with no recorded visits.
	//
	// The old enumeration paths below (GetChannels, GetUnreadCounts,
	// and the reconnect backfill) still run. That is
	// deliberate for this commit: they are deleted one at a time in
	// the tasks that follow, each next to the call that replaces it,
	// so no intermediate commit leaves slk unable to boot. Until then
	// slk does both, and the request tally goes UP.
	res, err := bootstrap.Run(ctx, newBootstrapDeps(client, db, token.AccessToken,
		mostRecentlyVisitedChannel(wctx.LastVisitedByChannel), wctx.EdgeHealth))
	if err != nil {
		return nil, fmt.Errorf("bootstrapping %s: %w", token.TeamName, err)
	}
	// Order matters between these two: applyBootUsers fills
	// wctx.BotUserIDs, which buildChannelItem reads to bucket app DMs,
	// and hydrateFirstSight writes the cache rows the sidebar's
	// channel list is later reconciled against.
	applyBootUsers(wctx, res)
	// conversations.view returns the custom emoji THAT CONVERSATION
	// USES next to the history it was asked for — not the workspace's
	// set. An earlier version of this code read it as the latter and
	// skipped emoji.list whenever it was non-empty, which left every
	// channel other than the restored one rendering its custom emoji as
	// literal `:name:`.
	//
	// So it is published as a head start, not an answer: the first
	// channel's emoji resolve without waiting on a round trip, and the
	// emoji.list fetch in run() replaces this with the full set as soon
	// as it lands. Empty on the conversations.history fallback and when
	// no channel was opened; emoji.list covers those the same way.
	if len(res.Emojis) > 0 {
		wctx.SetCustomEmoji(res.Emojis)
	}
	hydrateFirstSight(db, client.TeamID(), res)

	// Initialize Slack-native section store if enabled. Bootstrap is
	// best-effort: failure is logged, the field stays nil, and the
	// resolver falls through to config-glob behavior. Doing this
	// before GetChannels means the first pass through buildChannelItem
	// already sees a Ready store.
	if cfg.EffectiveUseSlackSections(client.TeamID()) {
		store := service.NewSectionStore()
		if err := store.Bootstrap(ctx, client); err != nil {
			log.Printf("section store bootstrap for %s failed: %v (falling back to config sections)", token.TeamName, err)
		} else {
			// Bootstrap repopulates the stars section from stars.list
			// itself (channelSections.list returns built-in section
			// types with empty channel_ids), so the Starred header is
			// live at first render and survives reconnect-triggered
			// re-bootstraps without caller help.
			wctx.SectionStore = store
			// One-time info log when the user has both Slack sections
			// active AND a non-empty [sections.*] config — the latter
			// is being shadowed.
			hasGlobSections := len(cfg.Sections) > 0
			if ws, ok := cfg.WorkspaceByTeamID(client.TeamID()); ok && len(ws.Sections) > 0 {
				hasGlobSections = true
			}
			if hasGlobSections {
				log.Printf("workspace %s: using Slack-native sections; [sections.*] from config are shadowed (set use_slack_sections=false to disable)", token.TeamName)
			}
		}
	}

	// Initialize the mute store. Best-effort: failure is logged and the
	// field stays nil; the sidebar then renders every channel as
	// unmuted (the conservative default). pref_change WS events for
	// muted_channels can still rebuild the store mid-session via
	// MuteStore.ApplyPrefChange even if this initial fetch failed.
	//
	// The source is client.userBoot's prefs, not a users.prefs.get
	// round trip: userBoot already returned all_notifications_prefs
	// (and muted_channels on the workspaces that still ship it), so
	// the second call asked the same server the same question. See
	// bootMutedChannels, which merges the two exactly as
	// slackclient.GetMutedChannels does.
	{
		store := service.NewMuteStore()
		if err := store.Bootstrap(ctx, bootMutedChannels{res}); err != nil {
			log.Printf("mute store bootstrap for %s failed: %v (channels will render as unmuted until first pref_change)", token.TeamName, err)
		} else {
			ids := store.MutedChannels()
			log.Printf("mute store bootstrap for %s: %d muted channel(s) loaded: %v", token.TeamName, len(ids), ids)
		}
		// Assign even if not Ready — the pref_change handler can fill
		// it in later, and IsMuted is a safe no-op while not ready.
		wctx.MuteStore = store
	}

	// Thread subscriptions are deliberately NOT fetched here. The
	// trigger is the WorkspaceReadyMsg the reducer handles after
	// connectWorkspace returns: ensureThreadSubscriptions runs the
	// sync throttled (one sweep per threadSubsSyncInterval) and
	// staggered per workspace, so a Grid boot doesn't fire every
	// workspace's ~62-request paginated sweep in the same second.
	// See that function for the full rationale.

	// There is deliberately no workspace-wide user fetch here.
	//
	// A users.list sweep used to run in the background at this point,
	// paginating the entire directory — ~50 pages on a 10k-user
	// workspace — to fill UserNames, UserNamesByHandle, BotUserIDs and
	// the users cache. The official web client issues users.list zero
	// times across all 8 captures, and it is the clearest single
	// "scraping" signal slk emitted. Four sources cover the same
	// ground without it:
	//
	//   - the cache seed above (db.ListUsers), which holds everyone
	//     slk has ever resolved on this workspace;
	//   - applyBootUsers, from conversations.view's users array — the
	//     authors of the messages about to be rendered;
	//   - edge.UsersInfo revalidation inside bootstrap.Run, which
	//     refreshes those records by version;
	//   - resolveUser, which fetches a single users.info on a miss and
	//     writes it to the cache, so each unknown user costs one call
	//     once rather than the whole directory every boot.
	//
	// The visible difference is that a name slk has never seen renders
	// as its user ID for the moment before resolveUser answers, rather
	// than for the (much longer) moment before the sweep finished.

	// The sidebar comes from users.conversations, with client.userBoot
	// as a fallback -- in that order, and the order was measured.
	//
	// userBoot cannot be the primary source. On a real 218-channel
	// workspace its channels[] carried 67 of them; on another, 60 of
	// 71. It is evidently not the complete joined-conversation list,
	// and preferring it silently drops channels from the sidebar,
	// which is a worse failure than the one this fixes because nobody
	// notices a channel that is merely absent.
	//
	// But users.conversations must not be FATAL either. On the
	// Enterprise Grid org in gammons/slk#5 it is rejected outright, and
	// treating that as fatal dropped the entire workspace: no
	// channels, no threads, no active workspace, and -- until the
	// commit before this one -- no logged reason. userBoot had already
	// returned all 217 of that user's conversations, so falling back to
	// a partial list beats losing the session.
	channels, err := client.GetChannels(ctx)
	if err != nil {
		log.Printf("workspace %s: users.conversations failed (%v); falling back to the conversations client.userBoot returned, which may be a subset", token.TeamName, err)
		debuglog.General("workspace %s: users.conversations failed: %v", token.TeamName, err)
		channels = bootConversations(res)
	}
	if len(channels) == 0 {
		log.Printf("workspace %s: no conversations from either users.conversations or client.userBoot; the sidebar will be empty", token.TeamName)
	}

	for _, ch := range channels {
		item, finderItem := buildChannelItem(ch, wctx, cfg, client.TeamID())
		upsertChannelInDB(db, ch, item.Type, client.TeamID())

		if ch.IsIM {
			if _, ok := wctx.UserNames[ch.User]; !ok {
				wctx.UnresolvedDMs = append(wctx.UnresolvedDMs, UnresolvedDM{
					ChannelID: ch.ID,
					UserID:    ch.User,
				})
			}
			seedDMFromCache(db, ch.User, &item, &finderItem)
		}
		wctx.Channels = append(wctx.Channels, item)
		finderItem.LastVisited = wctx.LastVisitedByChannel[ch.ID]
		wctx.FinderItems = append(wctx.FinderItems, finderItem)
	}

	// Unread counts come from the boot response rather than a second
	// client.counts call. bootstrap.Run has already made exactly this
	// request; asking again asked the same server the same question,
	// and it did so once per workspace.
	//
	// res.CountsOK carries what the error return used to: a FAILED
	// call and a workspace with nothing unread both produce an empty
	// slice, and only the second may be applied as a snapshot.
	unreadCounts, ucOK := res.Counts.Unreads, res.CountsOK
	if !ucOK {
		debuglog.Cache("workspace_unread_bootstrap: team=%s client.counts failed during bootstrap; leaving read state as cached", token.TeamName)
	}
	wctx.ThreadsHasUnreads = res.Counts.Threads.HasUnreads
	// Boot applies an authoritative FULL snapshot: reset every channel
	// in the workspace to read, then set the ones client.counts reports
	// unread. This runs BEFORE the WebSocket goes live (ConnMgr.Run is
	// started by the caller after connectWorkspace returns), so the
	// reset cannot race an inbound *_marked event.
	//
	// Guard on ucErr only (not len>0): a successful call returning zero
	// unreads legitimately means "everything is read" and must clear
	// stale dots carried over from a prior session. A FAILED call must
	// NOT reset — that would wipe every dot with no data to restore.
	if ucOK {
		updates := make([]cache.ChannelReadStateUpdate, 0, len(unreadCounts))
		for _, u := range unreadCounts {
			updates = append(updates, cache.ChannelReadStateUpdate{
				ChannelID:  u.ChannelID,
				LastReadTS: u.LastRead, // may be ""; ReplaceWorkspaceReadState preserves existing in that case
				HasUnread:  u.HasUnread,
				// Boot is the authoritative snapshot: channels absent
				// from client.counts get mention_count reset to 0 by
				// ReplaceWorkspaceReadState's workspace-wide reset.
				MentionCount: u.MentionCount,
			})
		}
		if err := db.ReplaceWorkspaceReadState(client.TeamID(), updates); err != nil {
			log.Printf("Warning: bootstrap ReplaceWorkspaceReadState for team=%s: %v", token.TeamName, err)
		}
	}
	mutedItemCount := 0
	for _, c := range wctx.Channels {
		if c.IsMuted {
			mutedItemCount++
		}
	}
	log.Printf("workspace %s: %d/%d channel items marked IsMuted after build", token.TeamName, mutedItemCount, len(wctx.Channels))

	// Bootstrap-time mute summary so a user can grep
	// `[cache] workspace_unread_bootstrap` after launch and see how
	// many channels are muted in this workspace. The per-channel
	// unread detail log that used to live here was driven by
	// ChannelItem.UnreadCount, which no longer exists -- unread state
	// is now sourced exclusively from the read-state DB (see
	// db.GetChannelReadState) and any equivalent dump would live
	// alongside the DB write path in updateReadStateFromCounts.
	if debuglog.Enabled() {
		var mutedChans int
		for _, ch := range wctx.Channels {
			if ch.IsMuted {
				mutedChans++
			}
		}
		debuglog.Cache("workspace_unread_bootstrap: team=%s total=%d muted=%d threads_has_unreads=%v threads_unread=%d",
			token.TeamName, len(wctx.Channels), mutedChans, res.Counts.Threads.HasUnreads, res.Counts.Threads.UnreadCount)
	}

	// Finder items are built alongside the sidebar items in the loop above
	// (see buildChannelItem). The user is a member of every channel returned
	// by GetChannels (it's backed by users.conversations), so those entries
	// have Joined=true. Channels the user has NOT joined are found on
	// demand by the finder's debounced channels/search -- see
	// searchChannelsRemote -- rather than enumerated up front.

	return wctx, nil
}

func (h *rtmEventHandler) OnConnect() {
	// connected doubles as "has this handler ever connected". It is
	// never cleared on disconnect, deliberately: what the catch-up
	// below needs to know is whether bootstrap.Run has already covered
	// this session, not whether the socket is up right now.
	firstConnect := !h.connected
	h.connected = true
	h.program.Send(ui.ConnectionStateMsg{State: int(statusbar.StateConnected)})
	if h.wsCtx != nil {
		go bootstrapPresenceAndDND(context.Background(), h.wsCtx, h.program)
	}
	// Refresh Slack-native section state on reconnect. MaybeRebootstrap
	// is debounced to once per 30s (Task 6) so a rapid flap doesn't
	// thunder; a real long-disconnect-then-reconnect refreshes section
	// state we may have missed during the gap.
	//
	// Run synchronously on the WS read goroutine. This briefly blocks
	// inbound event delivery during the bootstrap HTTP call, but that
	// cost is bounded — at most one call per 30s per workspace — and
	// avoids racing wsCtx.Channels mutations against the same loop's
	// next event (which could be an OnConversationOpened that also
	// touches wsCtx.Channels).
	if h.wsCtx != nil && h.wsCtx.SectionStore != nil && h.wsCtx.Client != nil {
		if err := h.wsCtx.SectionStore.MaybeRebootstrap(context.Background(), h.wsCtx.Client); err != nil {
			log.Printf("section store rebootstrap for %s failed: %v", h.wsCtx.TeamName, err)
		} else {
			h.refreshSectionsForActive()
		}
	}

	// Bounded reconnect catch-up: client.counts, the channel on
	// screen, and a staleness mark on everything else. The 30 s dedupe
	// in backfillGate prevents disconnect flaps from spawning
	// overlapping passes. Runs in its own goroutine so the WS read
	// loop isn't blocked on HTTP work.
	//
	// Skipped on the first connect, which fires moments after
	// connectWorkspace returns. bootstrap.Run has just done the same
	// work — client.counts, and the restored channel's history — so
	// running it again asked the same server the same questions, once
	// per workspace, and marked every other channel stale seconds
	// after boot had populated them.
	if firstConnect {
		debuglog.Backfill("team=%s first connect: skipping catch-up, bootstrap.Run already covered it", h.workspaceID)
	} else {
		h.syncOnReconnect("reconnect")
	}

	// Force-stale the active channel's membership cache and re-fetch.
	// The WS may have missed member_joined/left deltas during the
	// disconnect window; a fresh full fetch reconciles divergence.
	// Inactive channels stay as-is — they'll re-fetch on their next
	// EnsureFresh via the channel-switch fetcher path.
	h.refreshActiveMembership()
}

// refreshActiveMembership force-stales and re-fetches membership for
// the channel on screen. Gated on isActive because activeChannelID
// reads the GLOBAL UI active channel (app.ActiveChannelID), and every
// workspace's handler runs OnConnect: without the gate, workspaces
// that don't own the on-screen channel fetched it anyway, failed with
// channel_not_found, and — because a failed fetch leaves the cache
// stale — re-fired on every reconnect. Measured live: a flapping
// session started 42 conversations.members in 25 seconds with no user
// interaction.
func (h *rtmEventHandler) refreshActiveMembership() {
	if h.wsCtx == nil || h.wsCtx.Membership == nil || h.activeChannelID == nil {
		return
	}
	if h.isActive != nil && !h.isActive() {
		return
	}
	activeID := h.activeChannelID()
	if activeID == "" {
		return
	}
	h.wsCtx.Membership.ForceStale(activeID)
	h.wsCtx.Membership.EnsureFresh(context.Background(), activeID)
}

// syncOnReconnect kicks off the bounded catch-up pass for this
// workspace, subject to the per-handler 30 s dedupe gate. Called by
// OnConnect on every WS reconnect AND by the wake detector when the
// system wakes from sleep (where the WS may not have torn down — a
// short sleep can survive within the 60 s WS read deadline, so
// OnConnect never fires and no catch-up would happen without an
// explicit trigger).
//
// The dedupe gate is shared with OnConnect, so a wake event that
// coincides with a real WS reconnect runs the pass exactly once.
//
// Returns true if the pass was started, false if the gate suppressed
// it.
func (h *rtmEventHandler) syncOnReconnect(trigger string) bool {
	if h.wsCtx == nil || h.db == nil || h.wsCtx.Client == nil {
		return false
	}
	// Threads: same "socket replays nothing" gap as the channel pass
	// below. Fired BEFORE the dedupe check and unconditionally: the
	// threadSubsGate inside throttles to one sweep per
	// threadSubsSyncInterval, so the kick costs nothing when the last
	// sync was recent, and a long offline gap must not have its
	// reconciliation skipped just because a channel pass ran 10 s ago.
	if h.ensureThreadSubs != nil {
		h.ensureThreadSubs()
	}
	if !h.backfillGate.tryStart(time.Now()) {
		debuglog.Backfill("team=%s trigger=%s skipped reason=dedupe", h.workspaceID, trigger)
		return false
	}
	sync := &reconnectSync{
		client:         h.wsCtx.Client,
		db:             h.db,
		workspaceID:    h.workspaceID,
		program:        h.program,
		activeChannel:  h.activeChannelID,
		refreshChannel: h.refreshChannel,
	}
	go func() {
		if err := sync.run(context.Background()); err != nil {
			debuglog.Backfill("team=%s trigger=%s reconnect-sync err=%v", sync.workspaceID, trigger, err)
		}
	}()
	return true
}

func (h *rtmEventHandler) OnDisconnect() {
	h.program.Send(ui.ConnectionStateMsg{State: int(statusbar.StateDisconnected)})
}
