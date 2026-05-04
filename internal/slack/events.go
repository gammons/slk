package slackclient

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

// debugWS is true when SLK_DEBUG_WS is set. When enabled, dispatchWebSocketEvent
// logs every unknown WebSocket event type (with a truncated payload) to the
// standard logger — useful for reverse-engineering undocumented Slack events
// such as sidebar-section updates. Pair with SLK_DEBUG=1 to route logs to
// /tmp/slk-debug.log.
var debugWS = os.Getenv("SLK_DEBUG_WS") != ""

// EventHandler processes real-time events from Slack.
type EventHandler interface {
	// OnMessage delivers a new or edited message. subtype mirrors
	// Slack's `subtype` field; "" for normal messages, "bot_message"
	// for bot posts, "thread_broadcast" for thread replies that the
	// author also sent to the main channel. files carries any file
	// attachments on the message (empty for plain text messages).
	OnMessage(channelID, userID, ts, text, threadTS, subtype string, edited bool, files []slack.File, blocks slack.Blocks, attachments []slack.Attachment)
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
}

// wsEvent is the minimal structure for identifying a WebSocket event type.
type wsEvent struct {
	Type    string `json:"type"`
	SubType string `json:"subtype"`
}

// wsMessageEvent represents a message event from the WebSocket.
type wsMessageEvent struct {
	Type            string       `json:"type"`
	SubType         string       `json:"subtype"`
	Channel         string       `json:"channel"`
	User            string       `json:"user"`
	Text            string       `json:"text"`
	TS              string       `json:"ts"`
	ThreadTS        string       `json:"thread_ts"`
	DeletedTS       string       `json:"deleted_ts"`
	Files           []slack.File       `json:"files"`
	Blocks          slack.Blocks       `json:"blocks"`
	Attachments     []slack.Attachment `json:"attachments"`
	Message         *wsSubMsg          `json:"message"`          // for message_changed
	PreviousMessage *wsSubMsg          `json:"previous_message"` // for message_changed
}

// wsSubMsg is the inner message for message_changed events.
type wsSubMsg struct {
	User        string             `json:"user"`
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

// wsPresenceEvent represents a presence_change event.
type wsPresenceEvent struct {
	Type     string `json:"type"`
	User     string `json:"user"`
	Presence string `json:"presence"`
}

// wsTypingEvent represents a user_typing event.
type wsTypingEvent struct {
	Type    string `json:"type"`
	Channel string `json:"channel"`
	User    string `json:"user"`
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
			handler.OnMessage(msg.Channel, msg.User, msg.TS, msg.Text, msg.ThreadTS, msg.SubType, false, msg.Files, msg.Blocks, msg.Attachments)
		case "message_changed":
			if msg.Message != nil {
				handler.OnMessage(msg.Channel, msg.Message.User, msg.Message.TS, msg.Message.Text, msg.Message.ThreadTS, "", true, msg.Message.Files, msg.Message.Blocks, msg.Message.Attachments)
			}
		case "message_deleted":
			handler.OnMessageDeleted(msg.Channel, msg.DeletedTS)
		}

	case "reaction_added":
		var evt wsReactionEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		handler.OnReactionAdded(evt.Item.Channel, evt.Item.TS, evt.User, evt.Reaction)

	case "reaction_removed":
		var evt wsReactionEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		handler.OnReactionRemoved(evt.Item.Channel, evt.Item.TS, evt.User, evt.Reaction)

	case "presence_change":
		var evt wsPresenceEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		handler.OnPresenceChange(evt.User, evt.Presence)

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
		handler.OnThreadMarked(evt.Subscription.Channel, evt.Subscription.ThreadTS, evt.Subscription.LastRead, read)

	case "user_typing":
		var evt wsTypingEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		handler.OnUserTyping(evt.Channel, evt.User)

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
		handler.OnConnect()

	case "reconnect_url":
		// Could store for reconnection; ignoring for now

	default:
		// Ignore other event types. When SLK_DEBUG_WS is set, dump them
		// to the debug log so we can reverse-engineer undocumented
		// events (e.g. sidebar-section updates).
		if debugWS {
			payload := data
			if len(payload) > 4096 {
				payload = payload[:4096]
			}
			log.Printf("[ws] unknown event type=%q raw=%s", evt.Type, string(payload))
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
