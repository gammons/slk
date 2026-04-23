package slackclient

import (
	"testing"
)

type mockEventHandler struct {
	messages        []string
	deletedMessages []string
	reactions       []string
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
func (m *mockEventHandler) OnPresenceChange(userID, presence string)              {}
func (m *mockEventHandler) OnUserTyping(channelID, userID string)                 {}

func TestEventHandlerInterface(t *testing.T) {
	handler := &mockEventHandler{}

	// Verify the interface is satisfied
	var _ EventHandler = handler

	handler.OnMessage("C1", "U1", "123.456", "hello", "", false)
	if len(handler.messages) != 1 || handler.messages[0] != "hello" {
		t.Error("expected message to be recorded")
	}
}
