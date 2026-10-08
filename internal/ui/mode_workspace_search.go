// internal/ui/mode_workspace_search.go
//
// Workspace-search mode key handler: the ctrl+f modal.
//
// Forwards normalized keys to the searchresults overlay and
// translates its actions: Submit dispatches the server-side
// search.messages query via the SearchService, Select closes the
// modal and navigates to the chosen message via the pendingLinkNav
// mechanism (FetchAround completes off-buffer targets). Hits in
// channels the user isn't a member of toast instead of navigating.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/searchresults"
)

func handleWorkspaceSearchMode(a *App, msg tea.KeyMsg) tea.Cmd {
	keyStr := normalizeFinderKey(msg)
	switch action := a.searchResults.HandleKey(keyStr); action {
	case searchresults.ActionClose:
		a.SetMode(ModeNormal)
		return nil
	case searchresults.ActionSubmit:
		// If the service never answers (e.g. the noop service returns
		// a nil msg), the modal isn't stuck: backspace drops the widget
		// back to the input state and Esc closes it.
		query := a.searchResults.Query()
		search := a.searchSvc
		return func() tea.Msg { return search.SearchWorkspace(query) }
	case searchresults.ActionSelect:
		// HandleKey only returns ActionSelect when Selected() reports a
		// hit, so ok is guaranteed here. Read it before Close resets it.
		item, _ := a.searchResults.Selected()
		a.searchResults.Close()
		a.SetMode(ModeNormal)
		if item.ChannelID == a.activeChannelID {
			a.pendingLinkNav = &pendingLinkNav{
				channelID: item.ChannelID,
				messageTS: item.TS,
				threadTS:  item.ThreadTS,
			}
			return a.completePendingLinkNav(a.activeChannelID, true)
		}
		// Slack search also returns hits in public channels the user
		// hasn't joined. A Lookup miss is the not-a-member signal at
		// this layer: navigating there would fail with not_in_channel
		// and strand the user in an empty pane, so don't navigate —
		// tell them how to join instead.
		name, chType, ok := a.channels.Lookup(ids.ChannelID(item.ChannelID))
		if !ok {
			chName := item.ChannelName
			return func() tea.Msg {
				return ToastMsg{Text: "Not a member of #" + chName + " — join via ctrl+t to view"}
			}
		}
		a.pendingLinkNav = &pendingLinkNav{
			channelID: item.ChannelID,
			messageTS: item.TS,
			threadTS:  item.ThreadTS,
		}
		return func() tea.Msg {
			return ChannelSelectedMsg{ID: item.ChannelID, Name: name, Type: chType}
		}
	}
	return nil
}
