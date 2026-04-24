package slackclient

import (
	"testing"

	"github.com/slack-go/slack"
)

type mockEventHandler struct {
	messages        []string
	deletedMessages []string
	reactions       []string
	presenceChanges []string
	typingEvents    []string
}

func (m *mockEventHandler) OnMessage(channelID, userID, ts, text, threadTS string, edited bool) {
	m.messages = append(m.messages, text)
}

func (m *mockEventHandler) OnMessageDeleted(channelID, ts string) {
	m.deletedMessages = append(m.deletedMessages, ts)
}

func (m *mockEventHandler) OnReactionAdded(channelID, ts, userID, emoji string) {
	m.reactions = append(m.reactions, emoji)
}

func (m *mockEventHandler) OnReactionRemoved(channelID, ts, userID, emoji string) {}
func (m *mockEventHandler) OnPresenceChange(userID, presence string) {
	m.presenceChanges = append(m.presenceChanges, userID+":"+presence)
}
func (m *mockEventHandler) OnUserTyping(channelID, userID string) {
	m.typingEvents = append(m.typingEvents, channelID+":"+userID)
}

func TestEventHandlerInterface(t *testing.T) {
	handler := &mockEventHandler{}

	// Verify the interface is satisfied
	var _ EventHandler = handler

	handler.OnMessage("C1", "U1", "123.456", "hello", "", false)
	if len(handler.messages) != 1 || handler.messages[0] != "hello" {
		t.Error("expected message to be recorded")
	}
}

func TestDispatchRTMMessageEvent(t *testing.T) {
	handler := &mockEventHandler{}

	evt := slack.RTMEvent{
		Type: "message",
		Data: &slack.MessageEvent{
			Msg: slack.Msg{
				Channel:   "C1",
				User:      "U1",
				Text:      "hello world",
				Timestamp: "123.456",
			},
		},
	}

	dispatchRTMEvent(evt, handler)

	if len(handler.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(handler.messages))
	}
	if handler.messages[0] != "hello world" {
		t.Errorf("expected 'hello world', got %q", handler.messages[0])
	}
}

func TestDispatchRTMReactionAddedEvent(t *testing.T) {
	handler := &mockEventHandler{}

	evt := slack.RTMEvent{
		Type: "reaction_added",
		Data: &slack.ReactionAddedEvent{
			User:     "U1",
			Reaction: "thumbsup",
			Item: slack.ReactionItem{
				Channel:   "C1",
				Timestamp: "123.456",
			},
		},
	}

	dispatchRTMEvent(evt, handler)

	if len(handler.reactions) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(handler.reactions))
	}
	if handler.reactions[0] != "thumbsup" {
		t.Errorf("expected 'thumbsup', got %q", handler.reactions[0])
	}
}

func TestDispatchRTMPresenceChangeEvent(t *testing.T) {
	handler := &mockEventHandler{}

	evt := slack.RTMEvent{
		Type: "presence_change",
		Data: &slack.PresenceChangeEvent{
			User:     "U1",
			Presence: "active",
		},
	}

	dispatchRTMEvent(evt, handler)

	if len(handler.presenceChanges) != 1 {
		t.Fatalf("expected 1 presence change, got %d", len(handler.presenceChanges))
	}
	if handler.presenceChanges[0] != "U1:active" {
		t.Errorf("expected 'U1:active', got %q", handler.presenceChanges[0])
	}
}

func TestDispatchRTMMessageDeletedEvent(t *testing.T) {
	handler := &mockEventHandler{}

	evt := slack.RTMEvent{
		Type: "message",
		Data: &slack.MessageEvent{
			Msg: slack.Msg{
				Channel:          "C1",
				SubType:          "message_deleted",
				DeletedTimestamp: "123.456",
			},
		},
	}

	dispatchRTMEvent(evt, handler)

	if len(handler.deletedMessages) != 1 {
		t.Fatalf("expected 1 deleted message, got %d", len(handler.deletedMessages))
	}
	if handler.deletedMessages[0] != "123.456" {
		t.Errorf("expected '123.456', got %q", handler.deletedMessages[0])
	}
}
