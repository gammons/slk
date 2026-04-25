package channelfinder

import (
	"testing"
)

func testItems() []Item {
	return []Item{
		{ID: "C1", Name: "marketing", Type: "channel"},
		{ID: "C2", Name: "engineering", Type: "channel"},
		{ID: "C3", Name: "ext-automote", Type: "channel"},
		{ID: "C4", Name: "grant-planning", Type: "private"},
		{ID: "D1", Name: "Alice", Type: "dm", Presence: "active"},
		{ID: "D2", Name: "Bob", Type: "dm", Presence: "away"},
	}
}

func TestFilterEmpty(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	if len(m.filtered) != 6 {
		t.Errorf("expected 6 filtered items, got %d", len(m.filtered))
	}
}

func TestFilterSubstring(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("e")
	m.HandleKey("n")
	m.HandleKey("g")

	if len(m.filtered) != 1 {
		t.Errorf("expected 1 match for 'eng', got %d", len(m.filtered))
	}
	if m.filtered[0] != 1 {
		t.Errorf("expected match at index 1 (engineering), got %d", m.filtered[0])
	}
}

func TestFilterCaseInsensitive(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("A")
	m.HandleKey("l")
	m.HandleKey("i")

	if len(m.filtered) != 1 {
		t.Errorf("expected 1 match for 'Ali', got %d", len(m.filtered))
	}
}

func TestFilterPrefixFirst(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("m")
	m.HandleKey("a")

	if len(m.filtered) == 0 {
		t.Fatal("expected at least 1 match")
	}
	idx := m.filtered[0]
	if m.items[idx].Name != "marketing" {
		t.Errorf("expected first match to be 'marketing', got %q", m.items[idx].Name)
	}
}

func TestSelectChannel(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	result := m.HandleKey("enter")
	if result == nil {
		t.Fatal("expected a result on Enter")
	}
	if result.ID != "C1" {
		t.Errorf("expected first channel (C1), got %q", result.ID)
	}
}

func TestNavigateDown(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("down")
	m.HandleKey("down")

	result := m.HandleKey("enter")
	if result == nil {
		t.Fatal("expected a result on Enter")
	}
	if result.ID != "C3" {
		t.Errorf("expected third channel (C3), got %q", result.ID)
	}
}

func TestEscCloses(t *testing.T) {
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

	if len(m.filtered) != 6 {
		t.Errorf("expected 6 matches after clearing query, got %d", len(m.filtered))
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
