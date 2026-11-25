// internal/ui/reducer_app_view.go
//
// Reducer for app modals: installing a view Slack sent, fetching one
// Slack only announced, and handling a pressed button's result. The
// showing, chooser and press plumbing live in app_view.go.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
)

// appViewRefreshDelay is how long after a press slk asks for the
// modal's content. Slack usually publishes the result itself over the
// WebSocket; one bounded fetch covers an app that only answers through
// views.get. Tests shorten it.
var appViewRefreshDelay = 1200 * time.Millisecond

var reduceAppView reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case AppViewOpenedMsg:
		if m.TeamID != a.activeTeamID {
			return nil, true
		}
		a.openAppView(m.View)
		return nil, true

	case AppViewUpdatedMsg:
		if m.TeamID != a.activeTeamID || m.View == nil || a.appView.Current() == nil {
			// An update never brings back a dialog the user has
			// dismissed.
			return nil, true
		}
		a.appView.SetView(m.View)
		a.appViewGen++
		if a.appViewBehind {
			// The update takes the dialog back from the button
			// chooser, whose actions belong to the old content.
			a.closeAppViewPrompt()
			a.SetMode(ModeAppView)
		}
		return nil, true

	case AppViewNeedsRefreshMsg:
		if m.ViewID == "" || m.TeamID != a.activeTeamID || !a.appViewShows(m.ViewID) {
			return nil, true
		}
		return a.fetchAppViewCmd(m.TeamID, m.ViewID), true

	case AppViewLoadedMsg:
		if m.TeamID != a.activeTeamID {
			return nil, true
		}
		if m.Gen != 0 && m.Gen != a.appViewGen {
			// A newer inline frame arrived while this fetch was in
			// flight; its content is older than what is on screen.
			return nil, true
		}
		if m.Fetch != 0 && m.Fetch < a.appViewFetchDone {
			// A fetch started later has already landed with at least as
			// new a view.
			return nil, true
		}
		if !a.appViewShows(m.ViewID) {
			return nil, true
		}
		if m.Err != nil {
			// Keep what is on screen: a transient failure must not
			// close the dialog the user is reading.
			return toastWithClear(a, "Could not refresh dialog: "+m.Err.Error(), 4*time.Second), true
		}
		if m.View == nil {
			// Slack no longer has the view; there is nothing to show.
			a.closeAppView()
			return nil, true
		}
		// A fetched result does not start a new generation: two fetches
		// in flight may both carry useful content, and ordering them is
		// what Fetch is for.
		a.appViewFetchDone = max(a.appViewFetchDone, m.Fetch)
		a.appView.SetView(m.View)
		if a.appViewBehind {
			// The chooser holds the old actions; close it before its
			// buttons can press something the app has changed.
			a.closeAppViewPrompt()
			a.SetMode(ModeAppView)
		}
		return nil, true

	case appViewActionPressedMsg:
		if m.teamID != a.activeTeamID || !a.appViewShows(m.viewID) {
			return nil, true
		}
		if m.err != nil {
			return toastWithClear(a, "Could not press "+m.label+": "+m.err.Error(), 4*time.Second), true
		}
		// Slack re-publishes the view after a press; fetch once after
		// the app has had time to finish handling it, in case the
		// inline update never comes.
		team, viewID := m.teamID, m.viewID
		return tea.Tick(appViewRefreshDelay, func(time.Time) tea.Msg {
			return appViewRefreshMsg{teamID: team, viewID: viewID}
		}), true

	case appViewRefreshMsg:
		if m.teamID != a.activeTeamID || !a.appViewShows(m.viewID) {
			return nil, true
		}
		return a.fetchAppViewCmd(m.teamID, m.viewID), true
	}
	return nil, false
}

// appViewShows reports whether viewID is the modal on screen. A result
// for a view the user has dismissed or the app has replaced is stale; a
// result that names no view cannot be matched and is dropped too.
func (a *App) appViewShows(viewID string) bool {
	v := a.appView.Current()
	return v != nil && viewID != "" && viewID == v.ViewID
}

// fetchAppViewCmd starts a fetch of the dialog's content and tags the
// result with the inline generation it started at and its position in
// the fetch order.
func (a *App) fetchAppViewCmd(teamID, viewID string) tea.Cmd {
	a.appViewFetchSeq++
	return appViewFetchCmd(a.appViewSvc, teamID, viewID, a.appViewGen, a.appViewFetchSeq)
}

// appViewFetchCmd runs svc.Fetch and tags the result. A result that
// started before a newer inline frame, or before a fetch that already
// landed, is discarded by the reducer.
func appViewFetchCmd(svc core.AppViewService, teamID, viewID string, gen, fetch uint64) tea.Cmd {
	return func() tea.Msg {
		msg := svc.Fetch(teamID, viewID)
		if loaded, ok := msg.(AppViewLoadedMsg); ok {
			loaded.Gen = gen
			loaded.Fetch = fetch
			return loaded
		}
		return msg
	}
}
