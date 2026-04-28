package messages

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ui/selection"
)

// newTestModel returns a Model with two simple messages, with the cache
// already built (via View()) so layout offsets and the messageID index
// are populated.
func newTestModel(width int) *Model {
	m := New([]MessageItem{
		{TS: "1.0", UserName: "alice", UserID: "U1", Text: "hello world", Timestamp: "1:00 PM"},
		{TS: "2.0", UserName: "bob", UserID: "U2", Text: "second message", Timestamp: "1:01 PM"},
	}, "general")
	_ = m.View(40, width)
	return &m
}

func TestSelection_BeginExtendEndCopiesText(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(20, 60) // drag down + right, well past content
	text, ok := m.EndSelection()
	if !ok {
		t.Fatalf("EndSelection returned ok=false")
	}
	if text == "" {
		t.Fatal("EndSelection returned empty text")
	}
	if !strings.Contains(text, "hello") {
		t.Fatalf("expected text to contain 'hello'; got %q", text)
	}
	if !m.HasSelection() {
		t.Fatal("selection should persist after EndSelection (until cleared)")
	}
}

func TestSelection_ClickWithoutDragReturnsEmpty(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(0, 5)
	_, ok := m.EndSelection()
	if ok {
		t.Fatal("zero-length selection must return ok=false")
	}
	if m.HasSelection() {
		t.Fatal("zero-length EndSelection should clear hasSelection")
	}
}

func TestSelection_ExtendWithoutBeginIsNoop(t *testing.T) {
	m := newTestModel(60)
	m.ExtendSelectionAt(0, 10)
	if m.HasSelection() {
		t.Fatal("ExtendSelectionAt without prior BeginSelectionAt must not create a selection")
	}
}

func TestSelection_ClearRemovesSelection(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(0, 10)
	_, _ = m.EndSelection()
	m.ClearSelection()
	if m.HasSelection() {
		t.Fatal("ClearSelection must remove selection")
	}
}

func TestSelection_ScrollHintForDrag(t *testing.T) {
	m := newTestModel(60)
	// View() set lastViewHeight to 40 (msgAreaHeight = height - chrome).
	// Top edge:
	if got := m.ScrollHintForDrag(0); got != -1 {
		t.Errorf("top edge: want -1 got %d", got)
	}
	// Middle:
	if got := m.ScrollHintForDrag(20); got != 0 {
		t.Errorf("middle: want 0 got %d", got)
	}
	// Bottom edge: viewportY >= lastViewHeight - 1.
	bottom := m.lastViewHeight - 1
	if got := m.ScrollHintForDrag(bottom); got != +1 {
		t.Errorf("bottom edge: want +1 got %d", got)
	}
}

func TestSelection_ScrollHintForDragZeroHeight(t *testing.T) {
	m := New(nil, "x") // never rendered — lastViewHeight stays 0
	if got := (&m).ScrollHintForDrag(5); got != 0 {
		t.Fatalf("zero height: want 0 got %d", got)
	}
}

func TestSelection_SurvivesAppendMessage(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(2, 10)
	textBefore, ok := m.EndSelection()
	if !ok || textBefore == "" {
		t.Fatal("precondition: EndSelection must succeed")
	}

	m.AppendMessage(MessageItem{TS: "3.0", UserName: "carol", UserID: "U3", Text: "later", Timestamp: "1:02 PM"})
	_ = m.View(40, 60) // rebuild cache after append

	textAfter := m.SelectionText()
	if textBefore != textAfter {
		t.Fatalf("selection drifted after AppendMessage:\nbefore=%q\nafter =%q", textBefore, textAfter)
	}
}

func TestSelection_SetMessagesClearsSelection(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(0, 5)
	if !m.HasSelection() {
		t.Fatal("precondition: must have selection")
	}
	m.SetMessages([]MessageItem{{TS: "9.0", UserName: "x", UserID: "U9", Text: "z"}})
	if m.HasSelection() {
		t.Fatal("SetMessages (channel switch) must clear selection")
	}
}

func TestSelection_PrependMessagesDoesNotClear(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(0, 5)
	m.PrependMessages([]MessageItem{
		{TS: "0.5", UserName: "x", UserID: "U9", Text: "older", Timestamp: "12:59 PM"},
	})
	_ = m.View(40, 60)
	if !m.HasSelection() {
		t.Fatal("PrependMessages must NOT clear selection (anchors are ID-based)")
	}
}

func TestSelection_NoBorderCharsInClipboard(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(20, 60)
	text, ok := m.EndSelection()
	if !ok {
		t.Fatalf("EndSelection ok=false")
	}
	// The thick left border is rendered as ▌ (U+258C). It must NEVER
	// appear in copied text.
	if strings.ContainsRune(text, '▌') {
		t.Fatalf("clipboard text contains border char ▌: %q", text)
	}
}

func TestSelection_SeparatorClickSnapsToMessage(t *testing.T) {
	// Date separator is at line 0 of the cache for newTestModel.
	// Click on the separator and a single column further; the resulting
	// anchor must be on a real message (non-empty MessageID).
	m := newTestModel(60)
	m.BeginSelectionAt(0, 5)
	m.ExtendSelectionAt(0, 6)
	// EndSelection might return ok=false (single-col drag may collapse),
	// but HasSelection at the begin point must be true and the stored
	// selRange's Start must reference a real message.
	if !m.hasSelection {
		t.Fatal("BeginSelectionAt on separator must still record a selection")
	}
	if m.selRange.Start.MessageID == "" {
		t.Fatalf("separator anchor must snap to a real message; got empty MessageID")
	}
}

func TestSelection_DeletedMessageCollapsesToEmpty(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(0, 10)
	// resolveAnchor must return ok=false for an unknown MessageID — that's
	// the underlying invariant that protects against stale anchors after
	// SetMessages drops a message.
	if _, _, ok := m.resolveAnchor(selection.Anchor{MessageID: "nonexistent.0"}); ok {
		t.Fatal("resolveAnchor must return ok=false for unknown MessageID")
	}
}
