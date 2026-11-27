// internal/ui/mode_status_dialog.go
//
// The Ctrl+S "Set a status..." dialog. Typing edits the text, Up/Down
// pick when it clears, Tab opens the emoji picker on the dialog's
// behalf, Enter sets the status on the active workspace, Esc cancels.
package ui

import (
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/reactionpicker"
)

func handleStatusDialogMode(a *App, msg tea.KeyMsg) tea.Cmd {
	k := msg.Key()
	if k.Mod&tea.ModCtrl != 0 {
		switch k.Code {
		case 'v', 'V':
			if a.clipboardAvailable && a.desktop != nil {
				a.statusDialog.Insert(string(a.desktop.ReadClipboard(core.ClipboardText)))
			}
		case 'p':
			a.statusDialog.MoveClearAfter(-1)
		case 'n':
			a.statusDialog.MoveClearAfter(1)
		}
		return nil
	}
	switch k.Code {
	case tea.KeyEscape:
		a.closeStatusDialog()
		a.SetMode(ModeNormal)
		return nil
	case tea.KeyEnter:
		text, emoji, expiration, ok := a.statusDialog.Submit(time.Now())
		if !ok {
			return nil
		}
		a.closeStatusDialog()
		a.SetMode(ModeNormal)
		if a.presenceSvc != nil {
			a.presenceSvc.SetCustomStatus(text, emoji, expiration)
		}
		return nil
	case tea.KeyTab:
		a.statusEmojiPick = true
		a.reactionPicker.Open("", "", nil)
		a.SetMode(ModeReactionPicker)
		return nil
	case tea.KeyBackspace:
		a.statusDialog.Backspace()
		return nil
	case tea.KeyUp:
		a.statusDialog.MoveClearAfter(-1)
		return nil
	case tea.KeyDown:
		a.statusDialog.MoveClearAfter(1)
		return nil
	}
	s := msg.String()
	if s == "space" {
		s = " "
	}
	if r := []rune(s); len(r) == 1 && unicode.IsPrint(r[0]) {
		a.statusDialog.Insert(s)
	}
	return nil
}

// statusEmojiPicked handles the reaction picker while it is open for
// the status dialog: a pick or Esc goes back to the dialog.
func (a *App) statusEmojiPicked(result *reactionpicker.ReactionResult) tea.Cmd {
	if result == nil && a.reactionPicker.IsVisible() {
		return nil
	}
	if result != nil {
		a.statusDialog.SetEmoji(result.Emoji)
		a.recordEmojiUse(result.Emoji)
	}
	a.reactionPicker.Close()
	a.statusEmojiPick = false
	a.SetMode(ModeStatusDialog)
	return nil
}

// closeStatusDialog drops the dialog and any emoji pick made for it.
func (a *App) closeStatusDialog() {
	a.statusDialog.Close()
	a.statusEmojiPick = false
}
