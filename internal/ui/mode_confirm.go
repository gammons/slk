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
	return a.afterConfirmPrompt(cmd)
}

// afterConfirmPrompt runs after every key or click the confirm prompt
// handles: once the prompt reports hidden, mode drops back to normal.
func (a *App) afterConfirmPrompt(cmd tea.Cmd) tea.Cmd {
	if !a.confirmPrompt.IsVisible() {
		restore, thread := a.shownPromptInsert, a.shownPromptThread
		a.clearShownPrompt()
		if restore && thread && !a.threadVisible {
			// A channel switch closed the thread while the dialog was
			// up: the channel composer is the one left to type in.
			thread = false
			a.focusedPanel = PanelMessages
		}
		if restore {
			a.SetMode(ModeInsert)
			var focus tea.Cmd
			if thread {
				focus = a.threadCompose.Focus()
			} else {
				focus = a.compose.Focus()
			}
			// A read-only refusal that landed while the dialog was
			// up disabled the channel composer without leaving insert
			// mode (it wasn't in it): don't hand the user back into it.
			if drop := a.dropInsertIfComposeDisabled(); drop != nil {
				return tea.Batch(cmd, drop, a.tryOpenActionPrompt())
			}
			return tea.Batch(cmd, focus, a.tryOpenActionPrompt())
		}
		a.SetMode(ModeNormal)
		return tea.Batch(cmd, a.tryOpenActionPrompt())
	}
	return cmd
}
