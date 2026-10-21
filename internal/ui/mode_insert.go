// internal/ui/mode_insert.go
//
// Insert-mode key handler (Phase 5l).
//
// Insert mode routes keys to the active compose textarea (main
// compose or thread compose, depending on focusedPanel + thread
// visibility). It also owns:
//
//   - Any key with active upload -> "Upload in progress" toast (no
//     draft mutations or sends while an upload is in flight).
//   - Esc with active edit       -> close any open compose picker
//     first, else cancel the edit.
//   - Esc otherwise              -> close any open compose picker
//     first, else exit insert mode.
//   - Ctrl+V                     -> smartPaste (clipboard image /
//     file path / verbatim text).
//   - Ctrl+U                     -> clear compose (text +
//     attachments + uploading flag).
//   - Ctrl+O (thread compose)    -> toggle "also send to channel" for
//     the next thread reply.
//   - Ctrl+E                     -> edit the draft in $VISUAL/$EDITOR
//     (suspends the TUI; see editor.go).
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
	if a.compose.Uploading() || a.threadCompose.Uploading() {
		// An upload is in flight: no insert-mode key may mutate the
		// in-flight caption or send through the other composer. A
		// single early return covers every key (Esc included),
		// replacing the former Esc-only guard.
		return toastWithClear(a, "Upload in progress", 2*time.Second)
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
	if a.stayInInsertAfterSend && a.insertComposerEmpty() && a.isAppHotkey(msg) {
		// Every app Ctrl shortcut doubles as a textarea editing key
		// (ctrl+p previous line, ctrl+a line start, ...). With
		// stay_in_insert_after_send on, insert mode is where a send
		// leaves you, so with nothing in the composer the shortcut
		// means what it means in normal mode. Leaving insert mode
		// this way rechecks waiting dialogs, as Esc does, sequenced
		// after the shortcut's command so a modal or view that
		// command opens (ctrl+n's picker comes from the message it
		// returns) is in place first; a dialog under a modal waits
		// for it to close.
		a.exitInsert()
		cmd := handleNormalMode(a, msg)
		if cmd == nil {
			return a.tryOpenActionPrompt()
		}
		return tea.Sequence(cmd, func() tea.Msg { return actionPromptRecheckMsg{} })
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
		return a.tryOpenActionPrompt()
	}

	code := msg.Key().Code
	// Lock-state bits (NumLock/CapsLock) ride along in Mod on terminals
	// implementing the Kitty Keyboard Protocol, regardless of whether
	// they're relevant to the binding — strip them before comparing.
	mod := msg.Key().Mod &^ (tea.ModCapsLock | tea.ModNumLock | tea.ModScrollLock)
	isPaste := code == 'v' && mod == tea.ModCtrl
	if isPaste {
		return a.smartPaste()
	}
	if code == 'e' && mod == tea.ModCtrl {
		return a.openComposeInEditor()
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
	// Ctrl+O toggles Slack's "Also send to #channel" for the next
	// thread reply. Thread compose only -- the channel compose has no
	// broadcast concept. Skipped while a picker is active so picker
	// navigation keys keep precedence.
	if target == &a.threadCompose && !pickerActive && code == 'o' && mod == tea.ModCtrl {
		a.threadCompose.ToggleBroadcast()
		return nil
	}
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
	// newline. Alt+Enter sends with broadcast in thread compose
	// (one-shot send-with-broadcast without needing the Ctrl+O toggle).
	isAltEnter := code == tea.KeyEnter && mod.Contains(tea.ModAlt)
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
			// A channel a send was refused in as read-only, or whose
			// probe found it unsendable, takes no thread replies either.
			if chID := a.threadPanel.ChannelID(); !a.editing.IsActive() {
				if a.readOnlyChannels[chID] {
					return toastWithClear(a, readOnlyToast, 2*time.Second)
				}
				if a.unsendableChannels[chID] {
					return toastWithClear(a, appUnsendableToast, 2*time.Second)
				}
			}
			if cmd, ok := a.trySlashCommand(PanelThread, &a.threadCompose, a.threadPanel.ChannelID(), a.threadPanel.ThreadTS()); ok {
				return cmd
			}
			if len(a.threadCompose.Attachments()) > 0 {
				cmd := a.submitWithAttachments(&a.threadCompose)
				if a.threadCompose.Uploading() {
					// An upload keeps the composer busy until its
					// result lands, so leave insert mode whatever
					// stay_in_insert_after_send says.
					a.exitInsert()
				}
				return cmd
			}
			if a.editing.IsActive() && a.editing.Panel() == PanelThread {
				return a.submitEdit(a.threadCompose.Value(), a.threadCompose.TranslateMentionsForSend(a.threadCompose.Value()))
			}
			text := a.threadCompose.Value()
			if text != "" {
				text = a.threadCompose.TranslateMentionsForSend(text)
				broadcast := a.threadCompose.Broadcast() || isAltEnter
				a.threadCompose.Reset()
				threadTS := a.threadPanel.ThreadTS()
				channelID := a.threadPanel.ChannelID()
				a.exitInsertAfterSend()
				send := func() tea.Msg {
					return SendThreadReplyMsg{
						ChannelID: channelID,
						ThreadTS:  threadTS,
						Text:      text,
						Broadcast: broadcast,
					}
				}
				return tea.Batch(send, a.tryOpenActionPrompt())
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
		// Backstop: the channel cannot receive messages (the probe may
		// land after insert mode was entered by a path other than `i`).
		if a.composeDisabled && !a.editing.IsActive() {
			return toastWithClear(a, a.composeBlockedText(), 2*time.Second)
		}
		if cmd, ok := a.trySlashCommand(PanelMessages, &a.compose, a.activeChannelID, ""); ok {
			return cmd
		}
		if len(a.compose.Attachments()) > 0 {
			cmd := a.submitWithAttachments(&a.compose)
			if a.compose.Uploading() {
				// Same as the thread branch: an in-flight upload
				// leaves insert mode regardless of the option.
				a.exitInsert()
			}
			return cmd
		}
		if a.editing.IsActive() && a.editing.Panel() == PanelMessages {
			return a.submitEdit(a.compose.Value(), a.compose.TranslateMentionsForSend(a.compose.Value()))
		}
		text := a.compose.Value()
		if text != "" {
			text = a.compose.TranslateMentionsForSend(text)
			// Capture the target channel before the reset/closure:
			// a.activeChannelID may mutate before the deferred cmd
			// runs, so the send must use this local snapshot.
			channelID := a.activeChannelID
			a.compose.Reset()
			a.exitInsertAfterSend()
			send := func() tea.Msg {
				return SendMessageMsg{
					ChannelID: channelID,
					Text:      text,
				}
			}
			return tea.Batch(send, a.tryOpenActionPrompt())
		}
		return nil
	}

	var cmd tea.Cmd
	a.compose, cmd = a.compose.Update(msg)
	a.typingOut.MaybeSend(a.activeChannelID)
	return cmd
}

// insertComposerEmpty reports whether the composer insert mode is typing
// into holds nothing: no text, no attachments, and no edit in progress.
// The thread composer is the target when the thread pane is focused and
// visible, matching the Esc handling above.
func (a *App) insertComposerEmpty() bool {
	if a.editing.IsActive() {
		return false
	}
	c := &a.compose
	if a.focusedPanel == PanelThread && a.threadVisible {
		c = &a.threadCompose
	}
	return c.Value() == "" && len(c.Attachments()) == 0
}

// isAppHotkey reports whether msg is one of the app's Ctrl shortcuts that
// insert mode hands to the normal-mode handler when the composer is empty
// (see insertComposerEmpty). Keys people repeat to delete text stay out,
// so one press too many cannot change mode: Ctrl+W (delete word, the
// window prefix), Ctrl+H (backspace, back) and Ctrl+K (delete to line
// end, forward), and with Ctrl+K its forward twin Ctrl+L. So do Ctrl+E
// (external editor) and Ctrl+O (broadcast), which are composer keys, and
// Ctrl+U / Ctrl+D, which scroll a pane and would leave insert mode for
// no gain. The rest work again after Esc.
func (a *App) isAppHotkey(msg tea.KeyMsg) bool {
	return key.Matches(msg,
		a.keys.FuzzyFinder, a.keys.FuzzyFinderAlt,
		a.keys.NewMessage, a.keys.ActivityView,
		a.keys.WorkspaceSearch, a.keys.PresenceMenu,
		a.keys.ToggleSidebar, a.keys.ToggleThread,
		a.keys.ThemeSwitcher, a.keys.ThemeSwitcherGlobal,
	)
}
