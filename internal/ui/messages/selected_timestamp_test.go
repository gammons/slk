package messages

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// longTSItems: three messages on 2026-09-29 (the pinned clock's day).
// messages.New selects the newest (lee).
func longTSItems() []MessageItem {
	day := time.Date(2026, 9, 29, 15, 0, 0, 0, time.Local)
	return []MessageItem{
		{TS: longTSAt(day.Add(40 * time.Minute)), UserID: "U1", UserName: "priya", Text: "first", Timestamp: "3:40 PM"},
		{TS: longTSAt(day.Add(41 * time.Minute)), UserID: "U2", UserName: "sam", Text: "second", Timestamp: "3:41 PM"},
		{TS: longTSAt(day.Add(42 * time.Minute)), UserID: "U3", UserName: "lee", Text: "third", Timestamp: "3:42 PM"},
	}
}

func longTSModel(t *testing.T, items []MessageItem, width int) (*Model, string) {
	t.Helper()
	pinLongTSClock(t)
	m := New(items, "general")
	view := ansi.Strip(m.View(40, width))
	return &m, view
}

// selectedEntry returns the cache entry for the selected message.
func selectedEntry(t *testing.T, m *Model) viewEntry {
	t.Helper()
	for _, e := range m.cache {
		if e.msgIdx == m.selected {
			return e
		}
	}
	t.Fatalf("no cache entry for selected index %d", m.selected)
	return viewEntry{}
}

func assertHeightsMatch(t *testing.T, m *Model) {
	t.Helper()
	for i, e := range m.cache {
		if len(e.linesSelected) != len(e.linesNormal) {
			t.Errorf("entry %d: len(linesSelected)=%d != len(linesNormal)=%d",
				i, len(e.linesSelected), len(e.linesNormal))
		}
		if e.linesSelectedShort != nil && len(e.linesSelectedShort) != len(e.linesNormal) {
			t.Errorf("entry %d: len(linesSelectedShort)=%d != len(linesNormal)=%d",
				i, len(e.linesSelectedShort), len(e.linesNormal))
		}
	}
}

func TestSelectedTimestamp_SelectedRowShowsLong(t *testing.T) {
	m, view := longTSModel(t, longTSItems(), 80)
	for _, want := range []string{"lee  Tue Sep 29, 3:42 PM", "priya  3:40 PM", "sam  3:41 PM"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if n := strings.Count(view, "Tue Sep 29, "); n != 1 {
		t.Errorf("long timestamp appears %d times, want 1:\n%s", n, view)
	}
	assertHeightsMatch(t, m)
}

func TestSelectedTimestamp_MovesWithSelection(t *testing.T) {
	m, _ := longTSModel(t, longTSItems(), 80)
	m.MoveUp()
	view := ansi.Strip(m.View(40, 80))
	for _, want := range []string{"sam  Tue Sep 29, 3:41 PM", "lee  3:42 PM"} {
		if !strings.Contains(view, want) {
			t.Errorf("after MoveUp, view missing %q:\n%s", want, view)
		}
	}
}

func TestSelectedTimestamp_UnfocusedStillLong(t *testing.T) {
	m, _ := longTSModel(t, longTSItems(), 80)
	m.SetFocused(true)
	_ = m.View(40, 80)
	m.SetFocused(false)
	view := ansi.Strip(m.View(40, 80))
	if !strings.Contains(view, "lee  Tue Sep 29, 3:42 PM") {
		t.Fatalf("unfocused selection lost the long timestamp:\n%s", view)
	}
}

func TestSelectedTimestamp_NarrowFallsBackToShort(t *testing.T) {
	items := longTSItems()
	// 24-col name: short header 33 cols fits in contentWidth 36; long
	// header 45 does not, and would wrap inside the width-1 fill.
	items[2].UserName = "a-quite-long-displayname"
	m, view := longTSModel(t, items, 40)
	if strings.Contains(view, "Sep 29") {
		t.Fatalf("narrow pane shows long timestamp:\n%s", view)
	}
	if !strings.Contains(view, "a-quite-long-displayname  3:42 PM") {
		t.Fatalf("narrow pane lost the short timestamp:\n%s", view)
	}
	assertHeightsMatch(t, m)
}

// Review Focus 1: with an avatar at width 24, contentWidth is floored at
// 20 but the header row only has width-1-5 = 18 columns. A 20-col long
// header must be refused.
func TestSelectedTimestamp_AvatarNarrowFallsBack(t *testing.T) {
	pinLongTSClock(t)
	day := time.Date(2026, 9, 29, 15, 42, 0, 0, time.Local)
	m := New([]MessageItem{
		{TS: longTSAt(day), UserID: "U1", UserName: "a", Text: "hi", Timestamp: "15:42"},
	}, "general")
	m.SetAvatarFunc(func(string) string { return "AAAA\nAAAA" })
	_ = m.View(40, 24)
	e := selectedEntry(t, &m)
	if got := ansi.Strip(strings.Join(e.linesSelected, "\n")); strings.Contains(got, "Sep 29") {
		t.Fatalf("avatar-narrow selected row shows long timestamp:\n%s", got)
	}
	assertHeightsMatch(t, &m)
}

// Review Focus 3: the (edited) mark is part of the header row.
func TestSelectedTimestamp_EditedMarkCountsTowardWidth(t *testing.T) {
	items := longTSItems()
	// "abcdefghijklmn  Tue Sep 29, 3:42 PM" = 35 cols fits contentWidth 36
	// at width 40; " (edited)" makes it 44 and must force the short form.
	items[2].UserName = "abcdefghijklmn"
	items[2].IsEdited = true
	m, view := longTSModel(t, items, 40)
	if strings.Contains(view, "Sep 29") {
		t.Fatalf("edited header overflowed with long timestamp:\n%s", view)
	}
	assertHeightsMatch(t, m)
}

// selectedHeaderY returns the pane-local viewportY of the selected
// message's header row (BeginSelectionAt's coordinate system).
func selectedHeaderY(t *testing.T, m *Model) int {
	t.Helper()
	for i, e := range m.cache {
		if e.msgIdx == m.selected {
			return m.chromeHeight + m.entryOffsets[i] - m.yOffset
		}
	}
	t.Fatalf("no cache entry for selected index %d", m.selected)
	return 0
}

// The drag overlay splices linesPlain (short form) into the displayed row
// by column, so while a text selection exists the selected row must show
// the short form or the header is garbled.
func TestSelectedTimestamp_DragOverHeaderKeepsTextIntact(t *testing.T) {
	m, _ := longTSModel(t, longTSItems(), 80)
	y := selectedHeaderY(t, m)
	m.BeginSelectionAt(y, 0)
	m.ExtendSelectionAt(y, 8)
	view := ansi.Strip(m.View(40, 80))
	if !strings.Contains(view, "lee  3:42 PM") || strings.Contains(view, "Sep 29") {
		t.Fatalf("drag over selected header garbled it or kept the long form:\n%s", view)
	}
	m.ClearSelection()
	view = ansi.Strip(m.View(40, 80))
	if !strings.Contains(view, "lee  Tue Sep 29, 3:42 PM") {
		t.Fatalf("long form did not return after ClearSelection:\n%s", view)
	}
}

// The App caches the selection-free bordered render by Version(), so a
// change in which selected variant is shown must bump it, but drag motion
// must not (perf: one bump per cell of motion would defeat the cache).
func TestSelectedTimestamp_SelectionToggleBumpsVersion(t *testing.T) {
	m, _ := longTSModel(t, longTSItems(), 80)
	y := selectedHeaderY(t, m)
	v0 := m.Version()
	m.BeginSelectionAt(y, 0)
	v1 := m.Version()
	if v1 == v0 {
		t.Fatal("BeginSelectionAt did not bump Version; the App cache would keep the long-form row")
	}
	m.ExtendSelectionAt(y, 8)
	if m.Version() != v1 {
		t.Fatal("ExtendSelectionAt bumped Version; drag motion must stay cache-friendly")
	}
	m.ClearSelection()
	if m.Version() == v1 {
		t.Fatal("ClearSelection did not bump Version; the long form would not return")
	}
}

// The short-form selected variant is only ever drawn for the selected row
// while a text selection exists, so building it for every entry on every
// cache build is waste (it roughly doubles the fill+border cost). It is
// built on demand, and j/k while a selection is pinned must still get it.
func TestSelectedTimestamp_ShortVariantBuiltLazily(t *testing.T) {
	m, _ := longTSModel(t, longTSItems(), 80)
	for i, e := range m.cache {
		if e.linesSelectedShort != nil {
			t.Fatalf("entry %d built a short variant with no text selection", i)
		}
	}
	y := selectedHeaderY(t, m)
	m.BeginSelectionAt(y, 0)
	m.ExtendSelectionAt(y, 8)
	_ = m.View(40, 80)
	m.MoveUp() // selection stays pinned; the cursor lands on sam
	view := ansi.Strip(m.View(40, 80))
	if !strings.Contains(view, "sam  3:41 PM") || strings.Contains(view, "Sep 29") {
		t.Fatalf("j/k under a pinned selection should show the short form:\n%s", view)
	}
	assertHeightsMatch(t, m)
}

func TestSelectedTimestamp_CopyUsesShortForm(t *testing.T) {
	m, _ := longTSModel(t, longTSItems(), 80)
	m.BeginSelectionAt(m.chromeHeight, 0)
	m.ExtendSelectionAt(m.chromeHeight+40, 80)
	text, ok := m.EndSelection()
	if !ok {
		t.Fatal("EndSelection ok=false")
	}
	if !strings.Contains(text, "lee  3:42 PM") || strings.Contains(text, "Sep 29") {
		t.Fatalf("copied text should carry the short form only; got %q", text)
	}
}
