package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/notify"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

// rtmEventHandler bridges WebSocket events into bubbletea messages via p.Send()
// and caches all incoming messages to the SQLite database.
type rtmEventHandler struct {
	// program is the running *tea.Program, narrowed to teaSender so
	// tests can capture what the handler dispatches.
	program     teaSender
	userNames   map[string]string
	tsFormat    string
	db          *cache.DB
	workspaceID string
	connected   bool
	isActive    func() bool

	// Notifications
	notifier        *notify.Notifier
	notifyCfg       config.Notifications
	currentUserID   string
	channelNames    map[string]string
	channelTypes    map[string]string
	workspaceName   string
	activeChannelID func() string

	// cfg is the loaded user config; used by OnConversationOpened to
	// resolve sidebar section + section order via buildChannelItem.
	cfg config.Config

	// Back-reference for self-presence/DND state mutation.
	wsCtx *WorkspaceContext

	// backfillGate enforces a 30 s minimum between reconnect-driven
	// catch-up passes. Per-handler so each workspace has its own gate.
	// Initialized at construction with window = 30 * time.Second.
	backfillGate dedupeGate

	// refreshChannel reloads one channel from the server through the
	// same path a channel switch uses, and pushes the result into the
	// UI. The reconnect handler calls it for the channel on screen and
	// for nothing else — that is the whole of slk's post-reconnect
	// network work, alongside one client.counts.
	//
	// nil in tests that construct a handler for unrelated events.
	refreshChannel func(ctx context.Context, channelID string)

	// ensureThreadSubs kicks the workspace's throttled thread-
	// subscription sync (subscriptions.thread.getView). The reconnect
	// catch-up fires it on every pass — the threadSubsGate inside
	// owns the real throttling, so the kick itself is free.
	//
	// nil in tests that construct a handler for unrelated events.
	ensureThreadSubs func()

	// resolveConversation is conversations.info, for discoverConversation.
	// nil in tests that construct a handler for unrelated events.
	resolveConversation func(ctx context.Context, channelID string) (*slack.Channel, error)
	// lookupRetryAt holds when a refused or rate-limited discovery
	// lookup may run again. WebSocket goroutine only.
	lookupRetryAt map[string]time.Time
}

const discoveryRetryAfter = time.Minute

// discoverConversation adds a conversation the first time a message
// arrives on one slk does not know, and reports whether it did. The
// conversation-opened events are not enough: a group DM another user
// created mid-session delivered its messages here without ever getting
// a sidebar row. The message is the signal proven to arrive.
//
// A delivered message is treated as membership, so there is no
// is_member check (conversations.info has none for ims anyway).
func (h *rtmEventHandler) discoverConversation(channelID string) (sidebar.ChannelItem, channelfinder.Item, bool) {
	if h.resolveConversation == nil {
		return sidebar.ChannelItem{}, channelfinder.Item{}, false
	}
	if _, known := h.channelTypes[channelID]; known {
		return sidebar.ChannelItem{}, channelfinder.Item{}, false
	}
	if time.Now().Before(h.lookupRetryAt[channelID]) {
		return sidebar.ChannelItem{}, channelfinder.Item{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := h.resolveConversation(ctx, channelID)
	if err != nil {
		log.Printf("workspace %s: looking up the conversation of an incoming message: %v", h.workspaceID, err)
		// A refusal or a rate limit will not clear by the next message,
		// and retrying each one would cost a blocking request per message
		// on a busy channel. A network error or timeout may clear, and a
		// quick burst of messages can be the only retry it gets.
		var rateLimited *slack.RateLimitedError
		var refused slack.SlackErrorResponse
		var wait time.Duration
		switch {
		case errors.As(err, &rateLimited):
			wait = rateLimited.RetryAfter
		case errors.As(err, &refused):
			wait = discoveryRetryAfter
		}
		if wait > 0 {
			if h.lookupRetryAt == nil {
				h.lookupRetryAt = map[string]time.Time{}
			}
			h.lookupRetryAt[channelID] = time.Now().Add(wait)
		}
		return sidebar.ChannelItem{}, channelfinder.Item{}, false
	}
	delete(h.lookupRetryAt, channelID)
	debuglog.WS("discovered conversation from message: team=%s channel=%s mpim=%v im=%v", h.workspaceID, ch.ID, ch.IsMpIM, ch.IsIM)
	return h.addConversation(*ch)
}

func (h *rtmEventHandler) OnMessage(channelID, userID, ts, text, threadTS, subtype string, edited bool, files []slack.File, blocks slack.Blocks, attachments []slack.Attachment, botID, username string) {
	// Bot messages (bot_message) carry no user, only a bot_id + username.
	// Key the row on the bot_id and resolve its avatar/name via bots.info,
	// mirroring the fetch-path messageAuthor helper.
	authorID := userID
	if authorID == "" && botID != "" {
		authorID = botID
		if h.wsCtx != nil && h.wsCtx.UserResolver != nil {
			h.wsCtx.UserResolver.RequestBot(botID, username)
		}
	}
	// Synchronous, so the channel's row and type exist before the writes
	// below. The UI hears of it only after the unread write.
	discovered, discoveredFinder, isNew := h.discoverConversation(channelID)
	// Cache every message to SQLite, regardless of active workspace.
	// Guard against nil db so handlers constructed in tests (without
	// real persistence) don't panic.
	if h.db != nil {
		synthetic := slack.Message{Msg: slack.Msg{
			Type:            "message",
			Timestamp:       ts,
			User:            authorID,
			Text:            text,
			ThreadTimestamp: threadTS,
			SubType:         subtype,
			Files:           files,
			Blocks:          blocks,
			Attachments:     attachments,
		}}
		rawBytes, _ := json.Marshal(synthetic)
		h.db.UpsertMessage(cache.Message{
			TS:          ts,
			ChannelID:   channelID,
			WorkspaceID: h.workspaceID,
			UserID:      authorID,
			Text:        text,
			ThreadTS:    threadTS,
			Subtype:     subtype,
			RawJSON:     string(rawBytes),
			CreatedAt:   time.Now().Unix(),
		})
		if err := h.db.SetChannelSyncedAt(channelID, time.Now().Unix()); err != nil {
			debuglog.Cache("OnMessage: SetChannelSyncedAt %s: %v", channelID, err)
		}
		// Advance the per-channel ts watermark used by reconnect
		// backfill. Slack delivers WS messages in order, so receipt
		// of a message with ts=X implies we have no missing messages
		// with ts <= X on this channel — that is exactly the
		// invariant latest_synced_ts encodes. AdvanceChannelLatestSyncedTS
		// is no-regress, so out-of-order replay (e.g., a delayed
		// duplicate after reconnect) won't move the cursor backward.
		if _, err := h.db.AdvanceChannelLatestSyncedTS(channelID, ts); err != nil {
			debuglog.Cache("OnMessage: AdvanceChannelLatestSyncedTS %s ts=%s: %v", channelID, ts, err)
		}
	}

	// Check if this message should trigger a desktop notification.
	// Do this before the active workspace check so inactive workspaces
	// can still trigger notifications.
	if h.notifier != nil && h.notifyCfg.Enabled {
		isActiveWS := h.isActive != nil && h.isActive()
		activeChID := ""
		if h.activeChannelID != nil {
			activeChID = h.activeChannelID()
		}
		ctx := notify.NotifyContext{
			CurrentUserID:   h.currentUserID,
			ActiveChannelID: activeChID,
			IsActiveWS:      isActiveWS,
			OnMention:       h.notifyCfg.OnMention,
			OnDM:            h.notifyCfg.OnDM,
			OnKeyword:       h.notifyCfg.OnKeyword,
			IsDND:           h.wsCtx != nil && h.wsCtx.DNDEnabled && (h.wsCtx.DNDEndTS.IsZero() || time.Now().Before(h.wsCtx.DNDEndTS)),
			IsMuted:         h.wsCtx != nil && h.wsCtx.MuteStore != nil && h.wsCtx.MuteStore.IsMuted(channelID),
		}
		chType := h.channelTypes[channelID]
		// Pass the raw userID (not authorID): ShouldNotify's self-message
		// suppression keys on the human sender, and a bot message
		// (userID == "", authorID == botID) can never be "you" — so the
		// empty userID is intentional, not a bug to "fix" later.
		if notify.ShouldNotify(ctx, channelID, userID, text, chType) {
			senderName := authorID
			if resolved, ok := h.userNames[authorID]; ok {
				senderName = resolved
			} else if username != "" {
				senderName = username
			}
			chName := h.channelNames[channelID]
			title := h.workspaceName + ": #" + chName
			if chType == "dm" || chType == "group_dm" {
				title = h.workspaceName + ": " + senderName
			}
			var groupNames map[string]string
			if h.wsCtx != nil {
				groupNames = h.wsCtx.UserGroups()
			}
			body := senderName + ": " + notify.StripSlackMarkupWithUserGroups(text, h.userNames, groupNames)
			go func() {
				if err := h.notifier.Notify(title, body); err != nil {
					debuglog.Notify("notification failed: %v", err)
				}
			}()
		}
	}

	// Read-state: mark the channel has_unread=true for every eligible
	// message. Mirrors Slack's channel-unread semantics — non-broadcast
	// thread replies do not mark the parent channel unread (only
	// top-level messages and thread_broadcast subtypes do), and neither
	// your own sends nor edits of existing messages do (see the two
	// exclusions below).
	//
	// There is deliberately NO active-channel exemption here. Whether
	// the user can actually see the active channel depends on terminal
	// focus, which is only known on the UI goroutine. reduceNewMessage
	// owns that decision and clears this flag by marking read when the
	// terminal is focused; if the mark fails, or the terminal is
	// blurred, the flag correctly stands.
	//
	// This write runs for BOTH active and inactive workspaces; the
	// active/inactive split below only governs the UI dispatch path,
	// not durable read state.
	isThreadReply := threadTS != "" && threadTS != ts
	isBroadcast := subtype == "thread_broadcast"
	channelEligible := !isThreadReply || isBroadcast
	// Self-sends and edit echoes are excluded because reduceNewMessage
	// returns BEFORE its read-state tail for both (reducer_send.go, the
	// IsEdited and IsSelfSent arms). Nothing on the UI side would ever
	// clear a flag set here, so it would stick until the next channel
	// entry — the dot would appear on the channel you just posted in.
	//
	// Your own message never makes a channel unread on Slack, whichever
	// client sent it, so the exclusion is global rather than scoped to
	// the active channel.
	//
	// The userID != "" guard is defensive, not load-bearing. A bot
	// message carries userID == "", so a bare equality would treat
	// every one of them as self-authored — and silently stop flagging
	// their channels — the moment h.currentUserID were empty. That
	// cannot happen today: currentUserID is assigned once in the
	// handler's struct literal (main.go:2076) from wctx.UserID and is
	// never reassigned, so no message can reach this handler before it
	// is populated. The adjacent notification path ships the unguarded
	// form (internal/notify/notifier.go:68) without incident, which
	// corroborates that. The guard is kept anyway because the failure
	// it prevents is silent, it costs one comparison, and it matches
	// App.isOwnMessage (internal/ui/app.go:3082).
	//
	// Keyed on the raw userID for the same reason the notification
	// block above is: "did I write this" is a question about the human
	// sender. authorID would behave identically today, since it only
	// diverges for bot messages and a B-prefixed bot ID cannot equal a
	// U-prefixed user ID — the choice is about intent, not a
	// behavioral difference.
	isSelfMessage := userID != "" && userID == h.currentUserID
	// edited is true only for message_changed
	// (internal/slack/events.go:307): a re-delivery of a message the
	// channel already accounted for. Editing it does not make the
	// channel unread on Slack.
	shouldMarkChannel := channelEligible && !isSelfMessage && !edited
	if h.db != nil && shouldMarkChannel {
		if err := h.db.UpdateChannelReadState(channelID, "", true); err != nil {
			log.Printf("Warning: failed to set has_unread for %s: %v", channelID, err)
		}
		// Mention badge: bump the count when this message mentions the
		// user. Deliberately nested inside the has_unread write's own
		// gate rather than repeating its conditions, so the dot and the
		// badge can never disagree about whether a message "arrived
		// unread". Everything shouldMarkChannel excludes -- thread
		// replies that are not broadcasts, self-authored messages, and
		// edits -- is excluded from the badge for free.
		//
		// That an edit cannot badge is inherited, not incidental: a
		// message_changed re-delivery does not make a channel unread on
		// Slack, so a mention added by editing an existing message
		// surfaces at the next client.counts refresh rather than
		// immediately. Consistency with the dot is worth more than
		// immediacy here.
		//
		// Conversation type decides what counts. Slack reports every
		// unread message in mention_count for ims and mpims, and only
		// @-mentions for channels; matching that split here keeps local
		// increments consistent with the server value that will later
		// overwrite them. That split is unverified against a live
		// capture — see UnreadInfo's doc in internal/slack/client.go.
		//
		// "app" belongs in the DM branch because it is not one of
		// Slack's conversation kinds: buildChannelItem invents it for an
		// is_im conversation whose peer is a bot, purely so the sidebar
		// can group Apps separately. Slack reports human DMs and app DMs
		// alike in the `ims` block, so omitting "app" here would badge
		// an app DM from the server at boot and then never increment it
		// live. See "Conversation types: Slack's three kinds vs slk's
		// five" in docs/superpowers/specs/2026-09-09-mention-badges-design.md.
		//
		// No self-author check here: isSelfMessage above already
		// excludes it from shouldMarkChannel, so a duplicate test would
		// be dead code that a future reader could "fix" in one place and
		// not the other. A bot message still reaches this line, since
		// isSelfMessage's userID != "" guard lets it through, and
		// mention.InText's empty-self guard makes the direct-mention
		// probe a no-op for it while still honouring @here/@channel.
		if messageMentionsSelf(h.channelTypes[channelID], text, h.currentUserID) {
			if err := h.db.IncrementChannelMentionCount(channelID); err != nil {
				log.Printf("Warning: failed to increment mention count for %s: %v", channelID, err)
			}
		}
	}

	if isNew {
		// The sidebar's staleness filter reads read state when the row
		// arrives, and hides a never-opened DM that is not yet unread.
		h.publishConversation(discovered, discoveredFinder)
	}

	if h.isActive != nil && !h.isActive() {
		// Inactive workspace — durable read state is already settled
		// above (written, or deliberately skipped). Fire a
		// ReadStateChangedMsg so the workspace rail refreshes its dot
		// through railUnreadWorkspaces. The sidebar's Invalidate is
		// a no-op here because the active workspace's sidebar isn't
		// showing this channel anyway.
		switch {
		case shouldMarkChannel:
			debuglog.Cache("OnMessage: team=%s channel=%s ts=%s subtype=%q thread_ts=%s decision=inactive_workspace_persisted",
				h.workspaceID, channelID, ts, subtype, threadTS)
		case !channelEligible:
			debuglog.Cache("OnMessage: team=%s channel=%s ts=%s subtype=%q thread_ts=%s decision=skipped_thread_reply_inactive",
				h.workspaceID, channelID, ts, subtype, threadTS)
		default:
			debuglog.Cache("OnMessage: team=%s channel=%s ts=%s subtype=%q thread_ts=%s decision=skipped_self_or_edit_inactive",
				h.workspaceID, channelID, ts, subtype, threadTS)
		}
		if h.program != nil {
			h.program.Send(ui.ReadStateChangedMsg{
				WorkspaceID: h.workspaceID,
				ChannelID:   channelID,
			})
		}
		return
	}

	userName, ok := resolveUserCached(authorID, h.userNames, h.db)
	if !ok {
		userName = authorID
		if userID != "" {
			if h.wsCtx != nil && h.wsCtx.UserResolver != nil {
				h.wsCtx.UserResolver.Request(userID)
			}
		} else if username != "" {
			// Bot author: show its name immediately; bots.info (already
			// requested above) fills in the avatar.
			userName = username
		}
	}
	debuglog.Cache("OnMessage: team=%s channel=%s ts=%s subtype=%q thread_ts=%s decision=dispatched_to_app",
		h.workspaceID, channelID, ts, subtype, threadTS)
	if h.program != nil {
		h.program.Send(ui.NewMessageMsg{
			ChannelID: channelID,
			Message: messages.MessageItem{
				TS:                ts,
				UserID:            authorID,
				UserName:          userName,
				Text:              text,
				Timestamp:         formatTimestamp(ts, h.tsFormat),
				ThreadTS:          threadTS,
				Subtype:           subtype,
				IsEdited:          edited,
				Attachments:       extractAttachments(files),
				Blocks:            extractBlocks(blocks),
				LegacyAttachments: extractLegacyAttachments(attachments),
			},
		})
	}
}

func (h *rtmEventHandler) OnMessageDeleted(channelID, ts string) {
	if err := h.db.DeleteMessage(channelID, ts); err != nil {
		log.Printf("Warning: failed to soft-delete cached message %s/%s: %v", channelID, ts, err)
	}
	if h.isActive != nil && !h.isActive() {
		// Inactive workspace — nothing to update in the UI.
		return
	}
	h.program.Send(ui.WSMessageDeletedMsg{ChannelID: channelID, TS: ts})
}

func (h *rtmEventHandler) OnReactionAdded(channelID, ts, userID, emojiName string) {
	// Update cache regardless of active state
	rows, err := h.db.GetReactions(ts, channelID)
	if err == nil {
		found := false
		for _, r := range rows {
			if r.Emoji == emojiName {
				userIDs := append(r.UserIDs, userID)
				_ = h.db.UpsertReaction(ts, channelID, emojiName, userIDs, r.Count+1)
				found = true
				break
			}
		}
		if !found {
			_ = h.db.UpsertReaction(ts, channelID, emojiName, []string{userID}, 1)
		}
	}

	if h.isActive != nil && !h.isActive() {
		return
	}

	h.program.Send(ui.ReactionAddedMsg{
		ChannelID: channelID,
		MessageTS: ts,
		UserID:    userID,
		Emoji:     emojiName,
	})
}

func (h *rtmEventHandler) OnReactionRemoved(channelID, ts, userID, emojiName string) {
	// Update cache regardless of active state
	rows, err := h.db.GetReactions(ts, channelID)
	if err == nil {
		for _, r := range rows {
			if r.Emoji == emojiName {
				var newUserIDs []string
				for _, uid := range r.UserIDs {
					if uid != userID {
						newUserIDs = append(newUserIDs, uid)
					}
				}
				if len(newUserIDs) == 0 {
					_ = h.db.DeleteReaction(ts, channelID, emojiName)
				} else {
					_ = h.db.UpsertReaction(ts, channelID, emojiName, newUserIDs, r.Count-1)
				}
				break
			}
		}
	}

	if h.isActive != nil && !h.isActive() {
		return
	}

	h.program.Send(ui.ReactionRemovedMsg{
		ChannelID: channelID,
		MessageTS: ts,
		UserID:    userID,
		Emoji:     emojiName,
	})
}

func (h *rtmEventHandler) OnUserTyping(channelID, userID string) {
	if h.program == nil {
		return
	}
	h.program.Send(ui.UserTypingMsg{
		ChannelID:   channelID,
		UserID:      userID,
		WorkspaceID: h.workspaceID,
	})
}
