// internal/ui/mode_drafts.go
//
// The :drafts dialog's keys: Enter jumps to the highlighted draft's
// conversation, d asks to delete it, Esc closes.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/draftlist"
)

func handleDraftsMode(a *App, msg tea.KeyMsg) tea.Cmd {
	act, i := a.draftList.HandleKey(normalizeFinderKey(msg))
	if act != draftlist.None && i < len(a.draftListItems) {
		d := a.draftListItems[i]
		if act == draftlist.Delete {
			a.confirmDeleteDraft(d) // switches to ModeConfirm, closing the list
			return nil
		}
		a.draftList.Close()
		a.SetMode(ModeNormal)
		return a.jumpToDraft(d)
	}
	if !a.draftList.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return nil
}
