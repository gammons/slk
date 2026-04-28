package thread

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
)

func newTestThread() *Model {
	m := New()
	parent := messages.MessageItem{TS: "1.0", UserName: "alice", UserID: "U1", Text: "parent", Timestamp: "1:00 PM"}
	replies := []messages.MessageItem{
		{TS: "2.0", UserName: "bob", UserID: "U2", Text: "first reply", Timestamp: "1:01 PM"},
		{TS: "3.0", UserName: "carol", UserID: "U3", Text: "second reply", Timestamp: "1:02 PM"},
	}
	m.SetThread(parent, replies, "C1", "1.0")
	_ = m.View(40, 60)
	return m
}

func TestThreadSelection_BeginExtendEnd(t *testing.T) {
	m := newTestThread()
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(20, 60)
	text, ok := m.EndSelection()
	if !ok {
		t.Fatalf("EndSelection ok=false")
	}
	if text == "" {
		t.Fatal("EndSelection returned empty text")
	}
	if !strings.Contains(text, "reply") {
		t.Fatalf("expected text to contain 'reply'; got %q", text)
	}
}

func TestThreadSelection_NoBorderCharsInClipboard(t *testing.T) {
	m := newTestThread()
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(20, 60)
	text, ok := m.EndSelection()
	if !ok {
		t.Fatalf("EndSelection ok=false")
	}
	if strings.ContainsRune(text, '▌') {
		t.Fatalf("clipboard text contains border char ▌: %q", text)
	}
}

func TestThreadSelection_ClickWithoutDragReturnsEmpty(t *testing.T) {
	m := newTestThread()
	m.BeginSelectionAt(0, 5)
	_, ok := m.EndSelection()
	if ok {
		t.Fatal("zero-length selection must return ok=false")
	}
}

func TestThreadSelection_ClearOnSetThread(t *testing.T) {
	m := newTestThread()
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(0, 5)
	m.SetThread(messages.MessageItem{TS: "9.0", Text: "x"}, nil, "C2", "9.0")
	if m.HasSelection() {
		t.Fatal("SetThread must clear selection")
	}
}

func TestThreadSelection_ClearOnClear(t *testing.T) {
	m := newTestThread()
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(0, 5)
	m.Clear()
	if m.HasSelection() {
		t.Fatal("Clear must clear selection")
	}
}

func TestThreadSelection_ScrollHintForDrag(t *testing.T) {
	m := newTestThread()
	if got := m.ScrollHintForDrag(0); got != -1 {
		t.Errorf("top: want -1 got %d", got)
	}
	bottom := m.lastViewHeight - 1
	if bottom < 1 {
		t.Skip("test height too small")
	}
	if got := m.ScrollHintForDrag(bottom); got != +1 {
		t.Errorf("bottom: want +1 got %d", got)
	}
	if got := m.ScrollHintForDrag(bottom / 2); got != 0 {
		t.Errorf("middle: want 0 got %d", got)
	}
}

func TestThreadSelection_ViewIncludesHighlight(t *testing.T) {
	m := newTestThread()
	m.BeginSelectionAt(0, 5)
	m.ExtendSelectionAt(0, 15)
	out := m.View(40, 60)
	m.ClearSelection()
	out2 := m.View(40, 60)
	if out == out2 {
		t.Fatal("View output unchanged with active selection")
	}
}
