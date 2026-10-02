package thread

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// Tuesday 2026-09-29 16:00 LOCAL; see messages/longtimestamp_test.go.
func threadLongTSClock() time.Time {
	return time.Date(2026, 9, 29, 16, 0, 0, 0, time.Local)
}

func threadLongTSAt(minute int) string {
	return fmt.Sprintf("%d.000100", time.Date(2026, 9, 29, 15, minute, 0, 0, time.Local).Unix())
}

// threadLongTS: parent priya 3:40, replies sam 3:41 and lee 3:42.
// SetThread selects the newest reply (lee).
func threadLongTS(t *testing.T, parentName, lastName string, width, height int) (*Model, []string) {
	t.Helper()
	styles.Apply("dark", config.Theme{})
	messages.SetNowFunc(threadLongTSClock)
	t.Cleanup(func() { messages.SetNowFunc(nil) })
	m := New()
	parent := messages.MessageItem{TS: threadLongTSAt(40), UserID: "U1", UserName: parentName, Text: "parent", Timestamp: "3:40 PM"}
	replies := []messages.MessageItem{
		{TS: threadLongTSAt(41), UserID: "U2", UserName: "sam", Text: "first reply", Timestamp: "3:41 PM"},
		{TS: threadLongTSAt(42), UserID: "U3", UserName: lastName, Text: "second reply", Timestamp: "3:42 PM"},
	}
	m.SetThread(parent, replies, "C1", parent.TS)
	return m, stripRows(m.View(height, width))
}

func stripRows(view string) []string {
	rows := strings.Split(view, "\n")
	for i := range rows {
		rows[i] = ansi.Strip(rows[i])
	}
	return rows
}

func assertThreadHeightsMatch(t *testing.T, m *Model) {
	t.Helper()
	for i, e := range m.cache {
		if len(e.linesSelected) != len(e.linesNormal) {
			t.Errorf("reply %d: len(linesSelected)=%d != len(linesNormal)=%d",
				i, len(e.linesSelected), len(e.linesNormal))
		}
		if e.linesSelectedShort != nil && len(e.linesSelectedShort) != len(e.linesNormal) {
			t.Errorf("reply %d: len(linesSelectedShort)=%d != len(linesNormal)=%d",
				i, len(e.linesSelectedShort), len(e.linesNormal))
		}
	}
}

func assertPaneGeometry(t *testing.T, rows []string, width, height int) {
	t.Helper()
	if len(rows) != height {
		t.Errorf("pane has %d rows, want %d", len(rows), height)
	}
	for i, r := range rows {
		if w := ansi.StringWidth(r); w != width {
			t.Errorf("row %d width %d, want %d: %q", i, w, width, r)
		}
	}
}

func TestThreadSelectedTimestamp_SelectedReplyShowsLong(t *testing.T) {
	m, rows := threadLongTS(t, "priya", "lee", 80, 30)
	view := strings.Join(rows, "\n")
	for _, want := range []string{"lee  Tue Sep 29, 3:42 PM", "priya  3:40 PM", "sam  3:41 PM"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if n := strings.Count(view, "Tue Sep 29, "); n != 1 {
		t.Errorf("long timestamp appears %d times, want 1:\n%s", n, view)
	}
	assertThreadHeightsMatch(t, m)
}

func TestThreadSelectedTimestamp_MovesWithSelection(t *testing.T) {
	m, _ := threadLongTS(t, "priya", "lee", 80, 30)
	m.MoveUp()
	view := strings.Join(stripRows(m.View(30, 80)), "\n")
	for _, want := range []string{"sam  Tue Sep 29, 3:41 PM", "lee  3:42 PM"} {
		if !strings.Contains(view, want) {
			t.Errorf("after MoveUp, view missing %q:\n%s", want, view)
		}
	}
}

func TestThreadSelectedTimestamp_ParentSelectedShowsLong(t *testing.T) {
	m, _ := threadLongTS(t, "priya", "lee", 80, 30)
	m.GoToTop()
	m.MoveUp() // parent
	view := strings.Join(stripRows(m.View(30, 80)), "\n")
	if !strings.Contains(view, "priya  Tue Sep 29, 3:40 PM") {
		t.Fatalf("selected parent missing long timestamp:\n%s", view)
	}
	if !strings.Contains(view, "lee  3:42 PM") {
		t.Fatalf("unselected reply should be short:\n%s", view)
	}
	var plain []string
	for _, pl := range m.parentEntry.linesPlain {
		plain = append(plain, pl.Text)
	}
	if got := strings.Join(plain, "\n"); strings.Contains(got, "Sep 29") || !strings.Contains(got, "3:40 PM") {
		t.Fatalf("parent linesPlain must keep the short form; got %q", got)
	}
}

func TestThreadSelectedTimestamp_UnselectedParentShort(t *testing.T) {
	_, rows := threadLongTS(t, "priya", "lee", 80, 30)
	view := strings.Join(rows, "\n")
	if !strings.Contains(view, "priya  3:40 PM") || strings.Contains(view, "priya  Tue") {
		t.Fatalf("unselected parent should show the short timestamp:\n%s", view)
	}
}

func TestThreadSelectedTimestamp_NarrowReplyFallsBack(t *testing.T) {
	// 24-col name: short header 33 fits contentWidth 36; long 45 does not.
	m, rows := threadLongTS(t, "priya", "a-quite-long-displayname", 40, 30)
	view := strings.Join(rows, "\n")
	if strings.Contains(view, "Sep 29") {
		t.Fatalf("narrow thread shows long timestamp:\n%s", view)
	}
	assertThreadHeightsMatch(t, m)
}

// Review Focus 5: the parent is not width-filled; an overflowing header
// would be wrapped by the outer pane style and break the pane geometry.
func TestThreadSelectedTimestamp_NarrowParentFallsBack(t *testing.T) {
	m, _ := threadLongTS(t, "a-quite-long-displayname", "lee", 40, 30)
	m.GoToTop()
	m.MoveUp() // parent
	rows := stripRows(m.View(30, 40))
	if view := strings.Join(rows, "\n"); strings.Contains(view, "Sep 29") {
		t.Fatalf("narrow selected parent shows long timestamp:\n%s", view)
	}
	assertPaneGeometry(t, rows, 40, 30)
}

// Review Focus 1: at width 20, contentWidth is floored at 20 but the row
// has only width-1 = 19 columns. "a  Tue Sep 29, 15:42" is 20 wide.
func TestThreadSelectedTimestamp_TinyWidthFallsBack(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	messages.SetNowFunc(threadLongTSClock)
	t.Cleanup(func() { messages.SetNowFunc(nil) })
	m := New()
	parent := messages.MessageItem{TS: threadLongTSAt(40), UserID: "U1", UserName: "p", Text: "x", Timestamp: "15:40"}
	reply := messages.MessageItem{TS: threadLongTSAt(42), UserID: "U2", UserName: "a", Text: "hi", Timestamp: "15:42"}
	m.SetThread(parent, []messages.MessageItem{reply}, "C1", parent.TS)
	_ = m.View(30, 20)
	for _, e := range m.cache {
		if got := ansi.Strip(strings.Join(e.linesSelected, "\n")); strings.Contains(got, "Sep 29") {
			t.Fatalf("tiny-width selected reply shows long timestamp:\n%s", got)
		}
	}
	assertThreadHeightsMatch(t, m)
}

// The drag overlay splices linesPlain (short form) into the displayed row
// by column, so while a text selection exists the selected row must show
// the short form or the header is garbled.
func TestThreadSelectedTimestamp_DragOverReplyHeaderKeepsTextIntact(t *testing.T) {
	m, _ := threadLongTS(t, "priya", "lee", 80, 30)
	y := m.chromeHeight + m.entryOffsets[len(m.entryOffsets)-1] - m.vp.YOffset()
	m.BeginSelectionAt(y, 0)
	m.ExtendSelectionAt(y, 8)
	view := strings.Join(stripRows(m.View(30, 80)), "\n")
	if !strings.Contains(view, "lee  3:42 PM") || strings.Contains(view, "Sep 29") {
		t.Fatalf("drag over selected reply header garbled it or kept the long form:\n%s", view)
	}
	m.ClearSelection()
	view = strings.Join(stripRows(m.View(30, 80)), "\n")
	if !strings.Contains(view, "lee  Tue Sep 29, 3:42 PM") {
		t.Fatalf("long form did not return after ClearSelection:\n%s", view)
	}
}

func TestThreadSelectedTimestamp_DragOverParentHeaderKeepsTextIntact(t *testing.T) {
	m, _ := threadLongTS(t, "priya", "lee", 80, 30)
	m.GoToTop()
	m.MoveUp() // parent
	_ = m.View(30, 80)
	y := m.chromeHeight - m.vp.YOffset()
	m.BeginSelectionAt(y, 0)
	m.ExtendSelectionAt(y, 8)
	view := strings.Join(stripRows(m.View(30, 80)), "\n")
	if !strings.Contains(view, "priya  3:40 PM") || strings.Contains(view, "Sep 29") {
		t.Fatalf("drag over selected parent header garbled it or kept the long form:\n%s", view)
	}
	m.ClearSelection()
	view = strings.Join(stripRows(m.View(30, 80)), "\n")
	if !strings.Contains(view, "priya  Tue Sep 29, 3:40 PM") {
		t.Fatalf("long form did not return on the parent after ClearSelection:\n%s", view)
	}
}

// See messages TestSelectedTimestamp_ShortVariantBuiltLazily.
func TestThreadSelectedTimestamp_ShortVariantBuiltLazily(t *testing.T) {
	m, _ := threadLongTS(t, "priya", "lee", 80, 30)
	for i, e := range m.cache {
		if e.linesSelectedShort != nil {
			t.Fatalf("reply %d built a short variant with no text selection", i)
		}
	}
	y := m.chromeHeight + m.entryOffsets[len(m.entryOffsets)-1] - m.vp.YOffset()
	m.BeginSelectionAt(y, 0)
	m.ExtendSelectionAt(y, 8)
	_ = m.View(30, 80)
	m.MoveUp() // selection stays pinned; the cursor lands on sam
	view := strings.Join(stripRows(m.View(30, 80)), "\n")
	if !strings.Contains(view, "sam  3:41 PM") || strings.Contains(view, "Sep 29") {
		t.Fatalf("j/k under a pinned selection should show the short form:\n%s", view)
	}
	assertThreadHeightsMatch(t, m)
}

func TestThreadSelectedTimestamp_CopyUsesShortForm(t *testing.T) {
	m, _ := threadLongTS(t, "priya", "lee", 80, 30)
	m.BeginSelectionAt(firstContentY(m), 0)
	m.ExtendSelectionAt(firstContentY(m)+40, 80)
	text, ok := m.EndSelection()
	if !ok {
		t.Fatal("EndSelection ok=false")
	}
	if !strings.Contains(text, "lee  3:42 PM") || strings.Contains(text, "Sep 29") {
		t.Fatalf("copied text should carry the short form only; got %q", text)
	}
}
