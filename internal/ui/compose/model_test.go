// internal/ui/compose/model_test.go
package compose

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func TestTranslateDoesNotCorruptSimilarNames(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "heretic", Username: "heretic"},
	})

	// @heretic should NOT be corrupted by @here special mention
	result := m.TranslateMentionsForSend("hey @heretic check this")
	if result != "hey <@U1> check this" {
		t.Errorf("expected 'hey <@U1> check this', got %q", result)
	}

	// @here should still work
	result = m.TranslateMentionsForSend("@here look")
	if result != "<!here> look" {
		t.Errorf("expected '<!here> look', got %q", result)
	}
}

func TestCursorPosition(t *testing.T) {
	m := New("general")
	m.SetWidth(80)
	m.Focus()

	// Empty text => cursor at 0
	if pos := m.cursorPosition(); pos != 0 {
		t.Errorf("expected cursor at 0 for empty text, got %d", pos)
	}

	// Set value "hello" => cursor at end = 5
	m.SetValue("hello")
	if pos := m.cursorPosition(); pos != 5 {
		t.Errorf("expected cursor at 5 after SetValue(\"hello\"), got %d", pos)
	}
}

func TestAutoGrow(t *testing.T) {
	m := New("general")
	m.SetWidth(80)
	m.Focus()

	// Height should be 1 initially
	if m.input.Height() != 1 {
		t.Errorf("expected initial height 1, got %d", m.input.Height())
	}

	// Set multiline value and call autoGrow
	m.SetValue("line1\nline2\nline3")
	m.autoGrow()
	if m.input.Height() < 3 {
		t.Errorf("expected height >= 3 after multiline text, got %d", m.input.Height())
	}
}

func TestMentionTriggersOnAtWordBoundary(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})
	m.SetWidth(80)
	m.Focus()

	// Type @ at start of text (position 0 = word boundary)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})

	if !m.IsMentionActive() {
		t.Error("expected mention picker to be active after typing @ at word boundary")
	}
}

func TestMentionDoesNotTriggerMidWord(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})
	m.SetWidth(80)
	m.Focus()

	// Type "email" first
	for _, r := range "email" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	// Then type @ mid-word
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})

	if m.IsMentionActive() {
		t.Error("expected mention picker NOT to be active after typing @ mid-word")
	}
}

func TestMentionSelectInsertDisplayName(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})
	m.SetWidth(80)
	m.Focus()

	// Type "@" to trigger
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})

	if !m.IsMentionActive() {
		t.Fatal("expected mention picker to be active")
	}

	// Press Enter to select first filtered result
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.IsMentionActive() {
		t.Error("expected mention picker to close after selection")
	}

	// The value should contain an @ mention
	val := m.Value()
	if !strings.Contains(val, "@") {
		t.Errorf("expected value to contain @mention, got %q", val)
	}
}

func TestMentionEscDismisses(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})
	m.SetWidth(80)
	m.Focus()

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})
	if !m.IsMentionActive() {
		t.Fatal("expected mention picker to be active")
	}

	// Press Escape to dismiss
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})

	if m.IsMentionActive() {
		t.Error("expected mention picker to close after Escape")
	}

	if !strings.Contains(m.Value(), "@") {
		t.Error("expected @ to remain in text after dismiss")
	}
}

func TestMentionQueryFilters(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
		{ID: "U2", DisplayName: "Bob", Username: "bob"},
	})
	m.SetWidth(80)
	m.Focus()

	// Type "@a" to trigger and filter
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})
	if !m.IsMentionActive() {
		t.Fatal("expected mention picker to be active")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	// The picker should have filtered results - query should be "a"
	if q := m.mentionPicker.Query(); q != "a" {
		t.Errorf("expected query 'a', got %q", q)
	}
}

func TestMentionNavigateUpDown(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
		{ID: "U2", DisplayName: "Bob", Username: "bob"},
	})
	m.SetWidth(80)
	m.Focus()

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})
	if !m.IsMentionActive() {
		t.Fatal("expected mention picker to be active")
	}

	// Initially selected = 0
	if m.mentionPicker.Selected() != 0 {
		t.Errorf("expected initial selection 0, got %d", m.mentionPicker.Selected())
	}

	// Move down
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.mentionPicker.Selected() != 1 {
		t.Errorf("expected selection 1 after down, got %d", m.mentionPicker.Selected())
	}

	// Move up
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.mentionPicker.Selected() != 0 {
		t.Errorf("expected selection 0 after up, got %d", m.mentionPicker.Selected())
	}
}

func TestMentionBackspaceCancelsMention(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})
	m.SetWidth(80)
	m.Focus()

	// Type "@" to trigger
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})
	if !m.IsMentionActive() {
		t.Fatal("expected mention picker to be active")
	}

	// Backspace should delete the @ and cancel mention
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	if m.IsMentionActive() {
		t.Error("expected mention picker to close after backspacing past @")
	}
}

func TestMentionAfterSpace(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})
	m.SetWidth(80)
	m.Focus()

	// Type "hello " then "@"
	for _, r := range "hello " {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})

	if !m.IsMentionActive() {
		t.Error("expected mention picker to be active after typing @ after space")
	}
}

func TestTranslateLongestNameFirst(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "Al", Username: "al"},
		{ID: "U2", DisplayName: "Alice", Username: "alice"},
	})

	// "Alice" should match before "Al" to avoid "@Alice" -> "<@U1>ice"
	result := m.TranslateMentionsForSend("hey @Alice")
	if result != "hey <@U2>" {
		t.Errorf("expected 'hey <@U2>', got %q", result)
	}
}

func TestTranslateMultipleMentionsSameUser(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})

	result := m.TranslateMentionsForSend("@Alice said @Alice should")
	if result != "<@U1> said <@U1> should" {
		t.Errorf("expected '<@U1> said <@U1> should', got %q", result)
	}
}

func TestMentionPickerViewWhenNotActive(t *testing.T) {
	m := New("general")
	view := m.MentionPickerView(80)
	if view != "" {
		t.Error("expected empty view when mention not active")
	}
}

func TestCloseMention(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})
	m.SetWidth(80)
	m.Focus()

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})
	if !m.IsMentionActive() {
		t.Fatal("expected mention picker to be active")
	}

	m.CloseMention()
	if m.IsMentionActive() {
		t.Error("expected mention picker to close after CloseMention")
	}

	if !strings.Contains(m.Value(), "@") {
		t.Error("expected @ to remain in text after dismiss")
	}
}
