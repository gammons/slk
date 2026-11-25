// internal/ui/mode_presence_menu.go
//
// Presence-menu mode key handler (Phase 5f).
//
// Forwards normalised keys to the presence menu overlay. On a
// selection:
//   - Custom snooze opens a sub-mode (ModePresenceCustomSnooze)
//     and lets handlePresenceCustomSnoozeMode collect the value.
//   - Any other action is applied optimistically to local presence
//     state + status bar, then forwarded to the configured
//     setStatusFn for the API call. The WS echo (StatusChangeMsg,
//     routed through presenceController.Handle) reaffirms it.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/presencemenu"
)

func handlePresenceMenuMode(a *App, msg tea.KeyMsg) tea.Cmd {
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
	}

	result := a.presenceMenu.HandleKey(keyStr)
	if result != nil {
		a.presenceMenu.Close()
		// Custom snooze opens a sub-mode instead of firing immediately.
		if result.Action == presencemenu.ActionCustomSnooze {
			a.presence.ClearSnoozeBuf()
			a.SetMode(ModePresenceCustomSnooze)
			return nil
		}
		if result.Action == presencemenu.ActionSetCustom {
			a.statusDialog.Open(a.workspaceNameForActive())
			a.SetMode(ModeStatusDialog)
			return nil
		}
		a.SetMode(ModeNormal)
		a.applyPresenceAction(result.Action, result.SnoozeMinutes)
		return nil
	}
	if !a.presenceMenu.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return nil
}
