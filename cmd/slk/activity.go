package main

import (
	"context"
	"log"
	"time"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui"
)

// activityFetchFunc builds the ActivityService.Fetch closure: one page
// of the workspace's activity.feed.
func activityFetchFunc(router *workspaceRouter) core.ActivityFetchFunc {
	return func(teamID ids.TeamID, limit int, unreadOnly bool) core.Msg {
		teamIDStr := string(teamID)
		wctx := router.Active()
		if wctx == nil {
			return nil
		}
		fetchCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		result, err := wctx.Client.GetActivityFeed(fetchCtx, limit, "", unreadOnly)
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
			Items:  result.Items,
		}
	}
}

// activityHydrateFunc builds the ActivityService.Hydrate closure: the
// messages.list call that fills in bodies for a page of activity refs.
func activityHydrateFunc(router *workspaceRouter) core.ActivityHydrateFunc {
	return func(teamID ids.TeamID, refs map[string][]string) core.Msg {
		teamIDStr := string(teamID)
		wctx := router.Active()
		if wctx == nil {
			return nil
		}
		fetchCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		bodies, err := wctx.Client.GetActivityMessages(fetchCtx, refs)
		if err != nil {
			log.Printf("Warning: GetActivityMessages(%s): %v", teamIDStr, err)
			// nil = keep whatever bodies we already have.
			return nil
		}
		return ui.ActivityBodiesLoadedMsg{TeamID: teamIDStr, Bodies: bodies}
	}
}
