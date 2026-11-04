package help

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/bubbles/chrome"
)

// TestClickRowScrolledWindow checks that BoxSize and ClickRow follow the
// scroll window once the selection is past the row cap.
func TestClickRowScrolledWindow(t *testing.T) {
	entries := make([]Entry, 20)
	for i := range entries {
		entries[i] = Entry{Key: fmt.Sprintf("key%02d", i), Desc: fmt.Sprintf("desc%02d", i)}
	}
	m := New()
	m.SetEntries(entries)
	m.Open()
	for range maxVisibleRows(24) + 2 {
		m.HandleKey("down")
	}
	start, _ := chrome.Window(m.selected, len(m.filtered), maxVisibleRows(24))
	if start == 0 {
		t.Fatal("fixture should scroll the window")
	}
	if _, h := m.BoxSize(80, 24); h != lipgloss.Height(m.renderBox(80, 24)) {
		t.Errorf("BoxSize height = %d, rendered height = %d", h, lipgloss.Height(m.renderBox(80, 24)))
	}
	if !m.ClickRow(80, 24, listTopOffset()) {
		t.Fatal("ClickRow on the first visible row should return true")
	}
	if m.selected != start {
		t.Errorf("selected = %d, want the window's first row %d", m.selected, start)
	}
	want := fmt.Sprintf("desc%02d", start)
	marked := false
	for _, line := range strings.Split(ansi.Strip(m.renderBox(80, 24)), "\n") {
		if strings.Contains(line, "▌") && strings.Contains(line, want) {
			marked = true
		}
	}
	if !marked {
		t.Errorf("the selected row %q is not marked with ▌", want)
	}
}

func TestBoxSizeMatchesRender(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()

	w, h := m.BoxSize(80, 24)
	box := m.renderBox(80, 24)
	if gw := lipgloss.Width(box); w != gw {
		t.Errorf("BoxSize width = %d, rendered width = %d", w, gw)
	}
	if gh := lipgloss.Height(box); h != gh {
		t.Errorf("BoxSize height = %d, rendered height = %d", h, gh)
	}
}

func TestClickRowMovesHighlight(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open() // 6 entries, selected starts at 0

	if !m.ClickRow(80, 24, listTopOffset()+2) {
		t.Fatal("ClickRow on a populated row should return true")
	}
	if m.selected != 2 {
		t.Errorf("ClickRow set selected=%d, want 2", m.selected)
	}
	if m.ClickRow(80, 24, listTopOffset()-1) {
		t.Error("ClickRow above the list should return false")
	}
	if m.ClickRow(80, 24, listTopOffset()+6) {
		t.Error("ClickRow past the last row should return false")
	}
}
