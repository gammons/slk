package reactionpicker

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/core"
)

// TestClickRowScrolledWindow checks that BoxSize and ClickRow follow the
// scroll window once the selection is past the row cap.
func TestClickRowScrolledWindow(t *testing.T) {
	names := make([]string, maxVisibleRows+3)
	for i := range names {
		names[i] = fmt.Sprintf("emoji%02d", i)
	}
	m := withEmoji(names...)
	m.Open("C1", "1.2", nil)
	for range maxVisibleRows + 2 {
		m.HandleKey("down")
	}
	start, _ := chrome.Window(m.selected, len(m.displayedList()), maxVisibleRows)
	if start == 0 {
		t.Fatal("fixture should scroll the window")
	}
	box := m.renderBox(80)
	if _, h := m.BoxSize(80, 24); h != lipgloss.Height(box) {
		t.Errorf("BoxSize height = %d, rendered height = %d", h, lipgloss.Height(box))
	}
	if !m.ClickRow(80, 24, listTopOffset()) {
		t.Fatal("ClickRow on the first visible row should return true")
	}
	if m.selected != start {
		t.Errorf("selected = %d, want the window's first row %d", m.selected, start)
	}
	want := names[start]
	marked := false
	for _, line := range strings.Split(ansi.Strip(m.renderBox(80)), "\n") {
		if strings.Contains(line, "▌") && strings.Contains(line, want) {
			marked = true
		}
	}
	if !marked {
		t.Errorf("the selected row %q is not marked with ▌", want)
	}
}

func TestBoxSizeMatchesRender(t *testing.T) {
	m := withEmoji("a", "b", "c", "d")
	m.Open("C1", "1.2", nil)

	w, h := m.BoxSize(80, 24)
	box := m.renderBox(80)
	if gw := lipgloss.Width(box); w != gw {
		t.Errorf("BoxSize width = %d, rendered width = %d", w, gw)
	}
	if gh := lipgloss.Height(box); h != gh {
		t.Errorf("BoxSize height = %d, rendered height = %d", h, gh)
	}
}

func TestClickRowSelectsItem(t *testing.T) {
	m := withEmoji("a", "b", "c", "d")
	m.Open("C1", "1.2", nil)

	if !m.ClickRow(80, 24, listTopOffset()+2) {
		t.Fatal("ClickRow on a populated row should return true")
	}
	if m.selected != 2 {
		t.Errorf("ClickRow set selected=%d, want 2", m.selected)
	}
	if m.ClickRow(80, 24, listTopOffset()-1) {
		t.Error("ClickRow above the list should return false")
	}
	if m.ClickRow(80, 24, listTopOffset()+4) {
		t.Error("ClickRow past the last row should return false")
	}
}

// withEmoji returns a picker whose whole emoji list is names, so that
// with no query and no history it shows exactly those rows.
func withEmoji(names ...string) *Model {
	m := New()
	m.allEmoji = make([]core.EmojiEntry, len(names))
	for i, n := range names {
		m.allEmoji[i] = core.EmojiEntry{Name: n}
	}
	return m
}
