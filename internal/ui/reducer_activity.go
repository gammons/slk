// internal/ui/reducer_activity.go
//
// Activity-view reducer for App.Update. Modeled on reducer_threads.go's
// ThreadsView arms — the Activity view mirrors the Threads view. Six arms:
//
//	ActivityViewActivatedMsg  - user opened the Activity view (sidebar
//	                            row or :activity): switch view + focus,
//	                            mark the sidebar Activity row active,
//	                            and kick a feed fetch.
//	ActivityListLoadedMsg     - feed fetch returned: push items into
//	                            the view, refresh the sidebar unread
//	                            badge, and kick
//	                            a second fetch to hydrate message bodies
//	                            (activity.feed returns refs only).
//	ActivityBodiesLoadedMsg   - messages.list hydration returned: install
//	                            the bodies (team-guarded) via SetBodies so
//	                            rows show what the activity actually was.
//	ActivityToggleUnreadMsg   - flip the unread-only filter and re-fetch
//	                            with the new flag (the server filters).
//	ActivityReadSetMsg        - Slack answered a read-state change: send
//	                            the newest state if the user changed it
//	                            meanwhile; on error, undo to the state
//	                            Slack holds and toast.
//	ActivitySelectedMsg       - user pressed Enter on a row: open the
//	                            underlying thread (ThreadTS set) or the
//	                            channel message (jump to TS), reusing
//	                            the existing thread-open / permalink-jump
//	                            helpers. An item with no channel is
//	                            marked read and explained with a toast.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
)

// activityRefs groups a page of activity items' (channel, ts) refs by
// channel for a single batched messages.list hydration. Items with no
// channel or ts are skipped; duplicate ts within a channel are de-duped.
func activityRefs(items []core.ActivityItem) map[string][]string {
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

// activityRead tracks one Activity item's read-state requests to Slack.
// At most one is in flight per item: two concurrent requests could reach
// Slack in either order and leave it holding the older state.
type activityRead struct {
	team      ids.TeamID // the workspace the item belongs to
	item      core.ActivityItem
	want      bool // the read state the user asked for last
	confirmed bool // the read state Slack holds, as far as we know
	inFlight  bool
}

// activityReadKey scopes an Activity item key to its workspace.
func activityReadKey(teamID, itemKey string) string {
	return teamID + "\x00" + itemKey
}

// setActivityItemRead applies an Activity item's new read state at once
// (its dot and the sidebar badge) and returns the command that asks
// Slack for it, or nil when a request for the item is already in flight:
// the ActivityReadSetMsg arm sends the newest state when that returns,
// and undoes the change if Slack refuses.
func (a *App) setActivityItemRead(it core.ActivityItem, read bool) tea.Cmd {
	if a.activeTeamID == "" {
		return nil
	}
	a.activityView.SetItemRead(it.Key, read)
	a.sidebar.SetActivityUnreadCount(a.activityView.UnreadCount())

	k := activityReadKey(a.activeTeamID, it.Key)
	r := a.activityReads[k]
	if r == nil {
		if a.activityReads == nil {
			a.activityReads = map[string]*activityRead{}
		}
		r = &activityRead{team: ids.TeamID(a.activeTeamID), item: it, confirmed: !it.IsUnread}
		a.activityReads[k] = r
	}
	r.want = read
	if r.inFlight {
		return nil
	}
	return a.sendActivityRead(r)
}

// sendActivityRead starts r's request for its wanted state, in r's own
// workspace whichever one is on screen.
func (a *App) sendActivityRead(r *activityRead) tea.Cmd {
	r.inFlight = true
	activity := a.activity
	team, it, read := r.team, r.item, r.want
	return func() tea.Msg { return activity.SetRead(team, it, read) }
}

// activateUnopenableActivity is Enter on an item with no conversation (a
// Slack reminder, or a type slk does not know): there is nothing to
// open, so acknowledge it by marking it read, and say why nothing
// opened.
func (a *App) activateUnopenableActivity(it core.ActivityItem) tea.Cmd {
	text := "slk can't open this activity item yet"
	if it.Type == "saved_reminder" {
		text = "Reminder: open Slack to see it"
	}
	toast := toastWithClear(a, text, 3*time.Second)
	if !it.IsUnread {
		return toast
	}
	return tea.Batch(a.setActivityItemRead(it, true), toast)
}

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
			return activity.Fetch(team, activityFeedPageLimit, unreadOnly)
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
			return activity.Fetch(team, activityFeedPageLimit, unreadOnly)
		}, true

	case ActivityListLoadedMsg:
		if m.TeamID != a.activeTeamID {
			return nil, true
		}
		a.activityView.SetItems(m.Items)
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

	case ActivityReadSetMsg:
		k := activityReadKey(m.TeamID, m.Key)
		r := a.activityReads[k]
		if r == nil {
			return nil, true
		}
		r.inFlight = false
		if m.Err == nil {
			r.confirmed = m.Read
		}
		if r.want == r.confirmed {
			delete(a.activityReads, k)
			return nil, true
		}
		if m.Err == nil {
			// The user changed it again while this was in flight. Send
			// it even if another workspace is on screen now.
			return a.sendActivityRead(r), true
		}
		delete(a.activityReads, k)
		// Another workspace's view is not on screen, so there is no dot
		// to undo; its feed reloads from Slack when the user returns.
		if m.TeamID != a.activeTeamID {
			return nil, true
		}
		a.activityView.SetItemRead(m.Key, r.confirmed)
		a.sidebar.SetActivityUnreadCount(a.activityView.UnreadCount())
		state := "unread"
		if m.Read {
			state = "read"
		}
		return toastWithClear(a, "Couldn't mark activity item "+state+": "+m.Err.Error(), 3*time.Second), true

	case ActivitySelectedMsg:
		// A switch landed between the selection and its delivery: the
		// item belongs to the workspace no longer on screen.
		if m.TeamID != a.activeTeamID {
			return nil, true
		}
		it := m.Item
		if it.ChannelID == "" {
			return a.activateUnopenableActivity(it), true
		}
		// Thread target: open the thread panel directly, reusing the
		// permalink helper (builds the parent from the pane buffer or
		// thread cache, else a stub the replies-loaded handler
		// backfills). The Activity view stays visible behind the
		// thread panel, mirroring the Threads view.
		if it.ThreadTS != "" {
			return a.openThreadForPermalink(it.ChannelID, it.ThreadTS, it.TS != "" && it.TS != it.ThreadTS), true
		}
		// Channel message target: reuse the in-app permalink-jump
		// machinery. Set the pending nav so the ChannelSelectedMsg /
		// MessagesLoadedMsg arms select (or FetchAround) the target ts,
		// then switch to the channel.
		if it.TS != "" {
			a.pendingLinkNav = &pendingLinkNav{Location: Location{
				TeamID:    ids.TeamID(a.activeTeamID),
				ChannelID: ids.ChannelID(it.ChannelID),
				MessageTS: ids.MessageTS(it.TS),
			}}
		}
		name, chType, _ := a.channels.Lookup(ids.ChannelID(it.ChannelID))
		// Already viewing the channel: the loaded buffer is as good as
		// it gets, so complete the jump authoritatively right now.
		if it.ChannelID == a.activeChannelID && a.pendingLinkNav != nil {
			return a.completePendingLinkNav(a.activeChannelID, true), true
		}
		id := it.ChannelID
		return func() tea.Msg {
			return ChannelSelectedMsg{ID: id, Name: name, Type: chType}
		}, true
	}
	return nil, false
}
