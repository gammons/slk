// internal/ui/mode_marks.go
//
// Marks-overlay mode key handler. One widget, two entry modes: the '
// jump chord (when the jump overlay is enabled) and the :marks command
// both open it. A mark letter jumps immediately — the overlay is a
// reference list, so 'a never takes an extra keystroke. A second ' or
// backtick takes the back-jump. backspace/Delete removes the
// highlighted row via the same marksStore.Delete the :delmarks command
// will use, and the overlay stays open showing the remaining marks.
package ui

import tea "charm.land/bubbletea/v2"

func handleMarksMode(a *App, msg tea.KeyMsg) tea.Cmd {
	keyStr := normalizeFinderKey(msg)

	// A second ' or backtick is the back-jump, not a row action.
	if keyStr == "'" || keyStr == "`" {
		a.marksOverlay.Close()
		a.SetMode(ModeNormal)
		return a.takeBackJump()
	}

	result := a.marksOverlay.HandleKey(keyStr)
	if result != nil {
		if result.Delete != "" {
			// The overlay stays open so the user can keep clearing
			// marks; the list is repopulated without the removed row.
			a.marks.Delete(a.activeTeamID, result.Delete)
			a.refreshMarksOverlay()
			return nil
		}
		a.marksOverlay.Close()
		a.SetMode(ModeNormal)
		return a.jumpToMark(result.Select)
	}

	// The overlay closed itself (Esc).
	if !a.marksOverlay.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return nil
}
