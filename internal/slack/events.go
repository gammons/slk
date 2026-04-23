package slackclient

import (
	"log"

	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// EventHandler processes Socket Mode events from Slack.
type EventHandler interface {
	OnMessage(channelID, userID, ts, text, threadTS string, edited bool)
	OnMessageDeleted(channelID, ts string)
	OnReactionAdded(channelID, ts, userID, emoji string)
	OnReactionRemoved(channelID, ts, userID, emoji string)
	OnPresenceChange(userID, presence string)
	OnUserTyping(channelID, userID string)
}

// EventDispatcher routes socketmode events to the EventHandler.
type EventDispatcher struct {
	handler EventHandler
	client  *socketmode.Client
}

// NewEventDispatcher creates a dispatcher that acknowledges socket events
// and routes their inner payloads to the handler.
func NewEventDispatcher(client *socketmode.Client, handler EventHandler) *EventDispatcher {
	return &EventDispatcher{
		handler: handler,
		client:  client,
	}
}

// HandleEvent processes a single socketmode event, acknowledging it and
// dispatching to the appropriate handler method.
func (d *EventDispatcher) HandleEvent(evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		d.client.Ack(*evt.Request)
		d.handleEventsAPI(eventsAPIEvent)

	default:
		// Acknowledge unknown events to prevent retries
		if evt.Request != nil {
			d.client.Ack(*evt.Request)
		}
	}
}

func (d *EventDispatcher) handleEventsAPI(evt slackevents.EventsAPIEvent) {
	switch ev := evt.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		switch ev.SubType {
		case "":
			d.handler.OnMessage(ev.Channel, ev.User, ev.TimeStamp, ev.Text, ev.ThreadTimeStamp, false)
		case "message_changed":
			if ev.Message != nil {
				d.handler.OnMessage(ev.Channel, ev.Message.User, ev.Message.Timestamp, ev.Message.Text, ev.Message.ThreadTimestamp, true)
			}
		case "message_deleted":
			d.handler.OnMessageDeleted(ev.Channel, ev.DeletedTimeStamp)
		}

	case *slackevents.ReactionAddedEvent:
		d.handler.OnReactionAdded(ev.Item.Channel, ev.Item.Timestamp, ev.User, ev.Reaction)

	case *slackevents.ReactionRemovedEvent:
		d.handler.OnReactionRemoved(ev.Item.Channel, ev.Item.Timestamp, ev.User, ev.Reaction)

	default:
		log.Printf("unhandled event type: %T", ev)
	}
}
