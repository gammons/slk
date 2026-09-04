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
//	                           guards), append-to-pane or
//	                           mark-channel-unread, and threads-list
//	                           dirty-bump for replies.
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
// self-send dedup, early-arrival in-flight guard, active vs
// inactive channel, threads-list dirty bump).
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
	if a.threadVisible &&
		m.ChannelID == a.threadPanel.ChannelID() &&
		m.Message.ThreadTS == a.threadPanel.ThreadTS() {
		a.threadPanel.AddReply(m.Message)
	}
	if m.ChannelID == a.activeChannelID {
		// "active_channel_no_unread_bump": message arrived for the
		// FOCUSED channel, so no unread bump is applied -- the user
		// is actively reading (the read marker advances on focused
		// entry via MarkChannel/MarkRead elsewhere).
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=active_channel_no_unread_bump",
			m.ChannelID, m.Message.TS)
	} else {
		// Message arrived for a channel that is NOT focused -- bump
		// its unread state so the sidebar shows the dot + bold
		// indicator. This runs regardless of whether some UNFOCUSED
		// window views the channel: the read marker only advances on
		// focused entry, so a visible-but-unfocused window's channel
		// is still unread. Only the focused channel is auto-marked-
		// read (MarkChannel on entry), so only it skips the bump.
		//
		// Skip plain thread replies: a reply inside a thread does
		// not mark the parent channel as unread on Slack -- only
		// top-level messages and thread_broadcasts do. The Threads
		// view tracks its own unread state separately.
		isThreadReply := m.Message.ThreadTS != "" && m.Message.ThreadTS != m.Message.TS
		if !isThreadReply || m.Message.Subtype == "thread_broadcast" {
			debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=mark_unread",
				m.ChannelID, m.Message.TS)
			// The DB write that flips has_unread=true for this
			// channel already happened in the WS-handler path
			// (cache.UpdateChannelReadState). Force the sidebar
			// to re-read read state so the dot appears on the
			// next render, and refresh the workspace rail so its
			// HasUnread flag picks up the change too.
			a.notifyReadStateChanged()
		} else {
			debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=skipped_thread_reply_inactive",
				m.ChannelID, m.Message.TS)
		}
	}
	// A thread reply (regardless of channel) may have changed the
	// involved-threads list -- schedule a debounced re-query so a
	// burst of replies coalesces into a single fetch.
	if m.Message.ThreadTS != "" {
		if c := a.scheduleThreadsDirty(); c != nil {
			return c
		}
	}
	return nil
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
