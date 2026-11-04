package ui

import "github.com/gammons/slk/internal/core"

// applyPresenceAction applies a presence choice for the workspace on
// screen. Presence/DND is optimistic: the cached status and the status
// bar change at once, before the API call returns; the WebSocket echo
// reaffirms them. Custom status (WFH/Vacation/Clear) is NOT optimistic
// — Apply is a no-op for those actions, and the status bar only
// updates once the real UserStatusChangeMsg echo arrives (see
// CustomStatus), so a failed request never displays as if it had
// succeeded. The presence menu, its custom snooze and the /away family
// share this.
func (a *App) applyPresenceAction(action core.PresenceAction, snoozeMinutes int) {
	st := a.presence.Apply(a.activeTeamID, action, snoozeMinutes)
	a.statusbar.SetStatus(st.Presence, st.DNDEnabled, st.DNDEndTS)
	if a.presenceSvc != nil {
		a.presenceSvc.SetStatus(action, snoozeMinutes)
	}
}
