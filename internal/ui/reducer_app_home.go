// internal/ui/reducer_app_home.go
//
// Reducer for the app DM Home tab: installing a fetched view and
// handling button-press results. The fetching, toggling and prompting
// live in app_home.go.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
)

// appHomeRefetchDelay is how long after a press the second Home fetch
// waits. blocks.actions answers as soon as the press is dispatched; an
// app that re-publishes its view after doing the work (a Refresh that
// queries billing, say) can finish after the immediate fetch, so one
// bounded follow-up catches the re-published view. Tests shorten it.
var appHomeRefetchDelay = 1500 * time.Millisecond

// appHomeRefetchMsg asks for another Home fetch, after a press.
type appHomeRefetchMsg struct {
	teamID    string
	channelID string
}

var reduceAppHome reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case AppHomeLoadedMsg:
		// A result for a conversation the user has left, or for a
		// workspace no longer on screen, must not replace the tab in
		// front of them.
		if m.TeamID != a.activeTeamID || m.ChannelID != a.activeChannelID || m.ChannelID != a.appHomeChannelID {
			return nil, true
		}
		if m.Home == nil {
			if m.Err != nil {
				// A failed fetch says nothing about whether the app
				// has a Home tab. Keep what is on screen: a transient
				// error after a press must not wipe a working tab, and
				// a failed initial fetch leaves the messages as they
				// were.
				a.appHomeAutoShow = false
				return nil, true
			}
			a.clearAppHome()
			return nil, true
		}
		a.appHome.SetHome(m.Home)
		// Show the Home tab the first time it arrives, but never take
		// the user out of what they are doing: not while typing, and
		// not once they have switched to Messages themselves (a
		// refetch after a press leaves AutoShow false).
		show := a.appHomeAutoShow
		a.appHomeAutoShow = false
		if show && a.view == ViewChannels && a.mode == ModeNormal {
			// Reaction navigation belongs to the messages pane that
			// the tab replaces: leaving it live would let its handler
			// consume the tab's keys and toggle reactions on a message
			// nobody can see.
			a.messagepane.ExitReactionNav()
			a.view = ViewAppHome
		}
		return nil, true

	case appHomeActionPressedMsg:
		if m.teamID != a.activeTeamID || m.channelID != a.activeChannelID {
			return nil, true
		}
		if m.err != nil {
			return toastWithClear(a, "Could not press "+m.label+": "+m.err.Error(), 4*time.Second), true
		}
		// Slack re-publishes the view after a press, so fetch it again
		// to draw what the app rendered. The result lands as
		// AppHomeLoadedMsg; a delayed follow-up covers an app that
		// publishes only after it finished handling the press.
		svc := a.appHomeSvc
		team, channel := m.teamID, m.channelID
		return tea.Batch(
			func() tea.Msg { return svc.Fetch(team, ids.ChannelID(channel)) },
			tea.Tick(appHomeRefetchDelay, func(time.Time) tea.Msg {
				return appHomeRefetchMsg{teamID: team, channelID: channel}
			}),
		), true

	case appHomeRefetchMsg:
		if m.teamID != a.activeTeamID || m.channelID != a.activeChannelID {
			return nil, true
		}
		svc := a.appHomeSvc
		return func() tea.Msg { return svc.Fetch(m.teamID, ids.ChannelID(m.channelID)) }, true
	}
	return nil, false
}
