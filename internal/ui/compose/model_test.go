// internal/ui/compose/model_test.go
package compose

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/mentionpicker"
	"github.com/gammons/slk/internal/ui/styles"
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
	m, _ = m.Update(tea.KeyPressMsg{Code: '@', Text: "@"})

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
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	// Then type @ mid-word
	m, _ = m.Update(tea.KeyPressMsg{Code: '@', Text: "@"})

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
	m, _ = m.Update(tea.KeyPressMsg{Code: '@', Text: "@"})

	if !m.IsMentionActive() {
		t.Fatal("expected mention picker to be active")
	}

	// Press Enter to select first filtered result
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

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

	m, _ = m.Update(tea.KeyPressMsg{Code: '@', Text: "@"})
	if !m.IsMentionActive() {
		t.Fatal("expected mention picker to be active")
	}

	// Press Escape to dismiss
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

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
	m, _ = m.Update(tea.KeyPressMsg{Code: '@', Text: "@"})
	if !m.IsMentionActive() {
		t.Fatal("expected mention picker to be active")
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})

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

	m, _ = m.Update(tea.KeyPressMsg{Code: '@', Text: "@"})
	if !m.IsMentionActive() {
		t.Fatal("expected mention picker to be active")
	}

	// Initially selected = 0
	if m.mentionPicker.Selected() != 0 {
		t.Errorf("expected initial selection 0, got %d", m.mentionPicker.Selected())
	}

	// Move down
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.mentionPicker.Selected() != 1 {
		t.Errorf("expected selection 1 after down, got %d", m.mentionPicker.Selected())
	}

	// Move up
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
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
	m, _ = m.Update(tea.KeyPressMsg{Code: '@', Text: "@"})
	if !m.IsMentionActive() {
		t.Fatal("expected mention picker to be active")
	}

	// Backspace should delete the @ and cancel mention
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})

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
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: '@', Text: "@"})

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

	m, _ = m.Update(tea.KeyPressMsg{Code: '@', Text: "@"})
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

func sampleEmojiEntries() []emoji.EmojiEntry {
	return []emoji.EmojiEntry{
		{Name: "rock", Display: "🪨"},
		{Name: "rocket", Display: "🚀"},
		{Name: "rose", Display: "🌹"},
		{Name: "tada", Display: "🎉"},
	}
}

// typeChars feeds each rune in s through the compose Update loop as a
// character key press. This mirrors how the textarea receives input from
// bubbletea in real usage.
func typeChars(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func TestEmojiTrigger_OpensAfterColonAndTwoChars(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, ":")
	if m.IsEmojiActive() {
		t.Fatal("picker should NOT open with just ':'")
	}
	m = typeChars(t, m, "r")
	if m.IsEmojiActive() {
		t.Fatal("picker should NOT open with ':r' (1 char)")
	}
	m = typeChars(t, m, "o")
	if !m.IsEmojiActive() {
		t.Fatal("picker should open with ':ro'")
	}
}

func TestEmojiTrigger_RequiresWordBoundary(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, "foo:ro")
	if m.IsEmojiActive() {
		t.Errorf("picker should not open mid-word: value=%q", m.Value())
	}
}

func TestEmojiTrigger_OpensAfterSpace(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, "hi :ro")
	if !m.IsEmojiActive() {
		t.Error("picker should open after whitespace")
	}
}

func TestEmojiTrigger_ClosesOnSpace(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, ":ro")
	if !m.IsEmojiActive() {
		t.Fatal("precondition: picker open")
	}
	m = typeChars(t, m, " ")
	if m.IsEmojiActive() {
		t.Error("picker should close on space")
	}
}

func TestEmojiTrigger_ClosesOnSecondColon(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, ":ro")
	if !m.IsEmojiActive() {
		t.Fatal("precondition: picker open")
	}
	m = typeChars(t, m, ":")
	if m.IsEmojiActive() {
		t.Error("picker should close on closing ':'")
	}
}

func TestEmojiTrigger_ClosesOnEscape(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, ":ro")
	if !m.IsEmojiActive() {
		t.Fatal("precondition: picker open")
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.IsEmojiActive() {
		t.Error("picker should close on escape")
	}
}

func TestEmojiTrigger_BackspacePastTriggerCloses(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, ":ro")
	if !m.IsEmojiActive() {
		t.Fatal("precondition: picker open")
	}
	// Backspace 3 times: deletes 'o', 'r', then ':' (cursor crosses trigger).
	for i := 0; i < 3; i++ {
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	if m.IsEmojiActive() {
		t.Error("picker should close once cursor crosses the trigger ':'")
	}
}

func TestEmojiAccept_ReplacesQueryWithFullShortcode(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, ":ro")
	if !m.IsEmojiActive() {
		t.Fatal("precondition: picker open")
	}
	// First filtered match for "ro" against sampleEmojiEntries is :rock:
	// (alphabetical), then :rocket:, then :rose:. Press Enter to accept the default.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.IsEmojiActive() {
		t.Error("picker should be closed after accept")
	}
	if got := m.Value(); got != ":rock: " {
		t.Errorf("expected value=':rock: ', got %q", got)
	}
}

func TestEmojiAccept_PreservesSurroundingText(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, "hi :ros")
	if !m.IsEmojiActive() {
		t.Fatal("precondition: picker open")
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.Value(); got != "hi :rose: " {
		t.Errorf("expected 'hi :rose: ', got %q", got)
	}
}

func TestEmojiAccept_AppendsTrailingSpace(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, ":ros")
	if !m.IsEmojiActive() {
		t.Fatal("precondition: picker open")
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// User should be able to keep typing without manually adding a space.
	if got := m.Value(); got != ":rose: " {
		t.Errorf("expected trailing space after shortcode, got %q", got)
	}
}

func TestEmojiKeyPath_BumpsVersion(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()
	m = typeChars(t, m, ":ro")
	if !m.IsEmojiActive() {
		t.Fatal("precondition: picker open")
	}
	v0 := m.Version()
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.Version() == v0 {
		t.Errorf("expected version to bump on emoji-picker navigation, got same %d", m.Version())
	}
}

func TestEmojiAndMentionPickersAreMutuallyExclusive(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	m.SetUsers([]mentionpicker.User{{ID: "U1", DisplayName: "Alice", Username: "alice"}})
	_ = m.Focus()

	m = typeChars(t, m, "@a")
	if !m.IsMentionActive() {
		t.Fatal("precondition: mention picker open")
	}
	if m.IsEmojiActive() {
		t.Error("emoji picker should not be active when mention is")
	}
}

func TestSetPlaceholderOverride(t *testing.T) {
	m := New("general")

	// Apply override; Blur (which would normally restore the default) must
	// keep the override active.
	m.SetPlaceholderOverride("Editing message")
	m.Blur()
	got := m.View(40, false)
	if !strings.Contains(got, "Editing message") {
		t.Errorf("expected override placeholder in view, got: %q", got)
	}
	if strings.Contains(got, "Message #general") {
		t.Errorf("default placeholder should not appear while override active: %q", got)
	}

	// Clearing the override restores the default.
	m.SetPlaceholderOverride("")
	m.Blur()
	got2 := m.View(40, false)
	if !strings.Contains(got2, "Message #general") {
		t.Errorf("expected default placeholder after clearing override, got: %q", got2)
	}
}

func TestSetPlaceholderOverride_SetChannelDoesNotOverwrite(t *testing.T) {
	m := New("old")
	m.SetPlaceholderOverride("Editing message")
	m.SetChannel("new")
	m.Blur()
	got := m.View(40, false)
	if !strings.Contains(got, "Editing message") {
		t.Errorf("override should survive SetChannel: %q", got)
	}
}

func TestSetPlaceholderOverride_HiddenWhileFocused(t *testing.T) {
	m := New("general")
	m.SetPlaceholderOverride("Editing message")
	m.Focus()
	got := m.View(40, true)
	// While focused, no placeholder text should be visible.
	if strings.Contains(got, "Editing message") {
		t.Errorf("override should be hidden while focused: %q", got)
	}
	// On Blur, the override should be restored.
	m.Blur()
	got2 := m.View(40, false)
	if !strings.Contains(got2, "Editing message") {
		t.Errorf("override should be restored after Blur: %q", got2)
	}
}

func TestSetPlaceholderOverride_SurvivesReset(t *testing.T) {
	m := New("general")
	m.SetPlaceholderOverride("Editing message")
	m.SetValue("some draft")
	m.Reset()
	m.Blur()
	got := m.View(40, false)
	if !strings.Contains(got, "Editing message") {
		t.Errorf("override should survive Reset (caller controls clearing): %q", got)
	}
	if m.Value() != "" {
		t.Error("Reset should still clear the value")
	}
}

func TestAddAttachment_AppendsToPending(t *testing.T) {
	m := New("general")
	att := PendingAttachment{Filename: "a.png", Bytes: []byte("x"), Mime: "image/png", Size: 1}
	m.AddAttachment(att)

	got := m.Attachments()
	if len(got) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(got))
	}
	if got[0].Filename != "a.png" {
		t.Errorf("expected filename a.png, got %q", got[0].Filename)
	}
}

func TestAttachments_ReturnsCopy(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "a.png", Bytes: []byte("x"), Size: 1})
	got := m.Attachments()
	got[0].Filename = "MUTATED"

	again := m.Attachments()
	if again[0].Filename != "a.png" {
		t.Errorf("Attachments() must return a copy; got mutation: %q", again[0].Filename)
	}
}

func TestRemoveLastAttachment(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "a.png", Size: 1})
	m.AddAttachment(PendingAttachment{Filename: "b.png", Size: 2})

	removed, ok := m.RemoveLastAttachment()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if removed.Filename != "b.png" {
		t.Errorf("expected to remove b.png, got %q", removed.Filename)
	}
	if len(m.Attachments()) != 1 {
		t.Errorf("expected 1 remaining, got %d", len(m.Attachments()))
	}
}

func TestRemoveLastAttachment_Empty(t *testing.T) {
	m := New("general")
	_, ok := m.RemoveLastAttachment()
	if ok {
		t.Error("expected ok=false on empty pending")
	}
}

func TestClearAttachments(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "a.png", Size: 1})
	m.AddAttachment(PendingAttachment{Filename: "b.png", Size: 2})
	m.ClearAttachments()
	if len(m.Attachments()) != 0 {
		t.Errorf("expected empty after Clear, got %d", len(m.Attachments()))
	}
}

func TestSetUploading(t *testing.T) {
	m := New("general")
	if m.Uploading() {
		t.Error("expected !Uploading() initially")
	}
	m.SetUploading(true)
	if !m.Uploading() {
		t.Error("expected Uploading() after SetUploading(true)")
	}
	m.SetUploading(false)
	if m.Uploading() {
		t.Error("expected !Uploading() after SetUploading(false)")
	}
}

func TestComposeView_NoAttachments_NoChipRow(t *testing.T) {
	m := New("general")
	view := m.View(60, false)
	// No attachments → no 📎 glyph anywhere.
	if strings.Contains(view, "📎") {
		t.Errorf("did not expect chip glyph in view without attachments: %q", view)
	}
}

func TestComposeView_WithAttachment_RendersChip(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "screenshot.png", Size: 12345})
	view := m.View(60, false)
	if !strings.Contains(view, "📎") {
		t.Errorf("expected chip glyph in view: %q", view)
	}
	if !strings.Contains(view, "screenshot.png") {
		t.Errorf("expected filename in chip: %q", view)
	}
}

func TestComposeView_MultipleAttachments_AllChipsRender(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "a.png", Size: 1024})
	m.AddAttachment(PendingAttachment{Filename: "b.pdf", Size: 2048})
	view := m.View(80, false)
	if !strings.Contains(view, "a.png") {
		t.Errorf("expected a.png in view")
	}
	if !strings.Contains(view, "b.pdf") {
		t.Errorf("expected b.pdf in view")
	}
}

func TestUpdate_BackspaceAtColZeroEmpty_RemovesLastAttachment(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "a.png", Size: 1})
	m.AddAttachment(PendingAttachment{Filename: "b.png", Size: 2})

	// Cursor starts at (0, 0) and value is empty.
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	got := m2.Attachments()
	if len(got) != 1 {
		t.Fatalf("expected 1 attachment after backspace, got %d", len(got))
	}
	if got[0].Filename != "a.png" {
		t.Errorf("expected a.png to remain, got %q", got[0].Filename)
	}
}

func TestUpdate_BackspaceWithText_DoesNotRemoveAttachment(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "a.png", Size: 1})
	m.SetValue("hello")

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if len(m2.Attachments()) != 1 {
		t.Errorf("expected attachment to remain when text present, got %d", len(m2.Attachments()))
	}
}

func TestUpdate_BackspaceNoAttachments_PassesThrough(t *testing.T) {
	m := New("general")
	m.SetValue("hello")

	// Default cursor placement after SetValue is at the end. Backspace
	// should reduce the text, not touch attachments (there are none).
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if len(m2.Attachments()) != 0 {
		t.Errorf("expected no attachments, got %d", len(m2.Attachments()))
	}
	// We don't strictly assert the textarea reduced; the textarea library's
	// behavior is its own concern. The point of this test is that no
	// attachment-removal occurred when there were no attachments.
}

func TestUpdate_BackspaceWhileUploading_DoesNotRemove(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "a.png", Size: 1})
	m.SetUploading(true)

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if len(m2.Attachments()) != 1 {
		t.Errorf("expected attachment to remain while uploading, got %d", len(m2.Attachments()))
	}
}

func TestReset_ClearsPendingAttachmentsAndUploadingFlag(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "a.png", Size: 1})
	m.SetUploading(true)
	m.SetValue("draft")

	m.Reset()

	if len(m.Attachments()) != 0 {
		t.Errorf("expected attachments cleared, got %d", len(m.Attachments()))
	}
	if m.Uploading() {
		t.Error("expected Uploading=false after Reset")
	}
	if m.Value() != "" {
		t.Errorf("expected value cleared, got %q", m.Value())
	}
}

func TestCursorPosition_SingleLine(t *testing.T) {
	m := New("general")
	m.SetValue("hello world")
	// SetValue places cursor at end of input.
	if !m.CursorAtFirstLine() {
		t.Error("expected single-line value to be on first line")
	}
	if !m.CursorAtLastLine() {
		t.Error("expected single-line value to be on last line")
	}
}

func TestMoveCursorToStart_Then_BackToEnd(t *testing.T) {
	m := New("general")
	m.SetValue("line1\nline2\nline3")
	// After SetValue, cursor is at end of last line.
	if !m.CursorAtLastLine() {
		t.Fatal("setup: expected cursor on last line after SetValue")
	}

	m.MoveCursorToStart()
	if !m.CursorAtFirstLine() {
		t.Errorf("expected cursor on first line after MoveCursorToStart")
	}

	m.MoveCursorToEnd()
	if !m.CursorAtLastLine() {
		t.Errorf("expected cursor on last line after MoveCursorToEnd")
	}
}

// itoaU8 / fmtRGBBg are local helpers used by the tint-background tests.
func itoaU8(v uint8) string {
	if v == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// fmtRGBBg returns the SGR fragment lipgloss/v2 emits for an RGB
// background ("48;2;R;G;B"). Substring-matching this fragment in
// View()'s raw output is enough to assert the tint is being painted.
func fmtRGBBg(r, g, b uint8) string {
	return "48;2;" + itoaU8(r) + ";" + itoaU8(g) + ";" + itoaU8(b)
}

// composeInsertBgRGB returns the current theme's ComposeInsertBG as
// 8-bit RGB so the tests can build the expected SGR fragment.
func composeInsertBgRGB(t *testing.T) (uint8, uint8, uint8) {
	t.Helper()
	r, g, b, _ := styles.ComposeInsertBG.RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}

// TestView_FocusedRendersComposeInsertBackground asserts that when
// the compose box is focused, its rendered output contains the
// ComposeInsertBG tint as an ANSI background code. Guards against
// regressions where ComposeInsert loses its Background() call.
func TestView_FocusedRendersComposeInsertBackground(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	m := New("general")
	out := m.View(60, true /* focused */)

	r, g, b := composeInsertBgRGB(t)
	expected := fmtRGBBg(r, g, b)
	if !strings.Contains(out, expected) {
		t.Fatalf("focused compose output missing tint bg %q\nraw=%q",
			expected, out)
	}
}

// TestView_UnfocusedDoesNotUseComposeInsertBackground asserts that
// when the compose box is NOT focused, the ComposeInsertBG tint is
// absent. The unfocused box keeps SurfaceDark as its background.
func TestView_UnfocusedDoesNotUseComposeInsertBackground(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	m := New("general")
	out := m.View(60, false /* focused */)

	r, g, b := composeInsertBgRGB(t)
	expected := fmtRGBBg(r, g, b)
	if strings.Contains(out, expected) {
		t.Fatalf("unfocused compose output unexpectedly contains tint bg %q\nraw=%q",
			expected, out)
	}
}

// TestVisualLineCount_CursorOverflowAtExactWrapWidth captures the
// off-by-one wrap bug: when the user types exactly enough characters
// to fill a line at the textarea's wrap width, the textarea places
// the cursor at column W (one past the visible right edge) and
// internally moves it to (line 2, col 0). autoGrow must therefore
// grow the textarea to 2 visible lines, otherwise the height-1
// viewport scrolls to follow the cursor and shows the empty line 2
// instead of the filled line 1. visualLineCount drives autoGrow, so
// it must report 2 lines when the content width is exactly the wrap
// width (a multiple of the wrap width counts as "cursor on next line").
func TestVisualLineCount_CursorOverflowAtExactWrapWidth(t *testing.T) {
	m := New("test")
	// Pick a SetWidth where the textarea's inner wrap width is a known
	// small number so we can construct a fill-the-line value exactly.
	m.SetWidth(25) // current SetWidth: textarea wrap is panelW-5 = 20
	w := m.input.Width()
	if w <= 0 {
		t.Fatalf("textarea width=%d; expected >0", w)
	}
	m.SetValue(strings.Repeat("a", w))
	if got := m.visualLineCount(); got < 2 {
		t.Errorf("visualLineCount with content exactly filling wrap width (%d chars at width %d) = %d, want >=2 (cursor overflows to next line)", w, w, got)
	}
}

// TestSetWidthReservesBackgroundMargin asserts the textarea's wrap
// width is intentionally 2 cols narrower than the visible content
// area inside the compose box. The textarea uses Inline(true) styles
// that don't paint bg behind trailing whitespace; the 2-col margin
// gives View()'s lipgloss content wrapper (Width(width-3)) room to
// pad+paint the trailing cells so the compose box's background flows
// cleanly all the way to the inner-right padding. Tightening this
// margin re-introduces a visible "background not filling the box"
// regression on the right edge.
func TestSetWidthReservesBackgroundMargin(t *testing.T) {
	m := New("test")
	for _, panelW := range []int{20, 30, 60, 80} {
		m.SetWidth(panelW)
		got := m.input.Width()
		want := panelW - 5 // visible content (panelW - 3) minus a 2-col bg-padding margin
		if got != want {
			t.Errorf("SetWidth(%d): textarea inner width = %d, want %d (visible_content(panelW-3) - margin(2))", panelW, got, want)
		}
	}
}

// TestSetChannelMembershipPopulatesInChannel verifies that after
// SetChannelMembership is called for the active channel, the derived
// mention picker user list reflects InChannel based on the member set.
func TestSetChannelMembershipPopulatesInChannel(t *testing.T) {
	m := New("test")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "alice"},
		{ID: "U2", DisplayName: "bob"},
		{ID: "U3", DisplayName: "carol"},
	})
	m.SetActiveChannel("C1")
	m.SetChannelMembership("C1", []string{"U1", "U3"})

	got := map[string]bool{}
	for _, u := range m.MentionUsers() {
		got[u.DisplayName] = u.InChannel
	}
	if !got["alice"] || got["bob"] || !got["carol"] {
		t.Errorf("InChannel mapping wrong: %+v", got)
	}
}

// TestSetChannelMembershipFiltersExternalNotInChannel verifies the
// spec rule: external users not in the active channel are omitted
// entirely (no "(ext) (not in channel)" combination).
func TestSetChannelMembershipFiltersExternalNotInChannel(t *testing.T) {
	m := New("test")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "alice"},
		{ID: "U_EXT", DisplayName: "ext.user", IsExternal: true},
	})
	m.SetActiveChannel("C1")
	m.SetChannelMembership("C1", []string{"U1"}) // ext.user NOT in C1

	seen := map[string]bool{}
	for _, u := range m.MentionUsers() {
		seen[u.DisplayName] = true
	}
	if !seen["alice"] {
		t.Error("alice should be visible")
	}
	if seen["ext.user"] {
		t.Error("external user not in C1 should be filtered out, not just muted")
	}
}

// TestSetChannelMembershipIncludesExternalInChannel verifies that an
// external user who IS in the active channel passes through with both
// InChannel and IsExternal preserved.
func TestSetChannelMembershipIncludesExternalInChannel(t *testing.T) {
	m := New("test")
	m.SetUsers([]mentionpicker.User{
		{ID: "U_EXT", DisplayName: "ext.user", IsExternal: true},
	})
	m.SetActiveChannel("C1")
	m.SetChannelMembership("C1", []string{"U_EXT"})

	found := false
	for _, u := range m.MentionUsers() {
		if u.DisplayName == "ext.user" {
			found = true
			if !u.InChannel || !u.IsExternal {
				t.Errorf("external in-channel: got InChannel=%v IsExternal=%v",
					u.InChannel, u.IsExternal)
			}
		}
	}
	if !found {
		t.Error("external in-channel user missing")
	}
}

// TestSetChannelMembershipDefaultsToInChannelWhenNotLoaded verifies
// the spec's "Loading state": before any SetChannelMembership for the
// active channel, every user renders as in-channel (preserves the
// pre-channel-aware behavior while membership data is in flight).
func TestSetChannelMembershipDefaultsToInChannelWhenNotLoaded(t *testing.T) {
	m := New("test")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "alice"},
	})
	m.SetActiveChannel("C1")
	// No SetChannelMembership call yet.

	for _, u := range m.MentionUsers() {
		if !u.InChannel {
			t.Errorf("loading state should default InChannel=true; got %+v", u)
		}
	}
}

// TestSetChannelMembershipForInactiveChannelDoesNotRebuild verifies
// that membership data arriving for a non-active channel doesn't
// disturb the active picker view (it's still stored, but not visible).
func TestSetChannelMembershipForInactiveChannelDoesNotRebuild(t *testing.T) {
	m := New("test")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "alice"},
	})
	m.SetActiveChannel("C1")
	// Membership for C2 arrives but C1 is active — should not affect C1.
	m.SetChannelMembership("C2", []string{"U_OTHER"})

	// alice should still be in-channel (loading state for C1 — no C1 data yet).
	for _, u := range m.MentionUsers() {
		if u.DisplayName == "alice" && !u.InChannel {
			t.Error("inactive-channel membership should not affect active picker")
		}
	}
}

// TestSetChannelMembershipAfterLoadingFlipsInChannel verifies that
// once an empty member set arrives for the active channel, the
// loading-state default is replaced with the actual data.
func TestSetChannelMembershipAfterLoadingFlipsInChannel(t *testing.T) {
	m := New("test")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "alice"},
	})
	m.SetActiveChannel("C1")
	// Load C1 membership with empty set — alice is NOT a member.
	m.SetChannelMembership("C1", []string{})

	for _, u := range m.MentionUsers() {
		if u.DisplayName == "alice" && u.InChannel {
			t.Error("after empty C1 membership loads, alice should be not-in-channel")
		}
	}
}

func TestBroadcastToggle(t *testing.T) {
	m := New("general")
	if m.Broadcast() {
		t.Fatal("broadcast should default to off")
	}
	if m.ToggleBroadcast() != true || !m.Broadcast() {
		t.Fatal("ToggleBroadcast should turn the flag on")
	}
	if m.ToggleBroadcast() != false || m.Broadcast() {
		t.Fatal("second ToggleBroadcast should turn the flag off")
	}
	m.ToggleBroadcast()
	m.SetBroadcast(false)
	if m.Broadcast() {
		t.Fatal("SetBroadcast(false) should clear the flag")
	}
	// Idempotent SetBroadcast must not dirty an already-cleared model.
	v := m.Version()
	m.SetBroadcast(false)
	if m.Version() != v {
		t.Error("no-op SetBroadcast should not bump Version")
	}
}

func TestComposeViewBroadcastHint(t *testing.T) {
	m := New("general")
	m.SetValue("draft text")

	off := m.View(40, true)
	if strings.Contains(off, "also send to") {
		t.Errorf("hint must be hidden while broadcast is off, got:\n%s", off)
	}

	m.ToggleBroadcast()
	on := m.View(40, true)
	if !strings.Contains(on, "also send to #general") {
		t.Errorf("hint with channel name missing while broadcast is on, got:\n%s", on)
	}
	if !strings.Contains(on, "ctrl+o") {
		t.Errorf("hint should name the toggle key, got:\n%s", on)
	}
	if strings.Contains(on, "draft text") == false {
		t.Errorf("hint render must not drop compose content, got:\n%s", on)
	}
}

func TestComposeViewBroadcastHintUnfocused(t *testing.T) {
	// The hint must render even when the compose is blurred (e.g. the
	// user toggled and Esc'd out), otherwise the pending broadcast is
	// invisible until the next send.
	m := New("ops")
	m.ToggleBroadcast()
	view := m.View(40, false)
	if !strings.Contains(view, "also send to #ops") {
		t.Errorf("hint missing in unfocused view, got:\n%s", view)
	}
}

func TestResetClearsBroadcast(t *testing.T) {
	m := New("general")
	m.ToggleBroadcast()
	if !m.Broadcast() {
		t.Fatal("expected broadcast to be on")
	}
	m.Reset()
	if m.Broadcast() {
		t.Error("Reset() must clear broadcast flag (non-sticky across sends)")
	}
}

func TestBroadcastHintWrapping(t *testing.T) {
	styles.Apply("default", config.Theme{})
	m := New("newsroom")
	m.ToggleBroadcast()

	// With width=40, the compose box style has Width(39). The hint
	// "↪ also send to #newsroom  (ctrl+o to cancel)" is 44 display columns
	// wide, so it must wrap into 2 lines.
	view := m.View(40, true)
	lines := strings.Split(view, "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines (top pad, input, wrapped hint lines, bottom pad), got %d:\n%s", len(lines), view)
	}

	wantWidth := 39 // style.Width(width - 1)
	for i, l := range lines {
		w := lipgloss.Width(l)
		if w != wantWidth {
			t.Errorf("line %d visual width = %d, want %d (line: %q)", i, w, wantWidth, l)
		}
		if !strings.Contains(l, "▌") {
			t.Errorf("line %d missing left border ▌ (line: %q)", i, l)
		}
	}

	// Verify unfocused view also maintains uniform width and border.
	viewUnfocused := m.View(40, false)
	for i, l := range strings.Split(viewUnfocused, "\n") {
		w := lipgloss.Width(l)
		if w != wantWidth {
			t.Errorf("unfocused line %d visual width = %d, want %d", i, w, wantWidth)
		}
	}
}
