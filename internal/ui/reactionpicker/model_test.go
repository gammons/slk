package reactionpicker

import (
	"testing"
)

func TestNewModel(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Error("expected picker to start hidden")
	}
}

func TestOpenClose(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", []string{"thumbsup"})
	if !m.IsVisible() {
		t.Error("expected picker to be visible after Open")
	}
	if m.channelID != "C123" {
		t.Errorf("expected channelID C123, got %s", m.channelID)
	}
	m.Close()
	if m.IsVisible() {
		t.Error("expected picker to be hidden after Close")
	}
}

func TestFilterByQuery(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", nil)

	m.HandleKey("r")
	m.HandleKey("o")
	m.HandleKey("c")
	m.HandleKey("k")

	if m.query != "rock" {
		t.Errorf("expected query 'rock', got '%s'", m.query)
	}
	if len(m.filtered) == 0 {
		t.Error("expected filtered results for 'rock'")
	}
	for _, e := range m.filtered {
		if !stringContains(e.Name, "rock") {
			t.Errorf("filtered entry %s doesn't match query 'rock'", e.Name)
		}
	}
}

func TestNavigationUpDown(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", nil)
	m.HandleKey("h")
	m.HandleKey("e")
	m.HandleKey("a")
	m.HandleKey("r")
	m.HandleKey("t")

	if len(m.filtered) < 2 {
		t.Skip("not enough filtered results for navigation test")
	}

	if m.selected != 0 {
		t.Errorf("expected selected 0, got %d", m.selected)
	}

	m.HandleKey("down")
	if m.selected != 1 {
		t.Errorf("expected selected 1 after down, got %d", m.selected)
	}

	m.HandleKey("up")
	if m.selected != 0 {
		t.Errorf("expected selected 0 after up, got %d", m.selected)
	}
}

func TestSelectEmoji(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", nil)
	m.HandleKey("r")
	m.HandleKey("o")
	m.HandleKey("c")
	m.HandleKey("k")
	m.HandleKey("e")
	m.HandleKey("t")

	result := m.HandleKey("enter")
	if result == nil {
		t.Fatal("expected a result on enter")
	}
	if result.Emoji == "" {
		t.Error("expected non-empty emoji in result")
	}
	if result.Remove {
		t.Error("expected Remove=false for new reaction")
	}
}

func TestSelectExistingReactionTogglesRemove(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", []string{"rocket"})
	m.HandleKey("r")
	m.HandleKey("o")
	m.HandleKey("c")
	m.HandleKey("k")
	m.HandleKey("e")
	m.HandleKey("t")

	result := m.HandleKey("enter")
	if result == nil {
		t.Fatal("expected a result on enter")
	}
	if !result.Remove {
		t.Error("expected Remove=true for existing reaction")
	}
}

func TestEscapeCloses(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", nil)
	result := m.HandleKey("esc")
	if result != nil {
		t.Error("expected nil result on esc")
	}
	if m.IsVisible() {
		t.Error("expected picker to be hidden after esc")
	}
}

func TestBackspace(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", nil)
	m.HandleKey("r")
	m.HandleKey("o")
	m.HandleKey("c")
	if m.query != "roc" {
		t.Errorf("expected query 'roc', got '%s'", m.query)
	}
	m.HandleKey("backspace")
	if m.query != "ro" {
		t.Errorf("expected query 'ro' after backspace, got '%s'", m.query)
	}
}

func TestFrecentShownWhenQueryEmpty(t *testing.T) {
	m := New()
	m.SetFrecentEmoji([]EmojiEntry{
		{Name: "thumbsup", Unicode: "\U0001f44d"},
		{Name: "rocket", Unicode: "\U0001f680"},
	})
	m.Open("C123", "1234.5678", nil)

	displayed := m.displayedList()
	if len(displayed) < 2 {
		t.Fatalf("expected at least 2 frecent entries, got %d", len(displayed))
	}
	if displayed[0].Name != "thumbsup" {
		t.Errorf("expected first frecent entry thumbsup, got %s", displayed[0].Name)
	}
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
