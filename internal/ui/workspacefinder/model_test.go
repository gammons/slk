package workspacefinder

import (
	"testing"
)

func testItems() []Item {
	return []Item{
		{ID: "T1", Name: "Acme Corp", Initials: "AC"},
		{ID: "T2", Name: "Beta Labs", Initials: "BL"},
		{ID: "T3", Name: "Creative Studio", Initials: "CS"},
		{ID: "T4", Name: "Acme Design", Initials: "AD"},
	}
}

func TestNewModel(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Error("new model should not be visible")
	}
}

func TestOpenClose(t *testing.T) {
	m := New()
	m.SetItems(testItems())

	m.Open()
	if !m.IsVisible() {
		t.Fatal("expected visible after Open")
	}
	if len(m.filtered) != 4 {
		t.Errorf("expected 4 filtered items after open, got %d", len(m.filtered))
	}

	m.Close()
	if m.IsVisible() {
		t.Error("expected not visible after Close")
	}
}

func TestFilterByQuery(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("b")
	m.HandleKey("e")
	m.HandleKey("t")

	if len(m.filtered) != 1 {
		t.Errorf("expected 1 match for 'bet', got %d", len(m.filtered))
	}
	if m.filtered[0] != 1 {
		t.Errorf("expected match at index 1 (Beta Labs), got %d", m.filtered[0])
	}
}

func TestFilterPrefixFirst(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	// "ac" should match "Acme Corp" and "Acme Design" as prefix, not others
	m.HandleKey("a")
	m.HandleKey("c")

	if len(m.filtered) != 2 {
		t.Errorf("expected 2 matches for 'ac', got %d", len(m.filtered))
	}
	idx := m.filtered[0]
	if m.items[idx].Name != "Acme Corp" {
		t.Errorf("expected first match to be 'Acme Corp', got %q", m.items[idx].Name)
	}
}

func TestNavigation(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("down")
	m.HandleKey("down")

	result := m.HandleKey("enter")
	if result == nil {
		t.Fatal("expected a result on Enter")
	}
	if result.ID != "T3" {
		t.Errorf("expected third workspace (T3), got %q", result.ID)
	}
}

func TestNavigateUp(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("down")
	m.HandleKey("down")
	m.HandleKey("up")

	result := m.HandleKey("enter")
	if result == nil {
		t.Fatal("expected a result on Enter")
	}
	if result.ID != "T2" {
		t.Errorf("expected second workspace (T2), got %q", result.ID)
	}
}

func TestSelectWorkspace(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	result := m.HandleKey("enter")
	if result == nil {
		t.Fatal("expected a result on Enter")
	}
	if result.ID != "T1" {
		t.Errorf("expected first workspace (T1), got %q", result.ID)
	}
	if result.Name != "Acme Corp" {
		t.Errorf("expected name 'Acme Corp', got %q", result.Name)
	}
}

func TestEscapeCloses(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	if !m.IsVisible() {
		t.Fatal("expected visible after Open")
	}

	m.HandleKey("esc")
	if m.IsVisible() {
		t.Error("expected not visible after Esc")
	}
}

func TestBackspace(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("x")
	m.HandleKey("y")
	m.HandleKey("z")

	if len(m.filtered) != 0 {
		t.Errorf("expected 0 matches for 'xyz', got %d", len(m.filtered))
	}

	m.HandleKey("backspace")
	m.HandleKey("backspace")
	m.HandleKey("backspace")

	if len(m.filtered) != 4 {
		t.Errorf("expected 4 matches after clearing query, got %d", len(m.filtered))
	}
}

func TestNoMatchesNoResult(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("z")
	m.HandleKey("z")
	m.HandleKey("z")

	result := m.HandleKey("enter")
	if result != nil {
		t.Error("expected nil result when no matches")
	}
}
