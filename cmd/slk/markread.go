package main

import (
	"context"
	"log"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/mention"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
)

// threadMarker is the single Slack operation the thread mark-read path
// needs. Narrowed to an interface for the same reason as channelMarker
// below: it makes the "did Slack accept?" branch testable without real
// HTTP wiring.
type threadMarker interface {
	MarkThread(ctx context.Context, channelID, threadTS, ts string) error
}

// markThreadRead calls subscriptions.thread.mark and, ONLY if Slack
// accepts it, advances the local thread_subscriptions cursor. Before
// this gate existed the cursor advanced solely via the thread_marked WS
// echo, and a lost echo left last_read stale enough to flip the thread
// back to unread on the next threads-list refresh.
//
// A failed local write is logged, not returned: Slack accepted the mark,
// so the thread genuinely is read and the cache heals on the next
// getView reconcile. Only a rejected mark is an error, because that is
// the case where the UI must not clear the unread flag.
func markThreadRead(ctx context.Context, client threadMarker, db *cache.DB, teamID, channelID, threadTS, ts string) error {
	if err := client.MarkThread(ctx, channelID, threadTS, ts); err != nil {
		return err
	}
	if db != nil {
		// ...IfExists, not UpdateThreadLastRead: this path fires for
		// ANY thread the user opens, including ones opened from the
		// messages pane that they never subscribed to. The inserting
		// variant would fabricate an active=1 row and put a phantom
		// entry in the Threads list.
		//
		// Slack echoes this very mark back as thread_marked, so the
		// guard only holds because OnThreadMarked applies the same
		// rule to the echo, using the event's own subscription flag.
		if err := db.UpdateThreadLastReadIfExists(teamID, channelID, threadTS, ts); err != nil {
			debuglog.Cache("markThreadRead: UpdateThreadLastReadIfExists %s/%s: %v",
				channelID, threadTS, err)
		}
	}
	return nil
}

// channelMarker is the single Slack operation the mark-read path needs.
// Narrowing it to an interface (rather than taking *WorkspaceContext and
// reaching through to a concrete *slackclient.Client) is what makes the
// failure path testable without real HTTP wiring.
type channelMarker interface {
	MarkChannel(ctx context.Context, channelID, ts string) error
}

// markChannelRead calls conversations.mark and, ONLY if Slack accepts
// it, persists the local read state. On failure the channel stays
// unread locally, which is the honest state: it reconciles on the next
// channel entry or reconnect sync. Synchronous so the failure path is
// deterministically testable; markChannelReadAsync is the goroutine
// wrapper.
// messageMentionsSelf reports whether a message in a conversation of the
// given slk type counts toward that conversation's mention badge.
//
// Conversation type decides what counts. Slack reports every unread
// message in mention_count for ims and mpims, and only @-mentions for
// channels; matching that split locally keeps increments consistent with
// the server value that will later overwrite them. That split is
// unverified against a live capture — see UnreadInfo's doc in
// internal/slack/client.go.
//
// "app" belongs in the DM branch because it is not one of Slack's
// conversation kinds: buildChannelItem invents it for an is_im
// conversation whose peer is a bot, purely so the sidebar can group Apps
// separately. Slack reports human DMs and app DMs alike in the `ims`
// block. See "Conversation types: Slack's three kinds vs slk's five" in
// docs/superpowers/specs/2026-09-09-mention-badges-design.md.
//
// Deliberately says nothing about authorship or read state. Callers own
// those: the live path inherits them from the has_unread gate, and the
// mark-unread path applies its own.
func messageMentionsSelf(chType, text, selfUserID string) bool {
	switch chType {
	case "dm", "group_dm", "app":
		return true
	default:
		return mention.InText(text, selfUserID)
	}
}

// countMentionsSince counts the cached messages at or after sinceTS that
// mention the user, for the mark-unread path.
//
// Mark-unread moves the read boundary to a message the user picked off
// their own screen, so the messages it makes unread are exactly the ones
// just rendered — cached by construction. Counting them is arithmetic on
// data slk holds, not a guess.
//
// Self-authored messages are excluded to match the live path, where
// isSelfMessage keeps them out of the shared read-state gate.
func countMentionsSince(db *cache.DB, chType, channelID, sinceTS, selfUserID string) (int, error) {
	msgs, err := db.GetMessagesSince(channelID, sinceTS)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range msgs {
		if m.UserID != "" && m.UserID == selfUserID {
			continue
		}
		if messageMentionsSelf(chType, m.Text, selfUserID) {
			n++
		}
	}
	return n, nil
}

func markChannelRead(ctx context.Context, client channelMarker, db *cache.DB, channelID, ts string) error {
	if err := client.MarkChannel(ctx, channelID, ts); err != nil {
		return err
	}
	if db != nil {
		if err := db.UpdateChannelReadState(channelID, ts, false); err != nil {
			log.Printf("Warning: failed to update read state in markChannelRead %s/%s: %v", channelID, ts, err)
		}
		// Reading the channel clears its mention badge. Slack echoes a
		// *_marked event with mention_count=0 shortly after, which
		// would do this anyway — but doing it here means the badge
		// clears on the same render as the dot instead of one round
		// trip later.
		//
		// Inside the post-MarkChannel branch deliberately: a failed
		// mark returns above, so a badge is never cleared for a read
		// Slack did not accept. That is the same guarantee the
		// has_unread write above relies on.
		if err := db.SetChannelMentionCount(channelID, 0); err != nil {
			log.Printf("Warning: failed to clear mention count in markChannelRead %s: %v", channelID, err)
		}
	}
	return nil
}

// markChannelReadAndNotify marks the channel read and, only on success,
// hands ChannelMarkedReadMsg to notify. A rejected mark must not notify:
// the UI would optimistically clear an unread badge that Slack still
// considers unread.
//
// notify is a plain func rather than a *tea.Program so this whole
// decision stays synchronous and testable. Taking the program here
// would push the send decision back into the goroutine and make
// "did not notify" assertable only with a sleep.
func markChannelReadAndNotify(
	ctx context.Context,
	client channelMarker,
	db *cache.DB,
	notify func(tea.Msg),
	channelID, ts string,
) {
	if err := markChannelRead(ctx, client, db, channelID, ts); err != nil {
		log.Printf("Warning: conversations.mark %s/%s failed, leaving channel unread: %v", channelID, ts, err)
		return
	}
	if notify != nil {
		notify(ui.ChannelMarkedReadMsg{ChannelID: channelID})
	}
}

// markChannelReadAsync runs markChannelReadAndNotify in a background
// goroutine and returns immediately. The goroutine is required: p.Send
// blocks until the Update goroutine receives (bubbletea v2's program
// channel is unbuffered), so this must never run on Update.
//
// client must be non-nil and non-typed-nil: the guard below catches only
// an untyped nil interface, so a nil *slackclient.Client boxed into a
// channelMarker would pass it and panic inside the goroutine. Callers
// check wctx.Client themselves.
func markChannelReadAsync(
	ctx context.Context,
	client channelMarker,
	db *cache.DB,
	p *tea.Program,
	channelID, ts string,
) {
	if client == nil || ts == "" {
		return
	}
	var notify func(tea.Msg)
	if p != nil {
		notify = func(m tea.Msg) { p.Send(m) }
	}
	go markChannelReadAndNotify(ctx, client, db, notify, channelID, ts)
}

func (h *rtmEventHandler) OnChannelMarked(channelID, ts string, unreadCount, mentionCount int) {
	// Slack's *_marked events fire in BOTH directions: when the user
	// reads a channel (unreadCount=0) AND when the user marks one
	// unread (unreadCount>0). The event payload's
	// `unread_count_display` tells us which case we're in. We must
	// use it instead of always clearing the unread flag — the
	// original spec hardcoded false here, which meant a remote
	// mark-unread (via another client) silently cleared slk's dot.
	hasUnread := unreadCount > 0
	// Persist regardless of active workspace so the cache stays
	// authoritative across workspace switches.
	if err := h.db.UpdateChannelReadState(channelID, ts, hasUnread); err != nil {
		log.Printf("Warning: failed to update read state on channel_marked %s/%s: %v", channelID, ts, err)
	}
	// The event's mention_count is authoritative and replaces whatever
	// the local increment path accumulated, which is how @usergroup
	// undercounting gets corrected. A read event carries 0 and clears
	// the badge.
	if err := h.db.SetChannelMentionCount(channelID, mentionCount); err != nil {
		log.Printf("Warning: failed to set mention count on channel_marked %s: %v", channelID, err)
	}
	if h.program != nil {
		// Always notify so the workspace rail can refresh, regardless
		// of whether this workspace is active. The active-workspace
		// sidebar refresh and toast come from ChannelMarkedRemoteMsg
		// below; the rail refresh comes from ReadStateChangedMsg's
		// App.Update handler.
		h.program.Send(ui.ReadStateChangedMsg{WorkspaceID: h.workspaceID, ChannelID: channelID})
	}
	if h.isActive != nil && !h.isActive() {
		// Inactive workspace: persistence + rail refresh above are
		// the only visible effects; no sidebar/toast to update.
		return
	}
	if h.program == nil {
		return
	}
	h.program.Send(ui.ChannelMarkedRemoteMsg{
		ChannelID:   channelID,
		TS:          ts,
		UnreadCount: unreadCount,
	})
}

// OnThreadMarked persists a thread read-cursor move from Slack's
// thread_marked event. It writes last_read ONLY: `active` is owned by
// thread_subscribed / thread_unsubscribed / the getView reconcile.
// Writing `active` here used to tombstone the row on every read, which
// made the thread vanish from the Threads list until the next sweep.
//
// subscribed (Slack's subscription.active) chooses the cursor writer,
// and does nothing else. It is a subscription signal, never a
// read/unread one: whether the thread is unread stays a comparison of
// last_read against the newest known reply, computed downstream.
//
//   - subscribed: UpdateThreadLastRead, which inserts a missing row.
//     Slack says the user is subscribed, so a row the local cache lacks
//     is a gap to reconstruct rather than a phantom to invent.
//   - not subscribed: UpdateThreadLastReadIfExists, which never
//     inserts. slk's own subscriptions.thread.mark echoes back here, so
//     this handler sees marks for threads the user merely opened from
//     the messages pane and never subscribed to. markThreadRead
//     deliberately writes no row for those; inserting one here would
//     undo that guard one hop later and surface a phantom entry in the
//     Threads list, which filters on active=1.
//
// Neither writer touches `active` on a row that already exists, so a
// tombstoned row stays tombstoned either way — it takes
// UpdateThreadLastRead's ON CONFLICT path, which sets last_read and
// updated_at only. The subscribed branch's INSERT does create a
// missing row with active=1, which is the whole point of choosing it.
func (h *rtmEventHandler) OnThreadMarked(channelID, threadTS, lastRead string, subscribed slackclient.Subscribed) {
	// An empty cursor would erase the thread's read position and make
	// every reply render unread. UpdateThreadLastRead does not reject
	// it, so drop the event here instead of corrupting the row.
	if lastRead == "" {
		debuglog.Cache("OnThreadMarked: empty last_read for %s/%s, ignoring",
			channelID, threadTS)
		return
	}

	// Persist regardless of active-workspace state, matching OnMessage
	// and OnChannelMarked: dropping the write on inactive workspaces
	// leaves stale read state behind on the next switch.
	if h.db != nil {
		write := h.db.UpdateThreadLastReadIfExists
		writer := "UpdateThreadLastReadIfExists"
		if subscribed {
			write = h.db.UpdateThreadLastRead
			writer = "UpdateThreadLastRead"
		}
		if err := write(h.workspaceID, channelID, threadTS, lastRead); err != nil {
			debuglog.Cache("OnThreadMarked: %s %s/%s: %v",
				writer, channelID, threadTS, err)
		}
	}

	// The list and badge dispatch below is active-only: they live on
	// the active workspace, and an inactive one picks them up on the
	// next switch via threadsListFetcher. The rail is not: its thread
	// half reads the last_read written above (railThreadsUnread), so
	// a thread read in another client while the user is on a
	// different workspace must reach it now, or the dot stays lit
	// until an unrelated event. ReadStateChangedMsg is what
	// notifyReadStateChanged answers to, the same choice
	// muteRefreshMsg makes for an inactive mute change.
	if h.isActive != nil && !h.isActive() {
		if h.program != nil {
			h.program.Send(ui.ReadStateChangedMsg{WorkspaceID: h.workspaceID})
		}
		return
	}
	if h.program == nil {
		return
	}
	h.program.Send(ui.ThreadMarkedRemoteMsg{
		ChannelID: channelID,
		ThreadTS:  threadTS,
		LastRead:  lastRead,
	})
	// The message above carries this thread's cursor only, and its
	// reducer applies it to that one list row — and to the open
	// panel's landmark, but only when the mark came from another
	// client. Every mark slk issues itself is broadcast back here too,
	// and applyThreadMarkEcho deliberately holds the landmark for
	// those rather than dragging it off what the user was reading.
	//
	// This asks for the authoritative recompute of the whole list,
	// from the cache rows the write above just updated. Sent per
	// event with no deduplication here: the reducer coalesces dirty
	// messages on receipt, so an echo landing inside an open refresh
	// window costs no additional ListSubscribedThreads query.
	h.program.Send(ui.ThreadsListDirtyMsg{TeamID: h.workspaceID})
}
