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
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
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
		if result.Action == presencemenu.ActionSetRecent {
			a.SetMode(ModeNormal)
			if a.presenceSvc != nil {
				r := result.Recent
				a.presenceSvc.SetCustomStatus(r.Text, r.Emoji, r.Expiry(time.Now()))
			}
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

// RecentStatusesMsg carries a recent-status fetch for TeamID. Err set
// means the list could not be read (offline, or Slack changed the
// undocumented method): the menu then shows no recent rows.
type RecentStatusesMsg struct {
	Seq      int // which fetch this answers; only the latest one counts
	TeamID   string
	Statuses []core.RecentStatus
	Err      error
}

// fetchRecentStatuses loads the active workspace's recent statuses off
// the UI goroutine.
func (a *App) fetchRecentStatuses() tea.Cmd {
	svc, team := a.presenceSvc, a.activeTeamID
	if svc == nil || team == "" {
		return nil
	}
	a.recentStatusSeq++
	seq := a.recentStatusSeq
	return func() tea.Msg {
		list, err := svc.RecentStatuses(team)
		return RecentStatusesMsg{Seq: seq, TeamID: team, Statuses: list, Err: err}
	}
}

var reduceRecentStatuses reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	m, ok := msg.(RecentStatusesMsg)
	if !ok {
		return nil, false
	}
	if m.Seq != a.recentStatusSeq {
		return nil, true // a slower reply to an earlier Ctrl+S
	}
	if a.recentStatuses == nil {
		a.recentStatuses = map[string][]core.RecentStatus{}
	}
	if m.Err != nil {
		delete(a.recentStatuses, m.TeamID)
	} else {
		a.recentStatuses[m.TeamID] = m.Statuses
	}
	if m.TeamID == a.activeTeamID && a.mode == ModePresenceMenu && a.presenceMenu.IsVisible() {
		a.presenceMenu.SetRecent(a.recentStatuses[m.TeamID])
	}
	return nil, true
}
