package linkpicker

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// rowShown reports whether item i's URL appears in the rendered box.
func rowShown(view string, i int) bool {
	return strings.Contains(view, fmt.Sprintf("host%d.example/path/%d", i, i))
}

func TestLongListIsWindowed(t *testing.T) {
	const n = 200
	moves := func(m *Model, key string, count int) {
		for range count {
			m.HandleKey(key)
		}
	}
	cases := []struct {
		name string
		move func(m *Model)
		want int
	}{
		{"first", func(m *Model) {}, 0},
		{"middle", func(m *Model) { moves(m, "j", n/2) }, n / 2},
		{"last", func(m *Model) { moves(m, "j", n-1) }, n - 1},
		{"back up from last", func(m *Model) { moves(m, "j", n-1); moves(m, "k", 30) }, n - 31},
	}
	var wantHeight int
	for _, c := range cases {
		m := benchModel(benchItems(n))
		c.move(m)
		if m.Selected() != c.want {
			t.Fatalf("%s: selected = %d, want %d", c.name, m.Selected(), c.want)
		}
		view := m.View()
		if !rowShown(view, c.want) {
			t.Errorf("%s: selected row %d not visible", c.name, c.want)
		}
		shown := 0
		for i := range n {
			if rowShown(view, i) {
				shown++
			}
		}
		if shown != maxVisibleRows {
			t.Errorf("%s: %d rows shown, want %d", c.name, shown, maxVisibleRows)
		}
		if !strings.Contains(view, "█") {
			t.Errorf("%s: windowed list has no scrollbar thumb", c.name)
		}
		h := lipgloss.Height(view)
		if wantHeight == 0 {
			wantHeight = h
		} else if h != wantHeight {
			t.Errorf("%s: box height = %d, want %d", c.name, h, wantHeight)
		}
	}
}

func TestShortListHasNoScrollbar(t *testing.T) {
	m := benchModel(benchItems(maxVisibleRows))
	view := m.View()
	for i := range maxVisibleRows {
		if !rowShown(view, i) {
			t.Errorf("row %d missing", i)
		}
	}
	if strings.Contains(view, "█") {
		t.Error("list that fits has a scrollbar")
	}
}
