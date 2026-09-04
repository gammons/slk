// internal/ui/reducer_focus.go
//
// Terminal-focus reducer for App.Update.
//
// Owns:
//
//	tea.FocusMsg - the terminal running slk gained focus.
//	tea.BlurMsg  - the terminal running slk lost focus.
//
// Both are emitted only once App.View sets ReportFocus, and only by
// terminals that support focus reporting.
//
// Why the App tracks this at all: a channel being *selected* does not
// mean the user can see it. slk may be sitting in a background
// terminal, an inactive tmux window, or an unfocused tmux pane with
// the same channel still selected. Read-marking that follows selection
// alone would diverge from Slack's own read state and suppress mobile
// notifications for messages the user never saw.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

var reduceFocus reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch msg.(type) {
	case tea.FocusMsg:
		a.terminalFocused = true
		return nil, true

	case tea.BlurMsg:
		a.terminalFocused = false
		return nil, true
	}
	return nil, false
}
