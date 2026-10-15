// internal/ui/read_only.go
//
// Reactive gating for conversations Slack treats as read-only for the
// user. The app-home probe only covers app DMs; any other read-only
// conversation shows up as a send failing with
// restricted_action_read_only (and suffixes such as _channel). The first
// such failure marks the channel for the session, so its composer stays
// disabled, including when the user switches away and back.
package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/debuglog"
)

const (
	readOnlyToast      = "This conversation is read-only"
	appUnsendableToast = "You can't send messages to this app"
)

// isReadOnlySendError reports whether a send failure reason is Slack
// refusing the post because the conversation is read-only for the user.
func isReadOnlySendError(reason string) bool {
	return strings.Contains(reason, "restricted_action_read_only")
}

// setComposeDisabled sets composeDisabled, logging every change.
func (a *App) setComposeDisabled(v bool, why string) {
	if a.composeDisabled != v {
		debuglog.General("compose: composeDisabled %v -> %v channel=%s why=%s",
			a.composeDisabled, v, a.activeChannelID, why)
	}
	a.composeDisabled = v
}

// composeBlockedText is the toast for a refused compose attempt on the
// active channel.
func (a *App) composeBlockedText() string {
	if a.readOnlyChannels[a.activeChannelID] {
		return readOnlyToast
	}
	return appUnsendableToast
}

// dropInsertIfComposeDisabled takes the user out of insert mode on the
// channel composer when it has just become disabled (the probe and send
// failures are async), so the hidden composer stops taking input.
func (a *App) dropInsertIfComposeDisabled() tea.Cmd {
	if a.composeDisabled && a.mode == ModeInsert && !a.editing.IsActive() &&
		!(a.focusedPanel == PanelThread && a.threadVisible) {
		a.SetMode(ModeNormal)
		a.compose.Blur()
		a.focusedPanel = PanelMessages
		return toastWithClear(a, a.composeBlockedText(), 2*time.Second)
	}
	return nil
}

// sendFailedCmd handles a failed chat.postMessage for chID. A
// read-only refusal marks the channel and returns the read-only toast;
// any other reason returns nil so the caller shows the generic failure.
func (a *App) sendFailedCmd(chID, reason string) tea.Cmd {
	debuglog.General("compose: send failed channel=%s error=%q", chID, reason)
	if !isReadOnlySendError(reason) {
		return nil
	}
	if a.readOnlyChannels == nil {
		a.readOnlyChannels = map[string]bool{}
	}
	a.readOnlyChannels[chID] = true
	debuglog.General("compose: channel marked read-only channel=%s", chID)
	if chID == a.activeChannelID {
		a.setComposeDisabled(true, "send_failed_read_only")
		if cmd := a.dropInsertIfComposeDisabled(); cmd != nil {
			return cmd
		}
	}
	return toastWithClear(a, readOnlyToast, 3*time.Second)
}
