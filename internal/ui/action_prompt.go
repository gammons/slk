// internal/ui/action_prompt.go
//
// The action dialog: the confirm prompt showing an ephemeral message's
// legacy buttons. Building the choice and pressing live here; the reducer
// arm for press results is in reducer_action_prompt.go.
package ui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/bubbles/confirmprompt"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// actionPromptRef identifies an ephemeral message with buttons.
type actionPromptRef struct {
	channelID, ts, threadTS string
}

func (r actionPromptRef) key() string { return r.channelID + "/" + r.ts }

// actionPressedMsg reports a button press; err is nil on success, when
// Slack answers by deleting the ephemeral over the WebSocket.
type actionPressedMsg struct {
	label string
	err   error
}

// actionPromptRecheckMsg asks for waiting action dialogs to be reconsidered.
// dispatchModeKey sends it after the command of a key that closed a modal,
// so any navigation that command performs has landed first.
type actionPromptRecheckMsg struct{}

// findActionMessage looks ref's message up in the pane showing it: the
// open thread for a reply or for its parent selected there, otherwise any
// window on the channel. The thread pane may be the only one holding the
// parent (a thread opened from Activity, a parent older than the loaded
// history).
func (a *App) findActionMessage(ref actionPromptRef) (messages.MessageItem, bool) {
	threadShown := ref.threadTS != "" && a.threadVisible &&
		a.threadPanel.ChannelID() == ref.channelID && a.threadPanel.ThreadTS() == ref.threadTS
	if ref.threadTS != "" && ref.threadTS != ref.ts {
		if threadShown {
			for _, r := range a.threadPanel.Replies() {
				if r.TS == ref.ts {
					return r, true
				}
			}
		}
		return messages.MessageItem{}, false
	}
	if threadShown {
		if p := a.threadPanel.ParentMsg(); p.TS == ref.ts {
			return p, true
		}
	}
	for _, mm := range a.modelsForChannel(ref.channelID) {
		for _, m := range mm.Messages() {
			if m.TS == ref.ts {
				return m, true
			}
		}
	}
	return messages.MessageItem{}, false
}

// openActionPrompt opens the dialog for ref's buttons. False when the
// message is no longer shown or has no buttons.
func (a *App) openActionPrompt(ref actionPromptRef) bool {
	msg, ok := a.findActionMessage(ref)
	if !ok || !messages.HasButtonActions(msg) {
		return false
	}
	fromInsert := a.mode == ModeInsert
	fromThread := fromInsert && a.focusedPanel == PanelThread && a.threadVisible
	if fromInsert {
		a.compose.Blur()
		a.threadCompose.Blur()
	}
	kept := a.actionQueue[:0]
	for _, r := range a.actionQueue {
		if r.key() != ref.key() {
			kept = append(kept, r)
		}
	}
	a.actionQueue = kept
	delete(a.promptToasted, ref.key())
	a.prepareConfirmPrompt()
	a.confirmPrompt.OpenChoice(a.actionChoice(ref, msg))
	a.shownPrompt = &ref
	a.shownPromptInsert, a.shownPromptThread = fromInsert, fromThread
	a.SetMode(ModeConfirm)
	return true
}

// clearShownPrompt forgets the action dialog on screen, and where to put
// the user back.
func (a *App) clearShownPrompt() {
	a.shownPrompt = nil
	a.shownPromptInsert, a.shownPromptThread = false, false
}

// resetActionPrompts closes the action dialog and forgets every queued
// message: they belong to the workspace being left.
func (a *App) resetActionPrompts() {
	if a.shownPrompt != nil {
		a.confirmPrompt.Close()
	}
	a.clearShownPrompt()
	a.actionQueue = nil
	a.promptToasted = nil
}

// actionChoice is the dialog for msg: its sender, its text, one button per
// legacy button with number keys 1-9, nothing highlighted, and unbound keys
// ignored.
func (a *App) actionChoice(ref actionPromptRef, msg messages.MessageItem) confirmprompt.Choice {
	choice := &confirmprompt.Choice{Title: msg.UserName, Body: a.actionBody(msg), Default: -1}
	for i, ba := range messages.ButtonActions(msg) {
		b := confirmprompt.Button{Label: ba.Action.Text}
		if i < 9 {
			b.Keys = key.NewBinding(key.WithKeys(strconv.Itoa(i + 1)))
		}
		switch {
		case ba.Action.URL != "":
			url := ba.Action.URL
			b.Action = func() tea.Msg { return OpenLinkMsg{URL: url} }
		case ba.Action.Confirm != nil:
			b.Then = a.actionConfirmChoice(ref, ba, msg.BotID, choice)
		default:
			b.Action = a.pressAction(ref, ba, msg.BotID)
		}
		choice.Buttons = append(choice.Buttons, b)
	}
	return *choice
}

// actionConfirmChoice is Slack's confirmation step for ba. Its dismiss
// button returns to back; its OK button presses.
func (a *App) actionConfirmChoice(ref actionPromptRef, ba messages.ButtonAction, serviceID string, back *confirmprompt.Choice) *confirmprompt.Choice {
	c := ba.Action.Confirm
	title, dismiss, ok := c.Title, c.DismissText, c.OKText
	if title == "" {
		title = "Are you sure?"
	}
	if dismiss == "" {
		dismiss = "Cancel"
	}
	if ok == "" {
		ok = "Okay"
	}
	return &confirmprompt.Choice{
		Title:   title,
		Body:    c.Text,
		Default: -1,
		Buttons: []confirmprompt.Button{
			{Label: dismiss, Keys: key.NewBinding(key.WithKeys("1")), Then: back},
			{Label: ok, Keys: key.NewBinding(key.WithKeys("2")), Action: a.pressAction(ref, ba, serviceID)},
		},
	}
}

// actionBody is the message text, then each attachment's pretext and text,
// flattened to plain text.
func (a *App) actionBody(msg messages.MessageItem) string {
	user := func(id string) (string, bool) {
		n, ok := a.userNames[id]
		return n, ok && n != ""
	}
	parts := []string{messages.FlattenMrkdwn(messages.MessageTextSource(msg), user, nil)}
	for _, att := range msg.LegacyAttachments {
		for _, s := range []string{att.Pretext, att.Text} {
			if s != "" {
				parts = append(parts, messages.FlattenMrkdwn(s, user, nil))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// pressAction is a button's action: press it through the interaction
// service, off the UI goroutine, and report the result.
func (a *App) pressAction(ref actionPromptRef, ba messages.ButtonAction, serviceID string) func() tea.Msg {
	svc := a.interactions
	att, act := ba.Attachment, ba.Action
	return func() tea.Msg {
		err := svc.PressAttachmentAction(ids.ChannelID(ref.channelID), ids.MessageTS(ref.ts),
			att.ID, att.CallbackID, serviceID, act, true)
		return actionPressedMsg{label: act.Text, err: err}
	}
}

// enqueueActionPrompt queues ref and opens it if the user is not busy.
func (a *App) enqueueActionPrompt(ref actionPromptRef) tea.Cmd {
	a.actionQueue = append(a.actionQueue, ref)
	return a.tryOpenActionPrompt()
}

// tryOpenActionPrompt opens the oldest queued dialog unless the user is
// busy: another modal is up, or they are typing (insert mode with text in
// the focused composer, or a ':' command or search query), in which case
// it waits and toasts once; leaving command or search mode does not
// re-check, the user presses b. Queued
// messages no longer shown are dropped. Called on enqueue and from the
// re-check points only: the confirm prompt closing, any other modal
// closing from a key (dispatchModeKey, via actionPromptRecheckMsg, after
// the key's command has run), leaving insert mode, and a send clearing the
// composer.
func (a *App) tryOpenActionPrompt() tea.Cmd {
	for len(a.actionQueue) > 0 {
		ref := a.actionQueue[0]
		if a.mode.IsModalOverlay() {
			return a.toastActionWaiting(ref)
		}
		if a.mode == ModeCommand || a.mode == ModeSearch ||
			(a.mode == ModeInsert && a.focusedComposerValue() != "") {
			return a.toastActionWaiting(ref)
		}
		a.actionQueue = a.actionQueue[1:]
		if a.openActionPrompt(ref) {
			return nil
		}
		delete(a.promptToasted, ref.key())
	}
	return nil
}

func (a *App) focusedComposerValue() string {
	if a.focusedPanel == PanelThread && a.threadVisible {
		return a.threadCompose.Value()
	}
	return a.compose.Value()
}

// toastActionWaiting tells the user, once per message, that a dialog is
// waiting for them.
func (a *App) toastActionWaiting(ref actionPromptRef) tea.Cmd {
	if a.promptToasted[ref.key()] {
		return nil
	}
	if a.promptToasted == nil {
		a.promptToasted = map[string]bool{}
	}
	a.promptToasted[ref.key()] = true
	sender := "A message"
	if msg, ok := a.findActionMessage(ref); ok && msg.UserName != "" {
		sender = msg.UserName
	}
	text := sender + ": press b to respond"
	return func() tea.Msg { return ToastMsg{Text: text} }
}

// dropActionPrompt forgets a deleted message: it leaves the queue, and if
// its dialog is showing, the dialog closes (and the next one may open).
func (a *App) dropActionPrompt(channelID, ts string) tea.Cmd {
	kept := a.actionQueue[:0]
	for _, r := range a.actionQueue {
		if r.channelID != channelID || r.ts != ts {
			kept = append(kept, r)
		}
	}
	a.actionQueue = kept
	delete(a.promptToasted, actionPromptRef{channelID: channelID, ts: ts}.key())
	if s := a.shownPrompt; s != nil && s.channelID == channelID && s.ts == ts && a.mode == ModeConfirm {
		a.confirmPrompt.Close()
		return a.afterConfirmPrompt(nil)
	}
	return nil
}

// closeOffscreenActionPrompt closes the action dialog if its message is no
// longer shown, as after a channel switch: a dialog never outlives its
// message being on screen.
func (a *App) closeOffscreenActionPrompt() tea.Cmd {
	if a.shownPrompt == nil || a.mode != ModeConfirm {
		return nil
	}
	if _, ok := a.findActionMessage(*a.shownPrompt); ok {
		return nil
	}
	a.confirmPrompt.Close()
	return a.afterConfirmPrompt(nil)
}

// toastCmd is a local convenience for this file's toasts.
func toastCmd(text string) tea.Cmd { return func() tea.Msg { return ToastMsg{Text: text} } }

// selectedMessageInFocus returns the selected message in the focused pane
// and the ref that finds it again. In the activity and threads views the
// messages-pane slot shows a list, so there is no selected message.
func (a *App) selectedMessageInFocus() (messages.MessageItem, actionPromptRef, bool) {
	p, channelID, ok := a.focusedContentPane()
	if !ok || (a.focusedPanel == PanelMessages && (a.view == ViewActivity || a.view == ViewThreads)) {
		return messages.MessageItem{}, actionPromptRef{}, false
	}
	msg, ok := p.SelectedMessage()
	if !ok {
		return messages.MessageItem{}, actionPromptRef{}, false
	}
	ref := actionPromptRef{channelID: channelID, ts: msg.TS}
	if a.focusedPanel == PanelThread {
		// A reply's ref also names its thread.
		ref.threadTS = a.threadPanel.ThreadTS()
	}
	return msg, ref, true
}

// respondToSelected is b: open the selected ephemeral's dialog now (the user
// asked, so the interruption rule does not apply).
func (a *App) respondToSelected() tea.Cmd {
	msg, ref, ok := a.selectedMessageInFocus()
	if !ok || !msg.Ephemeral || !messages.HasButtonActions(msg) || !a.openActionPrompt(ref) {
		return toastCmd("No buttons on this message")
	}
	return nil
}

// isMessageOpKey reports keys that send the selected message's ts to Slack.
func (a *App) isMessageOpKey(msg tea.KeyMsg) bool {
	k := a.keys
	for _, b := range []key.Binding{k.Enter, k.Reaction, k.ReactionNav, k.ListReactions,
		k.CopyPermalink, k.ForwardMessage, k.MarkUnread, k.SaveThread, k.RemovePreview} {
		if key.Matches(msg, b) {
			return true
		}
	}
	return false
}

// ephemeralSelectionGuard blocks a message op on a selected ephemeral:
// Slack does not know its ts, so the op would fail or act on a ghost.
func (a *App) ephemeralSelectionGuard() (tea.Cmd, bool) {
	msg, _, ok := a.selectedMessageInFocus()
	if !ok {
		return nil, false
	}
	return ephemeralGuard(msg)
}

// ephemeralGuard is the guard's verdict on msg.
func ephemeralGuard(msg messages.MessageItem) (tea.Cmd, bool) {
	if !msg.Ephemeral {
		return nil, false
	}
	if messages.HasButtonActions(msg) {
		return toastCmd("Only visible to you — press b to respond"), true
	}
	return toastCmd("Only visible to you"), true
}
