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

// TestAppendMessage_AlwaysScrollsToBottom asserts that an incoming
// message scrolls the view to the bottom even when the user has
// scrolled up (selection is not at the last index). This matches
// chat-client expectations: new messages should always be visible.
func TestAppendMessage_AlwaysScrollsToBottom(t *testing.T) {
	msgs := make([]MessageItem, 5)
	for i := range msgs {
		msgs[i] = MessageItem{
			TS:        "1.0",
			UserName:  "alice",
			Text:      "old",
			Timestamp: "10:00 AM",
		}
	}
	m := New(msgs, "general")

	// Move selection up so we're explicitly NOT at the bottom.
	m.MoveUp()
	m.MoveUp()
	if m.SelectedIndex() == len(msgs)-1 {
		t.Fatalf("test setup: expected selection above bottom, got %d", m.SelectedIndex())
	}

	m.AppendMessage(MessageItem{TS: "2.0", UserName: "bob", Text: "incoming", Timestamp: "10:01 AM"})

	wantIdx := len(m.Messages()) - 1
	if got := m.SelectedIndex(); got != wantIdx {
		t.Errorf("AppendMessage should scroll to bottom: SelectedIndex=%d want=%d", got, wantIdx)
	}
	if !m.IsAtBottom() {
		t.Error("AppendMessage should leave model IsAtBottom() == true")
	}
}

// TestScrollPreservedAcrossRenders asserts that mouse-wheel-style scrolling
// (ScrollUp / ScrollDown) is not undone by the next View() call. Without the
// snap-decoupling logic, every render would pull yOffset back to the line
// containing the selected message.
func TestScrollPreservedAcrossRenders(t *testing.T) {
	msgs := make([]MessageItem, 60)
	for i := range msgs {
		msgs[i] = MessageItem{
			TS:        "1.0",
			UserName:  "alice",
			Text:      "msg body",
			Timestamp: "10:00 AM",
		}
	}
	m := New(msgs, "general")
	// Render once so selection is snapped to bottom, then scroll up.
	_ = m.View(20, 80)
	startOffset := m.yOffset
	m.ScrollUp(10)
	scrolled := m.yOffset
	if scrolled >= startOffset {
		t.Fatalf("ScrollUp did not decrease yOffset: before=%d after=%d", startOffset, scrolled)
	}

	// Render again WITHOUT changing selection. yOffset must NOT snap back.
	_ = m.View(20, 80)
	if m.yOffset != scrolled {
		t.Errorf("yOffset snapped back after render: want %d, got %d", scrolled, m.yOffset)
	}

	// Now move selection -- yOffset should re-snap to keep selection visible.
	m.MoveUp()
	_ = m.View(20, 80)
	if m.yOffset == scrolled {
		t.Error("expected yOffset to re-snap after selection change, but it did not")
	}
}
