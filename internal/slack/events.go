package slackclient

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/slack-go/slack"
)

// EventHandler processes real-time events from Slack.
type EventHandler interface {
	// OnMessage delivers a new or edited message. subtype mirrors
	// Slack's `subtype` field; "" for normal messages, "bot_message"
	// for bot posts, "thread_broadcast" for thread replies that the
	// author also sent to the main channel. files carries any file
	// attachments on the message (empty for plain text messages).
	OnMessage(channelID, userID, ts, text, threadTS, subtype string, edited bool, files []slack.File, blocks slack.Blocks, attachments []slack.Attachment, botID, username string)
	OnMessageDeleted(channelID, ts string)
	OnReactionAdded(channelID, ts, userID, emoji string)
	OnReactionRemoved(channelID, ts, userID, emoji string)
	OnPresenceChange(userID, presence string)
	OnUserTyping(channelID, userID string)
	OnConnect()
	OnDisconnect()
	OnSelfPresenceChange(presence string)
	OnDNDChange(enabled bool, endUnix int64)

	// OnChannelMarked is delivered when Slack pushes a channel_marked /
	// im_marked / group_marked / mpim_marked event (read state changed
	// in another client, or via slk's own MarkChannel/MarkChannelUnread
	// echoing back). ts is the new last_read watermark; unreadCount is
	// the canonical workspace-side unread count for the channel (use to
	// drive the sidebar badge).
	OnChannelMarked(channelID, ts string, unreadCount int)
	// OnThreadMarked is delivered when Slack pushes a thread_marked
	// event. read indicates whether the thread is now read (true) or
	// unread (false). ts is the new boundary within the thread.
	OnThreadMarked(channelID, threadTS, ts string, read bool)

	// OnThreadSubscriptionChanged is delivered for thread_subscribed and
	// thread_unsubscribed WS events. active=true on subscribe,
	// active=false on unsubscribe. lastRead is the per-thread last_read ts
	// the server reports — pass-through to thread_subscriptions.last_read.
	// The payload shape is identical to thread_marked.subscription, so
	// implementations can share state-update logic with OnThreadMarked
	// (this handler is the persistence-only path; OnThreadMarked also
	// drives the UI's read-state side effects).
	OnThreadSubscriptionChanged(channelID, threadTS, lastRead string, active bool)

	// OnConversationOpened is delivered when a new or previously-closed
	// conversation becomes visible to the user mid-session: mpim_open,
	// im_created, group_joined, or channel_joined. The full slack.Channel
	// payload is forwarded so the receiver can construct a sidebar item
	// without an extra conversations.info round-trip.
	OnConversationOpened(channel slack.Channel)

	// OnChannelSectionUpserted is called for channel_section_upserted
	// WS events: section create, rename, reorder, or emoji change.
	OnChannelSectionUpserted(ev ChannelSectionUpserted)
	// OnChannelSectionDeleted is called for channel_section_deleted.
	OnChannelSectionDeleted(sectionID string)
	// OnChannelSectionChannelsUpserted is called for
	// channel_sections_channels_upserted: one or more channels added
	// to the named section. A channel previously in another section
	// is implicitly moved.
	OnChannelSectionChannelsUpserted(sectionID string, channelIDs []string)
	// OnChannelSectionChannelsRemoved is called for
	// channel_sections_channels_removed.
	OnChannelSectionChannelsRemoved(sectionID string, channelIDs []string)

	// OnPrefChange is called for pref_change WS events. Slack ships these
	// for every user-pref mutation (mute/unmute, highlight words,
	// notifications, etc.); receivers are expected to dispatch on `name`
	// and ignore prefs they don't care about. value is the new pref
	// value as a string — for list-shaped prefs like muted_channels,
	// Slack ships the full updated list, comma-separated.
	OnPrefChange(name, value string)

	// OnMemberJoined is delivered for member_joined_channel WS events:
	// a user joined the conversation. For shared channels this includes
	// external (Slack Connect) users. The receiver should persist the
	// membership and trigger external-user resolution if the user is unknown.
	OnMemberJoined(channelID, userID string)

	// OnMemberLeft is delivered for member_left_channel WS events.
	OnMemberLeft(channelID, userID string)
}

// wsEvent is the minimal structure for identifying a WebSocket event type.
type wsEvent struct {
	Type    string `json:"type"`
	SubType string `json:"subtype"`
}

// wsMessageEvent represents a message event from the WebSocket.
type wsMessageEvent struct {
	Type            string             `json:"type"`
	SubType         string             `json:"subtype"`
	Channel         string             `json:"channel"`
	User            string             `json:"user"`
	BotID           string             `json:"bot_id"`   // set on bot_message (no User)
	Username        string             `json:"username"` // bot display name on bot_message
	Text            string             `json:"text"`
	TS              string             `json:"ts"`
	ThreadTS        string             `json:"thread_ts"`
	DeletedTS       string             `json:"deleted_ts"`
	Files           []slack.File       `json:"files"`
	Blocks          slack.Blocks       `json:"blocks"`
	Attachments     []slack.Attachment `json:"attachments"`
	Message         *wsSubMsg          `json:"message"`          // for message_changed
	PreviousMessage *wsSubMsg          `json:"previous_message"` // for message_changed
}

// wsSubMsg is the inner message for message_changed events.
type wsSubMsg struct {
	User        string             `json:"user"`
	BotID       string             `json:"bot_id"`
	Username    string             `json:"username"`
	Text        string             `json:"text"`
	TS          string             `json:"ts"`
	ThreadTS    string             `json:"thread_ts"`
	Files       []slack.File       `json:"files"`
	Blocks      slack.Blocks       `json:"blocks"`
	Attachments []slack.Attachment `json:"attachments"`
}

// wsReactionEvent represents a reaction_added or reaction_removed event.
type wsReactionEvent struct {
	Type     string `json:"type"`
	User     string `json:"user"`
	Reaction string `json:"reaction"`
	Item     struct {
		Channel string `json:"channel"`
		TS      string `json:"ts"`
	} `json:"item"`
}

// wsPresenceEvent represents a presence_change event. Slack sends either
// a single-user form ("user") or, on batch_presence_aware connections, a
// batched form ("users") carrying several user IDs that share the same
// presence value. Both are parsed.
type wsPresenceEvent struct {
	Type     string   `json:"type"`
	User     string   `json:"user"`
	Users    []string `json:"users"`
	Presence string   `json:"presence"`
}

// wsTypingEvent represents a user_typing event.
type wsTypingEvent struct {
	Type    string `json:"type"`
	Channel string `json:"channel"`
	User    string `json:"user"`
}

// wsMemberChannelEvent represents member_joined_channel /
// member_left_channel. Both events share the same shape.
type wsMemberChannelEvent struct {
	Type    string `json:"type"`
	Channel string `json:"channel"`
	User    string `json:"user"`
	Team    string `json:"team"`
}

// wsManualPresenceEvent represents a manual_presence_change event,
// emitted when the authenticated user's own presence flips.
type wsManualPresenceEvent struct {
	Type     string `json:"type"`
	Presence string `json:"presence"`
}

// wsDNDStatusInner mirrors the dnd_status payload Slack ships with
// dnd_updated and dnd_updated_user events.
type wsDNDStatusInner struct {
	Enabled        bool  `json:"dnd_enabled"`
	SnoozeEnabled  bool  `json:"snooze_enabled"`
	SnoozeEndTime  int64 `json:"snooze_endtime"`
	NextDNDStartTS int64 `json:"next_dnd_start_ts"`
	NextDNDEndTS   int64 `json:"next_dnd_end_ts"`
}

// wsDNDUpdatedEvent represents a dnd_updated or dnd_updated_user event.
type wsDNDUpdatedEvent struct {
	Type      string           `json:"type"`
	DNDStatus wsDNDStatusInner `json:"dnd_status"`
}

// wsChannelMarkedEvent represents a channel_marked / im_marked /
// group_marked / mpim_marked event. Slack uses the same payload
// shape across all four — the type field disambiguates.
type wsChannelMarkedEvent struct {
	Type               string `json:"type"`
	Channel            string `json:"channel"`
	TS                 string `json:"ts"`
	UnreadCountDisplay int    `json:"unread_count_display"`
}

// wsConversationOpenedEvent is the shared shape for mpim_open, im_created,
// group_joined, and channel_joined events. All four carry a top-level
// `channel` field with the full conversation object.
type wsConversationOpenedEvent struct {
	Type    string        `json:"type"`
	Channel slack.Channel `json:"channel"`
}

// wsPrefChangeEvent represents a pref_change WS event. Slack ships
// these for every user-pref mutation. The `value` field is polymorphic
// across prefs — string for scalar prefs (muted_channels is a
// comma-separated string), array for list prefs (highlight_words),
// object for nested prefs. Stored as a raw message and converted to a
// canonical string by stringValue() so the EventHandler interface can
// stay simple (most consumers only care about a couple of scalar
// prefs).
type wsPrefChangeEvent struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

// stringValue returns the pref value coerced to a string. JSON strings
// are returned with quotes stripped. Arrays of strings are joined with
// commas to mirror Slack's own scalar-string convention for
// list-shaped prefs (muted_channels). Anything else is returned as the
// raw JSON, which preserves enough information that a future receiver
// can reparse if needed.
func (e wsPrefChangeEvent) stringValue() string {
	if len(e.Value) == 0 {
		return ""
	}
	// String form: "..."
	var s string
	if err := json.Unmarshal(e.Value, &s); err == nil {
		return s
	}
	// Array of strings: ["a","b"]
	var arr []string
	if err := json.Unmarshal(e.Value, &arr); err == nil {
		return strings.Join(arr, ",")
	}
	// Fallback: raw JSON.
	return string(e.Value)
}

// wsThreadMarkedEvent represents a thread_marked event from Slack's
// browser-protocol WebSocket. The subscription block carries the
// channel/thread/last-read-ts and an `active` flag (true means the
// thread is now unread / subscribed for unread updates; false means
// the thread is now read).
type wsThreadMarkedEvent struct {
	Type         string `json:"type"`
	Subscription struct {
		Channel  string `json:"channel"`
		ThreadTS string `json:"thread_ts"`
		LastRead string `json:"last_read"`
		Active   bool   `json:"active"`
	} `json:"subscription"`
}

// wsThreadSubscribedEvent represents thread_subscribed and
// thread_unsubscribed events from Slack's browser-protocol
// WebSocket. The subscription block has the same shape as
// wsThreadMarkedEvent.subscription.
type wsThreadSubscribedEvent struct {
	Type         string `json:"type"`
	Subscription struct {
		Channel  string `json:"channel"`
		ThreadTS string `json:"thread_ts"`
		LastRead string `json:"last_read"`
		Active   bool   `json:"active"`
	} `json:"subscription"`
}

// dispatchWebSocketEvent parses a raw JSON WebSocket message and routes it
// to the appropriate EventHandler method.
func dispatchWebSocketEvent(data []byte, handler EventHandler) {
	var evt wsEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}

	switch evt.Type {
	case "message":
		var msg wsMessageEvent
		if err := json.Unmarshal(data, &msg); err != nil {
			return
		}
		switch msg.SubType {
		case "", "bot_message", "thread_broadcast", "file_share":
			// thread_broadcast is a thread reply that the author also
			// posted to the main channel; render it like a regular
			// message but with the subtype preserved so the UI can
			// label it. file_share is a regular message that has one
			// or more files attached (Slack's V2 upload flow uses
			// this subtype).
			debuglog.WS("message: channel=%s user=%s ts=%s subtype=%q thread_ts=%s files=%d",
				msg.Channel, msg.User, msg.TS, msg.SubType, msg.ThreadTS, len(msg.Files))
			handler.OnMessage(msg.Channel, msg.User, msg.TS, msg.Text, msg.ThreadTS, msg.SubType, false, msg.Files, msg.Blocks, msg.Attachments, msg.BotID, msg.Username)
		case "message_changed":
			if msg.Message != nil {
				debuglog.WS("message_changed: channel=%s user=%s ts=%s thread_ts=%s edited=true",
					msg.Channel, msg.Message.User, msg.Message.TS, msg.Message.ThreadTS)
				handler.OnMessage(msg.Channel, msg.Message.User, msg.Message.TS, msg.Message.Text, msg.Message.ThreadTS, "", true, msg.Message.Files, msg.Message.Blocks, msg.Message.Attachments, msg.Message.BotID, msg.Message.Username)
			}
		case "message_deleted":
			debuglog.WS("message_deleted: channel=%s deleted_ts=%s", msg.Channel, msg.DeletedTS)
			handler.OnMessageDeleted(msg.Channel, msg.DeletedTS)
		}

	case "reaction_added":
		var evt wsReactionEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		debuglog.WS("reaction_added: channel=%s ts=%s user=%s emoji=%q",
			evt.Item.Channel, evt.Item.TS, evt.User, evt.Reaction)
		handler.OnReactionAdded(evt.Item.Channel, evt.Item.TS, evt.User, evt.Reaction)

	case "reaction_removed":
		var evt wsReactionEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		debuglog.WS("reaction_removed: channel=%s ts=%s user=%s emoji=%q",
			evt.Item.Channel, evt.Item.TS, evt.User, evt.Reaction)
		handler.OnReactionRemoved(evt.Item.Channel, evt.Item.TS, evt.User, evt.Reaction)

	case "presence_change":
		var evt wsPresenceEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		if len(evt.Users) > 0 {
			for _, uid := range evt.Users {
				debuglog.WS("presence_change (batch): user=%s presence=%q", uid, evt.Presence)
				handler.OnPresenceChange(uid, evt.Presence)
			}
		} else if evt.User != "" {
			debuglog.WS("presence_change: user=%s presence=%q", evt.User, evt.Presence)
			handler.OnPresenceChange(evt.User, evt.Presence)
		}

	case "manual_presence_change":
		var evt wsManualPresenceEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		handler.OnSelfPresenceChange(evt.Presence)

	case "dnd_updated", "dnd_updated_user":
		var evt wsDNDUpdatedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		isDND, end := computeDNDState(evt.DNDStatus, time.Now().Unix())
		handler.OnDNDChange(isDND, end)

	case "channel_marked", "im_marked", "group_marked", "mpim_marked":
		var evt wsChannelMarkedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		debuglog.WS("%s: channel=%s ts=%s unread_count=%d",
			evt.Type, evt.Channel, evt.TS, evt.UnreadCountDisplay)
		handler.OnChannelMarked(evt.Channel, evt.TS, evt.UnreadCountDisplay)

	case "mpim_open", "im_created", "im_open", "group_joined", "channel_joined":
		var evt wsConversationOpenedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		if evt.Channel.ID != "" {
			handler.OnConversationOpened(evt.Channel)
		}

	case "thread_marked":
		var evt wsThreadMarkedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		// active=true means subscribed-for-unread, i.e. the thread is
		// now unread. Invert for the read flag we hand to the handler.
		read := !evt.Subscription.Active
		debuglog.WS("thread_marked: channel=%s thread_ts=%s last_read=%s read=%v",
			evt.Subscription.Channel, evt.Subscription.ThreadTS, evt.Subscription.LastRead, read)
		handler.OnThreadMarked(evt.Subscription.Channel, evt.Subscription.ThreadTS, evt.Subscription.LastRead, read)

	case "thread_subscribed", "thread_unsubscribed":
		var evt wsThreadSubscribedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		// thread_unsubscribed events should be treated as active=false
		// regardless of what the server marks the inner flag as; the
		// outer event type is authoritative.
		active := evt.Subscription.Active
		if evt.Type == "thread_unsubscribed" {
			active = false
		}
		debuglog.WS("%s: channel=%s thread_ts=%s last_read=%s active=%v",
			evt.Type, evt.Subscription.Channel, evt.Subscription.ThreadTS, evt.Subscription.LastRead, active)
		handler.OnThreadSubscriptionChanged(
			evt.Subscription.Channel,
			evt.Subscription.ThreadTS,
			evt.Subscription.LastRead,
			active,
		)

	case "user_typing":
		var evt wsTypingEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		debuglog.WS("user_typing: channel=%s user=%s", evt.Channel, evt.User)
		handler.OnUserTyping(evt.Channel, evt.User)

	case "member_joined_channel":
		var evt wsMemberChannelEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		debuglog.WS("member_joined_channel: channel=%s user=%s", evt.Channel, evt.User)
		handler.OnMemberJoined(evt.Channel, evt.User)

	case "member_left_channel":
		var evt wsMemberChannelEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		debuglog.WS("member_left_channel: channel=%s user=%s", evt.Channel, evt.User)
		handler.OnMemberLeft(evt.Channel, evt.User)

	case "channel_section_upserted":
		var raw wsChannelSectionUpserted
		if err := json.Unmarshal(data, &raw); err != nil {
			return
		}
		handler.OnChannelSectionUpserted(raw.toUpserted())

	case "channel_section_deleted":
		var raw wsChannelSectionDeleted
		if err := json.Unmarshal(data, &raw); err != nil {
			return
		}
		handler.OnChannelSectionDeleted(raw.ID)

	case "channel_sections_channels_upserted":
		var raw wsChannelSectionsChannelsDelta
		if err := json.Unmarshal(data, &raw); err != nil {
			return
		}
		handler.OnChannelSectionChannelsUpserted(raw.SectionID, raw.ChannelIDs)

	case "channel_sections_channels_removed":
		var raw wsChannelSectionsChannelsDelta
		if err := json.Unmarshal(data, &raw); err != nil {
			return
		}
		handler.OnChannelSectionChannelsRemoved(raw.SectionID, raw.ChannelIDs)

	case "pref_change":
		var evt wsPrefChangeEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		handler.OnPrefChange(evt.Name, evt.stringValue())

	case "hello":
		debuglog.WS("hello: connected")
		handler.OnConnect()

	case "reconnect_url":
		// Could store for reconnection; ignoring for now

	default:
		// Ignore other event types. When debug logging is on, dump them
		// to the [ws] category so we can reverse-engineer undocumented
		// events (e.g. sidebar-section updates).
		if debuglog.Enabled() {
			payload := data
			if len(payload) > 4096 {
				payload = payload[:4096]
			}
			debuglog.WS("unknown event type=%q raw=%s", evt.Type, string(payload))
		}
	}
}

// computeDNDState evaluates whether the user is currently in DND from
// Slack's dnd_status payload, and returns the relevant end timestamp.
//
// Slack reports dnd_enabled=true whenever a notification schedule is
// configured for the user, regardless of whether the current time is
// inside the next scheduled window. The actual "currently in DND" state
// is therefore derived from:
//
//   - manual snooze: SnoozeEnabled && SnoozeEndTime > now
//   - active scheduled window: Enabled && NextDNDStartTS <= now < NextDNDEndTS
//
// When neither holds, the user is between sessions (or has no DND set)
// and computeDNDState returns (false, 0).
//
// `now` is supplied as a parameter to keep the function pure for tests.
func computeDNDState(s wsDNDStatusInner, now int64) (bool, int64) {
	if s.SnoozeEnabled && s.SnoozeEndTime > now {
		return true, s.SnoozeEndTime
	}
	if s.Enabled && s.NextDNDStartTS > 0 && s.NextDNDStartTS <= now && now < s.NextDNDEndTS {
		return true, s.NextDNDEndTS
	}
	return false, 0
}
