package themeswitcher

import (
	"testing"
)

func TestOpenClose(t *testing.T) {
	m := New()
	m.SetItems([]string{"Dark", "Light", "Dracula"})
	if m.IsVisible() {
		t.Error("should not be visible initially")
	}
	m.Open()
	if !m.IsVisible() {
		t.Error("should be visible after Open")
	}
	m.Close()
	if m.IsVisible() {
		t.Error("should not be visible after Close")
	}
}

func TestSelectTheme(t *testing.T) {
	m := New()
	m.SetItems([]string{"Dark", "Light", "Dracula"})
	m.Open()
	result := m.HandleKey("enter")
	if result == nil || result.Name != "Dark" {
		t.Errorf("expected Dark, got %v", result)
	}
}

func TestNavigation(t *testing.T) {
	m := New()
	m.SetItems([]string{"Dark", "Light", "Dracula"})
	m.Open()
	m.HandleKey("down")
	result := m.HandleKey("enter")
	if result == nil || result.Name != "Light" {
		t.Errorf("expected Light after down, got %v", result)
	}
}

func TestFilter(t *testing.T) {
	m := New()
	m.SetItems([]string{"Dark", "Light", "Dracula", "Nord"})
	m.Open()
	m.HandleKey("d")
	// Should match "Dark" and "Dracula" (prefix first)
	result := m.HandleKey("enter")
	if result == nil || result.Name != "Dark" {
		t.Errorf("expected Dark (prefix match first), got %v", result)
	}
}

func TestEscapeCloses(t *testing.T) {
	m := New()
	m.SetItems([]string{"Dark"})
	m.Open()
	result := m.HandleKey("esc")
	if result != nil {
		t.Error("expected nil result on escape")
	}
	if m.IsVisible() {
		t.Error("should be closed after escape")
	}
}
