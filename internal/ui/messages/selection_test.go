package messages

import (
	"fmt"
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

// firstContentY returns the smallest pane-local viewportY that lands on
// message content (past the chrome — channel header + separator). The
// selection model treats viewportY in App.panelAt's coordinate system: 0
// is just below the panel border, so values 0..chromeHeight-1 are inside
// the chrome and ignored. Tests use this helper rather than hard-coding
// the chrome height because the chrome may grow (e.g. topic) in future.
func firstContentY(m *Model) int { return m.chromeHeight }

func TestSelection_BeginExtendEndCopiesText(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(firstContentY(m), 0)
	m.ExtendSelectionAt(firstContentY(m)+20, 60) // drag down + right, well past content
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
	m.BeginSelectionAt(firstContentY(m), 5)
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
	m.BeginSelectionAt(firstContentY(m), 0)
	m.ExtendSelectionAt(firstContentY(m), 10)
	_, _ = m.EndSelection()
	m.ClearSelection()
	if m.HasSelection() {
		t.Fatal("ClearSelection must remove selection")
	}
}

func TestSelection_ScrollHintForDrag(t *testing.T) {
	m := newTestModel(60)
	// View() set lastViewHeight to msgAreaHeight = height - chromeHeight.
	// ScrollHintForDrag receives a pane-local viewportY (App.panelAt
	// space): the top content row is at firstContentY(m), the bottom at
	// firstContentY(m) + lastViewHeight - 1.
	h := m.lastViewHeight
	// A row inside the chrome should be treated as "above the top edge"
	// so an upward drag continues to auto-scroll toward older messages.
	if got := m.ScrollHintForDrag(0); got != -1 {
		t.Errorf("chrome row: want -1 (treated as above top edge) got %d", got)
	}
	// First content row is the top edge.
	if got := m.ScrollHintForDrag(firstContentY(m)); got != -1 {
		t.Errorf("top edge: want -1 got %d", got)
	}
	// Middle of the content area.
	if got := m.ScrollHintForDrag(firstContentY(m) + h/2); got != 0 {
		t.Errorf("middle: want 0 got %d", got)
	}
	// Bottom edge: pane-local Y == firstContentY + lastViewHeight - 1.
	if got := m.ScrollHintForDrag(firstContentY(m) + h - 1); got != +1 {
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
	m.BeginSelectionAt(firstContentY(m), 0)
	m.ExtendSelectionAt(firstContentY(m)+2, 10)
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
	m.BeginSelectionAt(firstContentY(m), 0)
	m.ExtendSelectionAt(firstContentY(m), 5)
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
	m.BeginSelectionAt(firstContentY(m), 0)
	m.ExtendSelectionAt(firstContentY(m), 5)
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
	m.BeginSelectionAt(firstContentY(m), 0)
	m.ExtendSelectionAt(firstContentY(m)+20, 60)
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
	// Date separator is at line 0 of the cache for newTestModel — i.e.
	// the FIRST content row inside the message area, sitting at pane-local
	// y == firstContentY(m). Click on it and a single column further; the
	// resulting anchor must be on a real message (non-empty MessageID).
	m := newTestModel(60)
	m.BeginSelectionAt(firstContentY(m), 5)
	m.ExtendSelectionAt(firstContentY(m), 6)
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

func TestSelection_ViewIncludesHighlight(t *testing.T) {
	m := newTestModel(60)
	// Begin and extend on the FIRST message's content (one row past the
	// date separator). Cache layout (within the message area):
	// [date_separator, msg1+spacer, msg2] — so msg1 starts at content
	// line 1, which is pane-local y = firstContentY(m) + 1.
	y := firstContentY(m) + 1
	m.BeginSelectionAt(y, 5)
	m.ExtendSelectionAt(y, 15)
	out := m.View(40, 60)
	m.ClearSelection()
	out2 := m.View(40, 60)
	if out == out2 {
		t.Fatal("View output unchanged with active selection — highlight not applied")
	}
}

func TestSelection_HighlightDoesNotCorruptScrollIndicators(t *testing.T) {
	m := newTestModel(60)
	m.SetLoading(true) // forces visible[0] to be the loading hint
	// Begin a selection at the FIRST content row (the row that will hold
	// the hint), extending down so the selection definitely covers that
	// row in the rendered output.
	m.BeginSelectionAt(firstContentY(m), 0)
	m.ExtendSelectionAt(firstContentY(m)+5, 60)
	out := m.View(40, 60)
	// The loading hint string must appear unchanged in the output.
	if !strings.Contains(out, "Loading older messages...") {
		t.Fatalf("expected loading hint string verbatim in output; got %q", out)
	}
	// And specifically: the FIRST line of the message-area portion of
	// the output must equal the cached loading hint exactly. We can
	// derive that line from `out` by splitting on "\n" and finding the
	// row at chrome height.
	lines := strings.Split(out, "\n")
	// Chrome is the channel header (no separator -- intentionally
	// removed so the panel border alone provides the visual boundary).
	// Loading hint is the first line after the chrome.
	if len(lines) < m.chromeHeight+1 {
		t.Fatalf("expected at least %d output lines; got %d", m.chromeHeight+1, len(lines))
	}
	hintRow := lines[m.chromeHeight]
	wantHint := m.renderLoadingOlderHint(60)
	if hintRow != wantHint {
		t.Fatalf("loading hint row corrupted by selection overlay\nwant: %q\ngot:  %q",
			wantHint, hintRow)
	}
}

func TestSelection_HighlightDoesNotCorruptMoreBelowIndicator(t *testing.T) {
	// Build a tall model so View pagination triggers the "more below"
	// indicator. Move selection to top so we can scroll up and pin a
	// "more below" at the visible end.
	msgs := make([]MessageItem, 30)
	for i := range msgs {
		msgs[i] = MessageItem{
			TS:        fmt.Sprintf("%d.0", i+1),
			UserName:  "u",
			UserID:    "U1",
			Text:      fmt.Sprintf("message %d", i),
			Timestamp: "1:00 PM",
		}
	}
	m := New(msgs, "general")
	_ = m.View(15, 60)
	m.GoToTop()
	_ = m.View(15, 60) // ensure cache + yOffset reflect top position with "more below"
	// Selection covers a wide vertical range to overlap the bottom row.
	mp := &m
	m.BeginSelectionAt(firstContentY(mp), 0)
	m.ExtendSelectionAt(firstContentY(mp)+20, 60)
	out := m.View(15, 60)
	if !strings.Contains(out, "-- more below --") {
		t.Fatalf("expected more-below indicator in output; got %q", out)
	}
	// The last message-area line must START with cacheMoreBelow byte-for-byte.
	// (A scrollbar gutter glyph may legitimately follow when the content
	// overflows the visible height, but the cached indicator prefix must
	// be present untouched — otherwise the selection overlay corrupted it.)
	lines := strings.Split(out, "\n")
	lastRow := lines[len(lines)-1]
	if !strings.HasPrefix(lastRow, m.cacheMoreBelow) {
		t.Fatalf("more-below row corrupted by selection overlay\nwant prefix: %q\ngot:        %q",
			m.cacheMoreBelow, lastRow)
	}
}

func TestSelection_HighlightSurvivesScroll(t *testing.T) {
	// Build a model with enough messages that the cache exceeds the
	// pane height; verify that a selection set on row 1 still produces
	// a different View() output after the viewport scrolls.
	//
	// NOTE: New() snaps the initial selection to the LAST message, and
	// the first View() snaps yOffset to maxOffset (bottom). ScrollDown
	// from there is a no-op (clamped). We use ScrollUp so visible
	// content actually changes.
	msgs := make([]MessageItem, 30)
	for i := range msgs {
		msgs[i] = MessageItem{
			TS:        fmt.Sprintf("%d.0", i+1),
			UserName:  "u",
			UserID:    "U1",
			Text:      fmt.Sprintf("message %d", i),
			Timestamp: "1:00 PM",
		}
	}
	m := New(msgs, "general")
	_ = m.View(20, 60)
	mp := &m
	y := firstContentY(mp) + 2
	m.BeginSelectionAt(y, 0)
	m.ExtendSelectionAt(y, 20)
	withSel := m.View(20, 60)
	m.ScrollUp(5)
	withSelScrolled := m.View(20, 60)
	// They should differ (different visible content).
	if withSel == withSelScrolled {
		t.Fatal("scroll did not change View output")
	}
	// And both should still have the selection (HasSelection true).
	if !m.HasSelection() {
		t.Fatal("HasSelection should remain true across scroll")
	}
}

// TestSelection_ChromeRowsAreNotSelectable pins the off-by-chrome bug
// fix: a click on the chrome (channel header / separator at the top of
// the message pane) must NOT anchor a selection. The pane-local y
// values 0..chromeHeight-1 are inside the chrome.
func TestSelection_ChromeRowsAreNotSelectable(t *testing.T) {
	m := newTestModel(60)
	if m.chromeHeight < 1 {
		t.Fatalf("test precondition: expected non-zero chromeHeight; got %d", m.chromeHeight)
	}
	m.BeginSelectionAt(0, 5)
	if m.HasSelection() {
		t.Fatal("BeginSelectionAt on chrome must not anchor a selection")
	}
}

// TestSelection_FirstContentRowAnchorsAtFirstMessage pins the other
// half of the off-by-chrome fix: pane-local y == firstContentY(m) is
// the FIRST message-content row (the date separator), which the drag
// snaps forward onto msg 1.0 = "alice  hello world". A drag spanning
// the next row (so the end anchor lands on real message content)
// must produce text containing the first message.
func TestSelection_FirstContentRowAnchorsAtFirstMessage(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(firstContentY(m), 0)
	// Drag down two content rows to land squarely inside msg 1's body.
	m.ExtendSelectionAt(firstContentY(m)+2, 30)
	text, ok := m.EndSelection()
	if !ok || text == "" {
		t.Fatalf("first-content-row drag should produce text; got ok=%v text=%q", ok, text)
	}
	// The text should come from the FIRST message — alice's "hello world".
	if !strings.Contains(text, "alice") && !strings.Contains(text, "hello") {
		t.Fatalf("expected first-message content; got %q", text)
	}
}
