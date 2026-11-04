package searchresults

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/chrome"
)

// TestBoxSizeMatchesRender locks BoxSize to the actual rendered box so the
// analytic geometry used for mouse hit-testing can't silently drift from
// what renderBox produces. Covers the footer row (server total > fetched)
// and the scrollbar (fetched > visible window) at the same time.
func TestBoxSizeMatchesRender(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetResults(manyItems(50), 80)

	w, h := m.BoxSize(80, 24)
	box := m.renderBox(80, 24)
	if gw := lipgloss.Width(box); w != gw {
		t.Errorf("BoxSize width = %d, rendered width = %d", w, gw)
	}
	if gh := lipgloss.Height(box); h != gh {
		t.Errorf("BoxSize height = %d, rendered height = %d", h, gh)
	}
}

// TestClickRowSelectsItem verifies a box-local click on a list row moves the
// selection to that item, and that clicks above/below the list are no-ops.
// Rows are rowLines tall: row k starts at line listTopOffset() + k*rowLines.
func TestClickRowSelectsItem(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetResults(manyItems(6), 6)

	// visibleRowCap(30) = 3: three rows are visible.
	if cap := m.visibleRowCap(30); cap != 3 {
		t.Fatalf("visibleRowCap(30) = %d, want 3", cap)
	}

	// Third visible row: its first line is at offset 2*rowLines.
	if !m.ClickRow(80, 30, listTopOffset()+2*rowLines) {
		t.Fatal("ClickRow on a populated row should return true")
	}
	if m.selected != 2 {
		t.Errorf("ClickRow set selected=%d, want 2", m.selected)
	}

	// Clicking the input/title area (above the list) is a no-op.
	if m.ClickRow(80, 30, listTopOffset()-1) {
		t.Error("ClickRow above the list should return false")
	}

	// Clicking past the last visible row's separator is a no-op.
	if m.ClickRow(80, 30, listTopOffset()+3*rowLines) {
		t.Error("ClickRow past the last visible row should return false")
	}
}

// TestClickAnyLineSelectsRow verifies a click on any line of a
// four-line row — metadata, either snippet line, or blank separator —
// selects that row.
func TestClickAnyLineSelectsRow(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetResults(manyItems(6), 6)

	// First snippet line of row 0.
	if !m.ClickRow(80, 30, listTopOffset()+1) {
		t.Fatal("ClickRow on row 0 line 2 should return true")
	}
	if m.selected != 0 {
		t.Errorf("selected = %d, want 0", m.selected)
	}

	// Second snippet line of row 2.
	if !m.ClickRow(80, 30, listTopOffset()+2*rowLines+2) {
		t.Fatal("ClickRow on row 2 line 3 should return true")
	}
	if m.selected != 2 {
		t.Errorf("selected = %d, want 2", m.selected)
	}

	// Separator line of row 1 maps to row 1.
	if !m.ClickRow(80, 30, listTopOffset()+1*rowLines+3) {
		t.Fatal("ClickRow on row 1's separator should return true")
	}
	if m.selected != 1 {
		t.Errorf("selected = %d, want 1", m.selected)
	}
}

// TestClickRowScrolledWindow verifies hit-testing agrees with the scroll
// window: with the selection at the bottom of a long list, row k maps to
// window start + k, not absolute index k.
func TestClickRowScrolledWindow(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetResults(manyItems(15), 15)
	m.selected = 14 // window is [12, 15) at a 30-row terminal

	if !m.ClickRow(80, 30, listTopOffset()+0) {
		t.Fatal("ClickRow on first visible row should return true")
	}
	if m.selected != 12 {
		t.Errorf("ClickRow set selected=%d, want 12 (window start)", m.selected)
	}
}

// TestShortTerminalClampsRows verifies the modal shrinks its scroll
// window on short terminals: the outer box must fit within
// termHeight-2, BoxSize must match the render, and ClickRow's
// hit-testing must agree with the clamped window.
func TestShortTerminalClampsRows(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetResults(manyItems(50), 80) // scrollbar + "showing K of N" footer

	const termH = 20
	w, h := m.BoxSize(80, termH)
	if h > termH-2 {
		t.Errorf("BoxSize height = %d, must fit in %d-row terminal (max %d)", h, termH, termH-2)
	}
	if h > termH*7/10 {
		t.Errorf("BoxSize height = %d, must fit the 70%% budget (%d)", h, termH*7/10)
	}
	box := m.renderBox(80, termH)
	if gw, gh := lipgloss.Width(box), lipgloss.Height(box); gw != w || gh != h {
		t.Errorf("rendered %dx%d, BoxSize %dx%d", gw, gh, w, h)
	}

	// ClickRow agrees with the clamped window.
	start, end := chrome.Window(m.selected, len(m.items), m.visibleRowCap(termH))
	rows := end - start
	if rows < 1 {
		t.Fatalf("clamped window is empty: [%d,%d)", start, end)
	}
	if !m.ClickRow(80, termH, listTopOffset()+(rows-1)*rowLines) {
		t.Error("last clamped row should be clickable")
	}
	if m.ClickRow(80, termH, listTopOffset()+rows*rowLines) {
		t.Error("row past the clamped window should not be clickable")
	}
}

// TestTinyTerminalKeepsOneRow verifies the clamp never goes below one
// visible row, even when the terminal can't fit the full chrome.
func TestTinyTerminalKeepsOneRow(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetResults(manyItems(50), 80)

	start, end := chrome.Window(m.selected, len(m.items), m.visibleRowCap(5))
	if end-start != 1 {
		t.Errorf("tiny-terminal window = [%d,%d), want exactly 1 row", start, end)
	}
	box := m.renderBox(80, 5)
	w, h := m.BoxSize(80, 5)
	if gw, gh := lipgloss.Width(box), lipgloss.Height(box); gw != w || gh != h {
		t.Errorf("rendered %dx%d, BoxSize %dx%d", gw, gh, w, h)
	}
}

// TestClickRowOnlyInResultsState verifies clicks on body rows in the
// input/loading/error states do not fabricate a selection.
func TestClickRowOnlyInResultsState(t *testing.T) {
	m := New()
	m.Open()
	if m.ClickRow(80, 24, listTopOffset()) {
		t.Error("ClickRow in input state should return false")
	}
	submitQuery(&m, "deploy") // now loading
	if m.ClickRow(80, 24, listTopOffset()) {
		t.Error("ClickRow in loading state should return false")
	}
	m.SetError("boom")
	if m.ClickRow(80, 24, listTopOffset()) {
		t.Error("ClickRow in error state should return false")
	}
}
