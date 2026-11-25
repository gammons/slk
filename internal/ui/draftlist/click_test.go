package draftlist

import (
	"testing"

	"charm.land/lipgloss/v2"
)

func clickRows() []Row {
	return []Row{
		{Destination: "#general", Age: "5m", Preview: "one"},
		{Destination: "#random", Age: "1h", Preview: "two"},
		{Destination: "#dev", Age: "2d", Preview: "three"},
	}
}

// TestBoxSizeMatchesRender locks BoxSize to the rendered box, so the
// geometry the click router uses cannot drift from what renderBox draws.
func TestBoxSizeMatchesRender(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []Row
	}{
		{"rows", clickRows()},
		{"empty", nil},
		{"scrolling", make([]Row, maxVisibleRows+4)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New()
			m.SetRows(tc.rows)
			m.Open()
			w, h := m.BoxSize(80, 24)
			box := m.renderBox(80, 24)
			if gw := lipgloss.Width(box); w != gw {
				t.Errorf("BoxSize width = %d, rendered width = %d", w, gw)
			}
			if gh := lipgloss.Height(box); h != gh {
				t.Errorf("BoxSize height = %d, rendered height = %d", h, gh)
			}
		})
	}
}

// TestClickRowSelectsRow checks that a click on a list row selects it,
// and that clicks on the title or below the last row do nothing.
func TestClickRowSelectsRow(t *testing.T) {
	m := New()
	m.SetRows(clickRows())
	m.Open()

	if !m.ClickRow(80, 24, listTopOffset()+2) {
		t.Fatal("ClickRow on a row should return true")
	}
	if m.selected != 2 {
		t.Errorf("selected = %d, want 2", m.selected)
	}
	if m.ClickRow(80, 24, listTopOffset()-1) {
		t.Error("ClickRow above the list should return false")
	}
	if m.ClickRow(80, 24, listTopOffset()+3) {
		t.Error("ClickRow past the last row should return false")
	}
	if m.selected != 2 {
		t.Errorf("a missed click moved the selection to %d", m.selected)
	}
}
