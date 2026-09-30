// internal/ui/reducer_theme.go
//
// Theme reducer for App.Update. Owns the one message the theme-cycle
// keys produce after the press itself:
//
//	themeSaveDueMsg - themeSaveDelay passed since a cycle press. Save
//	                  the pending theme, unless a later press made
//	                  this tick stale (theme.go).
package ui

import (
	tea "charm.land/bubbletea/v2"
)

var reduceTheme reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	m, ok := msg.(themeSaveDueMsg)
	if !ok {
		return nil, false
	}
	if m.gen == a.themeSaveGen {
		a.SavePendingTheme()
	}
	return nil, true
}
