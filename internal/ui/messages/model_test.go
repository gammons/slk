// internal/ui/messages/model_test.go
package messages

import (
	"strings"
	"testing"
)

func TestMessagePaneView(t *testing.T) {
	msgs := []MessageItem{
		{UserName: "alice", Text: "Hello world", Timestamp: "10:30 AM"},
		{UserName: "bob", Text: "Hey there!", Timestamp: "10:31 AM"},
	}

	m := New(msgs, "general")
	view := m.View(20, 60) // height=20, width=60

	if !strings.Contains(view, "alice") {
		t.Error("expected 'alice' in view")
	}
	if !strings.Contains(view, "Hello world") {
		t.Error("expected 'Hello world' in view")
	}
	if !strings.Contains(view, "general") {
		t.Error("expected channel name in header")
	}
}

func TestMessagePaneNavigation(t *testing.T) {
	msgs := []MessageItem{
		{TS: "1.0", UserName: "alice", Text: "msg 1"},
		{TS: "2.0", UserName: "bob", Text: "msg 2"},
		{TS: "3.0", UserName: "carol", Text: "msg 3"},
	}

	m := New(msgs, "general")
	// Should start at bottom (newest message)
	if m.SelectedIndex() != 2 {
		t.Errorf("expected selected index 2, got %d", m.SelectedIndex())
	}

	m.MoveUp()
	if m.SelectedIndex() != 1 {
		t.Errorf("expected index 1 after move up, got %d", m.SelectedIndex())
	}
}

func TestMessagePaneAppend(t *testing.T) {
	m := New(nil, "general")

	m.AppendMessage(MessageItem{TS: "1.0", UserName: "alice", Text: "new message"})
	if len(m.Messages()) != 1 {
		t.Errorf("expected 1 message, got %d", len(m.Messages()))
	}
}
