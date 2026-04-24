package slackclient

import (
	"log"

	"github.com/slack-go/slack"
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

// dispatchRTMEvent routes a single RTM event to the appropriate EventHandler method.
func dispatchRTMEvent(msg slack.RTMEvent, handler EventHandler) {
	switch ev := msg.Data.(type) {
	case *slack.MessageEvent:
		switch ev.SubType {
		case "":
			handler.OnMessage(ev.Channel, ev.User, ev.Timestamp, ev.Text, ev.ThreadTimestamp, false)
		case "message_changed":
			if ev.SubMessage != nil {
				handler.OnMessage(ev.Channel, ev.SubMessage.User, ev.SubMessage.Timestamp, ev.SubMessage.Text, ev.SubMessage.ThreadTimestamp, true)
			}
		case "message_deleted":
			handler.OnMessageDeleted(ev.Channel, ev.DeletedTimestamp)
		}

	case *slack.ReactionAddedEvent:
		handler.OnReactionAdded(ev.Item.Channel, ev.Item.Timestamp, ev.User, ev.Reaction)

	case *slack.ReactionRemovedEvent:
		handler.OnReactionRemoved(ev.Item.Channel, ev.Item.Timestamp, ev.User, ev.Reaction)

	case *slack.PresenceChangeEvent:
		handler.OnPresenceChange(ev.User, ev.Presence)

	case *slack.UserTypingEvent:
		handler.OnUserTyping(ev.Channel, ev.User)

	case *slack.ConnectedEvent:
		log.Printf("RTM connected: %s (connection count: %d)", ev.Info.User.Name, ev.ConnectionCount)

	case *slack.InvalidAuthEvent:
		log.Printf("RTM authentication expired -- re-run --add-workspace to re-authenticate")

	case *slack.LatencyReport:
		// Silently ignore latency reports

	case *slack.HelloEvent:
		// Silently ignore hello events

	case *slack.ConnectingEvent:
		log.Printf("RTM connecting (attempt %d)...", ev.Attempt)

	case *slack.ConnectionErrorEvent:
		log.Printf("RTM connection error: %v", ev.Error())

	default:
		// Ignore other event types
	}
}
