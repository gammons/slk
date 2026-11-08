// internal/ui/mode_new_message.go
//
// New-message mode key handler. Forwards normalised keys to the
// newmessagepicker overlay. When the picker returns a Result, the
// handler dispatches a new submit via ChannelService.OpenConversation
// (with a monotonic RequestID) and tracks the in-flight state on App.
// Leaving the mode (Esc, ctrl+c) marks the in-flight as cancelled so a
// late result is dropped (see closeModal in app.go, and
// reducer_new_message.go for the dropping logic).
package ui

import (
	tea "charm.land/bubbletea/v2"
)

func handleNewMessageMode(a *App, msg tea.KeyMsg) tea.Cmd {
	keyStr := msg.String()
	switch msg.Key().Code {
	case tea.KeyEnter:
		keyStr = "enter"
	case tea.KeyEscape:
		keyStr = "esc"
	case tea.KeyUp:
		keyStr = "up"
	case tea.KeyDown:
		keyStr = "down"
	case tea.KeyBackspace:
		keyStr = "backspace"
	case tea.KeyTab:
		keyStr = "tab"
	case tea.KeySpace:
		keyStr = " "
	}

	result := a.newMessagePicker.HandleKey(keyStr)
	if result != nil {
		return a.openConversation(result.UserIDs, msgPrefill{})
	}

	// Picker closed itself (Esc). Leaving the mode cancels any
	// in-flight submit (closeModal).
	if !a.newMessagePicker.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return nil
}
