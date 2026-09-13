package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/statusbar"
	"golang.design/x/clipboard"
)

// handleVisualMode implements character-wise Vim selection over the rendered
// text of the selected message or reply. h/j/k/l and 0/$ move the active end,
// o swaps ends, y copies, and Esc cancels.
func handleVisualMode(a *App, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "v":
		a.clearSelections()
		a.SetMode(ModeNormal)
	case "h", "left", "j", "down", "k", "up", "l", "right", "0", "$":
		a.moveVisualSelection(msg.String())
	case "o":
		a.swapVisualSelectionEnds()
	case "y":
		text := a.visualSelectionText()
		if text == "" {
			return nil
		}
		a.finishVisualSelection()
		a.SetMode(ModeNormal)
		return func() tea.Msg {
			if !a.clipboardAvailable {
				return statusbar.CopyFailedMsg{}
			}
			_ = a.clipboardWrite(clipboard.FmtText, []byte(text))
			return statusbar.CopiedMsg{N: len([]rune(text))}
		}
	}
	return nil
}

func (a *App) beginVisualSelection() bool {
	switch a.focusedPanel {
	case PanelMessages:
		return a.messagepane.BeginVisualSelection()
	case PanelThread:
		return a.threadPanel.BeginVisualSelection()
	}
	return false
}

func (a *App) moveVisualSelection(motion string) {
	if a.focusedPanel == PanelMessages {
		a.messagepane.MoveVisualSelection(motion)
	} else if a.focusedPanel == PanelThread {
		a.threadPanel.MoveVisualSelection(motion)
	}
}

func (a *App) swapVisualSelectionEnds() {
	if a.focusedPanel == PanelMessages {
		a.messagepane.SwapSelectionEnds()
	} else if a.focusedPanel == PanelThread {
		a.threadPanel.SwapSelectionEnds()
	}
}

func (a *App) visualSelectionText() string {
	if a.focusedPanel == PanelMessages {
		return a.messagepane.SelectionText()
	}
	if a.focusedPanel == PanelThread {
		return a.threadPanel.SelectionText()
	}
	return ""
}

func (a *App) finishVisualSelection() {
	if a.focusedPanel == PanelMessages {
		_, _ = a.messagepane.EndSelection()
	} else if a.focusedPanel == PanelThread {
		_, _ = a.threadPanel.EndSelection()
	}
}
