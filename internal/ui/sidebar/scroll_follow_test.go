package sidebar

import (
	"fmt"
	"testing"
)

// scrollFollowModel builds a sidebar with enough channel rows to scroll
// well past a 10-row viewport, with the Channels section expanded so the
// rows are navigable. Nav order: Threads → "Channels" header → C01..C40.
func scrollFollowModel(t *testing.T) Model {
	t.Helper()
	items := make([]ChannelItem, 40)
	for i := range items {
		items[i] = ChannelItem{ID: fmt.Sprintf("C%02d", i+1), Name: fmt.Sprintf("chan-%02d", i+1), Type: "channel"}
	}
	m := New(items)
	m.ToggleCollapse("Channels")
	return m
}

// selectedLine returns the cacheRows line index of the cursor row, or -1.
// Mirrors the lookup View() performs before snapping.
func selectedLine(m *Model) int {
	for i, r := range m.cacheRows {
		if r.navIdx == m.cursor {
			return i
		}
	}
	return -1
}

func selectionVisible(m *Model, height int) bool {
	line := selectedLine(m)
	return line >= m.yOffset && line < m.yOffset+height
}

// TestCursorFollowsScrollDown asserts that scrolling the viewport down far
// enough to push the selected row off the top edge drags the cursor with
// it, clamping to the topmost still-visible navigable row.
func TestCursorFollowsScrollDown(t *testing.T) {
	const height = 10
	m := scrollFollowModel(t)
	_ = m.View(height, 30)
	if !m.IsThreadsSelected() {
		t.Fatal("setup: expected the Threads row selected at the top")
	}

	m.ScrollDown(20)
	_ = m.View(height, 30)

	if m.IsThreadsSelected() {
		t.Error("cursor did not follow scroll down: still on the Threads row")
	}
	if !selectionVisible(&m, height) {
		t.Errorf("selection not visible after scroll down: line=%d window=[%d,%d)",
			selectedLine(&m), m.yOffset, m.yOffset+height)
	}
	// Topmost visible navigable row: the cursor row must be the first
	// line of the window (no blank separators sit inside the channel run).
	if got := selectedLine(&m); got != m.yOffset {
		t.Errorf("cursor line = %d, want the top of the window %d", got, m.yOffset)
	}
}

// TestCursorFollowsScrollUp asserts the mirror case: scrolling up past the
// selected (bottom) row drags the cursor to the bottommost visible row.
func TestCursorFollowsScrollUp(t *testing.T) {
	const height = 10
	m := scrollFollowModel(t)
	m.GoToBottom()
	_ = m.View(height, 30)
	if got := m.SelectedID(); got != "C40" {
		t.Fatalf("setup: expected C40 selected at the bottom, got %q", got)
	}

	m.ScrollUp(20)
	_ = m.View(height, 30)

	if got := m.SelectedID(); got == "C40" {
		t.Error("cursor did not follow scroll up: still on C40")
	}
	if !selectionVisible(&m, height) {
		t.Errorf("selection not visible after scroll up: line=%d window=[%d,%d)",
			selectedLine(&m), m.yOffset, m.yOffset+height)
	}
	if got, want := selectedLine(&m), m.yOffset+height-1; got != want {
		t.Errorf("cursor line = %d, want the bottom of the window %d", got, want)
	}
}

// TestCursorUnchangedWhenScrollKeepsSelectionVisible asserts that a small
// scroll that leaves the selected row on-screen does not move the cursor,
// so pure viewport nudges stay decoupled from selection.
func TestCursorUnchangedWhenScrollKeepsSelectionVisible(t *testing.T) {
	const height = 10
	m := scrollFollowModel(t)
	m.GoToBottom()
	_ = m.View(height, 30)
	// Park the cursor mid-window (rows are one line each, so a bottom
	// row would leave the window on any upward scroll).
	for range 5 {
		m.MoveUp()
	}
	_ = m.View(height, 30)
	if got := m.SelectedID(); got != "C35" {
		t.Fatalf("setup: expected C35 selected mid-window, got %q", got)
	}
	startOff := m.yOffset

	m.ScrollUp(2)
	_ = m.View(height, 30)

	if got := m.yOffset; got != startOff-2 {
		t.Errorf("yOffset = %d, want %d: the viewport must still scroll", got, startOff-2)
	}
	if got := m.SelectedID(); got != "C35" {
		t.Errorf("cursor moved on a scroll that kept selection visible: got %q, want C35", got)
	}
	if !selectionVisible(&m, height) {
		t.Error("selection unexpectedly off-screen after a 2-row scroll")
	}
}

// screenRow is the cursor's line relative to the viewport top.
func screenRow(m *Model) int { return selectedLine(m) - m.yOffset }

// TestPageDownKeepsScreenRow asserts the vim ctrl+d contract: viewport
// and cursor move together, so the cursor stays on the same screen row
// and lands on the channel that scrolled into it.
func TestPageDownKeepsScreenRow(t *testing.T) {
	const height = 10
	m := scrollFollowModel(t)
	_ = m.View(height, 30)
	for range 5 {
		m.MoveDown()
	}
	_ = m.View(height, 30)
	if got := m.SelectedID(); got != "C04" {
		t.Fatalf("setup: expected C04 selected, got %q", got)
	}
	row := screenRow(&m)

	m.PageDown(5)
	_ = m.View(height, 30)

	if got := m.yOffset; got != 5 {
		t.Errorf("yOffset = %d, want 5", got)
	}
	if got := m.SelectedID(); got != "C09" {
		t.Errorf("selected = %q, want C09 (five rows below C04)", got)
	}
	if got := screenRow(&m); got != row {
		t.Errorf("screen row = %d, want %d (unchanged)", got, row)
	}
}

// TestPageUpKeepsScreenRow is the ctrl+u mirror of the test above.
func TestPageUpKeepsScreenRow(t *testing.T) {
	const height = 10
	m := scrollFollowModel(t)
	m.GoToBottom()
	_ = m.View(height, 30)
	for range 4 {
		m.MoveUp()
	}
	_ = m.View(height, 30)
	if got := m.SelectedID(); got != "C36" {
		t.Fatalf("setup: expected C36 selected, got %q", got)
	}
	row := screenRow(&m)
	startOff := m.yOffset

	m.PageUp(3)
	_ = m.View(height, 30)

	if got := m.yOffset; got != startOff-3 {
		t.Errorf("yOffset = %d, want %d", got, startOff-3)
	}
	if got := m.SelectedID(); got != "C33" {
		t.Errorf("selected = %q, want C33 (three rows above C36)", got)
	}
	if got := screenRow(&m); got != row {
		t.Errorf("screen row = %d, want %d (unchanged)", got, row)
	}
}

// TestPageDownAtBottomJumpsToLastRow asserts that when the viewport
// cannot scroll any further, ctrl+d moves the cursor to the last row.
func TestPageDownAtBottomJumpsToLastRow(t *testing.T) {
	const height = 10
	m := scrollFollowModel(t)
	m.GoToBottom()
	_ = m.View(height, 30)
	for range 3 {
		m.MoveUp()
	}
	_ = m.View(height, 30)
	if got := m.SelectedID(); got != "C37" {
		t.Fatalf("setup: expected C37 selected, got %q", got)
	}
	startOff := m.yOffset

	m.PageDown(5)
	_ = m.View(height, 30)

	if got := m.yOffset; got != startOff {
		t.Errorf("yOffset = %d, want %d (already at the bottom)", got, startOff)
	}
	if got := m.SelectedID(); got != "C40" {
		t.Errorf("selected = %q, want C40 (last row)", got)
	}
}

// TestPageUpAtTopJumpsToFirstRow is the mirror: at the top, ctrl+u moves
// the cursor to the first navigable row (the Threads row).
func TestPageUpAtTopJumpsToFirstRow(t *testing.T) {
	const height = 10
	m := scrollFollowModel(t)
	_ = m.View(height, 30)
	for range 3 {
		m.MoveDown()
	}
	_ = m.View(height, 30)
	if got := m.SelectedID(); got != "C02" {
		t.Fatalf("setup: expected C02 selected, got %q", got)
	}

	m.PageUp(5)
	_ = m.View(height, 30)

	if got := m.yOffset; got != 0 {
		t.Errorf("yOffset = %d, want 0", got)
	}
	if !m.IsThreadsSelected() {
		t.Errorf("selected = %q, want the Threads row (first row)", m.SelectedID())
	}
}

// TestPageUpPartialScrollMovesCursorByTheSameAmount asserts that when
// the viewport can only move part of the way, the cursor moves by that
// same amount, so its screen row is still preserved.
func TestPageUpPartialScrollMovesCursorByTheSameAmount(t *testing.T) {
	const height = 10
	m := scrollFollowModel(t)
	_ = m.View(height, 30)
	m.ScrollDown(3) // wheel: viewport to 3, cursor clamps onto the top row
	_ = m.View(height, 30)
	if got, want := m.yOffset, 3; got != want {
		t.Fatalf("setup: yOffset = %d, want %d", got, want)
	}
	if got := m.SelectedID(); got == "" {
		t.Fatal("setup: expected a channel row (topmost visible) selected")
	}
	row := screenRow(&m)
	if row != 0 {
		t.Fatalf("setup: screen row = %d, want 0", row)
	}

	m.PageUp(5) // only 3 rows of scroll are available
	_ = m.View(height, 30)

	if got := m.yOffset; got != 0 {
		t.Errorf("yOffset = %d, want 0", got)
	}
	if !m.IsThreadsSelected() {
		t.Errorf("selected = %q, want the Threads row (three rows up, on the same screen row)", m.SelectedID())
	}
	if got := screenRow(&m); got != row {
		t.Errorf("screen row = %d, want %d (unchanged)", got, row)
	}
}

// TestPageDownSkipsNonNavigableRows sweeps a two-section list one row at
// a time and checks the cursor never lands on a blank separator and
// always stays inside the viewport.
func TestPageDownSkipsNonNavigableRows(t *testing.T) {
	const height = 10
	items := make([]ChannelItem, 0, 30)
	for i := 0; i < 15; i++ {
		items = append(items, ChannelItem{ID: fmt.Sprintf("A%02d", i), Name: fmt.Sprintf("alpha-%02d", i), Type: "channel", Section: "Alpha"})
	}
	for i := 0; i < 15; i++ {
		items = append(items, ChannelItem{ID: fmt.Sprintf("B%02d", i), Name: fmt.Sprintf("beta-%02d", i), Type: "channel", Section: "Beta"})
	}
	m := New(items)
	_ = m.View(height, 30)

	for step := 1; step < len(m.cacheRows); step++ {
		m.PageDown(1)
		_ = m.View(height, 30)
		line := selectedLine(&m)
		if line < 0 || m.cacheRows[line].navIdx < 0 {
			t.Fatalf("step %d: cursor on a non-navigable row (line %d)", step, line)
		}
		if !selectionVisible(&m, height) {
			t.Fatalf("step %d: selection line %d outside window [%d,%d)", step, line, m.yOffset, m.yOffset+height)
		}
	}
	if got := m.SelectedID(); got != "B14" {
		t.Errorf("after sweeping to the end selected = %q, want B14", got)
	}
}

// TestPageDownBeforeFirstRenderStepsByNavItems covers a key arriving
// before any frame has been rendered: with no row geometry the cursor
// steps by nav items so the key is not silently dropped.
func TestPageDownBeforeFirstRenderStepsByNavItems(t *testing.T) {
	m := scrollFollowModel(t)
	m.PageDown(3) // Threads → header → C01 → C02
	if got := m.SelectedID(); got != "C02" {
		t.Errorf("selected = %q, want C02", got)
	}
}

// TestCursorFollowsScrollSkipsNonNavigableRows asserts the clamp never
// lands on a blank separator: with two sections, the window edge can fall
// on the blank row between them, and the cursor must skip to the next
// navigable row instead.
func TestCursorFollowsScrollSkipsNonNavigableRows(t *testing.T) {
	const height = 10
	items := make([]ChannelItem, 0, 30)
	for i := 0; i < 15; i++ {
		items = append(items, ChannelItem{ID: fmt.Sprintf("A%02d", i), Name: fmt.Sprintf("alpha-%02d", i), Type: "channel", Section: "Alpha"})
	}
	for i := 0; i < 15; i++ {
		items = append(items, ChannelItem{ID: fmt.Sprintf("B%02d", i), Name: fmt.Sprintf("beta-%02d", i), Type: "channel", Section: "Beta"})
	}
	m := New(items)
	_ = m.View(height, 30)

	// Walk every scroll offset from the top and check the invariant at
	// each step, so whichever offset puts the blank separator on the
	// window's top edge is covered.
	for off := 1; off < len(m.cacheRows); off++ {
		m.ScrollDown(1)
		_ = m.View(height, 30)
		line := selectedLine(&m)
		if line < 0 {
			t.Fatalf("offset %d: cursor %d not found in cacheRows", off, m.cursor)
		}
		if m.cacheRows[line].navIdx < 0 {
			t.Fatalf("offset %d: cursor landed on a non-navigable row at line %d", off, line)
		}
		if !selectionVisible(&m, height) {
			t.Fatalf("offset %d: selection line %d outside window [%d,%d)", off, line, m.yOffset, m.yOffset+height)
		}
	}
}
