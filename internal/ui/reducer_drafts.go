// internal/ui/reducer_drafts.go
//
// Draft sync's messages (drafts.go): the reload a connect asks for, a
// list's arrival, the comparison tick, save answers, and the Drafts
// list's delete.
package ui

import tea "charm.land/bubbletea/v2"

var reduceDrafts reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case DraftsStaleMsg:
		return a.loadDraftsCmd(m.TeamID), true
	case draftsLoadedMsg:
		return a.reduceDraftsLoaded(m), true
	case draftTickMsg:
		return a.reduceDraftTick(), true
	case draftSavedMsg:
		return a.reduceDraftSaved(m), true
	}
	return nil, false
}
