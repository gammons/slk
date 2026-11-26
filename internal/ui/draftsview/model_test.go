package draftsview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/draftlist"
)

func TestView_EmptySaysNoDrafts(t *testing.T) {
	m := New()
	out := ansi.Strip(m.View(8, 40))
	if !strings.Contains(out, "no drafts") {
		t.Fatalf("empty view = %q, want the no drafts placeholder", out)
	}
}

func TestView_ShowsTheRowAndTheSelectionBar(t *testing.T) {
	m := New()
	m.SetRows([]draftlist.Row{
		{Destination: "#general", Age: "5m", Preview: "hello there"},
		{Destination: "#random", Age: "2h", Marker: "saved", Preview: "later"},
	})
	m.SetFocused(true)
	out := ansi.Strip(m.View(6, 60))
	if !strings.Contains(out, "#general") || !strings.Contains(out, "hello there") {
		t.Fatalf("view missing the first draft:\n%s", out)
	}
	if !strings.Contains(out, "[saved]") {
		t.Fatalf("view missing the hands-off marker:\n%s", out)
	}
	first := strings.Split(out, "\n")[0]
	if !strings.Contains(first, "▌") || !strings.Contains(first, "#general") {
		t.Fatalf("selected row = %q, want the ▌ bar on #general", first)
	}
	m.MoveDown()
	if i, ok := m.Selected(); !ok || i != 1 {
		t.Fatalf("Selected = %d %v, want 1", i, ok)
	}
	second := strings.Split(ansi.Strip(m.View(6, 60)), "\n")[1]
	if !strings.Contains(second, "#random") || !strings.Contains(second, "[saved]") {
		t.Fatalf("second row = %q, want #random and its marker", second)
	}
}

func TestGoToBottomAndClick(t *testing.T) {
	m := New()
	m.SetRows([]draftlist.Row{{Destination: "a"}, {Destination: "b"}, {Destination: "c"}})
	m.GoToBottom()
	if i, _ := m.Selected(); i != 2 {
		t.Fatalf("bottom selected %d, want 2", i)
	}
	if !m.ClickAt(0) {
		t.Fatal("click on the first visible row should land")
	}
	if i, _ := m.Selected(); i != 0 {
		t.Fatalf("after click selected %d, want 0", i)
	}
	if m.ClickAt(9) {
		t.Fatal("click past the list should miss")
	}
}
