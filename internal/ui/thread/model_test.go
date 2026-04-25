package thread

import (
	"strings"
	"testing"

	"github.com/gammons/slack-tui/internal/ui/messages"
)

func TestSetThread(t *testing.T) {
	m := New()

	parent := messages.MessageItem{
		TS:       "1700000001.000000",
		UserName: "alice",
		Text:     "parent message",
	}
	replies := []messages.MessageItem{
		{TS: "1700000002.000000", UserName: "bob", Text: "reply 1"},
		{TS: "1700000003.000000", UserName: "charlie", Text: "reply 2"},
	}

	m.SetThread(parent, replies, "C123", "1700000001.000000")

	if m.ThreadTS() != "1700000001.000000" {
		t.Errorf("expected thread ts 1700000001.000000, got %s", m.ThreadTS())
	}
	if m.IsEmpty() {
		t.Error("expected thread to not be empty after SetThread")
	}
	if m.ReplyCount() != 2 {
		t.Errorf("expected 2 replies, got %d", m.ReplyCount())
	}
}

func TestClear(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1700000001.000000", UserName: "alice", Text: "hi"}
	m.SetThread(parent, nil, "C123", "1700000001.000000")

	m.Clear()

	if !m.IsEmpty() {
		t.Error("expected thread to be empty after Clear")
	}
	if m.ThreadTS() != "" {
		t.Errorf("expected empty thread ts after Clear, got %s", m.ThreadTS())
	}
}

func TestAddReply(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1700000001.000000", UserName: "alice", Text: "hi"}
	m.SetThread(parent, nil, "C123", "1700000001.000000")

	m.AddReply(messages.MessageItem{TS: "1700000002.000000", UserName: "bob", Text: "hey"})

	if m.ReplyCount() != 1 {
		t.Errorf("expected 1 reply, got %d", m.ReplyCount())
	}
}

func TestNavigation(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1700000001.000000", UserName: "alice", Text: "hi"}
	replies := []messages.MessageItem{
		{TS: "1700000002.000000", UserName: "bob", Text: "r1"},
		{TS: "1700000003.000000", UserName: "charlie", Text: "r2"},
		{TS: "1700000004.000000", UserName: "dave", Text: "r3"},
	}
	m.SetThread(parent, replies, "C123", "1700000001.000000")

	// Should start at bottom (newest reply)
	if m.selected != 2 {
		t.Errorf("expected selected=2, got %d", m.selected)
	}

	m.MoveUp()
	if m.selected != 1 {
		t.Errorf("expected selected=1, got %d", m.selected)
	}

	m.MoveUp()
	m.MoveUp() // should not go below 0
	if m.selected != 0 {
		t.Errorf("expected selected=0, got %d", m.selected)
	}

	m.GoToBottom()
	if m.selected != 2 {
		t.Errorf("expected selected=2 after GoToBottom, got %d", m.selected)
	}
}

func TestViewRendersContent(t *testing.T) {
	m := New()
	parent := messages.MessageItem{
		TS:        "1700000001.000000",
		UserName:  "alice",
		Text:      "parent message",
		Timestamp: "10:30 AM",
	}
	replies := []messages.MessageItem{
		{TS: "1700000002.000000", UserName: "bob", Text: "reply one", Timestamp: "10:31 AM"},
	}
	m.SetThread(parent, replies, "C123", "1700000001.000000")

	view := m.View(20, 40)

	if !strings.Contains(view, "Thread") {
		t.Error("expected view to contain 'Thread'")
	}
	if !strings.Contains(view, "alice") {
		t.Error("expected view to contain parent username 'alice'")
	}
	if !strings.Contains(view, "bob") {
		t.Error("expected view to contain reply username 'bob'")
	}
}
