// internal/ui/mode_confirm.go
//
// Confirm-mode key handler (Phase 5j).
//
// Forwards normalised keys to the confirm prompt overlay.
// HandleKey returns a result whose Cmd carries the action the
// caller registered when opening the prompt (e.g. "really quit?"
// returns the quit cmd on Enter). Mode drops back to Normal when
// the prompt closes itself.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

func handleConfirmMode(a *App, msg tea.KeyMsg) tea.Cmd {
	keyStr := msg.String()
	// Collapse a modified Enter ("shift+enter") to "enter" so it
	// confirms. Escape needs no such arm: confirmprompt cancels on
	// every key it does not recognise.
	if msg.Key().Code == tea.KeyEnter {
		keyStr = "enter"
	}

	res := a.confirmPrompt.HandleKey(keyStr)
	if !a.confirmPrompt.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return res.Cmd
}
