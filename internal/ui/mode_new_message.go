// internal/ui/mode_new_message.go
//
// New-message mode key handler. Forwards normalised keys to the
// newmessagepicker overlay. When the picker returns a Result, the
// handler dispatches a new submit via ChannelService.OpenConversation
// (with a monotonic RequestID) and tracks the in-flight state on App.
// On Esc, marks the in-flight as cancelled so a late result is
// dropped (see reducer_new_message.go for the dropping logic).
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
		// Submit. Bump the in-flight ID and clear cancellation
		// before dispatch so a fresh result is honored.
		a.newMessageInFlightID++
		a.newMessageCancelled = false
		reqID := a.newMessageInFlightID
		userIDs := result.UserIDs
		return teaCmd(a.channels.OpenConversation(userIDs, reqID))
	}

	// Picker closed itself (Esc). Mark any in-flight submit as
	// cancelled so its eventual result is dropped. Switch back to
	// ModeNormal.
	if !a.newMessagePicker.IsVisible() {
		if a.newMessageInFlightID != 0 {
			a.newMessageCancelled = true
		}
		a.SetMode(ModeNormal)
	}
	return nil
}
