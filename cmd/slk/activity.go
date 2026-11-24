package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
)

// sameUserOrgClient returns the org-level session of the same
// authenticated user on wctx's grid, or nil. The same-user rule is the
// boundary that keeps browser credentials for a second account on the
// grid (which connect as sibling sessions too) from standing in for
// this user's org session. Nil also when wctx is not a grid member, no
// org-level sibling is connected, or wctx has no client.
func sameUserOrgClient(router *workspaceRouter, wctx *WorkspaceContext) *slackclient.Client {
	if wctx.Client == nil || !wctx.Client.IsGridMember() {
		return nil
	}
	ent, user := wctx.Client.EnterpriseID(), wctx.Client.UserID()
	for _, sibling := range router.All() {
		if sibling.Client == nil || sibling.Client.IsGridMember() {
			continue
		}
		if sibling.Client.EnterpriseID() == ent && sibling.Client.UserID() == user {
			return sibling.Client
		}
	}
	return nil
}

// orgOrOwnClient returns the client an org-served call should run on
// for wctx's workspace: the same-user org session when one is connected,
// else the workspace's own. A member-workspace token is the one session
// Slack rejects org-wide endpoints for (team_is_restricted: activity,
// blocks.actions, the app-home conversations.info). Falling back to the
// workspace's client keeps the failure visible as it is today, rather
// than silently doing nothing.
func orgOrOwnClient(router *workspaceRouter, wctx *WorkspaceContext) *slackclient.Client {
	if org := sameUserOrgClient(router, wctx); org != nil {
		return org
	}
	return wctx.Client
}

// activityFetchFunc builds the ActivityService.Fetch closure: the
// workspace's activity.feed, paged until the limit is met.
//
// Both Activity closures resolve the workspace by the requested teamID,
// not router.Active(): a workspace switch can land between dispatch and
// execution, and the result is labelled with teamID.
func activityFetchFunc(router *workspaceRouter) core.ActivityFetchFunc {
	return func(teamID ids.TeamID, limit int, unreadOnly bool) core.Msg {
		teamIDStr := string(teamID)
		wctx := router.ByID(teamIDStr)
		if wctx == nil {
			return nil
		}
		fetchCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		client := orgOrOwnClient(router, wctx)
		// The feed came off the org session, so it is org-wide: it
		// holds items whose conversations live in other grid
		// workspaces, which this workspace cannot open (its channel
		// store and its token are both workspace-scoped). Keep the
		// items this workspace can act on.
		var filter func([]core.ActivityItem) []core.ActivityItem
		if client != wctx.Client {
			filter = func(items []core.ActivityItem) []core.ActivityItem {
				return filterActivityItemsToWorkspace(items, wctx)
			}
		}
		fetchPage := func(cursor string) (slackclient.ActivityFeedResult, error) {
			return client.GetActivityFeed(fetchCtx, limit, cursor, unreadOnly)
		}
		items, err := collectActivityItems(limit, fetchPage, filter)
		if err != nil {
			log.Printf("Warning: GetActivityFeed(%s): %v", teamIDStr, err)
			// Return nil (no message) on failure so the existing
			// feed + badge are kept rather than blanked. Mirrors
			// the "nil = keep cache" pattern used for message
			// loads; a transient network error must not wipe the
			// Activity view.
			return nil
		}
		return ui.ActivityListLoadedMsg{
			TeamID: teamIDStr,
			Items:  items,
		}
	}
}

// collectActivityItems pages the feed until limit items survive the
// optional per-page filter (a member workspace's org-wide feed is
// filtered down to its own conversations, so one page may hold fewer
// than the limit - or none at all, when the newest items all belong to
// sibling workspaces) or the feed ends. Any page failure is the
// caller's error, so the feed already on screen stays: the loop only
// pages while short of the limit, so what was collected when a later
// page fails is a partial list - possibly empty, when every earlier
// page held only sibling items - and returning it as a success would
// replace the loaded feed with less than a full page.
func collectActivityItems(limit int, fetchPage func(cursor string) (slackclient.ActivityFeedResult, error), filter func([]core.ActivityItem) []core.ActivityItem) ([]core.ActivityItem, error) {
	var items []core.ActivityItem
	cursor := ""
	for {
		result, err := fetchPage(cursor)
		if err != nil {
			return nil, err
		}
		pageItems := result.Items
		if filter != nil {
			pageItems = filter(pageItems)
		}
		items = append(items, pageItems...)
		if len(items) >= limit || result.NextCursor == "" {
			return items, nil
		}
		cursor = result.NextCursor
	}
}

// filterActivityItemsToWorkspace keeps the activity items whose
// conversation belongs to wctx (or that have none - a Slack reminder,
// which nothing opens anyway), dropping the ones pointing at another
// workspace's channel. Membership comes from wctx's race-safe
// conversation ID set, not its Channels slice (written on the connect
// goroutine and the WebSocket event owner, while this runs on the
// goroutine behind the fetch command) and not the cache (whose channel
// rows are owned by whichever session cached the channel first, so a
// shared channel the org session cached would be dropped here even
// though this workspace can open it). A workspace whose conversations
// are not known yet (a fetch racing the first boot) keeps every item:
// the alternative - an empty view on a slow boot - is the bug the
// org-session fallback exists to fix.
//
// unjoined_channel_mention items are kept regardless: their channel is
// by definition not in the sidebar (the user was mentioned without
// being a member), so membership cannot tell this workspace's from a
// sibling's, and the fetch has no cheap ownership oracle for a channel
// it holds no row for. Fail-open: a mention that turns out to belong
// to another workspace costs a failed open with a toast, while hiding
// a real mention costs the mention.
func filterActivityItemsToWorkspace(items []core.ActivityItem, wctx *WorkspaceContext) []core.ActivityItem {
	if len(items) == 0 || !wctx.HasAnyConversation() {
		return items
	}
	out := make([]core.ActivityItem, 0, len(items))
	for _, it := range items {
		if it.ChannelID == "" || it.Type == "unjoined_channel_mention" || wctx.HasConversation(it.ChannelID) {
			out = append(out, it)
		}
	}
	return out
}

// activityHydrateFunc builds the ActivityService.Hydrate closure: the
// messages.list call that fills in bodies for a page of activity refs.
func activityHydrateFunc(router *workspaceRouter) core.ActivityHydrateFunc {
	return func(teamID ids.TeamID, refs map[string][]string) core.Msg {
		teamIDStr := string(teamID)
		wctx := router.ByID(teamIDStr)
		if wctx == nil {
			return nil
		}
		fetchCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		bodies, err := orgOrOwnClient(router, wctx).GetActivityMessages(fetchCtx, refs)
		if err != nil {
			log.Printf("Warning: GetActivityMessages(%s): %v", teamIDStr, err)
			// nil = keep whatever bodies we already have.
			return nil
		}
		return ui.ActivityBodiesLoadedMsg{TeamID: teamIDStr, Bodies: bodies}
	}
}

// activitySetReadFunc builds the ActivityService.SetRead closure:
// activity.markRead / activity.markUnread for one item. It always
// returns an ActivityReadSetMsg, so the UI can undo its optimistic
// change when the call fails.
func activitySetReadFunc(router *workspaceRouter) core.ActivitySetReadFunc {
	return func(teamID ids.TeamID, item core.ActivityItem, read bool) core.Msg {
		teamIDStr := string(teamID)
		msg := ui.ActivityReadSetMsg{TeamID: teamIDStr, Key: item.Key, Read: read}
		wctx := router.ByID(teamIDStr)
		if wctx == nil {
			msg.Err = fmt.Errorf("workspace %s is not connected", teamIDStr)
			return msg
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := orgOrOwnClient(router, wctx).SetActivityRead(ctx, item.Type, item.Key, item.FeedTS, read); err != nil {
			log.Printf("Warning: SetActivityRead(%s, %s, read=%v): %v", teamIDStr, item.Key, read, err)
			msg.Err = err
		}
		return msg
	}
}
