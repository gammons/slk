package marks

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestHandleKey_LetterJumpsImmediately(t *testing.T) {
	m := New()
	m.SetRows([]Row{
		{Letter: "a", ChannelName: "general", AuthorName: "alice", Excerpt: "hello"},
		{Letter: "b", ChannelName: "random", AuthorName: "bob", Excerpt: "world"},
	})
	if got := m.HandleKey("a"); got == nil || got.Select != "a" || got.Delete != "" {
		t.Fatalf("letter 'a' produced %+v, want Select=a", got)
	}
}

func TestHandleKey_EnterSelectsHighlightedRow(t *testing.T) {
	m := New()
	m.SetRows([]Row{
		{Letter: "a", ChannelName: "general", Excerpt: "x"},
		{Letter: "b", ChannelName: "random", Excerpt: "y"},
	})
	m.HandleKey("down")
	if got := m.HandleKey("enter"); got == nil || got.Select != "b" {
		t.Fatalf("enter produced %+v, want Select=b", got)
	}
}

func TestHandleKey_DeleteRemovesHighlightedRow(t *testing.T) {
	m := New()
	m.SetRows([]Row{
		{Letter: "a", ChannelName: "general", Excerpt: "x"},
		{Letter: "b", ChannelName: "random", Excerpt: "y"},
	})
	for _, key := range []string{"backspace", "delete"} {
		if got := m.HandleKey(key); got == nil || got.Delete != "a" || got.Select != "" {
			t.Fatalf("%s produced %+v, want Delete=a", key, got)
		}
	}
}

func TestHandleKey_EscCloses(t *testing.T) {
	m := New()
	m.Open()
	m.HandleKey("esc")
	if m.IsVisible() {
		t.Fatal("esc must close the overlay")
	}
}

func TestHandleKey_EmptyListEnterIsNoop(t *testing.T) {
	m := New()
	if got := m.HandleKey("enter"); got != nil {
		t.Fatalf("enter on an empty list produced %+v", got)
	}
}

func TestView_RendersSnapshot(t *testing.T) {
	m := New()
	m.SetRows([]Row{
		{Letter: "a", ChannelName: "general", AuthorName: "alice", Excerpt: "hello world"},
	})
	m.Open()
	view := m.View(80)
	for _, want := range []string{"a", "#general", "alice: hello world"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestView_EmptyState(t *testing.T) {
	m := New()
	m.Open()
	if !strings.Contains(m.View(80), "No marks set") {
		t.Fatalf("empty overlay must report no marks set:\n%s", m.View(80))
	}
}

// The excerpt may be long or contain wide glyphs (emoji); the row is
// truncated on measured width, so every rendered line must fit the box
// and long excerpts must carry a truncation tail.
func TestView_TruncatesWideExcerptsToWidth(t *testing.T) {
	m := New()
	var rows []Row
	for i := 0; i < 12; i++ {
		rows = append(rows, Row{
			Letter:      string(rune('a' + i)),
			ChannelName: "general",
			AuthorName:  "alice",
			Excerpt:     strings.Repeat("hello 😀 ", 10),
		})
	}
	m.SetRows(rows)
	m.Open()
	view := m.View(60)
	boxWidth := 30 // boxWidth(60) == 60/2
	for _, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > boxWidth {
			t.Errorf("row overflows the box (%d cells, want <= %d): %q", w, boxWidth, line)
		}
	}
	if !strings.Contains(view, "…") {
		t.Fatalf("long excerpts must be truncated with a tail marker:\n%s", view)
	}
}
