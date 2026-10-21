package ui

import (
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/thread"
)

// contentPane is what the App does to either message pane: the channel
// pane (messages.Model) and the thread pane (thread.Model). The two
// share their scroll, selection and rendering code (Phase 3), and the
// App's mirror pairs collapse onto this interface. What only one pane
// has (image context, loading, the thread's parent) stays off it.
type contentPane interface {
	SelectedMessage() (messages.MessageItem, bool)
	ReactionNavActive() bool
	EnterReactionNav()
	ExitReactionNav()
	ReactionNavLeft()
	ReactionNavRight()
	SelectedReaction() (emoji string, isPlus bool)
	ChromeHeight() int
	HitTestReaction(row, col int) (idx int, emoji string, ok bool)
	HitTestImage(row, col int) (ts string, attIdx int, ok bool)
	BeginSelectionAt(y, x int)
	ExtendSelectionAt(y, x int)
	EndSelection() (string, bool)
	ClearSelection()
	ClickAt(y int) bool
	ScrollHintForDrag(y int) int
	ScrollUp(n int)
	ScrollDown(n int)
	// UpdateMessageInPlace applies a message_changed echo to the message
	// with the item's TS; false when the pane does not hold it.
	UpdateMessageInPlace(item messages.MessageItem) bool
}

var (
	_ contentPane = (*messages.Model)(nil)
	_ contentPane = (*thread.Model)(nil)
)

// contentPaneFor returns the pane behind panel, or nil.
func (a *App) contentPaneFor(panel Panel) contentPane {
	switch panel {
	case PanelMessages:
		return a.messagepane
	case PanelThread:
		return a.threadPanel
	}
	return nil
}

// focusedContentPane returns the focused message pane and the channel its
// messages belong to (reactions and pickers need it). ok is false when
// neither message pane has focus.
func (a *App) focusedContentPane() (contentPane, string, bool) {
	switch a.focusedPanel {
	case PanelMessages:
		return a.messagepane, a.activeChannelID, true
	case PanelThread:
		return a.threadPanel, a.threadPanel.ChannelID(), true
	}
	return nil, "", false
}
