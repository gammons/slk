package slackclient

import (
	"encoding/json"
)

// EventHandler processes real-time events from Slack.
type EventHandler interface {
	// OnMessage delivers a new or edited message. subtype mirrors
	// Slack's `subtype` field; "" for normal messages, "bot_message"
	// for bot posts, "thread_broadcast" for thread replies that the
	// author also sent to the main channel.
	OnMessage(channelID, userID, ts, text, threadTS, subtype string, edited bool)
	OnMessageDeleted(channelID, ts string)
	OnReactionAdded(channelID, ts, userID, emoji string)
	OnReactionRemoved(channelID, ts, userID, emoji string)
	OnPresenceChange(userID, presence string)
	OnUserTyping(channelID, userID string)
	OnConnect()
	OnDisconnect()
	OnSelfPresenceChange(presence string)
	OnDNDChange(enabled bool, endUnix int64)
}

// wsEvent is the minimal structure for identifying a WebSocket event type.
type wsEvent struct {
	Type    string `json:"type"`
	SubType string `json:"subtype"`
}

// wsMessageEvent represents a message event from the WebSocket.
type wsMessageEvent struct {
	Type            string    `json:"type"`
	SubType         string    `json:"subtype"`
	Channel         string    `json:"channel"`
	User            string    `json:"user"`
	Text            string    `json:"text"`
	TS              string    `json:"ts"`
	ThreadTS        string    `json:"thread_ts"`
	DeletedTS       string    `json:"deleted_ts"`
	Message         *wsSubMsg `json:"message"`          // for message_changed
	PreviousMessage *wsSubMsg `json:"previous_message"` // for message_changed
}

// wsSubMsg is the inner message for message_changed events.
type wsSubMsg struct {
	User     string `json:"user"`
	Text     string `json:"text"`
	TS       string `json:"ts"`
	ThreadTS string `json:"thread_ts"`
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
		case "", "bot_message", "thread_broadcast":
			// thread_broadcast is a thread reply that the author also
			// posted to the main channel; render it like a regular
			// message but with the subtype preserved so the UI can
			// label it.
			handler.OnMessage(msg.Channel, msg.User, msg.TS, msg.Text, msg.ThreadTS, msg.SubType, false)
		case "message_changed":
			if msg.Message != nil {
				handler.OnMessage(msg.Channel, msg.Message.User, msg.Message.TS, msg.Message.Text, msg.Message.ThreadTS, "", true)
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
		end := pickDNDEnd(evt.DNDStatus)
		handler.OnDNDChange(evt.DNDStatus.Enabled, end)

	case "user_typing":
		var evt wsTypingEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		handler.OnUserTyping(evt.Channel, evt.User)

	case "hello":
		handler.OnConnect()

	case "reconnect_url":
		// Could store for reconnection; ignoring for now

	default:
		// Ignore other event types
	}
}

// pickDNDEnd unifies snooze and admin-DND end timestamps. Slack delivers
// either snooze_endtime (when the user has a manual snooze) or
// next_dnd_end_ts (when an admin DND schedule is active). Returns 0 when
// neither is in the future.
func pickDNDEnd(s wsDNDStatusInner) int64 {
	if s.SnoozeEnabled && s.SnoozeEndTime > 0 {
		return s.SnoozeEndTime
	}
	if s.NextDNDEndTS > 0 {
		return s.NextDNDEndTS
	}
	return 0
}
