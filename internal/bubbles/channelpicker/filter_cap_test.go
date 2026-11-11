package channelpicker

import (
	"fmt"
	"testing"
)

// The filter keeps the first MaxVisible matches in channel order and stops
// scanning once it has them.
func TestFilterKeepsFirstMaxVisibleMatches(t *testing.T) {
	var chs []Channel
	for i := 0; i < 50; i++ {
		chs = append(chs, Channel{ID: fmt.Sprintf("C%d", i), Name: fmt.Sprintf("chan-%02d", i)})
	}
	m := New()
	m.SetChannels(chs)
	m.Open()
	m.SetQuery("chan-")
	got := m.Filtered()
	if len(got) != MaxVisible {
		t.Fatalf("len = %d, want %d", len(got), MaxVisible)
	}
	for i, c := range got {
		if c.ID != chs[i].ID {
			t.Errorf("row %d = %s, want %s", i, c.ID, chs[i].ID)
		}
	}
	m.SetQuery("chan-4")
	if got := m.Filtered(); len(got) != MaxVisible || got[0].ID != "C40" {
		t.Errorf("chan-4: %v", got)
	}
}
