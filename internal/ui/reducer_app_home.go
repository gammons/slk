// internal/ui/reducer_app_home.go
//
// Reducer for the app DM Home tab: installing a fetched view and
// handling button-press results. The fetching, toggling and prompting
// live in app_home.go.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// appHomeSpinnerInterval paces the status-bar dots while a press's
// refetches are in flight.
const appHomeSpinnerInterval = 300 * time.Millisecond

// appHomeSpinnerMsg advances the dots.
type appHomeSpinnerMsg struct{}

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
	seq       uint64
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
		// One of a press's refetches landed (or failed); the indicator
		// ends when the last of that press's has.
		a.finishAppHomeFetch(m.Seq)
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
			// This press never counted its refetches; leave an earlier
			// press's dots alone. The error toast replaces them, and the
			// next tick stops the chain without clearing the newer toast.
			return toastWithClear(a, "Could not press "+m.label+": "+m.err.Error(), 4*time.Second), true
		}
		// Slack re-publishes the view after a press, so fetch it again
		// to draw what the app rendered. The result lands as
		// AppHomeLoadedMsg; a delayed follow-up covers an app that
		// publishes only after it finished handling the press.
		svc := a.appHomeSvc
		team, channel := m.teamID, m.channelID
		spinner, seq := a.startAppHomeBusy()
		return tea.Batch(
			spinner,
			appHomeFetchCmd(svc, team, channel, seq),
			tea.Tick(appHomeRefetchDelay, func(time.Time) tea.Msg {
				return appHomeRefetchMsg{teamID: team, channelID: channel, seq: seq}
			}),
		), true

	case appHomeRefetchMsg:
		if m.teamID != a.activeTeamID || m.channelID != a.activeChannelID {
			return nil, true
		}
		svc := a.appHomeSvc
		return appHomeFetchCmd(svc, m.teamID, m.channelID, m.seq), true

	case appHomeSpinnerMsg:
		if a.appHomeBusy.pending == 0 {
			a.appHomeBusy.tickArmed = false
			return nil, true
		}
		if a.statusbar.ToastSeq() != a.appHomeBusy.toastSeq {
			// A newer message replaced the dots; do not overwrite or
			// clear it, and end this chain.
			a.appHomeBusy.pending = 0
			a.appHomeBusy.tickArmed = false
			a.appHomeBusy.toastSeq = 0
			return nil, true
		}
		a.appHomeBusy.dots = a.appHomeBusy.dots%3 + 1
		toastUntilReplaced(a, appHomeBusyText(a.appHomeBusy.dots))
		a.appHomeBusy.toastSeq = a.statusbar.ToastSeq()
		return tea.Tick(appHomeSpinnerInterval, func(time.Time) tea.Msg {
			return appHomeSpinnerMsg{}
		}), true
	}
	return nil, false
}
