// internal/ui/mode_presence_snooze.go
//
// Presence custom-snooze numeric-input key handler (Phase 5d).
//
// This mode collects a free-form minutes value for "snooze for N
// minutes". Esc cancels, Enter applies (with validation toast on
// bad input), Backspace deletes a digit, any other digit appends
// to the snooze buffer. Non-digit input is ignored by
// presence.AppendSnoozeDigit.
//
// The optimistic SetStatus + setStatusFn dispatch mirrors the
// presence-menu Apply path (handlePresenceMenuMode).
package ui

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/presencemenu"
	"github.com/gammons/slk/internal/ui/statusbar"
)

func handlePresenceCustomSnoozeMode(a *App, msg tea.KeyMsg) tea.Cmd {
	switch msg.Key().Code {
	case tea.KeyEscape:
		a.presence.ClearSnoozeBuf()
		a.SetMode(ModeNormal)
		return nil
	case tea.KeyEnter:
		mins, err := strconv.Atoi(a.presence.SnoozeBuf())
		a.presence.ClearSnoozeBuf()
		a.SetMode(ModeNormal)
		if err != nil || mins <= 0 {
			a.statusbar.SetToast("Invalid snooze duration")
			return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return statusbar.CopiedClearMsg{} })
		}
		st := a.presence.Apply(a.activeTeamID, presencemenu.ActionSnooze, mins)
		a.statusbar.SetStatus(st.Presence, st.DNDEnabled, st.DNDEndTS)
		if a.presenceSvc != nil {
			a.presenceSvc.SetStatus(presencemenu.ActionSnooze, mins)
		}
		return nil
	case tea.KeyBackspace:
		a.presence.BackspaceSnooze()
		return nil
	}
	a.presence.AppendSnoozeDigit(msg.String())
	return nil
}
