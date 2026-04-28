package themeswitcher

import "testing"

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

func TestOpenWithScope(t *testing.T) {
	m := New()
	m.SetItems([]string{"Dark", "Light"})

	m.OpenWithScope(ScopeWorkspace, "Theme for ACME")
	if !m.IsVisible() {
		t.Error("expected picker to be visible")
	}
	if m.Scope() != ScopeWorkspace {
		t.Errorf("Scope = %v, want ScopeWorkspace", m.Scope())
	}
	if m.HeaderText() != "Theme for ACME" {
		t.Errorf("HeaderText = %q, want Theme for ACME", m.HeaderText())
	}

	m.Close()
	m.OpenWithScope(ScopeGlobal, "Default theme")
	if m.Scope() != ScopeGlobal {
		t.Errorf("Scope after re-open = %v, want ScopeGlobal", m.Scope())
	}
	if m.HeaderText() != "Default theme" {
		t.Errorf("HeaderText = %q, want Default theme", m.HeaderText())
	}
}

func TestSelectionReturnsScope(t *testing.T) {
	m := New()
	m.SetItems([]string{"Dark"})
	m.OpenWithScope(ScopeWorkspace, "Theme for X")

	result := m.HandleKey("enter")
	if result == nil {
		t.Fatal("expected ThemeResult, got nil")
	}
	if result.Name != "Dark" {
		t.Errorf("result.Name = %q, want Dark", result.Name)
	}
	if result.Scope != ScopeWorkspace {
		t.Errorf("result.Scope = %v, want ScopeWorkspace", result.Scope)
	}
}

func TestLegacyOpenStillWorks(t *testing.T) {
	// Open() (no args) should default to ScopeGlobal with no header.
	m := New()
	m.SetItems([]string{"Dark"})
	m.Open()
	if m.Scope() != ScopeGlobal {
		t.Errorf("Open() default scope = %v, want ScopeGlobal", m.Scope())
	}
}
