// internal/ui/reducer_send.go
//
// Message-lifecycle reducer for App.Update (Phase 4i).
//
// Owns the eleven Update arms that cover the inbound and outbound
// message lifecycle for channel messages (thread-reply lifecycle
// lives in reducer_threads.go):
//
//	NewMessageMsg            - inbound WS event for any channel:
//	                           edit-echo update, self-send dedup
//	                           (recorded + early-arrival in-flight
//	                           guards), append-to-pane, staging a
//	                           focus-gated read-cursor advance or
//	                           surfacing the unread dot, and
//	                           threads-list dirty-bump for replies.
//	SendMessageMsg           - user send: optimistic placeholder +
//	                           chat.postMessage call.
//	MessageSentMsg           - send landed: swap placeholder for
//	                           authoritative message.
//	MessageSendFailedMsg     - send failed: roll back placeholder
//	                           + fire SendFailed toast.
//	EditMessageMsg           - user edit: chat.update call.
//	MessageEditedMsg         - edit result: leave edit mode + on
//	                           failure fire EditFailed toast.
//	DeleteMessageMsg         - user delete: chat.delete call.
//	MessageDeletedMsg        - delete result: on failure fire
//	                           DeleteFailed toast.
//	MarkUnreadMsg            - user mark-unread: subscriptions
//	                           mark call.
//	MessageMarkedUnreadMsg   - mark-unread result: apply local
//	                           read-state mark + fire success or
//	                           failure toast.
//	WSMessageDeletedMsg      - inbound WS delete echo: remove from
//	                           both panes, cancel any in-flight
//	                           edit of this message, close the
//	                           thread panel if the deleted message
//	                           is the open thread's parent.
//
// Free reducer (not controller-absorbed): these arms cooperate on
// the messagepane, threadPanel, selfSend, editController, sidebar
// read-state, and the message service. No single existing
// controller owns all of that cross-section.
//
// Helpers (applyChannelMark, applyThreadMarkUnread, scheduleThreadsDirty,
// recordChannelMark, recordThreadMark, scheduleMarkFlush,
// notifyReadStateChanged, userNameFor, nowFormatted, cancelEdit,
// CloseThread) stay on App; the reducer calls them via `a`.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/slack/mrkdwn"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/statusbar"
)

var reduceSend reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case NewMessageMsg:
		return reduceNewMessage(a, m), true

	case SendMessageMsg:
		return reduceSendMessage(a, m), true

	case MessageSentMsg:
		// The chat.postMessage HTTP response landed. If a
		// "local:..." placeholder is in the pane from the
		// instant-display path (SendMessageMsg above), swap it for
		// the authoritative message. Otherwise -- e.g. test paths
		// firing MessageSentMsg directly, or the user navigated
		// away and back between Enter and the HTTP response --
		// fall back to UpsertSelfSent which appends-or-replaces
		// by Slack TS.
		//
		// UpsertSelfSent is also the fallback for any racing WS
		// echo that managed to slip past selfSendInFlight: if
		// AppendMessage stored the echo's normalised text first,
		// UpsertSelfSent replaces it with our converted-mrkdwn
		// text. See internal/ui/messages/model.go for both
		// methods' contracts.
		if m.Message.TS == "" {
			return nil, true
		}
		a.selfSend.RecordSent(m.Message.TS)
		for _, mm := range a.modelsForChannel(m.ChannelID) {
			// Per-model clone: fresh post responses carry no
			// reactions today, but a shared Reactions array across
			// sibling models would corrupt on the first in-place
			// UpdateReaction — clone is the cheap insurance.
			item := cloneMessageItem(m.Message)
			if !mm.SwapLocalSent(m.LocalTS, item) {
				mm.UpsertSelfSent(item)
			}
		}
		return nil, true

	case MessageSendFailedMsg:
		// The chat.postMessage HTTP call failed; roll back the
		// optimistic placeholder so the user can see the send
		// didn't go through. A toast surfaces the reason.
		if m.LocalTS != "" {
			for _, mm := range a.modelsForChannel(m.ChannelID) {
				mm.RemoveLocalSent(m.LocalTS)
			}
		}
		reason := m.Reason
		return func() tea.Msg {
			return statusbar.SendFailedMsg{Reason: reason}
		}, true

	case EditMessageMsg:
		a.selfSend.MarkInFlight(m.ChannelID)
		messageSvc := a.messageSvc
		chID, ts, text := ids.ChannelID(m.ChannelID), ids.MessageTS(m.TS), m.NewText
		return func() tea.Msg {
			return messageSvc.Edit(chID, ts, text)
		}, true

	case MessageEditedMsg:
		// Only exit edit mode if this result matches the edit
		// that's currently in flight. A stale result from a
		// previously cancelled or replaced edit must not clobber
		// the current one.
		if a.editing.Matches(m.ChannelID, m.TS) {
			a.cancelEdit()
		}
		if m.Err == nil {
			return nil, true
		}
		reason := m.Err.Error()
		return func() tea.Msg {
			return statusbar.EditFailedMsg{Reason: reason}
		}, true

	case DeleteMessageMsg:
		messageSvc := a.messageSvc
		chID, ts := ids.ChannelID(m.ChannelID), ids.MessageTS(m.TS)
		return func() tea.Msg {
			return messageSvc.Delete(chID, ts)
		}, true

	case MarkUnreadMsg:
		messageSvc := a.messageSvc
		chID := ids.ChannelID(m.ChannelID)
		threadTS := ids.ThreadTS(m.ThreadTS)
		boundaryTS := ids.MessageTS(m.BoundaryTS)
		n := m.UnreadCount
		return func() tea.Msg {
			return messageSvc.MarkUnread(chID, threadTS, boundaryTS, n)
		}, true

	case MessageDeletedMsg:
		if m.Err == nil {
			return nil, true
		}
		reason := m.Err.Error()
		return func() tea.Msg {
			return statusbar.DeleteFailedMsg{Reason: reason}
		}, true

	case MessageMarkedUnreadMsg:
		if m.Err != nil {
			reason := m.Err.Error()
			return func() tea.Msg {
				return statusbar.MarkUnreadFailedMsg{Reason: reason}
			}, true
		}
		if m.ThreadTS == "" {
			a.applyChannelMark(m.ChannelID, m.BoundaryTS, m.UnreadCount)
		} else {
			a.applyThreadMarkUnread(m.ChannelID, m.ThreadTS, m.BoundaryTS)
		}
		return func() tea.Msg {
			return statusbar.MarkedUnreadMsg{}
		}, true

	case WSMessageDeletedMsg:
		debuglog.Cache("WSMessageDeletedMsg: channel=%s ts=%s active=%s",
			m.ChannelID, m.TS, a.activeChannelID)
		for _, mm := range a.modelsForChannel(m.ChannelID) {
			mm.RemoveMessageByTS(m.TS)
		}
		if m.ChannelID == a.threadPanel.ChannelID() {
			a.threadPanel.RemoveMessageByTS(m.TS)
		}
		// If the deleted message is the one currently being
		// edited, cancel the edit (the message is gone --
		// submitting would fail).
		if a.editing.Matches(m.ChannelID, m.TS) {
			a.cancelEdit()
		}
		// If the deleted message was the open thread's parent,
		// close the thread panel -- Slack deletes the entire
		// thread when the parent is deleted. Cancel any in-flight
		// edit first so we don't leave the user in insert mode
		// with a hidden compose.
		if a.threadVisible && a.threadPanel.ThreadTS() == m.TS && m.ChannelID == a.threadPanel.ChannelID() {
			a.cancelEdit()
			a.CloseThread()
		}
		return nil, true
	}
	return nil, false
}

// reduceNewMessage handles NewMessageMsg. Extracted because the
// arm is ~100 lines covering five decision branches (edit echo,
// self-send dedup, early-arrival in-flight guard, the read-state
// decision for the channel and thread cursors, threads-list dirty
// bump).
func reduceNewMessage(a *App, m NewMessageMsg) tea.Cmd {
	debuglog.Cache("NewMessageMsg: channel=%s ts=%s thread_ts=%s active=%s",
		m.ChannelID, m.Message.TS, m.Message.ThreadTS, a.activeChannelID)
	if m.Message.IsEdited {
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=skipped_edit_echo",
			m.ChannelID, m.Message.TS)
		// Edit echo: update existing message in place rather than
		// appending. Fan out to every window viewing the channel;
		// gate on the thread panel's channel for the thread cache
		// -- avoids touching panes showing a different channel.
		// This branch must run BEFORE the isSelfSent dedup below,
		// since edits to messages we recently sent would otherwise
		// be silently dropped (the TS is still in selfSentTSes).
		for _, mm := range a.modelsForChannel(m.ChannelID) {
			mm.UpdateMessageInPlace(m.Message.TS, m.Message.Text)
		}
		if m.ChannelID == a.threadPanel.ChannelID() {
			a.threadPanel.UpdateMessageInPlace(m.Message.TS, m.Message.Text)
			a.threadPanel.UpdateParentInPlace(m.Message.TS, m.Message.Text)
		}
		return nil
	}
	// Skip the WS echo of our own optimistic add. The corresponding
	// MessageSentMsg / ThreadReplySentMsg already updated the UI
	// and scheduled side effects; redoing them here would
	// double-render.
	if a.selfSend.IsSelfSent(m.Message.TS) {
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=skipped_self_send",
			m.ChannelID, m.Message.TS)
		return nil
	}
	// Early-arrival suppression: if the WS echo for an
	// slk-originated send arrives BEFORE the chat.postMessage HTTP
	// response (and therefore before recordSelfSent could fire),
	// drop it for self-user messages. Otherwise the WS-echo
	// version -- which carries Slack's normalised text (paragraph
	// breaks flattened for rich_text_block messages) -- renders
	// briefly, then flicker-replaces with the optimistic version.
	// See markSelfSendInFlight / selfSendInFlight comments.
	//
	// Cross-session messages from this user (sent via the official
	// Slack client while slk is open) do NOT update
	// lastSelfSendByChannel, so they pass through this guard.
	if m.Message.UserID != "" && m.Message.UserID == a.currentUserID && a.selfSend.InFlight(m.ChannelID) {
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=skipped_self_send_in_flight",
			m.ChannelID, m.Message.TS)
		return nil
	}
	// Model writes fan out to EVERY window viewing the channel,
	// focused or not (Phase 3): visible-but-unfocused windows show
	// realtime traffic too.
	for _, mm := range a.modelsForChannel(m.ChannelID) {
		// Always add to the pane if it's a top-level message (no
		// ThreadTS or is the parent); update the parent's reply
		// count when a thread reply arrives. cloneMessageItem: fresh
		// WS messages carry no reactions today, but a shared
		// Reactions array across sibling models would corrupt on the
		// first in-place UpdateReaction — clone is the cheap
		// insurance.
		if m.Message.ThreadTS == "" || m.Message.ThreadTS == m.Message.TS {
			mm.AppendMessage(cloneMessageItem(m.Message))
		} else {
			mm.IncrementReplyCount(m.Message.ThreadTS, m.Message.TS)
		}
	}
	// Route thread replies to the open thread panel keyed on the
	// PANEL's identity (its channel + thread ts), not the active
	// channel. The panel can be showing a thread from a non-active
	// channel — opened from the Threads view, or from a permalink
	// pointing into another channel; neither path touches
	// activeChannelID — and live replies must still land.
	// (Previously this lived inside the active-channel branch below,
	// so those panels went stale until a reopen forced a refetch.)
	//
	// Shared with the thread read-cursor gate below, which must not
	// drift from this one: the message is staged for a read-cursor
	// advance exactly when it is rendered into the open panel. Whether
	// that advance is actually issued is decided later, at flush time,
	// by terminal focus.
	inOpenThreadPanel := a.threadVisible &&
		m.ChannelID == a.threadPanel.ChannelID() &&
		m.Message.ThreadTS == a.threadPanel.ThreadTS()
	if inOpenThreadPanel {
		a.threadPanel.AddReply(m.Message)
	}
	isThreadReply := m.Message.ThreadTS != "" && m.Message.ThreadTS != m.Message.TS
	isBroadcast := m.Message.Subtype == "thread_broadcast"

	// Thread-eligible: a reply the user has on screen in the open
	// thread panel. The visibility gate mirrors the channel rule --
	// "the panel is open on this thread", not "the thread pane holds
	// slk-internal focus". Terminal focus is NOT checked here; it
	// gates the flush (scheduleMarkFlush), so a blurred reply stays
	// staged until the FocusMsg catch-up.
	if isThreadReply && inOpenThreadPanel {
		a.recordThreadMark(m.ChannelID, m.Message.ThreadTS, m.Message.TS)
	}

	// Channel-eligible: top-level messages and thread_broadcasts. Plain
	// thread replies never advance the parent channel cursor on Slack.
	if !isThreadReply || isBroadcast {
		switch {
		case m.ChannelID != a.activeChannelID:
			// Not the channel on screen. The has_unread=true DB write
			// already happened in the WS handler; force the sidebar and
			// workspace rail to re-read it.
			debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=mark_unread",
				m.ChannelID, m.Message.TS)
			a.notifyReadStateChanged()
		case a.terminalFocused:
			// On screen AND the user can actually see it: stage a read-
			// cursor advance. No notifyReadStateChanged, so this event
			// triggers no sidebar repaint -- the has_unread=true the WS
			// handler just wrote is cleared by the flush before
			// anything normally repaints the dot. Inside a tmux
			// session that has not yet proven focus reporting no flush
			// is armed (see autoMarkArmed), so there has_unread
			// survives and the dot appears at the next repaint --
			// which is what this arm did before it marked anything.
			debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=active_focused_mark_read",
				m.ChannelID, m.Message.TS)
			a.recordChannelMark(m.ChannelID, m.Message.TS)
		default:
			// Selected, but slk is in a background terminal / inactive
			// tmux window or pane. The user has NOT seen this, so show
			// the dot from the has_unread the WS handler just wrote.
			// The advance is still staged, but scheduleMarkFlush arms
			// no tick while blurred, so it sits until the FocusMsg
			// catch-up in reducer_focus.go issues it.
			debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=active_blurred_stay_unread",
				m.ChannelID, m.Message.TS)
			a.recordChannelMark(m.ChannelID, m.Message.TS)
			a.notifyReadStateChanged()
		}
	} else {
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=skipped_thread_reply",
			m.ChannelID, m.Message.TS)
	}

	var cmds []tea.Cmd
	if c := a.scheduleMarkFlush(); c != nil {
		cmds = append(cmds, c)
	}
	// A thread reply (regardless of channel) may have changed the
	// involved-threads list -- schedule a debounced re-query so a
	// burst of replies coalesces into a single fetch.
	if m.Message.ThreadTS != "" {
		if c := a.scheduleThreadsDirty(); c != nil {
			cmds = append(cmds, c)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// reduceSendMessage handles SendMessageMsg. Extracted to keep the
// reduceSend dispatch switch readable -- this arm does optimistic
// placeholder + async chat.postMessage + LocalTS attachment.
func reduceSendMessage(a *App, m SendMessageMsg) tea.Cmd {
	// Mark in-flight regardless of whether a sender is wired --
	// the user's send intent is what controls WS-echo suppression
	// for self-user messages on this channel.
	a.selfSend.MarkInFlight(m.ChannelID)
	// Instant-display: append an optimistic placeholder for the
	// active channel immediately, before the chat.postMessage HTTP
	// round-trip. The placeholder carries a "local:<n>" TS so the
	// MessageSentMsg / MessageSendFailedMsg handler can find and
	// swap (or remove) it once the HTTP result lands.
	//
	// We only render the placeholder in windows viewing the send's
	// channel (the focused window plus any same-channel siblings —
	// they must show the optimistic message too). For background
	// sends (rare -- would require sending while in a different
	// view) no window matches and we skip the placeholder; the HTTP
	// response will fall back to UpsertSelfSent's append path.
	//
	// Convert the user-typed CommonMark to Slack mrkdwn before
	// rendering so the placeholder picks up bold / italic / code /
	// link styling immediately. Without this, "**bold**" would
	// render literally until the chat.postMessage HTTP response
	// landed and the swap dropped in Slack's converted form. The
	// converter is the same one used by client.SendMessage, so the
	// placeholder and the swapped message render identically for
	// the common case (no rich_text_block paragraph quirks).
	localTS := a.selfSend.NextLocalTS()
	optimisticText, _ := mrkdwn.Convert(m.Text)
	for _, mm := range a.modelsForChannel(m.ChannelID) {
		mm.AppendMessage(messages.MessageItem{
			TS:        localTS,
			UserID:    a.currentUserID,
			UserName:  a.userNameFor(a.currentUserID),
			Text:      optimisticText,
			Timestamp: a.nowFormatted(),
		})
	}
	messageSvc := a.messageSvc
	chID, text := ids.ChannelID(m.ChannelID), m.Text
	return func() tea.Msg {
		result := messageSvc.Send(chID, text)
		// Attach LocalTS so the receiving handler can swap or
		// remove the placeholder. Senders shouldn't need to know
		// about LocalTS themselves.
		switch r := result.(type) {
		case MessageSentMsg:
			r.LocalTS = localTS
			return r
		case MessageSendFailedMsg:
			r.LocalTS = localTS
			return r
		}
		return result
	}
}
