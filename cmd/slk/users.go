package main

import (
	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/avatar"
	"github.com/gammons/slk/internal/cache"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/ui"
	"github.com/slack-go/slack"
)

// lookupUserCached returns the display name for userID using only
// local sources: the in-memory userNames map and the cached users
// table. Never hits the network and NEVER writes to userNames — safe
// to call from goroutines off the UI loop (e.g. the search cmd),
// where a map write would race the UI goroutine (see the
// concurrent-map-writes note on userResolver.Request). Returns
// ("", false) when the user is unknown.
func lookupUserCached(userID string, userNames map[string]string, db *cache.DB) (string, bool) {
	if userID == "" {
		return "", false
	}
	if name, ok := userNames[userID]; ok && name != "" {
		return name, true
	}
	if db != nil {
		if u, err := db.GetUser(userID); err == nil {
			name := u.DisplayName
			if name == "" {
				name = u.Name
			}
			if name != "" {
				return name, true
			}
		}
	}
	return "", false
}

// resolveUserCached is lookupUserCached plus map memoization: a DB hit
// is written back to userNames so subsequent lookups skip SQLite.
// UI-goroutine callers only — the map write is what lookupUserCached
// exists to avoid. Returns ("", false) when the user is unknown —
// caller is expected to fall back to userID-as-name and enqueue an
// async lookup via wctx.UserResolver.Request.
func resolveUserCached(userID string, userNames map[string]string, db *cache.DB) (string, bool) {
	name, ok := lookupUserCached(userID, userNames, db)
	if ok {
		userNames[userID] = name
	}
	return name, ok
}

// resolveUser ensures we have the display name and avatar for a user.
// If the user is unknown, fetches their profile from Slack on demand.
// Returns the resolved display name (or the userID as a fallback) and a
// boolean indicating whether the user is a Slack app or bot. The bool
// is best-effort: if the user was already in the userNames cache and
// the avatar lookup hasn't fired, we don't have a fresh IsBot signal
// and return false. Callers that care (the unresolved-DM goroutine)
// only invoke resolveUser for users not yet in the cache, so the
// fast-path miss is irrelevant for them.
func resolveUser(client *slackclient.Client, userID string, userNames map[string]string, db *cache.DB, avatarCache *avatar.Cache, send func(tea.Msg)) (string, bool) {
	if name, ok := userNames[userID]; ok {
		// Check if avatar is also cached
		if avatarCache.Get(userID) == "" {
			// Have name but no avatar — try to fetch profile for avatar URL
			if u, err := client.GetUserProfile(userID); err == nil {
				isBot := u.IsBot || u.IsAppUser
				isExternal := u.TeamID != "" && u.TeamID != client.TeamID()
				avatarCache.Preload(userID, u.Profile.Image32)
				db.UpsertUser(cache.User{
					ID:          userID,
					WorkspaceID: client.TeamID(),
					Name:        u.Name,
					DisplayName: name,
					AvatarURL:   u.Profile.Image32,
					Presence:    "away",
					IsBot:       isBot,
					IsExternal:  isExternal,
				})
				// UpsertUser doesn't touch status on an existing row.
				applyProfileStatus(client.TeamID(), userID, u.Profile, db, send)
				return name, isBot
			}
		}
		return name, false
	}
	// Unknown user — fetch profile
	if u, err := client.GetUserProfile(userID); err == nil {
		name := u.Profile.DisplayName
		if name == "" {
			name = u.RealName
		}
		if name == "" {
			name = u.Name
		}
		isBot := u.IsBot || u.IsAppUser
		isExternal := u.TeamID != "" && u.TeamID != client.TeamID()
		userNames[userID] = name
		avatarCache.Preload(userID, u.Profile.Image32)
		db.UpsertUser(cache.User{
			ID:          userID,
			WorkspaceID: client.TeamID(),
			Name:        u.Name,
			DisplayName: name,
			AvatarURL:   u.Profile.Image32,
			Presence:    "away",
			IsBot:       isBot,
			IsExternal:  isExternal,
		})
		applyProfileStatus(client.TeamID(), userID, u.Profile, db, send)
		return name, isBot
	}
	return userID, false
}

// resolveDMNames resolves the display names of unresolved DM
// counterparties, one edge users/info batch for the whole sweep, with
// the per-user resolveUser loop as the fallback for ids edge did not
// return. Batched because the sweep is the dominant cold-boot
// users.info source: one synchronous GetUserProfile per unresolved DM,
// measured at ~100 calls on a two-workspace cold boot and 282 in a
// full Grid session. The mapping to channel ids is why this cannot go
// through UserResolver.Request: DMNameResolvedMsg renames the sidebar
// row and re-buckets app DMs, while UserResolvedMsg only patches
// in-history names.
func resolveDMNames(wctx *WorkspaceContext, db *cache.DB, avatarCache *avatar.Cache, send func(tea.Msg)) {
	dmIDs := make([]string, 0, len(wctx.UnresolvedDMs))
	for _, dm := range wctx.UnresolvedDMs {
		dmIDs = append(dmIDs, dm.UserID)
	}
	byEdge := make(map[string]edge.User)
	for _, u := range wctx.UserResolver.ResolveNow(dmIDs) {
		byEdge[u.ID] = u
	}
	for _, dm := range wctx.UnresolvedDMs {
		if u, ok := byEdge[dm.UserID]; ok {
			name := u.Profile.DisplayName
			if name == "" {
				name = u.Profile.RealName
			}
			if name == "" {
				name = u.Name
			}
			if name != "" {
				// edge users/info carries no is_app_user: a Slack app's DM
				// resolved here may bucket as "dm" rather than "app" until
				// something else classifies it. No capture shows that
				// field on this endpoint, so none is invented; the
				// per-user fallback below classifies the ids edge missed.
				if u.IsBot {
					wctx.BotUserIDs[dm.UserID] = true
				}
				if send != nil {
					send(ui.DMNameResolvedMsg{
						ChannelID:   dm.ChannelID,
						DisplayName: name,
						IsBot:       u.IsBot,
					})
				}
				continue
			}
			// An edge record with all three name fields empty is no
			// resolution at all — and applyEdgeUser has already
			// upserted its empty-DisplayName row, which satisfies
			// Request's cache-skip gate. Fall through to the per-user
			// path, which re-fetches and repairs the row.
		}
		resolved, isBot := resolveUser(wctx.Client, dm.UserID, wctx.UserNames, db, avatarCache, send)
		if isBot {
			wctx.BotUserIDs[dm.UserID] = true
		}
		if resolved != dm.UserID && send != nil {
			send(ui.DMNameResolvedMsg{
				ChannelID:   dm.ChannelID,
				DisplayName: resolved,
				IsBot:       isBot,
			})
		}
	}
}

// messageAuthor resolves the display identity for a fetched message.
// Human messages carry a `user` ID, resolved (and lazily fetched) the
// usual way. Bot messages (bot_message) have an empty `user` and only a
// `bot_id` + `username`; those are keyed on the bot_id, use the message's
// username for the name, and enqueue a bots.info lookup for the avatar
// (and a name fallback). The returned userID is what both the cache row
// and the MessageItem are keyed on so the avatar pipeline can attach.
func messageAuthor(m slack.Message, userNames map[string]string, db *cache.DB, router *workspaceRouter) (userID, userName string) {
	if m.User != "" {
		name, ok := resolveUserCached(m.User, userNames, db)
		if !ok {
			name = m.User
			if router != nil {
				if wctx := router.Active(); wctx != nil && wctx.UserResolver != nil {
					wctx.UserResolver.Request(m.User)
				}
			}
		}
		return m.User, name
	}
	if m.BotID != "" {
		name := m.Username
		if name == "" {
			if cached, ok := resolveUserCached(m.BotID, userNames, db); ok {
				name = cached
			} else {
				name = m.BotID
			}
		}
		if router != nil {
			if wctx := router.Active(); wctx != nil && wctx.UserResolver != nil {
				wctx.UserResolver.RequestBot(m.BotID, m.Username)
			}
		}
		return m.BotID, name
	}
	return m.User, m.User
}
