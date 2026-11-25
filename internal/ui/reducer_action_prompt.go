// internal/ui/reducer_action_prompt.go
//
// Reducer for the action dialog's asynchronous results.
package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

var reduceActionPrompt reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case actionPromptRecheckMsg:
		return a.tryOpenActionPrompt(), true
	case actionPressedMsg:
		if m.err == nil && m.block {
			text := "Pressed " + m.label
			return func() tea.Msg { return ToastMsg{Text: text} }, true
		}
		if m.err == nil {
			return nil, true // Slack deletes the ephemeral over the WebSocket
		}
		text := fmt.Sprintf("Couldn't send %s: %v", m.label, m.err)
		return func() tea.Msg { return ToastMsg{Text: text} }, true
	}
	return nil, false
}
