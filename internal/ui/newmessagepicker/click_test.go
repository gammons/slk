package newmessagepicker

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/bubbles/chrome"
)

// TestClickRowScrolledWindow checks that BoxSize and ClickRow follow the
// scroll window once the highlight is past the row cap.
func TestClickRowScrolledWindow(t *testing.T) {
	users := make([]User, maxVisibleRows+3)
	for i := range users {
		users[i] = User{
			ID:          fmt.Sprintf("U%02d", i),
			DisplayName: fmt.Sprintf("Person%02d", i),
			Username:    fmt.Sprintf("p%02d", i),
			Recency:     int64(1000 - i),
		}
	}
	m := New()
	m.SetUsers(users)
	m.Open()
	for range maxVisibleRows + 2 {
		m.HandleKey("down")
	}
	start, _ := chrome.Window(m.highlight, len(m.filtered), maxVisibleRows)
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
	if m.highlight != start {
		t.Errorf("highlight = %d, want the window's first row %d", m.highlight, start)
	}
	want := m.users[m.filtered[start]].DisplayName
	marked := false
	for _, line := range strings.Split(ansi.Strip(m.renderBox(80)), "\n") {
		if strings.Contains(line, "▌") && strings.Contains(line, want) {
			marked = true
		}
	}
	if !marked {
		t.Errorf("the highlighted row %q is not marked with ▌", want)
	}
}

func TestBoxSizeMatchesRender(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
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

func TestClickRowMovesHighlight(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open() // 5 users, highlight starts at 0

	if !m.ClickRow(80, 24, listTopOffset()+2) {
		t.Fatal("ClickRow on a populated row should return true")
	}
	if m.highlight != 2 {
		t.Errorf("ClickRow set highlight=%d, want 2", m.highlight)
	}
	if m.ClickRow(80, 24, listTopOffset()-1) {
		t.Error("ClickRow above the list should return false")
	}
	if m.ClickRow(80, 24, listTopOffset()+5) {
		t.Error("ClickRow past the last row should return false")
	}
}
