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

	"github.com/gammons/slk/internal/ids"
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
	if a.focusedPanel == PanelSidebar {
		return a.setChannelMark(letter)
	}
	loc, ok := a.currentLocation()
	if !ok {
		return toastWithClear(a, "No message selected to mark", 2*time.Second)
	}
	m := Mark{
		Location: loc,
		Letter:   letter,
	}
	if name, chType, found := a.channels.Lookup(loc.ChannelID); found {
		m.ChannelName = name
		m.ChannelType = chType
	}
	// The same order as currentLocation: the thread pane, then the
	// Threads view, then the channel pane.
	var pane contentPane
	switch {
	case a.focusedPanel == PanelThread:
		pane = a.threadPanel
	case a.view == ViewDrafts:
		if i, ok := a.draftsView.Selected(); ok && i < len(a.draftListItems) {
			m.Excerpt = truncateReason(a.draftRowPreview(a.draftListItems[i]), 100)
		}
	case a.view == ViewThreads:
		if s, ok := a.threadsView.SelectedSummary(); ok {
			m.AuthorName = a.userNameFor(s.ParentUserID)
			m.Excerpt = truncateReason(s.ParentText, 100)
			if m.ChannelName == "" {
				m.ChannelName = s.ChannelName
			}
		}
	default:
		pane = a.messagepane
	}
	if pane != nil {
		if msg, ok := pane.SelectedMessage(); ok {
			m.AuthorName = msg.UserName
			m.Excerpt = truncateReason(msg.Text, 100)
		}
	}
	if err := a.marks.Set(a.activeTeamID, letter, m); err != nil {
		return toastWithClear(a, "Failed to save mark "+letter, 3*time.Second)
	}
	return nil
}

// setChannelMark records the conversation highlighted in the sidebar
// under letter: a channel-only location, so the jump opens it at the
// latest message rather than at a remembered one.
func (a *App) setChannelMark(letter string) tea.Cmd {
	item, ok := a.sidebar.SelectedItem()
	if !ok {
		return toastWithClear(a, "No conversation selected to mark", 2*time.Second)
	}
	m := Mark{
		Location:    Location{TeamID: ids.TeamID(a.activeTeamID), ChannelID: ids.ChannelID(item.ID)},
		Letter:      letter,
		ChannelName: item.Name,
		ChannelType: item.Type,
	}
	if err := a.marks.Set(a.activeTeamID, letter, m); err != nil {
		return toastWithClear(a, "Failed to save mark "+letter, 3*time.Second)
	}
	return nil
}
