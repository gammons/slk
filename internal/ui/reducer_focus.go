// internal/ui/reducer_focus.go
//
// Terminal-focus reducer for App.Update.
//
// Owns:
//
//	tea.FocusMsg - the terminal running slk gained focus.
//	tea.BlurMsg  - the terminal running slk lost focus.
//	markFlushMsg - the debounced read-cursor flush tick.
//
// The first two are emitted only once App.View sets ReportFocus, and
// only by terminals that support focus reporting. markFlushMsg lives
// here because whether a staged mark may be issued is a question of
// focus (and, at the FocusMsg catch-up, of whether its pane is still
// drawn: dropMarksForHiddenPanes).
//
// Why the App tracks this at all: a channel being *selected* does not
// mean the user can see it. slk may be sitting in a background
// terminal, an inactive tmux window, or an unfocused tmux pane with
// the same channel still selected. Read-marking that follows selection
// alone would diverge from Slack's own read state and suppress mobile
// notifications for messages the user never saw.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

// markFlushMsg fires after the mark debounce interval; see
// App.scheduleMarkFlush.
type markFlushMsg struct{}

var reduceFocus reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch msg.(type) {
	case tea.FocusMsg:
		a.terminalFocused = true
		a.focusEverReported = true
		// Not gated on autoMarkArmed: reaching this arm is itself the
		// proof that arming waits for, and the user is by now looking
		// at the channel. Catch-up — marks staged while blurred, or
		// staged inside tmux before arming, go out now. Without this,
		// a user who alt-tabs back, reads everything, and never
		// switches channels leaves the channel unread on Slack.
		a.dropMarksForHiddenPanes()
		return a.flushPendingMarks(), true

	case tea.BlurMsg:
		a.terminalFocused = false
		// A blur proves focus reporting is wired up just as well as a
		// focus does, and inside tmux it is the likelier first event.
		a.focusEverReported = true
		return nil, true

	case markFlushMsg:
		a.markFlushScheduled = false
		if !a.terminalFocused {
			// Blurred between scheduling and firing: keep the slots
			// staged rather than dropping them. The FocusMsg arm
			// above issues them when the user comes back.
			return nil, true
		}
		return a.flushPendingMarks(), true
	}
	return nil, false
}

// dropMarksForHiddenPanes discards staged marks whose pane is not drawn
// now, and re-reads read state so the unread dot shows. Only the
// FocusMsg catch-up calls it: its marks may have been staged while
// blurred, unseen, and the layout can change before the user returns
// (a resize stacking the panes). The debounce tick is not gated: its
// marks were staged for arrivals the user saw, and hiding the pane
// within the debounce must not make a read message unread.
func (a *App) dropMarksForHiddenPanes() {
	channelDrawn, threadDrawn := a.contentPanesDrawn()
	dropped := false
	if !channelDrawn && a.pendingChannelMark.ts != "" {
		a.pendingChannelMark = pendingChannelMarkState{}
		dropped = true
	}
	if !threadDrawn && a.pendingThreadMark.ts != "" {
		a.pendingThreadMark = pendingThreadMarkState{}
		dropped = true
	}
	if dropped {
		a.notifyReadStateChanged()
	}
}
