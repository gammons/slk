// internal/ui/reducer_activity.go
//
// Activity-view reducer for App.Update. Modeled on reducer_threads.go's
// ThreadsView arms — the Activity view mirrors the Threads view. Five arms:
//
//	ActivityViewActivatedMsg  - user opened the Activity view (sidebar
//	                            row or :activity): switch view + focus,
//	                            mark the sidebar Activity row active,
//	                            and kick a feed fetch.
//	ActivityListLoadedMsg     - feed fetch returned: push items into
//	                            the view, store the pagination cursor,
//	                            refresh the sidebar unread badge, and kick
//	                            a second fetch to hydrate message bodies
//	                            (activity.feed returns refs only).
//	ActivityBodiesLoadedMsg   - messages.list hydration returned: install
//	                            the bodies (team-guarded) via SetBodies so
//	                            rows show what the activity actually was.
//	ActivityToggleUnreadMsg   - flip the unread-only filter and re-fetch
//	                            with the new flag (the server filters).
//	ActivitySelectedMsg       - user pressed Enter on a row: open the
//	                            underlying thread (ThreadTS set) or the
//	                            channel message (jump to TS), reusing
//	                            the existing thread-open / permalink-jump
//	                            helpers.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	slack "github.com/gammons/slk/internal/slack"
)

// activityRefs groups a page of activity items' (channel, ts) refs by
// channel for a single batched messages.list hydration. Items with no
// channel or ts are skipped; duplicate ts within a channel are de-duped.
func activityRefs(items []slack.ActivityItem) map[string][]string {
	seen := map[string]map[string]bool{}
	refs := map[string][]string{}
	for _, it := range items {
		if it.ChannelID == "" || it.TS == "" {
			continue
		}
		if seen[it.ChannelID] == nil {
			seen[it.ChannelID] = map[string]bool{}
		}
		if seen[it.ChannelID][it.TS] {
			continue
		}
		seen[it.ChannelID][it.TS] = true
		refs[it.ChannelID] = append(refs[it.ChannelID], it.TS)
	}
	return refs
}

// activityFeedPageLimit is the number of items requested per
// activity.feed page. Matches the desktop client's default page size.
const activityFeedPageLimit = 50

var reduceActivity reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case ActivityViewActivatedMsg:
		_ = m
		a.view = ViewActivity
		a.sidebar.SetActivityActive(true)
		a.focusedPanel = PanelMessages
		if a.activeTeamID == "" {
			return nil, true
		}
		// Activation is a single event -- fire the fetch immediately so
		// the feed populates without artificial delay. The unread-only
		// flag lives on the view; the server does the filtering.
		activity := a.activity
		team := ids.TeamID(a.activeTeamID)
		unreadOnly := a.activityView.UnreadOnly()
		return func() tea.Msg {
			return activity.Fetch(team, activityFeedPageLimit, "", unreadOnly)
		}, true

	case ActivityToggleUnreadMsg:
		_ = m
		// Flip the unread-only filter and re-fetch so the server
		// re-filters. The view only tracks the flag; it does no
		// client-side filtering.
		unreadOnly := a.activityView.ToggleUnreadOnly()
		if a.activeTeamID == "" {
			return nil, true
		}
		activity := a.activity
		team := ids.TeamID(a.activeTeamID)
		return func() tea.Msg {
			return activity.Fetch(team, activityFeedPageLimit, "", unreadOnly)
		}, true

	case ActivityListLoadedMsg:
		if m.TeamID != a.activeTeamID {
			return nil, true
		}
		a.activityView.SetItems(m.Items)
		a.activityNextCursor = m.NextCursor
		a.sidebar.SetActivityUnreadCount(a.activityView.UnreadCount())
		// activity.feed returns refs only. Kick a second fetch to hydrate
		// the message bodies (mention text, reply, reacted-to message, DM)
		// so rows show what the activity actually was.
		refs := activityRefs(m.Items)
		if len(refs) == 0 {
			return nil, true
		}
		activity := a.activity
		team := ids.TeamID(a.activeTeamID)
		return func() tea.Msg { return activity.Hydrate(team, refs) }, true

	case ActivityBodiesLoadedMsg:
		if m.TeamID != a.activeTeamID {
			return nil, true
		}
		a.activityView.SetBodies(m.Bodies)
		return nil, true

	case ActivitySelectedMsg:
		// Thread target: open the thread panel directly, reusing the
		// permalink helper (builds the parent from the pane buffer or
		// thread cache, else a stub the replies-loaded handler
		// backfills). The Activity view stays visible behind the
		// thread panel, mirroring the Threads view.
		if m.ThreadTS != "" {
			return a.openThreadForPermalink(m.ChannelID, m.ThreadTS), true
		}
		// Channel message target: reuse the in-app permalink-jump
		// machinery. Set the pending nav so the ChannelSelectedMsg /
		// MessagesLoadedMsg arms select (or FetchAround) the target ts,
		// then switch to the channel.
		if m.ChannelID == "" {
			return nil, true
		}
		if m.TS != "" {
			a.pendingLinkNav = &pendingLinkNav{
				channelID: m.ChannelID,
				messageTS: m.TS,
			}
		}
		name, chType, _ := a.channels.Lookup(ids.ChannelID(m.ChannelID))
		// Already viewing the channel: the loaded buffer is as good as
		// it gets, so complete the jump authoritatively right now.
		if m.ChannelID == a.activeChannelID && a.pendingLinkNav != nil {
			return a.completePendingLinkNav(a.activeChannelID, true), true
		}
		id := m.ChannelID
		return func() tea.Msg {
			return ChannelSelectedMsg{ID: id, Name: name, Type: chType}
		}, true
	}
	return nil, false
}
