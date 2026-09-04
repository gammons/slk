// internal/ui/reducer_threads.go
//
// Thread-family reducer for App.Update (Phase 4h).
//
// Owns the ten Update arms that drive the thread panel, the
// threads-list view, and the thread-reply send path:
//
//	ThreadMarkedRemoteMsg       - apply a remote subscriptions.thread.mark
//	                              echo to the local read state.
//	ThreadMarkedLocalMsg        - outcome of an slk-initiated
//	                              subscriptions.thread.mark: apply the
//	                              accepted cursor, or log and leave the
//	                              read state alone when Slack rejected it.
//	threadFetchDebounceMsg      - debounced j/k stop: fire the actual
//	                              thread fetch (drops stale generations
//	                              and post-navigation ticks).
//	ThreadRepliesLoadedMsg      - replies fetch returned: refresh the
//	                              panel, mark the thread as read, and
//	                              refresh the sidebar badge.
//	ThreadsViewActivatedMsg     - user opened the threads-list view:
//	                              switch view + focus, kick a list
//	                              fetch, open the highlighted thread.
//	ThreadsListLoadedMsg        - threads-list fetch returned: push
//	                              summaries + refresh badge, re-open
//	                              the highlighted thread if visible.
//	ThreadsListDirtyMsg         - a debounced "list might be stale"
//	                              trigger: kick a refresh fetch.
//	SendThreadReplyMsg          - user sent a reply: optimistic
//	                              placeholder + chat.postMessage call.
//	ThreadReplySentMsg          - reply landed: swap placeholder for
//	                              authoritative message, bump parent
//	                              reply count, mark threads list dirty.
//	ThreadReplySendFailedMsg    - reply failed: roll back the
//	                              placeholder + fire SendFailed toast.
//
// Free reducer (not controller-absorbed): these arms cooperate on
// the thread panel, the threads-list view, the sidebar's threads
// badge, the active channel's messages pane, the self-send dedup,
// and the threads service. No single existing controller owns all
// of that cross-section, and creating one would be a rename rather
// than an extraction (see Phase 3's WorkspaceService skip rationale).
//
// The helpers (applyThreadMark, scheduleThreadsDirty,
// openSelectedThreadCmd) stay on App; this reducer calls them via
// `a`.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/slack/mrkdwn"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/statusbar"
)

var reduceThreads reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case ThreadMarkedRemoteMsg:
		a.applyThreadMark(m.ChannelID, m.ThreadTS, m.LastRead)
		return nil, true

	case ThreadMarkedLocalMsg:
		if m.Err != nil {
			debuglog.Cache("ThreadMarkedLocalMsg: channel=%s thread_ts=%s failed: %v",
				m.ChannelID, m.ThreadTS, m.Err)
			return nil, true
		}
		a.applyThreadMark(m.ChannelID, m.ThreadTS, m.TS)
		return nil, true

	case threadFetchDebounceMsg:
		// Drop stale debounce ticks: a later j/k has scheduled a
		// fresh fetch and bumped the generation past this one.
		if m.gen != a.pendingThreadFetchGen {
			return nil, true
		}
		// Also drop if the user has navigated away (e.g. switched
		// to a different thread or closed the threads view) since
		// scheduling.
		if m.channelID != a.lastOpenedChannelID || m.threadTS != a.lastOpenedThreadTS {
			return nil, true
		}
		threads := a.threads
		chID := ids.ChannelID(m.channelID)
		threadTS := ids.ThreadTS(m.threadTS)
		parentTS := m.threadTS
		var batch []tea.Cmd
		if cached := threads.CacheRead(chID, threadTS); len(cached) > 1 {
			replies := cached[1:] // strip parent; reducer expects replies-only
			batch = append(batch, func() tea.Msg {
				return ThreadRepliesLoadedMsg{ThreadTS: parentTS, Replies: replies}
			})
		}
		batch = append(batch, func() tea.Msg { return threads.Fetch(chID, threadTS) })
		return tea.Batch(batch...), true

	case ThreadRepliesLoadedMsg:
		if !(a.threadVisible && m.ThreadTS == a.threadPanel.ThreadTS()) {
			return nil, true
		}
		// nil Replies signals network failure (the fetcher logs the
		// error and returns nil); empty []MessageItem{} signals
		// "no replies yet". Skip the panel update on failure so a
		// transient blip doesn't blank a successfully-rendered
		// cached thread view.
		if m.Replies == nil {
			return nil, true
		}
		channelID := a.threadPanel.ChannelID()
		parentMsg := a.threadPanel.ParentMsg()
		// Permalink-opened threads start with a stub parent (TS only).
		// The fetch that produced this msg also wrote the full thread
		// to cache — backfill the parent row from there.
		if parentMsg.Text == "" {
			if cached := a.threads.CacheRead(ids.ChannelID(channelID), ids.ThreadTS(m.ThreadTS)); len(cached) > 0 && cached[0].Text != "" {
				parentMsg = cached[0]
			}
		}
		a.threadPanel.SetThread(parentMsg, m.Replies, channelID, m.ThreadTS)

		// Mark the thread as read now that the user has actually
		// seen the replies. Server-side: subscriptions.thread.mark
		// with the latest reply ts (or the parent ts when the thread
		// has no replies); the returned cmd persists the cursor and
		// reports back as ThreadMarkedLocalMsg.
		// Local-side: clear the Unread flag in the threads-list
		// view and refresh the sidebar's threads-row badge so the
		// UI reflects the change immediately, regardless of which
		// path (messages pane or threads view) opened the thread.
		latestTS := m.ThreadTS
		if n := len(m.Replies); n > 0 {
			if t := m.Replies[n-1].TS; t != "" {
				latestTS = t
			}
		}
		var cmd tea.Cmd
		if channelID != "" && m.ThreadTS != "" {
			cmd = a.threads.Mark(
				ids.ChannelID(channelID),
				ids.ThreadTS(m.ThreadTS),
				ids.MessageTS(latestTS),
			)
		}
		// Optimistic local recompute so the badge updates immediately
		// rather than waiting on the round-trip. Usually this clears
		// Unread, but it re-derives the flag from latestTS, so it can
		// also set it (a reply that landed after the summary's
		// LastReplyTS leaves the thread legitimately unread).
		// ThreadMarkedLocalMsg reconciles this against the cursor Slack
		// accepted on success; a rejected mark leaves state untouched
		// and heals on the next list refresh.
		if a.threadsView.MarkByThreadTSReadAt(channelID, m.ThreadTS, latestTS) {
			a.sidebar.SetThreadsUnreadCount(a.threadsView.UnreadCount())
		}
		return cmd, true

	case ThreadsViewActivatedMsg:
		_ = m
		a.view = ViewThreads
		a.sidebar.SetThreadsActive(true)
		a.focusedPanel = PanelMessages
		var batch []tea.Cmd
		if a.activeTeamID != "" {
			threads := a.threads
			team := ids.TeamID(a.activeTeamID)
			// Activation is the sync's safety-net trigger (boot via
			// workspace-ready is the primary one). The implementation
			// throttles, so this fires unconditionally; it returns
			// immediately and refreshes the list via
			// ThreadsListDirtyMsg when a fetch lands, so the ListFetch
			// below still renders from cache first.
			batch = append(batch, func() tea.Msg {
				threads.EnsureSubscriptions(team)
				return nil
			})
			batch = append(batch, func() tea.Msg { return threads.ListFetch(team) })
		}
		// Activation is a single event -- fire the fetch immediately
		// so the right thread panel populates without artificial delay.
		if cmd := a.openSelectedThreadCmd(false); cmd != nil {
			batch = append(batch, cmd)
		}
		if len(batch) == 0 {
			return nil, true
		}
		return tea.Batch(batch...), true

	case ThreadsListLoadedMsg:
		if m.TeamID != a.activeTeamID {
			return nil, true
		}
		a.threadsView.SetSummaries(m.Summaries)
		a.threadsView.SetSubscriptionsAvailable(m.SubscriptionsAvailable)
		a.sidebar.SetThreadsUnreadCount(a.threadsView.UnreadCount())
		if a.view != ViewThreads {
			return nil, true
		}
		// List reload is a single event; if the dedup short-circuits
		// no fetch happens anyway. Don't add 200ms latency here.
		if cmd := a.openSelectedThreadCmd(false); cmd != nil {
			return cmd, true
		}
		return nil, true

	case ThreadsListDirtyMsg:
		if m.TeamID != a.activeTeamID {
			return nil, true
		}
		threads := a.threads
		team := ids.TeamID(a.activeTeamID)
		return func() tea.Msg { return threads.ListFetch(team) }, true

	case SendThreadReplyMsg:
		a.selfSend.MarkInFlight(m.ChannelID)
		// Instant-display: append an optimistic placeholder to the
		// thread panel immediately, before the chat.postMessage HTTP
		// round-trip. Mirrors the SendMessageMsg path; see there for
		// the LocalTS / swap-or-remove contract and the
		// mrkdwn.Convert rationale.
		localTS := a.selfSend.NextLocalTS()
		optimisticText, _ := mrkdwn.Convert(m.Text)
		if a.threadVisible && m.ThreadTS == a.threadPanel.ThreadTS() && m.ChannelID == a.threadPanel.ChannelID() {
			a.threadPanel.AddReply(messages.MessageItem{
				TS:        localTS,
				UserID:    a.currentUserID,
				UserName:  a.userNameFor(a.currentUserID),
				Text:      optimisticText,
				Timestamp: a.nowFormatted(),
				ThreadTS:  m.ThreadTS,
			})
		}
		threads := a.threads
		chID := ids.ChannelID(m.ChannelID)
		ts := ids.ThreadTS(m.ThreadTS)
		text := m.Text
		return func() tea.Msg {
			result := threads.SendReply(chID, ts, text)
			switch r := result.(type) {
			case ThreadReplySentMsg:
				r.LocalTS = localTS
				return r
			case ThreadReplySendFailedMsg:
				r.LocalTS = localTS
				return r
			}
			return result
		}, true

	case ThreadReplySentMsg:
		// chat.postMessage for the thread reply landed. If a
		// "local:..." placeholder is in the thread panel from the
		// instant-display path (SendThreadReplyMsg above), swap it
		// for the authoritative message; otherwise fall back to
		// UpsertSelfSentReply.
		//
		// Note: the internal Slack flannel WebSocket does not always
		// echo self-posted thread replies as a plain "message" event,
		// so we cannot rely on the WS echo alone -- the HTTP response
		// must apply all the side effects (parent reply count, threads
		// dirty) here.
		if m.Message.TS == "" {
			return nil, true
		}
		a.selfSend.RecordSent(m.Message.TS)
		// Update the thread panel whenever the visible thread
		// matches, regardless of activeChannelID. When a thread is
		// opened from the threads view, activeChannelID is not
		// switched to the thread's channel, so gating on it here
		// meant the user's own reply was sent to Slack but never
		// appended locally -- they had to leave and re-enter the
		// thread to see it.
		if a.threadVisible && m.ThreadTS == a.threadPanel.ThreadTS() && m.ChannelID == a.threadPanel.ChannelID() {
			if !a.threadPanel.SwapLocalSentReply(m.LocalTS, m.Message) {
				a.threadPanel.UpsertSelfSentReply(m.Message)
			}
		}
		for _, mm := range a.modelsForChannel(m.ChannelID) {
			mm.IncrementReplyCount(m.ThreadTS, m.Message.TS)
		}
		if c := a.scheduleThreadsDirty(); c != nil {
			return c, true
		}
		return nil, true

	case ThreadReplySendFailedMsg:
		// chat.postMessage for the thread reply failed; roll back
		// the optimistic placeholder. Mirrors MessageSendFailedMsg.
		if a.threadVisible && m.ThreadTS == a.threadPanel.ThreadTS() && m.ChannelID == a.threadPanel.ChannelID() && m.LocalTS != "" {
			a.threadPanel.RemoveLocalSentReply(m.LocalTS)
		}
		reason := m.Reason
		return func() tea.Msg {
			return statusbar.SendFailedMsg{Reason: reason}
		}, true
	}
	return nil, false
}
