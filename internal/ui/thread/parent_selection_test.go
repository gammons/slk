package thread

import (
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/selection"
)

// The parent message is a selectable row: MoveUp from the first reply
// lands on it, and SelectedReply returns it so every message operation
// (react, permalink, yank, open links) works on the parent too.
// Regression: the cursor was clamped to the replies slice, making the
// first message of every thread impossible to select or react to.

func newThreadWithReplies(replyCount int) *Model {
	m := New()
	parent := messages.MessageItem{TS: "100.0", UserID: "U1", Text: "parent"}
	var replies []messages.MessageItem
	for i := 0; i < replyCount; i++ {
		replies = append(replies, messages.MessageItem{
			TS: "101." + string(rune('0'+i)), UserID: "U2", Text: "reply",
		})
	}
	m.SetThread(parent, replies, "C1", "100.0")
	return m
}

func TestMoveUpFromFirstReplySelectsParent(t *testing.T) {
	m := newThreadWithReplies(2)
	m.GoToTop()
	m.MoveUp()
	sel := m.SelectedReply()
	if sel == nil || sel.TS != "100.0" {
		t.Fatalf("SelectedReply = %+v, want parent (TS 100.0)", sel)
	}
}

func TestMoveDownFromParentSelectsFirstReply(t *testing.T) {
	m := newThreadWithReplies(2)
	m.GoToTop()
	m.MoveUp() // parent
	m.MoveDown()
	sel := m.SelectedReply()
	if sel == nil || sel.TS != "101.0" {
		t.Fatalf("SelectedReply = %+v, want first reply (TS 101.0)", sel)
	}
}

func TestMoveUpFromParentStays(t *testing.T) {
	m := newThreadWithReplies(1)
	m.GoToTop()
	m.MoveUp()
	m.MoveUp() // no-op at top
	sel := m.SelectedReply()
	if sel == nil || sel.TS != "100.0" {
		t.Fatalf("SelectedReply = %+v, want parent after double MoveUp", sel)
	}
}

func TestEmptyThreadSelectsParent(t *testing.T) {
	m := newThreadWithReplies(0)
	sel := m.SelectedReply()
	if sel == nil || sel.TS != "100.0" {
		t.Fatalf("SelectedReply = %+v, want parent in reply-less thread", sel)
	}
}

func TestAddReplySelectsNewestFromParent(t *testing.T) {
	m := newThreadWithReplies(0)
	m.AddReply(messages.MessageItem{TS: "102.0", UserID: "U2", Text: "new"})
	sel := m.SelectedReply()
	if sel == nil || sel.TS != "102.0" {
		t.Fatalf("SelectedReply = %+v, want newest reply", sel)
	}
}

func TestClearedThreadSelectsNothing(t *testing.T) {
	m := newThreadWithReplies(1)
	m.Clear()
	if sel := m.SelectedReply(); sel != nil {
		t.Fatalf("SelectedReply = %+v, want nil after Clear", sel)
	}
}

func TestClickAtParentSelectsParent(t *testing.T) {
	m := newThreadWithReplies(1)
	m.View(20, 80)

	m.ClickAt(m.chromeHeight)

	sel := m.SelectedReply()
	if sel == nil || sel.TS != "100.0" {
		t.Fatalf("SelectedReply = %+v, want parent (TS 100.0)", sel)
	}
}

// Making the parent clickable must not make the chrome clickable: the
// header rows sit above the scrolling content, and absoluteLineAt clamps
// them onto the parent's first line, so ClickAt needs its own guard.
func TestClickAtChromeKeepsSelection(t *testing.T) {
	m := newThreadWithReplies(2)
	m.View(20, 80)
	before := m.selected

	m.ClickAt(0)

	if m.selected != before {
		t.Fatalf("selected = %d after clicking the header, want %d (unchanged)",
			m.selected, before)
	}
}

func TestUpdateReactionUpdatesParent(t *testing.T) {
	m := newThreadWithReplies(1)
	m.SetCurrentUser("U1")

	m.UpdateReaction("100.0", "eyes", "U1", false)
	if len(m.parent.Reactions) != 1 || !m.parent.Reactions[0].HasReacted {
		t.Fatalf("parent reactions = %+v, want current-user eyes", m.parent.Reactions)
	}

	m.UpdateReaction("100.0", "eyes", "U1", true)
	if len(m.parent.Reactions) != 0 {
		t.Fatalf("parent reactions = %+v, want none", m.parent.Reactions)
	}
}

func TestBeginSelectionAtParentAnchorsParent(t *testing.T) {
	m := newThreadWithReplies(1)
	m.View(20, 80)

	m.BeginSelectionAt(m.chromeHeight, 1)

	if !m.hasSelection || m.selRange.Start.MessageID != "100.0" {
		t.Fatalf("selection = %+v, want parent anchor", m.selRange)
	}
}

func TestSelectionTextIncludesParent(t *testing.T) {
	m := newThreadWithReplies(1)
	m.View(20, 80)
	m.selRange = selection.Range{
		Start:  selection.Anchor{MessageID: "100.0", Line: 1, Col: 0},
		End:    selection.Anchor{MessageID: "100.0", Line: 1, Col: 1},
		Active: false,
	}
	m.hasSelection = true

	if got := m.SelectionText(); got == "" {
		t.Fatal("SelectionText() = empty, want parent text")
	}
}
