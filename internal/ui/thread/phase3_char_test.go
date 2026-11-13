package thread

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// Phase 3 characterization (plan Task 0.2). Each test pins one behaviour
// the shared substrate must preserve for the thread pane; the comment
// names the spec item (§5) it pins.

func p3Thread(t *testing.T, n int) *Model {
	t.Helper()
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
	m := New()
	m.SetThread(
		messages.MessageItem{TS: "100.0", UserName: "alice", Text: "parent", Timestamp: "9:00 AM"},
		markedReplies(n), "C1", "100.0",
	)
	return m
}

func p3Rows(view string) []string {
	rows := strings.Split(view, "\n")
	for i := range rows {
		rows[i] = ansi.Strip(rows[i])
	}
	return rows
}

// p3RowWith returns the first row containing anchor, failing if none does.
func p3RowWith(t *testing.T, rows []string, anchor string) int {
	t.Helper()
	for i, r := range rows {
		if strings.Contains(r, anchor) {
			return i
		}
	}
	t.Fatalf("no row contains %q:\n%s", anchor, strings.Join(rows, "\n"))
	return -1
}

// U6: thread clamps the scroll offset eagerly (bubbles/viewport today), so
// scrolling past the bottom is a no-op and a following ScrollUp(1) moves
// one line up from the bottom.
func TestChar_ThreadScrollClampsEagerly(t *testing.T) {
	a := p3Thread(t, 40)
	bottom := a.View(20, 80)
	a.ScrollDown(50)
	a.ScrollUp(1)
	got := a.View(20, 80)

	b := p3Thread(t, 40)
	_ = b.View(20, 80)
	b.ScrollUp(1)
	want := b.View(20, 80)

	if want == bottom {
		t.Fatal("test setup: ScrollUp(1) from the bottom did not move the view")
	}
	if got != want {
		t.Error("ScrollDown past the bottom was not a no-op (thread clamps eagerly, U6)")
	}
}

// U6: with no content yet, scrolling is a no-op but still bumps Version.
func TestChar_ThreadScrollWithoutContentStillDirties(t *testing.T) {
	m := New()
	v := m.Version()
	m.ScrollUp(3)
	m.ScrollDown(3)
	if got := m.Version(); got != v+2 {
		t.Errorf("Version after two scrolls on an empty thread = %d, want %d", got, v+2)
	}
}

// U1 off: scrolling the selected reply off-screen leaves the cursor alone.
func TestChar_ThreadDoesNotFollowCursorOnScroll(t *testing.T) {
	m := p3Thread(t, 40)
	_ = m.View(20, 80)
	before := m.SelectedReply().TS
	m.ScrollUp(200)
	_ = m.View(20, 80)
	if got := m.SelectedReply().TS; got != before {
		t.Errorf("selection followed the scroll: %s -> %s (thread must not, U1)", before, got)
	}
}

// U2 off: a shorter pane keeps the same top row (no bottom pin).
func TestChar_ThreadDoesNotPinBottomOnShrink(t *testing.T) {
	m := p3Thread(t, 40)
	before := p3Rows(m.View(20, 80))
	after := p3Rows(m.View(17, 80))
	top := m.chromeHeight
	if before[top] != after[top] {
		t.Errorf("shrinking moved the top content row (thread must not pin, U2):\n before %q\n after  %q",
			before[top], after[top])
	}
}

// U3 off: no "-- more below --" row, however far up the pane is scrolled.
func TestChar_ThreadHasNoMoreBelowRow(t *testing.T) {
	m := p3Thread(t, 40)
	_ = m.View(20, 80)
	m.ScrollUp(30)
	if v := strings.Join(p3Rows(m.View(20, 80)), "\n"); strings.Contains(v, "more below") {
		t.Errorf("thread drew a more-below row (U3):\n%s", v)
	}
}

// U4 on: every drag step that changes the selection bumps Version.
func TestChar_ThreadDragBumpsVersion(t *testing.T) {
	m := p3Thread(t, 3)
	rows := p3Rows(m.View(40, 80))
	r := p3RowWith(t, rows, "mark001")
	v0 := m.Version()
	m.BeginSelectionAt(r, 3)
	v1 := m.Version()
	m.ExtendSelectionAt(r, 10)
	v2 := m.Version()
	m.ExtendSelectionAt(r, 10)
	v3 := m.Version()
	if _, ok := m.EndSelection(); !ok {
		t.Fatal("test setup: the drag selected nothing")
	}
	v4 := m.Version()
	if !(v1 > v0 && v2 > v1 && v3 == v2 && v4 > v3) {
		t.Errorf("Version over a drag = %d,%d,%d,%d,%d; want begin+, extend+, no-op extend =, end+ (U4)",
			v0, v1, v2, v3, v4)
	}
}

// 8(c) off: a press on a separator rule anchors nothing.
func TestChar_ThreadSelectionSkipsSeparators(t *testing.T) {
	m := p3Thread(t, 3)
	rows := p3Rows(m.View(40, 80))
	rule := -1
	for i := m.chromeHeight; i < len(rows); i++ {
		if r := strings.TrimSpace(rows[i]); r != "" && strings.Trim(r, "─") == "" {
			rule = i
			break
		}
	}
	if rule < 0 {
		t.Fatalf("test setup: no separator rule found:\n%s", strings.Join(rows, "\n"))
	}
	m.BeginSelectionAt(rule, 3)
	if m.HasSelection() {
		t.Error("a press on a separator started a selection (thread must not snap, 8c)")
	}
}

// 8(c) off: copied text never contains a separator rule.
func TestChar_ThreadCopyExcludesSeparators(t *testing.T) {
	m := p3Thread(t, 3)
	rows := p3Rows(m.View(40, 80))
	from := p3RowWith(t, rows, "mark000")
	to := p3RowWith(t, rows, "mark001")
	m.BeginSelectionAt(from, 3)
	m.ExtendSelectionAt(to, 8)
	text, ok := m.EndSelection()
	if !ok || !strings.HasPrefix(text, "rk000") {
		t.Fatalf("test setup: copy = %q, ok=%v", text, ok)
	}
	if strings.Contains(text, "─") {
		t.Errorf("copy contains a separator rule:\n%s", text)
	}
}

// Review focus 5: a new reply while scrolled up snaps to the bottom.
func TestChar_ThreadNewReplySnapsToBottom(t *testing.T) {
	m := p3Thread(t, 40)
	_ = m.View(20, 80)
	m.ScrollUp(100)
	_ = m.View(20, 80)
	m.AddReply(messages.MessageItem{TS: "1800000000.000000", UserName: "carol", Text: "mark999 fresh", Timestamp: "11:00 AM"})
	if v := strings.Join(p3Rows(m.View(20, 80)), "\n"); !strings.Contains(v, "mark999") {
		t.Errorf("new reply not on screen after AddReply:\n%s", v)
	}
}

// Review focus 4: a resize during a selection keeps it, and the copy
// still starts where the drag started.
func TestChar_ThreadSelectionSurvivesResize(t *testing.T) {
	m := p3Thread(t, 3)
	rows := p3Rows(m.View(40, 80))
	m.BeginSelectionAt(p3RowWith(t, rows, "mark000"), 3)
	m.ExtendSelectionAt(p3RowWith(t, rows, "mark001"), 8)
	_ = m.View(40, 60)
	if !m.HasSelection() {
		t.Fatal("resize dropped the selection")
	}
	if got := m.SelectionText(); !strings.HasPrefix(got, "rk000") {
		t.Errorf("after resize the copy = %q, want prefix %q", got, "rk000")
	}
}

// Review focus 2: in a zero-reply thread the parent is selectable.
func TestChar_ThreadZeroRepliesParentSelectable(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
	m := New()
	m.SetThread(messages.MessageItem{TS: "100.0", UserName: "alice", Text: "zero-reply parent text", Timestamp: "9:00 AM"},
		nil, "C1", "100.0")
	rows := p3Rows(m.View(20, 60))
	r := p3RowWith(t, rows, "zero-reply")
	m.BeginSelectionAt(r, 3)
	m.ExtendSelectionAt(r, 12)
	if text, ok := m.EndSelection(); !ok || text != "ro-reply" {
		t.Errorf("copy from a zero-reply parent = %q, ok=%v; want %q", text, ok, "ro-reply")
	}
}

// Review focus 2: a parent taller than the pane still scrolls with no replies.
func TestChar_ThreadZeroRepliesTallParentScrolls(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
	m := New()
	m.SetThread(messages.MessageItem{TS: "100.0", UserName: "alice", Text: strings.Repeat("word ", 200), Timestamp: "9:00 AM"},
		nil, "C1", "100.0")
	before := m.View(10, 30)
	m.ScrollDown(3)
	if after := m.View(10, 30); after == before {
		t.Error("a tall zero-reply parent did not scroll")
	}
}

// §2 item 2: no content line is ever wider than the pane, so dropping
// viewport's ansi.Cut (PR 2+3) cannot change output. If this fails, stop
// PR 2+3: the Cut matters.
func TestChar_ThreadOverwideLinesNeverExceedPane(t *testing.T) {
	bodies := []string{
		"☺ ❤ ☀ ✔ ✈ ♥ ⚠ ↔ ™ ©", "🇺🇸🏳️‍🌈👩‍👩‍👧 x", "```\n" + strings.Repeat("abcdefghij", 20) + "\n```",
		strings.Repeat("ｗ", 50), "e\u0301\u0301\u0301 zalgo", "\t\ttabs\there",
		"a" + strings.Repeat("\u200b", 40) + "b", "<https://example.com/" + strings.Repeat("x", 120) + "|link>",
		strings.Repeat("🎉", 30),
	}
	for _, body := range bodies {
		m := p3Thread(t, 0)
		m.SetThread(messages.MessageItem{TS: "1.0", UserName: "p", Text: "parent", Timestamp: "9:00"},
			[]messages.MessageItem{{TS: "2.0", UserName: "u", Text: body, Timestamp: "9:01"}}, "C", "1.0")
		_ = m.View(30, 40)
		for _, l := range strings.Split(joinedContent(m), "\n") {
			if w := ansi.StringWidth(l); w > 40 {
				t.Errorf("body %q: a content line is %d columns in a 40-column pane", body, w)
			}
		}
	}
}

// Leaving the thread repaints the selected reply with the unfocused
// shadow. It used to keep the focused border (the reply cache ignored
// focus).
func TestThreadFocusFlipUsesUnfocusedBorder(t *testing.T) {
	mk := func() *Model { return p3Thread(t, 1) }
	a := mk()
	a.SetFocused(true)
	_ = a.View(20, 40)
	a.SetFocused(false)
	flipped := a.View(20, 40)

	c := mk()
	c.SetFocused(false)
	if fresh := c.View(20, 40); flipped != fresh {
		t.Error("focus leaving the thread did not repaint the selected reply")
	}
}

// Between an invalidation (here SelectByIndex) and the next View, the
// reply cache is gone: lookups see only the parent, whose entry is
// re-rendered every frame. The substrate keeps this via KeepLookups(1).
func TestChar_ThreadLookupsSeeOnlyTheParentUntilRebuilt(t *testing.T) {
	m := p3Thread(t, 2)
	rows := p3Rows(m.View(40, 80))
	reply := p3RowWith(t, rows, "mark001")
	parent := p3RowWith(t, rows, "parent")
	m.SelectByIndex(0) // the cursor was on reply 1, so this invalidates
	m.ClickAt(reply)
	if got := m.SelectedReply(); got == nil || got.TS != markedReplies(2)[0].TS {
		t.Errorf("a click on a discarded reply moved the cursor to %+v", got)
	}
	m.ClickAt(parent)
	if got := m.SelectedReply(); got == nil || got.TS != "100.0" {
		t.Errorf("a click on the parent did not select it: %+v", got)
	}
}

// Review focus 1: a theme switch reaches the window padding and the
// scrollbar on the next frame (App.applyTheme invalidates the pane).
func TestThread_ThemeSwitchRestylesWindow(t *testing.T) {
	m := p3Thread(t, 1)
	before := strings.Split(m.View(30, 60), "\n")
	styles.Apply("light", config.Theme{}) // p3Thread's cleanup restores dark
	m.InvalidateCache()                   // what App.applyTheme does
	after := strings.Split(m.View(30, 60), "\n")
	last := len(after) - 1
	if ansi.Strip(before[last]) != ansi.Strip(after[last]) {
		t.Fatalf("test setup: the padding row's text changed: %q -> %q", ansi.Strip(before[last]), ansi.Strip(after[last]))
	}
	if before[last] == after[last] {
		t.Error("the padding row kept the old theme's background after a theme switch")
	}
}

// Thread clamps a click below the content to the last line, so it
// selects the last reply (messages' ClickAt misses instead).
func TestChar_ThreadClickBelowContentSelectsLastReply(t *testing.T) {
	m := p3Thread(t, 2)
	_ = m.View(40, 80)
	m.SelectByIndex(0)
	_ = m.View(40, 80) // SelectByIndex invalidates the reply cache; ClickAt reads it
	m.ClickAt(39)
	if got := m.SelectedReply(); got == nil || got.TS != markedReplies(2)[1].TS {
		t.Errorf("click below the content selected %+v, want the last reply", got)
	}
}
