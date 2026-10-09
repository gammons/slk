// internal/ui/set_mark.go
//
// The m chord: `m` arms a pending-key state and the next key names the
// mark to set, modelled on the ctrl+w window chord (windows.go).
// Unmapped keys — including Esc — cancel silently, matching vim. The
// recorded mark carries a preview snapshot so the overlay can render
// offline and immediately after a restart.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// isMarkLetter reports whether b names a mark letter (a-z, A-Z).
func isMarkLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// handleMarkChord consumes the key following `m`. Only a-z/A-Z name a
// mark; any other key — Esc included — cancels silently, matching vim
// (see handleWindowChord, internal/ui/windows.go).
func (a *App) handleMarkChord(msg tea.KeyMsg) tea.Cmd {
	keyStr := msg.String()
	if len(keyStr) != 1 || !isMarkLetter(keyStr[0]) {
		return nil
	}
	return a.setMark(keyStr)
}

// setMark records the current location under letter, with a preview
// snapshot taken now (channel name, author display name, truncated
// excerpt) so the overlay can draw it offline and after a restart.
// With no message selected nothing is recorded and the user is told
// why. A failed persist write surfaces as a toast: an uppercase mark
// has no session copy, so a lost write means the mark silently does
// not exist.
func (a *App) setMark(letter string) tea.Cmd {
	loc, ok := a.currentLocation()
	if !ok {
		return toastWithClear(a, "No message selected to mark", 2*time.Second)
	}
	m := Mark{
		Location: loc,
		Letter:   letter,
	}
	if name, _, found := a.channels.Lookup(loc.ChannelID); found {
		m.ChannelName = name
	}
	if a.focusedPanel == PanelThread {
		if reply := a.threadPanel.SelectedReply(); reply != nil {
			m.AuthorName = reply.UserName
			m.Excerpt = truncateReason(reply.Text, 100)
		}
	} else if msg, ok := a.messagepane.SelectedMessage(); ok {
		m.AuthorName = msg.UserName
		m.Excerpt = truncateReason(msg.Text, 100)
	}
	if err := a.marks.Set(a.activeTeamID, letter, m); err != nil {
		return toastWithClear(a, "Failed to save mark "+letter, 3*time.Second)
	}
	return nil
}
