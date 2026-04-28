package emojipicker

import (
	"testing"

	"github.com/gammons/slk/internal/emoji"
)

func sampleEntries() []emoji.EmojiEntry {
	return []emoji.EmojiEntry{
		{Name: "apple", Display: "🍎"},
		{Name: "rock", Display: "🪨"},
		{Name: "rocket", Display: "🚀"},
		{Name: "rose", Display: "🌹"},
		{Name: "zebra", Display: "🦓"},
	}
}

func TestOpenClose(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	if m.IsVisible() {
		t.Fatal("expected not visible initially")
	}
	m.Open("ro")
	if !m.IsVisible() {
		t.Fatal("expected visible after Open")
	}
	m.Close()
	if m.IsVisible() {
		t.Fatal("expected not visible after Close")
	}
}

func TestPrefixFilterCaseInsensitive(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("RO")
	got := m.Filtered()
	wantNames := []string{"rock", "rocket", "rose"}
	if len(got) != len(wantNames) {
		t.Fatalf("expected %d filtered, got %d", len(wantNames), len(got))
	}
	for i, n := range wantNames {
		if got[i].Name != n {
			t.Errorf("filtered[%d] = %q, want %q", i, got[i].Name, n)
		}
	}
}

func TestEmptyQueryShowsFirstN(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("")
	if len(m.Filtered()) != len(sampleEntries()) {
		// MaxVisible=5 and we provided exactly 5 entries.
		t.Errorf("expected all entries visible, got %d", len(m.Filtered()))
	}
}

func TestMoveUpDownClamps(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro") // 3 results: rock, rocket, rose
	if m.Selected() != 0 {
		t.Errorf("initial selected = %d, want 0", m.Selected())
	}
	m.MoveDown()
	m.MoveDown()
	m.MoveDown() // clamp at 2
	if m.Selected() != 2 {
		t.Errorf("after 3 down on 3 items, selected = %d, want 2", m.Selected())
	}
	m.MoveUp()
	m.MoveUp()
	m.MoveUp() // clamp at 0
	if m.Selected() != 0 {
		t.Errorf("after 3 up, selected = %d, want 0", m.Selected())
	}
}

func TestSelectedEntry(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro")
	m.MoveDown() // rocket
	got, ok := m.SelectedEntry()
	if !ok {
		t.Fatal("expected selectedEntry ok=true")
	}
	if got.Name != "rocket" {
		t.Errorf("selected = %q, want rocket", got.Name)
	}
}

func TestSelectedEntryEmpty(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("zzz") // no matches
	if got, ok := m.SelectedEntry(); ok {
		t.Errorf("expected ok=false, got %+v", got)
	}
}

func TestSetEntriesWhileVisibleClampsSelection(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro")
	m.MoveDown()
	m.MoveDown() // selected=2 (rose)
	// Now restrict to a smaller list.
	m.SetEntries([]emoji.EmojiEntry{
		{Name: "rocket", Display: "🚀"},
	})
	got := m.Filtered()
	if len(got) != 1 {
		t.Fatalf("expected 1 filtered, got %d", len(got))
	}
	if m.Selected() != 0 {
		t.Errorf("expected selection clamped to 0, got %d", m.Selected())
	}
}

func TestSetQueryUpdatesFilter(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro")
	m.SetQuery("ros")
	got := m.Filtered()
	if len(got) != 1 || got[0].Name != "rose" {
		t.Errorf("expected only rose, got %+v", got)
	}
	if m.Selected() != 0 {
		t.Errorf("selection should reset on SetQuery, got %d", m.Selected())
	}
}

func TestViewEmptyWhenInvisible(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	if m.View(40) != "" {
		t.Error("expected empty view when not visible")
	}
}

func TestViewNonEmptyWhenVisibleWithMatches(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro")
	if m.View(40) == "" {
		t.Error("expected non-empty view with matches")
	}
}
