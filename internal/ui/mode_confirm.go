// internal/ui/mode_confirm.go
//
// Confirm-mode key handler. The prompt closes itself on any key, so mode drops
// back once it reports hidden.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

func handleConfirmMode(a *App, msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	a.confirmPrompt, cmd = a.confirmPrompt.Update(msg)
	if !a.confirmPrompt.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return cmd
}
