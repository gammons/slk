package themeswitcher

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
	items := make([]string, maxVisibleRows+3)
	for i := range items {
		items[i] = fmt.Sprintf("Theme%02d", i)
	}
	m := New()
	m.SetItems(items)
	m.Open()
	for range maxVisibleRows + 2 {
		m.HandleKey("down")
	}
	start, _ := chrome.Window(m.selected, len(m.filtered), maxVisibleRows)
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
	want := items[start]
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
	m := New()
	m.SetItems([]string{"Dark", "Light", "Dracula", "Nord"})
	m.Open()

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
	m := New()
	m.SetItems([]string{"Dark", "Light", "Dracula", "Nord"})
	m.Open()

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
