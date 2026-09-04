// internal/ui/mode_insert.go
//
// Insert-mode key handler (Phase 5l).
//
// Insert mode routes keys to the active compose textarea (main
// compose or thread compose, depending on focusedPanel + thread
// visibility). It also owns:
//
//   - Esc with active upload     -> "Upload in progress" toast (Esc
//     doesn't cancel an in-flight
//     upload).
//   - Esc with active edit       -> close any open compose picker
//     first, else cancel the edit.
//   - Esc otherwise              -> close any open compose picker
//     first, else exit insert mode.
//   - Ctrl+V                     -> smartPaste (clipboard image /
//     file path / verbatim text).
//   - Ctrl+U                     -> clear compose (text +
//     attachments + uploading flag).
//   - Up / Down on first/last line -> jump to start/end of textarea.
//   - Plain Enter                -> send (or commit edit, or upload-
//     then-send if attachments present).
//   - Shift+Enter / Ctrl+J       -> insert literal newline.
//   - Other keys                 -> forward to compose; throttled
//     typing-indicator emit on every
//     text keystroke.
//
// Compose-overlay pickers (emoji / @mention / #channel) get
// priority on Up/Down/Enter: when a picker is active, those keys
// go to the picker so the user can navigate / select.
package ui

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func handleInsertMode(a *App, msg tea.KeyMsg) tea.Cmd {
	if (a.compose.Uploading() || a.threadCompose.Uploading()) && key.Matches(msg, a.keys.Escape) {
		return a.uploadToastCmd("Upload in progress", 2*time.Second)
	}
	if a.editing.IsActive() && key.Matches(msg, a.keys.Escape) {
		// If a picker is active in the relevant compose, close it
		// instead of cancelling the edit.
		if a.editing.Panel() == PanelThread {
			if a.threadCompose.IsEmojiActive() {
				a.threadCompose.CloseEmoji()
				return nil
			}
			if a.threadCompose.IsMentionActive() {
				a.threadCompose.CloseMention()
				return nil
			}
			if a.threadCompose.IsChannelActive() {
				a.threadCompose.CloseChannel()
				return nil
			}
		} else {
			if a.compose.IsEmojiActive() {
				a.compose.CloseEmoji()
				return nil
			}
			if a.compose.IsMentionActive() {
				a.compose.CloseMention()
				return nil
			}
			if a.compose.IsChannelActive() {
				a.compose.CloseChannel()
				return nil
			}
		}
		a.cancelEdit()
		return nil
	}
	if key.Matches(msg, a.keys.Escape) {
		// If a picker is active, close it instead of exiting insert mode.
		if a.focusedPanel == PanelThread && a.threadVisible {
			if a.threadCompose.IsEmojiActive() {
				a.threadCompose.CloseEmoji()
				return nil
			}
			if a.threadCompose.IsMentionActive() {
				a.threadCompose.CloseMention()
				return nil
			}
			if a.threadCompose.IsChannelActive() {
				a.threadCompose.CloseChannel()
				return nil
			}
		} else {
			if a.compose.IsEmojiActive() {
				a.compose.CloseEmoji()
				return nil
			}
			if a.compose.IsMentionActive() {
				a.compose.CloseMention()
				return nil
			}
			if a.compose.IsChannelActive() {
				a.compose.CloseChannel()
				return nil
			}
		}
		a.SetMode(ModeNormal)
		a.compose.Blur()
		a.threadCompose.Blur()
		return nil
	}

	code := msg.Key().Code
	mod := msg.Key().Mod
	isPaste := code == 'v' && mod == tea.ModCtrl
	if isPaste {
		return a.smartPaste()
	}

	// Insert-mode shortcuts that operate on the active compose:
	//   Ctrl+U  -> clear compose (text + attachments + uploading flag)
	//   Up      -> if cursor on first line, jump to start of textarea
	//   Down    -> if cursor on last line,  jump to end of textarea
	target := &a.compose
	if a.focusedPanel == PanelThread && a.threadVisible {
		target = &a.threadCompose
	}
	if code == 'u' && mod == tea.ModCtrl {
		target.Reset()
		return nil
	}
	// If a compose-overlay picker (emoji / @mention / #channel)
	// is active, let it own Up/Down so users can navigate the
	// suggestion list. Without this guard, the jump-to-start/end
	// shortcuts below swallow the arrow keys before the picker
	// ever sees them.
	pickerActive := target.IsEmojiActive() || target.IsMentionActive() || target.IsChannelActive()
	if !pickerActive {
		if code == tea.KeyUp && mod == 0 && target.CursorAtFirstLine() {
			target.MoveCursorToStart()
			return nil
		}
		if code == tea.KeyDown && mod == 0 && target.CursorAtLastLine() {
			target.MoveCursorToEnd()
			return nil
		}
	}
	// Plain Enter sends; Shift+Enter (and Ctrl+J as a fallback
	// for terminals that don't disambiguate modifiers) inserts a
	// newline.
	isSend := code == tea.KeyEnter && !mod.Contains(tea.ModShift)
	isNewline := (code == tea.KeyEnter && mod.Contains(tea.ModShift)) ||
		(code == 'j' && mod == tea.ModCtrl)

	// Determine which compose box is active based on focused panel.
	if a.focusedPanel == PanelThread && a.threadVisible {
		// If a picker is active, forward all keys to compose
		// (including Enter).
		if a.threadCompose.IsEmojiActive() || a.threadCompose.IsMentionActive() || a.threadCompose.IsChannelActive() {
			var cmd tea.Cmd
			a.threadCompose, cmd = a.threadCompose.Update(msg)
			return cmd
		}

		// Thread reply compose.
		if isNewline {
			var cmd tea.Cmd
			a.threadCompose, cmd = a.threadCompose.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			return cmd
		}
		if isSend {
			if len(a.threadCompose.Attachments()) > 0 {
				cmd := a.submitWithAttachments(&a.threadCompose)
				if a.threadCompose.Uploading() {
					a.exitInsertAfterSend()
				}
				return cmd
			}
			if a.editing.IsActive() && a.editing.Panel() == PanelThread {
				return a.submitEdit(a.threadCompose.Value(), a.threadCompose.TranslateMentionsForSend(a.threadCompose.Value()))
			}
			text := a.threadCompose.Value()
			if text != "" {
				text = a.threadCompose.TranslateMentionsForSend(text)
				a.threadCompose.Reset()
				threadTS := a.threadPanel.ThreadTS()
				channelID := a.threadPanel.ChannelID()
				a.exitInsertAfterSend()
				return func() tea.Msg {
					return SendThreadReplyMsg{
						ChannelID: channelID,
						ThreadTS:  threadTS,
						Text:      text,
					}
				}
			}
			return nil
		}
		var cmd tea.Cmd
		a.threadCompose, cmd = a.threadCompose.Update(msg)
		a.typingOut.MaybeSend(a.threadPanel.ChannelID())
		return cmd
	}

	// Channel message compose.
	// If a picker is active, forward all keys to compose
	// (including Enter).
	if a.compose.IsEmojiActive() || a.compose.IsMentionActive() || a.compose.IsChannelActive() {
		var cmd tea.Cmd
		a.compose, cmd = a.compose.Update(msg)
		return cmd
	}

	if isNewline {
		var cmd tea.Cmd
		a.compose, cmd = a.compose.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		return cmd
	}
	if isSend {
		if len(a.compose.Attachments()) > 0 {
			cmd := a.submitWithAttachments(&a.compose)
			if a.compose.Uploading() {
				a.exitInsertAfterSend()
			}
			return cmd
		}
		if a.editing.IsActive() && a.editing.Panel() == PanelMessages {
			return a.submitEdit(a.compose.Value(), a.compose.TranslateMentionsForSend(a.compose.Value()))
		}
		text := a.compose.Value()
		if text != "" {
			text = a.compose.TranslateMentionsForSend(text)
			a.compose.Reset()
			a.exitInsertAfterSend()
			return func() tea.Msg {
				return SendMessageMsg{
					ChannelID: a.activeChannelID,
					Text:      text,
				}
			}
		}
		return nil
	}

	var cmd tea.Cmd
	a.compose, cmd = a.compose.Update(msg)
	a.typingOut.MaybeSend(a.activeChannelID)
	return cmd
}
