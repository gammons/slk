// internal/ui/compose/model_test.go
package compose

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ui/mentionpicker"
)

func TestComposeViewPlaceholder(t *testing.T) {
	m := New("general")
	view := m.View(40, false)

	if !strings.Contains(view, "general") {
		t.Error("expected channel name in placeholder")
	}
}

func TestComposeViewFocused(t *testing.T) {
	m := New("general")
	view := m.View(40, true)

	// When focused, should have a different style (focused border)
	if view == "" {
		t.Error("expected non-empty view when focused")
	}
}

func TestComposeValue(t *testing.T) {
	m := New("general")
	m.SetValue("hello world")

	if m.Value() != "hello world" {
		t.Errorf("expected 'hello world', got %q", m.Value())
	}

	m.Reset()
	if m.Value() != "" {
		t.Error("expected empty after reset")
	}
}

func TestTranslateMentionsForSend(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1234", DisplayName: "Alice", Username: "alice"},
		{ID: "U5678", DisplayName: "Bob Jones", Username: "bjones"},
	})
	tests := []struct {
		input    string
		expected string
	}{
		{"hey @Alice can you review?", "hey <@U1234> can you review?"},
		{"@Bob Jones please look", "<@U5678> please look"},
		{"no mentions here", "no mentions here"},
		{"@Alice and @Bob Jones both", "<@U1234> and <@U5678> both"},
	}
	for _, tt := range tests {
		result := m.TranslateMentionsForSend(tt.input)
		if result != tt.expected {
			t.Errorf("TranslateMentionsForSend(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestTranslateSpecialMentions(t *testing.T) {
	m := New("general")
	m.SetUsers(nil)
	tests := []struct {
		input    string
		expected string
	}{
		{"@here look at this", "<!here> look at this"},
		{"@channel important", "<!channel> important"},
		{"@everyone heads up", "<!everyone> heads up"},
	}
	for _, tt := range tests {
		result := m.TranslateMentionsForSend(tt.input)
		if result != tt.expected {
			t.Errorf("TranslateMentionsForSend(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestIsMentionActive(t *testing.T) {
	m := New("general")
	if m.IsMentionActive() {
		t.Error("expected mention not active initially")
	}
}

func TestIsAtWordBoundary(t *testing.T) {
	tests := []struct {
		text     string
		col      int
		expected bool
	}{
		{"@", 0, true},
		{"hello @", 6, true},
		{"hello\n@", 0, true},
		{"email@", 5, false},
		{"a@", 1, false},
	}
	for _, tt := range tests {
		result := isAtWordBoundary(tt.text, tt.col)
		if result != tt.expected {
			t.Errorf("isAtWordBoundary(%q, %d) = %v, want %v", tt.text, tt.col, result, tt.expected)
		}
	}
}
