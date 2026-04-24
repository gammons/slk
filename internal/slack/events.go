package slackclient

import (
	"encoding/json"
	"log"
)

// EventHandler processes real-time events from Slack.
type EventHandler interface {
	OnMessage(channelID, userID, ts, text, threadTS string, edited bool)
	OnMessageDeleted(channelID, ts string)
	OnReactionAdded(channelID, ts, userID, emoji string)
	OnReactionRemoved(channelID, ts, userID, emoji string)
	OnPresenceChange(userID, presence string)
	OnUserTyping(channelID, userID string)
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
		case "":
			handler.OnMessage(msg.Channel, msg.User, msg.TS, msg.Text, msg.ThreadTS, false)
		case "bot_message":
			handler.OnMessage(msg.Channel, msg.User, msg.TS, msg.Text, msg.ThreadTS, false)
		case "message_changed":
			if msg.Message != nil {
				handler.OnMessage(msg.Channel, msg.Message.User, msg.Message.TS, msg.Message.Text, msg.Message.ThreadTS, true)
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

	case "user_typing":
		var evt wsTypingEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		handler.OnUserTyping(evt.Channel, evt.User)

	case "hello":
		log.Printf("WebSocket connected to Slack")

	case "reconnect_url":
		// Could store for reconnection; ignoring for now

	default:
		// Ignore other event types
	}
}
