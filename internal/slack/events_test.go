package slackclient

import (
	"testing"
)

type mockEventHandler struct {
	messages        []string
	subtypes        []string
	deletedMessages []string
	reactions       []string
	presenceChanges []string
	typingEvents    []string
}

func (m *mockEventHandler) OnMessage(channelID, userID, ts, text, threadTS, subtype string, edited bool) {
	m.messages = append(m.messages, text)
	m.subtypes = append(m.subtypes, subtype)
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
func (m *mockEventHandler) OnConnect()    {}
func (m *mockEventHandler) OnDisconnect() {}

func TestEventHandlerInterface(t *testing.T) {
	handler := &mockEventHandler{}
	var _ EventHandler = handler

	handler.OnMessage("C1", "U1", "123.456", "hello", "", "", false)
	if len(handler.messages) != 1 || handler.messages[0] != "hello" {
		t.Error("expected message to be recorded")
	}
}

func TestDispatchWebSocketMessageEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"message","channel":"C1","user":"U1","text":"hello world","ts":"123.456"}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(handler.messages))
	}
	if handler.messages[0] != "hello world" {
		t.Errorf("expected 'hello world', got %q", handler.messages[0])
	}
}

func TestDispatchWebSocketBotMessageEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"message","subtype":"bot_message","channel":"C1","text":"bot says hi","ts":"123.456","bot_id":"B123"}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(handler.messages))
	}
	if handler.messages[0] != "bot says hi" {
		t.Errorf("expected 'bot says hi', got %q", handler.messages[0])
	}
}

func TestDispatchWebSocketReactionAddedEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"reaction_added","user":"U1","reaction":"thumbsup","item":{"channel":"C1","ts":"123.456"}}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.reactions) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(handler.reactions))
	}
	if handler.reactions[0] != "thumbsup" {
		t.Errorf("expected 'thumbsup', got %q", handler.reactions[0])
	}
}

func TestDispatchWebSocketPresenceChangeEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"presence_change","user":"U1","presence":"active"}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.presenceChanges) != 1 {
		t.Fatalf("expected 1 presence change, got %d", len(handler.presenceChanges))
	}
	if handler.presenceChanges[0] != "U1:active" {
		t.Errorf("expected 'U1:active', got %q", handler.presenceChanges[0])
	}
}

func TestDispatchWebSocketMessageDeletedEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"message","subtype":"message_deleted","channel":"C1","deleted_ts":"123.456"}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.deletedMessages) != 1 {
		t.Fatalf("expected 1 deleted message, got %d", len(handler.deletedMessages))
	}
	if handler.deletedMessages[0] != "123.456" {
		t.Errorf("expected '123.456', got %q", handler.deletedMessages[0])
	}
}

func TestDispatchWebSocketMessageChangedEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"message","subtype":"message_changed","channel":"C1","message":{"user":"U1","text":"edited text","ts":"123.456"},"previous_message":{"text":"original"}}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(handler.messages))
	}
	if handler.messages[0] != "edited text" {
		t.Errorf("expected 'edited text', got %q", handler.messages[0])
	}
}

// TestDispatchWebSocketThreadBroadcastEvent asserts that a
// thread_broadcast subtype is forwarded as a regular OnMessage call
// with the subtype preserved so the UI can render the
// "replied to a thread" label.
func TestDispatchWebSocketThreadBroadcastEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"message","subtype":"thread_broadcast","channel":"C1","user":"U1","text":"broadcast","ts":"200.0","thread_ts":"100.0"}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(handler.messages))
	}
	if handler.messages[0] != "broadcast" {
		t.Errorf("expected 'broadcast', got %q", handler.messages[0])
	}
	if handler.subtypes[0] != "thread_broadcast" {
		t.Errorf("expected subtype 'thread_broadcast', got %q", handler.subtypes[0])
	}
}

func TestDispatchWebSocketUserTypingEvent(t *testing.T) {
	handler := &mockEventHandler{}

	data := []byte(`{"type":"user_typing","channel":"C1","user":"U1"}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.typingEvents) != 1 {
		t.Fatalf("expected 1 typing event, got %d", len(handler.typingEvents))
	}
	if handler.typingEvents[0] != "C1:U1" {
		t.Errorf("expected 'C1:U1', got %q", handler.typingEvents[0])
	}
}
