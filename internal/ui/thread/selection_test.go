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

// firstContentY returns the smallest pane-local viewportY that lands on
// reply content (past the chrome — header + separator + parent message
// + separator). Used by tests rather than hard-coding the chrome height
// because the parent's render width / wrap can change it.
func firstContentY(m *Model) int { return m.chromeHeight }

func TestThreadSelection_BeginExtendEnd(t *testing.T) {
	m := newTestThread()
	m.BeginSelectionAt(firstContentY(m), 0)
	m.ExtendSelectionAt(firstContentY(m)+20, 60)
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
	m.BeginSelectionAt(firstContentY(m), 0)
	m.ExtendSelectionAt(firstContentY(m)+20, 60)
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
	m.BeginSelectionAt(firstContentY(m), 5)
	_, ok := m.EndSelection()
	if ok {
		t.Fatal("zero-length selection must return ok=false")
	}
}

func TestThreadSelection_ClearOnSetThread(t *testing.T) {
	m := newTestThread()
	m.BeginSelectionAt(firstContentY(m), 0)
	m.ExtendSelectionAt(firstContentY(m), 5)
	m.SetThread(messages.MessageItem{TS: "9.0", Text: "x"}, nil, "C2", "9.0")
	if m.HasSelection() {
		t.Fatal("SetThread must clear selection")
	}
}

func TestThreadSelection_ClearOnClear(t *testing.T) {
	m := newTestThread()
	m.BeginSelectionAt(firstContentY(m), 0)
	m.ExtendSelectionAt(firstContentY(m), 5)
	m.Clear()
	if m.HasSelection() {
		t.Fatal("Clear must clear selection")
	}
}

func TestThreadSelection_ScrollHintForDrag(t *testing.T) {
	m := newTestThread()
	h := m.lastViewHeight
	if h < 2 {
		t.Skip("test height too small")
	}
	// A row sitting inside the chrome should be treated as "above the
	// top edge" so an upward drag continues to auto-scroll.
	if got := m.ScrollHintForDrag(0); got != -1 {
		t.Errorf("chrome row: want -1 (treated as above top edge) got %d", got)
	}
	// First content row is the top edge.
	if got := m.ScrollHintForDrag(firstContentY(m)); got != -1 {
		t.Errorf("top: want -1 got %d", got)
	}
	if got := m.ScrollHintForDrag(firstContentY(m) + h - 1); got != +1 {
		t.Errorf("bottom: want +1 got %d", got)
	}
	if got := m.ScrollHintForDrag(firstContentY(m) + h/2); got != 0 {
		t.Errorf("middle: want 0 got %d", got)
	}
}

func TestThreadSelection_ViewIncludesHighlight(t *testing.T) {
	m := newTestThread()
	m.BeginSelectionAt(firstContentY(m), 5)
	m.ExtendSelectionAt(firstContentY(m), 15)
	out := m.View(40, 60)
	m.ClearSelection()
	out2 := m.View(40, 60)
	if out == out2 {
		t.Fatal("View output unchanged with active selection")
	}
}

// TestThreadSelection_ChromeRowsAreNotSelectable mirrors the messages-
// pane regression test: a click on the thread chrome (header / separator
// / parent message / separator at pane-local y < chromeHeight) must NOT
// anchor a selection.
func TestThreadSelection_ChromeRowsAreNotSelectable(t *testing.T) {
	m := newTestThread()
	if m.chromeHeight < 1 {
		t.Fatalf("test precondition: expected non-zero chromeHeight; got %d", m.chromeHeight)
	}
	m.BeginSelectionAt(0, 5)
	if m.HasSelection() {
		t.Fatal("BeginSelectionAt on chrome must not anchor a selection")
	}
}

// TestThreadSelection_FirstContentRowAnchorsAtFirstReply pins the
// other half of the off-by-chrome fix: a drag starting at pane-local y
// == firstContentY(m) lands on the FIRST reply's content. The drag
// extends down so the end anchor sits squarely on reply content.
func TestThreadSelection_FirstContentRowAnchorsAtFirstReply(t *testing.T) {
	m := newTestThread()
	m.BeginSelectionAt(firstContentY(m), 0)
	m.ExtendSelectionAt(firstContentY(m)+1, 30)
	text, ok := m.EndSelection()
	if !ok || text == "" {
		t.Fatalf("first-content-row drag should produce text; got ok=%v text=%q", ok, text)
	}
	// The first reply is bob's "first reply".
	if !strings.Contains(text, "bob") && !strings.Contains(text, "first reply") {
		t.Fatalf("expected first-reply content; got %q", text)
	}
}
