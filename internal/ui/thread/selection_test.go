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
// reply content. Chrome is now just `header + separator`, and the parent
// message lives at the top of the scrollable viewContent (it scrolls with
// replies so a long parent does not block them). m.entryOffsets[0] is the
// line index of the first reply inside viewContent, which equals the
// parent block's height. Pane-local row = chromeHeight + entryOffsets[0]
// minus the viewport's YOffset. Tests start at YOffset=0 (newTestThread
// calls View once and selection isn't moved), so we don't subtract YOffset
// here.
func firstContentY(m *Model) int {
	if len(m.entryOffsets) == 0 {
		return m.chromeHeight
	}
	return m.chromeHeight + m.entryOffsets[0] - m.vp.YOffset()
}

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

func TestThreadVisualSelection_Motions(t *testing.T) {
	m := newTestThread()
	if !m.BeginVisualSelection() {
		t.Fatal("BeginVisualSelection returned false")
	}
	initial := m.SelectionText()
	m.MoveVisualSelection("$")
	if got := m.SelectionText(); len(got) <= len(initial) {
		t.Fatalf("$ did not extend selection: initial=%q got=%q", initial, got)
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
	// A row sitting inside the chrome (header / separator) should be
	// treated as "above the top edge" so an upward drag continues to
	// auto-scroll. Chrome is now just header+separator -- the parent
	// message has moved into the scrollable content.
	if got := m.ScrollHintForDrag(0); got != -1 {
		t.Errorf("chrome row: want -1 (treated as above top edge) got %d", got)
	}
	// The first row of the scrollable area IS the top edge -- this lands
	// at the start of the parent message block now (parent scrolls).
	if got := m.ScrollHintForDrag(m.chromeHeight); got != -1 {
		t.Errorf("top of scrollable: want -1 got %d", got)
	}
	// Bottom edge of the reply area.
	if got := m.ScrollHintForDrag(m.chromeHeight + h - 1); got != +1 {
		t.Errorf("bottom: want +1 got %d", got)
	}
	// Middle of the reply area.
	if got := m.ScrollHintForDrag(m.chromeHeight + h/2); got != 0 {
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

// TestThreadSelection_ExtendUnchangedDoesNotDirty mirrors the messages-
// pane perf guard: with MouseModeCellMotion the terminal emits a
// MouseMotionMsg for every cell traversed while the button is held,
// and many of those resolve to the same end anchor. Bumping
// m.version on each one needlessly invalidates the thread-pane
// render cache. ExtendSelectionAt must early-out when the resolved
// end anchor matches the current m.selRange.End.
func TestThreadSelection_ExtendUnchangedDoesNotDirty(t *testing.T) {
	m := newTestThread()
	m.BeginSelectionAt(firstContentY(m), 0)
	// First extend establishes a real end anchor and dirties.
	m.ExtendSelectionAt(firstContentY(m)+1, 10)
	end := m.selRange.End
	beforeVersion := m.version

	// Re-call with the exact same coordinates: the resolved end
	// anchor must be identical and no version bump should occur.
	m.ExtendSelectionAt(firstContentY(m)+1, 10)

	if m.selRange.End != end {
		t.Fatalf("selRange.End drifted across identical Extend: before=%+v after=%+v", end, m.selRange.End)
	}
	if m.version != beforeVersion {
		t.Errorf("ExtendSelectionAt with unchanged end anchor must not bump version (before=%d after=%d)", beforeVersion, m.version)
	}
}
