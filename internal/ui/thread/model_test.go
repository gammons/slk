package thread

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
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

// TestAddReply_AlwaysScrollsToBottom asserts that an incoming thread
// reply scrolls the thread panel to the bottom even when the user has
// scrolled up.
func TestAddReply_AlwaysScrollsToBottom(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1700000001.000000", UserName: "alice", Text: "hi"}
	replies := []messages.MessageItem{
		{TS: "1700000002.000000", UserName: "bob", Text: "r1"},
		{TS: "1700000003.000000", UserName: "carol", Text: "r2"},
		{TS: "1700000004.000000", UserName: "dave", Text: "r3"},
	}
	m.SetThread(parent, replies, "C123", "1700000001.000000")

	// Move selection up so we're explicitly NOT at the bottom.
	m.MoveUp()
	m.MoveUp()
	if m.selected == m.ReplyCount()-1 {
		t.Fatalf("test setup: expected selection above bottom, got %d", m.selected)
	}

	m.AddReply(messages.MessageItem{TS: "1700000005.000000", UserName: "eve", Text: "r4"})

	wantIdx := m.ReplyCount() - 1
	if m.selected != wantIdx {
		t.Errorf("AddReply should scroll to bottom: selected=%d want=%d", m.selected, wantIdx)
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

	// Should start at the bottom (newest reply) per SetThread's contract,
	// so opening a long thread lands the user on the latest activity.
	if m.selected != 2 {
		t.Errorf("expected selected=2 (bottom), got %d", m.selected)
	}

	m.GoToTop()
	if m.selected != 0 {
		t.Errorf("expected selected=0 after GoToTop, got %d", m.selected)
	}

	m.MoveDown()
	if m.selected != 1 {
		t.Errorf("expected selected=1, got %d", m.selected)
	}

	m.MoveDown()
	m.MoveDown() // should not go past end
	if m.selected != 2 {
		t.Errorf("expected selected=2, got %d", m.selected)
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

func TestUpdateMessageInPlace_Found(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "P1", Text: "parent"}
	replies := []messages.MessageItem{
		{TS: "R1", Text: "old reply"},
		{TS: "R2", Text: "other"},
	}
	m.SetThread(parent, replies, "C1", "P1")

	if !m.UpdateMessageInPlace("R1", "new reply") {
		t.Fatal("expected true updating R1")
	}
	if m.replies[0].Text != "new reply" {
		t.Errorf("text not updated: %q", m.replies[0].Text)
	}
	if !m.replies[0].IsEdited {
		t.Error("IsEdited not set")
	}
	if m.replies[1].Text != "other" {
		t.Error("other reply should be untouched")
	}
}

func TestUpdateMessageInPlace_NotFound(t *testing.T) {
	m := New()
	m.SetThread(messages.MessageItem{TS: "P1"}, []messages.MessageItem{
		{TS: "R1", Text: "a"},
	}, "C1", "P1")
	if m.UpdateMessageInPlace("nope", "x") {
		t.Error("expected false for missing TS")
	}
}

func TestRemoveMessageByTS_Middle(t *testing.T) {
	m := New()
	replies := []messages.MessageItem{
		{TS: "R1", Text: "a"},
		{TS: "R2", Text: "b"},
		{TS: "R3", Text: "c"},
	}
	m.SetThread(messages.MessageItem{TS: "P1"}, replies, "C1", "P1")
	if !m.RemoveMessageByTS("R2") {
		t.Fatal("expected true")
	}
	if len(m.replies) != 2 || m.replies[0].TS != "R1" || m.replies[1].TS != "R3" {
		t.Errorf("unexpected replies: %+v", m.replies)
	}
}

func TestRemoveMessageByTS_NotFound(t *testing.T) {
	m := New()
	m.SetThread(messages.MessageItem{TS: "P1"}, []messages.MessageItem{
		{TS: "R1", Text: "a"},
	}, "C1", "P1")
	if m.RemoveMessageByTS("nope") {
		t.Error("expected false for missing TS")
	}
	if len(m.replies) != 1 {
		t.Error("replies should be unchanged")
	}
}

func TestRemoveMessageByTS_LastBecomesEmpty(t *testing.T) {
	m := New()
	m.SetThread(messages.MessageItem{TS: "P1"}, []messages.MessageItem{
		{TS: "R1", Text: "only"},
	}, "C1", "P1")
	if !m.RemoveMessageByTS("R1") {
		t.Fatal("expected true")
	}
	if len(m.replies) != 0 {
		t.Error("expected empty replies")
	}
	if m.SelectedReply() != nil {
		t.Error("SelectedReply should be nil when empty")
	}
}

func TestRemoveMessageByTS_RemovesSelected(t *testing.T) {
	m := New()
	replies := []messages.MessageItem{
		{TS: "R1", Text: "a"},
		{TS: "R2", Text: "b"},
		{TS: "R3", Text: "c"},
	}
	m.SetThread(messages.MessageItem{TS: "P1"}, replies, "C1", "P1")
	// SetThread sets selected = 0, so explicitly select the last reply
	// to mirror the messages.Model test setup.
	for m.SelectedReply() == nil || m.SelectedReply().TS != "R3" {
		m.MoveDown()
	}
	if !m.RemoveMessageByTS("R3") {
		t.Fatal("expected true")
	}
	// Removing the selected (last) item should clamp selected to len-1 = 1.
	if m.selected != 1 {
		t.Errorf("expected selected=1 after removing last selected reply, got %d", m.selected)
	}
}

func TestUpdateParentInPlace_Match(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "P1", Text: "parent original"}
	m.SetThread(parent, nil, "C1", "P1")
	if !m.UpdateParentInPlace("P1", "parent edited") {
		t.Fatal("expected true")
	}
	if m.ParentMsg().Text != "parent edited" {
		t.Errorf("parent text not updated: %q", m.ParentMsg().Text)
	}
	if !m.ParentMsg().IsEdited {
		t.Error("parent IsEdited not set")
	}
}

func TestUpdateParentInPlace_NoMatch(t *testing.T) {
	m := New()
	m.SetThread(messages.MessageItem{TS: "P1", Text: "parent"}, nil, "C1", "P1")
	if m.UpdateParentInPlace("OTHER", "x") {
		t.Error("expected false when TS does not match parent")
	}
	if m.ParentMsg().Text != "parent" {
		t.Error("parent should be unchanged when TS does not match")
	}
}
