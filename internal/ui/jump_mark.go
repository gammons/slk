// internal/ui/jump_mark.go
//
// The ' chord (and its backtick alias) arms a pending-key state and
// the next key names the mark to jump to, modelled on the m chord
// (set_mark.go). Unmapped keys cancel silently. The jump hands the
// mark's Location to the shared applyLocation applier, so it inherits
// the permalink/history pipeline: channel open, surrounding-history
// load on a stale target, thread open with the reply selected.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
)

// handleJumpChord consumes the key following `'`. Only a-z/A-Z name a
// mark; a second `'` or backtick takes the back-jump; any other key —
// Esc included — cancels silently, matching vim.
func (a *App) handleJumpChord(msg tea.KeyMsg) tea.Cmd {
	keyStr := msg.String()
	if keyStr == "'" || keyStr == "`" {
		return a.takeBackJump()
	}
	if len(keyStr) != 1 || !isMarkLetter(keyStr[0]) {
		return nil
	}
	return a.jumpToMark(keyStr)
}

// jumpToMark jumps to the mark recorded under letter in the active
// workspace, handing its Location to applyLocation with fromHistory
// false (a jump is a new visit, and the in-place completion path is
// what keeps a same-channel jump from reloading the channel).
//
// Refusals, all leaving the mark retained (marks are never deleted
// automatically):
//
//   - an unset letter changes nothing and toasts;
//   - a mark whose TeamID is not the active workspace does not jump
//     and toasts. This is the deliberate seam for a future
//     cross-workspace change: routeLink (reducer_links.go) sends
//     other-workspace permalinks to the OS browser today, so no
//     in-app cross-workspace navigation exists to build on;
//   - a mark whose channel no longer resolves (applyLocation ok=false)
//     leaves the user in place and toasts.
//
// A mark whose MESSAGE is gone is not a refusal: the channel opens and
// the existing completePendingLinkNav / MessagesAroundLoadedMsg
// pipeline toasts "Message not found", leaving the mark intact.
func (a *App) jumpToMark(letter string) tea.Cmd {
	m, ok := a.marks.Load(a.activeTeamID, letter)
	if !ok {
		return toastWithClear(a, "Mark "+letter+" is not set", 2*time.Second)
	}
	if m.TeamID != ids.TeamID(a.activeTeamID) {
		return toastWithClear(a, "Cross-workspace mark jumps are not supported", 3*time.Second)
	}
	// The departing location is captured before applyLocation because a
	// same-channel jump completes in place and moves the selection — by
	// the time the slot would be read afterwards, it is gone.
	if a.compose.Uploading() || a.threadCompose.Uploading() {
		// Refused before departing is captured, so the '' slot keeps
		// pointing where it did.
		return a.uploadToastCmd("Upload in progress", 2*time.Second)
	}
	departing := a.currentPosition()
	cmd, ok := a.applyLocation(m.Location, false)
	if !ok {
		return toastWithClear(a, "Channel for mark "+letter+" is unavailable", 3*time.Second)
	}
	// Record the departure for the '' back-jump. Only a jump that
	// actually navigates (not a refusal) departs anything.
	a.backJump = &departing
	return cmd
}

// takeBackJump handles the doubled apostrophe (or doubled backtick):
// it navigates to the single most recently departed location — vim's
// ' pseudo-mark, a slot overwritten on every jump rather than a
// navigation-history entry, so it works whether or not the jump changed
// channel. With nothing recorded it is a silent no-op. The back-jump
// itself records the location it departs, so repeating it toggles
// between two locations, as in vim.
func (a *App) takeBackJump() tea.Cmd {
	if a.backJump == nil {
		return nil
	}
	if a.compose.Uploading() || a.threadCompose.Uploading() {
		return a.uploadToastCmd("Upload in progress", 2*time.Second)
	}
	departing := a.currentPosition()
	cmd, ok := a.applyLocation(*a.backJump, false)
	if !ok {
		// The slot's channel no longer resolves: nothing navigates,
		// so nothing is departed and the slot is retained.
		return toastWithClear(a, "Back-jump channel is unavailable", 3*time.Second)
	}
	a.backJump = &departing
	return cmd
}
