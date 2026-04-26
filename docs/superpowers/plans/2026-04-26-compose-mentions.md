# Compose @Mention Autocomplete Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add inline @mention autocomplete to the compose box so users can mention workspace members by typing `@` and selecting from a filtered dropdown, with the mention translated to Slack's `<@UserID>` wire format on send.

**Architecture:** A new `mentionpicker` component renders an inline dropdown above the compose box. The `compose.Model` gains mention state (active flag, cursor anchor, user list) and intercepts keys when the picker is showing. On send, `@DisplayName` text is reverse-mapped to `<@UserID>`. Both channel compose and thread compose use `compose.Model`, so both get mentions automatically.

**Tech Stack:** Go, bubbletea, lipgloss, bubbles/textarea

---

### Task 1: Create mention picker model with filtering

**Files:**
- Create: `internal/ui/mentionpicker/model.go`
- Create: `internal/ui/mentionpicker/model_test.go`

- [ ] **Step 1: Write failing tests for User type and filtering**

Create `internal/ui/mentionpicker/model_test.go`:

```go
package mentionpicker

import "testing"

func TestFilterByDisplayNamePrefix(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
		{ID: "U2", DisplayName: "Bob", Username: "bob"},
		{ID: "U3", DisplayName: "Alicia", Username: "alicia.j"},
	})
	m.Open()
	m.SetQuery("ali")

	if len(m.Filtered()) != 2 {
		t.Fatalf("expected 2 filtered users, got %d", len(m.Filtered()))
	}
	if m.Filtered()[0].ID != "U1" {
		t.Errorf("expected Alice first, got %s", m.Filtered()[0].DisplayName)
	}
}

func TestFilterByUsernamePrefix(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "Alice Smith", Username: "asmith"},
		{ID: "U2", DisplayName: "Bob Jones", Username: "bjones"},
	})
	m.Open()
	m.SetQuery("asm")

	if len(m.Filtered()) != 1 {
		t.Fatalf("expected 1 filtered user, got %d", len(m.Filtered()))
	}
	if m.Filtered()[0].ID != "U1" {
		t.Errorf("expected Alice Smith, got %s", m.Filtered()[0].DisplayName)
	}
}

func TestFilterCaseInsensitive(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})
	m.Open()
	m.SetQuery("ALI")

	if len(m.Filtered()) != 1 {
		t.Fatalf("expected 1 filtered user, got %d", len(m.Filtered()))
	}
}

func TestFilterEmptyQueryShowsAll(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
		{ID: "U2", DisplayName: "Bob", Username: "bob"},
		{ID: "U3", DisplayName: "Carol", Username: "carol"},
		{ID: "U4", DisplayName: "Dave", Username: "dave"},
		{ID: "U5", DisplayName: "Eve", Username: "eve"},
		{ID: "U6", DisplayName: "Frank", Username: "frank"},
	})
	m.Open()
	m.SetQuery("")

	// Should show max 5
	if len(m.Filtered()) != 5 {
		t.Fatalf("expected 5 filtered users (max), got %d", len(m.Filtered()))
	}
}

func TestFilterSpecialMentions(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "Henry", Username: "henry"},
	})
	m.Open()
	m.SetQuery("he")

	filtered := m.Filtered()
	// Should include @here special mention and Henry
	foundHere := false
	for _, u := range filtered {
		if u.ID == "special:here" {
			foundHere = true
		}
	}
	if !foundHere {
		t.Error("expected @here in filtered results")
	}
}

func TestOpenClose(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Error("expected not visible initially")
	}
	m.Open()
	if !m.IsVisible() {
		t.Error("expected visible after Open")
	}
	m.Close()
	if m.IsVisible() {
		t.Error("expected not visible after Close")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/mentionpicker/ -v`
Expected: Compilation error -- package does not exist.

- [ ] **Step 3: Implement mention picker model**

Create `internal/ui/mentionpicker/model.go`:

```go
package mentionpicker

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slk/internal/ui/styles"
)

const MaxVisible = 5

// User represents a mentionable user.
type User struct {
	ID          string
	DisplayName string
	Username    string
}

// MentionResult is returned when the user selects a mention.
type MentionResult struct {
	UserID      string
	DisplayName string
}

// specialMentions are always available in the picker.
var specialMentions = []User{
	{ID: "special:here", DisplayName: "here", Username: "here"},
	{ID: "special:channel", DisplayName: "channel", Username: "channel"},
	{ID: "special:everyone", DisplayName: "everyone", Username: "everyone"},
}

type Model struct {
	users    []User
	filtered []User
	query    string
	selected int
	visible  bool
}

func New() Model {
	return Model{}
}

func (m *Model) SetUsers(users []User) {
	m.users = users
}

func (m *Model) Open() {
	m.visible = true
	m.query = ""
	m.selected = 0
	m.filter()
}

func (m *Model) Close() {
	m.visible = false
	m.query = ""
	m.selected = 0
}

func (m Model) IsVisible() bool {
	return m.visible
}

func (m *Model) SetQuery(q string) {
	m.query = q
	m.selected = 0
	m.filter()
}

func (m Model) Query() string {
	return m.query
}

func (m Model) Filtered() []User {
	return m.filtered
}

func (m Model) Selected() int {
	return m.selected
}

func (m *Model) MoveUp() {
	if m.selected > 0 {
		m.selected--
	}
}

func (m *Model) MoveDown() {
	if m.selected < len(m.filtered)-1 {
		m.selected++
	}
}

// Select returns the currently selected user as a MentionResult, or nil if no results.
func (m Model) Select() *MentionResult {
	if len(m.filtered) == 0 {
		return nil
	}
	u := m.filtered[m.selected]
	return &MentionResult{
		UserID:      u.ID,
		DisplayName: u.DisplayName,
	}
}

func (m *Model) filter() {
	q := strings.ToLower(m.query)
	var results []User

	// Special mentions first
	for _, s := range specialMentions {
		if q == "" || strings.HasPrefix(s.DisplayName, q) {
			results = append(results, s)
		}
	}

	// Then regular users
	for _, u := range m.users {
		if q == "" ||
			strings.HasPrefix(strings.ToLower(u.DisplayName), q) ||
			strings.HasPrefix(strings.ToLower(u.Username), q) {
			results = append(results, u)
		}
		if len(results) >= MaxVisible {
			break
		}
	}

	if len(results) > MaxVisible {
		results = results[:MaxVisible]
	}
	m.filtered = results
}

// View renders the mention picker dropdown.
func (m Model) View(width int) string {
	if !m.visible || len(m.filtered) == 0 {
		return ""
	}

	var rows []string
	for i, u := range m.filtered {
		indicator := "  "
		nameStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary)
		usernameStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)

		if i == m.selected {
			indicator = lipgloss.NewStyle().
				Foreground(styles.Accent).
				Render("▌ ")
			nameStyle = nameStyle.Bold(true)
		}

		display := u.DisplayName
		if u.Username != "" && u.Username != u.DisplayName {
			display = u.DisplayName + " " + usernameStyle.Render("("+u.Username+")")
		}

		row := indicator + nameStyle.Render(display)
		rows = append(rows, row)
	}

	content := strings.Join(rows, "\n")

	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Background(styles.SurfaceDark).
		Padding(0, 1).
		Width(width - 2).
		Render(content)

	return box
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/mentionpicker/ -v`
Expected: All 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/mentionpicker/
git commit -m "feat: add mention picker model with user filtering"
```

---

### Task 2: Add mention picker key handling tests and implementation

**Files:**
- Modify: `internal/ui/mentionpicker/model.go`
- Modify: `internal/ui/mentionpicker/model_test.go`

- [ ] **Step 1: Write failing tests for key handling**

Append to `internal/ui/mentionpicker/model_test.go`:

```go
func TestMoveUpDown(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
		{ID: "U2", DisplayName: "Bob", Username: "bob"},
	})
	m.Open()
	m.SetQuery("") // shows specials + Alice + Bob

	if m.Selected() != 0 {
		t.Errorf("expected selected=0, got %d", m.Selected())
	}
	m.MoveDown()
	if m.Selected() != 1 {
		t.Errorf("expected selected=1, got %d", m.Selected())
	}
	m.MoveUp()
	if m.Selected() != 0 {
		t.Errorf("expected selected=0, got %d", m.Selected())
	}
	// MoveUp at 0 stays at 0
	m.MoveUp()
	if m.Selected() != 0 {
		t.Errorf("expected selected=0 (clamped), got %d", m.Selected())
	}
}

func TestSelectReturnsResult(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})
	m.Open()
	m.SetQuery("alice")

	// Special mentions won't match "alice" prefix, so first result should be Alice
	result := m.Select()
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.UserID != "U1" {
		t.Errorf("expected U1, got %s", result.UserID)
	}
	if result.DisplayName != "Alice" {
		t.Errorf("expected Alice, got %s", result.DisplayName)
	}
}

func TestSelectEmptyReturnsNil(t *testing.T) {
	m := New()
	m.SetUsers([]User{})
	m.Open()
	m.SetQuery("zzz")

	result := m.Select()
	if result != nil {
		t.Error("expected nil result for empty filtered list")
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/ui/mentionpicker/ -v`
Expected: All 9 tests PASS (these use already-implemented methods).

- [ ] **Step 3: Commit**

```bash
git add internal/ui/mentionpicker/
git commit -m "feat: add mention picker navigation and selection tests"
```

---

### Task 3: Integrate mention picker into compose model

**Files:**
- Modify: `internal/ui/compose/model.go`
- Modify: `internal/ui/compose/model_test.go`

- [ ] **Step 1: Write failing tests for compose mention integration**

Append to `internal/ui/compose/model_test.go`:

```go
import (
	"github.com/gammons/slk/internal/ui/mentionpicker"
)

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
		{"@", 0, true},           // at position 0
		{"hello @", 6, true},     // after space
		{"hello\n@", 0, true},    // after newline (col resets)
		{"email@", 5, false},     // mid-word
		{"a@", 1, false},         // after letter
	}

	for _, tt := range tests {
		result := isAtWordBoundary(tt.text, tt.col)
		if result != tt.expected {
			t.Errorf("isAtWordBoundary(%q, %d) = %v, want %v", tt.text, tt.col, result, tt.expected)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/compose/ -v`
Expected: Compilation error -- `SetUsers`, `TranslateMentionsForSend`, `IsMentionActive`, `isAtWordBoundary` not defined.

- [ ] **Step 3: Add mention state and methods to compose model**

Modify `internal/ui/compose/model.go`. Add import for `mentionpicker` and `sort` packages. Add new fields to the Model struct and implement the new methods.

Add to imports:

```go
"sort"
"github.com/gammons/slk/internal/ui/mentionpicker"
```

Add new fields to Model struct (after the existing `width` field):

```go
type Model struct {
	input       textarea.Model
	channelName string
	width       int

	// Mention picker state
	mentionPicker   mentionpicker.Model
	mentionActive   bool
	mentionStartCol int // cursor column where @ was typed
	users           []mentionpicker.User
	reverseNames    map[string]string // displayName -> userID
}
```

Add new methods:

```go
// SetUsers provides the list of workspace users for mention autocomplete.
func (m *Model) SetUsers(users []mentionpicker.User) {
	m.users = users
	m.mentionPicker.SetUsers(users)

	// Build reverse name map (displayName -> userID)
	m.reverseNames = make(map[string]string)
	for _, u := range users {
		if u.DisplayName != "" {
			m.reverseNames[u.DisplayName] = u.ID
		}
	}
}

// IsMentionActive returns whether the mention picker is currently showing.
func (m Model) IsMentionActive() bool {
	return m.mentionActive
}

// CloseMention dismisses the mention picker without selecting.
func (m *Model) CloseMention() {
	m.mentionActive = false
	m.mentionPicker.Close()
}

// TranslateMentionsForSend replaces @DisplayName with <@UserID> in the text.
func (m Model) TranslateMentionsForSend(text string) string {
	// Handle special mentions first
	specials := map[string]string{
		"@here":     "<!here>",
		"@channel":  "<!channel>",
		"@everyone": "<!everyone>",
	}
	for name, replacement := range specials {
		text = strings.ReplaceAll(text, name, replacement)
	}

	if len(m.reverseNames) == 0 {
		return text
	}

	// Sort display names by length (longest first) to avoid partial matches
	names := make([]string, 0, len(m.reverseNames))
	for name := range m.reverseNames {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return len(names[i]) > len(names[j])
	})

	for _, name := range names {
		userID := m.reverseNames[name]
		text = strings.ReplaceAll(text, "@"+name, "<@"+userID+">")
	}

	return text
}

// MentionPickerView returns the rendered mention picker dropdown, or "" if not active.
func (m Model) MentionPickerView(width int) string {
	if !m.mentionActive {
		return ""
	}
	return m.mentionPicker.View(width)
}
```

Add the word boundary helper function:

```go
// isAtWordBoundary checks if the character at the given column in the text
// is at a word boundary (preceded by space, newline, or at position 0).
func isAtWordBoundary(text string, col int) bool {
	if col == 0 {
		return true
	}
	// Get the last line's content up to the col
	lines := strings.Split(text, "\n")
	lastLine := lines[len(lines)-1]
	if col > len(lastLine) {
		return false
	}
	if col == 0 {
		return true
	}
	prev := lastLine[col-1]
	return prev == ' ' || prev == '\t'
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/compose/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/compose/ internal/ui/mentionpicker/
git commit -m "feat: add mention state and translate-for-send to compose model"
```

---

### Task 4: Wire mention trigger and key interception into compose Update

**Files:**
- Modify: `internal/ui/compose/model.go`
- Modify: `internal/ui/compose/model_test.go`

- [ ] **Step 1: Write failing tests for mention trigger and key routing**

Append to `internal/ui/compose/model_test.go`:

```go
import (
	tea "github.com/charmbracelet/bubbletea"
)

func TestMentionTriggersOnAtWordBoundary(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})
	m.SetWidth(80)
	m.Focus()

	// Type a space then @
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})

	if !m.IsMentionActive() {
		t.Error("expected mention picker to be active after typing @ at word boundary")
	}
}

func TestMentionSelectInsertDisplayName(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})
	m.SetWidth(80)
	m.Focus()

	// Type "@ali" to trigger and filter
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})

	if !m.IsMentionActive() {
		t.Fatal("expected mention picker to be active")
	}

	// Press Enter to select
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.IsMentionActive() {
		t.Error("expected mention picker to close after selection")
	}

	val := m.Value()
	if !strings.Contains(val, "@Alice") {
		t.Errorf("expected '@Alice' in compose value, got %q", val)
	}
}

func TestMentionEscDismisses(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})
	m.SetWidth(80)
	m.Focus()

	// Type @ to trigger
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})
	if !m.IsMentionActive() {
		t.Fatal("expected mention picker to be active")
	}

	// Press Esc to dismiss -- note: compose does NOT handle Esc itself,
	// app.go handles Esc to exit insert mode. So we test CloseMention directly.
	m.CloseMention()
	if m.IsMentionActive() {
		t.Error("expected mention picker to close after Esc")
	}

	// The @ should remain in the text
	if !strings.Contains(m.Value(), "@") {
		t.Error("expected @ to remain in text after dismiss")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/compose/ -v`
Expected: `TestMentionTriggersOnAtWordBoundary` FAILS -- `IsMentionActive()` returns false because Update doesn't trigger the picker yet.

- [ ] **Step 3: Implement mention trigger and key interception in Update**

Replace the `Update` method in `internal/ui/compose/model.go` with the version that handles mention state:

```go
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	keyMsg, isKey := msg.(tea.KeyMsg)

	// If mention picker is active, intercept keys
	if m.mentionActive && isKey {
		return m.handleMentionKey(keyMsg)
	}

	// Normal textarea update
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	// Check if @ was just typed at a word boundary
	if isKey && keyMsg.Type == tea.KeyRunes && len(keyMsg.Runes) == 1 && keyMsg.Runes[0] == '@' {
		val := m.input.Value()
		col := m.input.Position()
		// Check word boundary: the @ is now in the text, so check char before @
		atPos := strings.LastIndex(val[:col], "@")
		if atPos >= 0 && (atPos == 0 || val[atPos-1] == ' ' || val[atPos-1] == '\n') {
			m.mentionActive = true
			m.mentionStartCol = col // cursor is after the @
			m.mentionPicker.Open()
		}
	}

	m.autoGrow()
	return m, cmd
}

func (m *Model) autoGrow() {
	lines := m.visualLineCount()
	if lines < 1 {
		lines = 1
	}
	if lines > m.input.MaxHeight {
		lines = m.input.MaxHeight
	}
	if m.input.Height() != lines {
		m.input.SetHeight(lines)
		val := m.input.Value()
		m.input.SetValue(val)
	}
}

func (m Model) handleMentionKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyUp || msg.Type == tea.KeyCtrlP:
		m.mentionPicker.MoveUp()
		return m, nil

	case msg.Type == tea.KeyDown || msg.Type == tea.KeyCtrlN:
		m.mentionPicker.MoveDown()
		return m, nil

	case msg.Type == tea.KeyEnter || msg.Type == tea.KeyTab:
		result := m.mentionPicker.Select()
		if result != nil {
			m.insertMention(result)
		}
		m.mentionActive = false
		m.mentionPicker.Close()
		return m, nil

	case msg.Type == tea.KeyEscape:
		m.mentionActive = false
		m.mentionPicker.Close()
		return m, nil

	case msg.Type == tea.KeyBackspace:
		// Forward to textarea
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		// Check if we've deleted back past the @
		pos := m.input.Position()
		if pos < m.mentionStartCol {
			m.mentionActive = false
			m.mentionPicker.Close()
		} else {
			m.updateMentionQuery()
		}
		m.autoGrow()
		return m, cmd

	case msg.Type == tea.KeyRunes:
		// Forward to textarea and update query
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.updateMentionQuery()
		m.autoGrow()
		return m, cmd

	default:
		// Any other key closes the picker and forwards to textarea
		m.mentionActive = false
		m.mentionPicker.Close()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.autoGrow()
		return m, cmd
	}
}

func (m *Model) updateMentionQuery() {
	val := m.input.Value()
	pos := m.input.Position()
	if pos > len(val) {
		pos = len(val)
	}
	if m.mentionStartCol > pos {
		m.mentionActive = false
		m.mentionPicker.Close()
		return
	}
	query := val[m.mentionStartCol:pos]
	m.mentionPicker.SetQuery(query)
}

func (m *Model) insertMention(result *mentionpicker.MentionResult) {
	val := m.input.Value()
	pos := m.input.Position()

	// Replace from @ (mentionStartCol-1) to current cursor with @DisplayName + space
	atPos := m.mentionStartCol - 1 // the @ character position
	if atPos < 0 {
		atPos = 0
	}

	before := val[:atPos]
	after := ""
	if pos < len(val) {
		after = val[pos:]
	}

	newText := before + "@" + result.DisplayName + " " + after
	m.input.SetValue(newText)
	// Move cursor to after the inserted mention + space
	newPos := len(before) + 1 + len(result.DisplayName) + 1
	m.input.SetCursor(newPos)
}
```

Note: `textarea.Model.Position()` returns the cursor position in the text. `textarea.Model.SetCursor()` sets the cursor column. We need to check the actual bubbles/textarea API -- if `Position()` doesn't exist, we'll use `CursorDown`/`CursorUp` and `Col()` / `Line()` methods. The implementer should verify the exact textarea API and adapt. Key methods to check: `m.input.Value()`, `m.input.Line()`, `m.input.CursorDown()`, `m.input.Col()`, and how to get/set cursor position.

**Important:** The `textarea.Model` cursor position API may differ. The implementer should check `bubbles/textarea` docs. The key concept is:
- Get cursor position in the value string to extract the query substring
- After inserting a mention, set the cursor after the inserted text
- The textarea's `.Value()` returns the full text and `.Col()` / `.Line()` give the cursor position

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/compose/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/compose/
git commit -m "feat: wire mention trigger and key interception into compose Update"
```

---

### Task 5: Integrate mention picker into app.go view and key routing

**Files:**
- Modify: `internal/ui/app.go:560-629` (handleInsertMode)
- Modify: `internal/ui/app.go:1222-1226` (SetUserNames)
- Modify: `internal/ui/app.go:1329-1342` (compose view rendering)
- Modify: `internal/ui/app.go:1345-1364` (thread compose view rendering)

- [ ] **Step 1: Modify handleInsertMode to respect mention picker state**

In `internal/ui/app.go`, modify `handleInsertMode` (line 560). The key change: when the mention picker is active, Enter should NOT send the message -- it should go to the compose model which will select the mention.

Replace the `handleInsertMode` function:

```go
func (a *App) handleInsertMode(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, a.keys.Escape) {
		// If mention picker is active, close it instead of exiting insert mode
		if a.focusedPanel == PanelThread && a.threadVisible && a.threadCompose.IsMentionActive() {
			a.threadCompose.CloseMention()
			return nil
		}
		if a.focusedPanel != PanelThread && a.compose.IsMentionActive() {
			a.compose.CloseMention()
			return nil
		}
		a.SetMode(ModeNormal)
		a.compose.Blur()
		a.threadCompose.Blur()
		return nil
	}

	isSend := msg.Type == tea.KeyEnter
	isNewline := msg.Type == tea.KeyCtrlJ

	// Determine which compose box is active based on focused panel
	if a.focusedPanel == PanelThread && a.threadVisible {
		// If mention picker is active, forward all keys to compose (including Enter)
		if a.threadCompose.IsMentionActive() {
			var cmd tea.Cmd
			a.threadCompose, cmd = a.threadCompose.Update(msg)
			return cmd
		}

		// Thread reply compose
		if isNewline {
			var cmd tea.Cmd
			a.threadCompose, cmd = a.threadCompose.Update(tea.KeyMsg{Type: tea.KeyEnter})
			return cmd
		}
		if isSend {
			text := a.threadCompose.Value()
			if text != "" {
				text = a.threadCompose.TranslateMentionsForSend(text)
				a.threadCompose.Reset()
				threadTS := a.threadPanel.ThreadTS()
				channelID := a.threadPanel.ChannelID()
				return func() tea.Msg {
					return SendThreadReplyMsg{
						ChannelID: channelID,
						ThreadTS:  threadTS,
						Text:      text,
					}
				}
			}
			return nil
		}
		var cmd tea.Cmd
		a.threadCompose, cmd = a.threadCompose.Update(msg)
		return cmd
	}

	// Channel message compose
	// If mention picker is active, forward all keys to compose (including Enter)
	if a.compose.IsMentionActive() {
		var cmd tea.Cmd
		a.compose, cmd = a.compose.Update(msg)
		return cmd
	}

	if isNewline {
		var cmd tea.Cmd
		a.compose, cmd = a.compose.Update(tea.KeyMsg{Type: tea.KeyEnter})
		return cmd
	}
	if isSend {
		text := a.compose.Value()
		if text != "" {
			text = a.compose.TranslateMentionsForSend(text)
			a.compose.Reset()
			return func() tea.Msg {
				return SendMessageMsg{
					ChannelID: a.activeChannelID,
					Text:      text,
				}
			}
		}
		return nil
	}

	var cmd tea.Cmd
	a.compose, cmd = a.compose.Update(msg)
	return cmd
}
```

- [ ] **Step 2: Add SetUsers to SetUserNames**

Modify `SetUserNames` in `internal/ui/app.go` (line 1222) to also pass user data to both compose boxes:

```go
func (a *App) SetUserNames(names map[string]string) {
	a.messagepane.SetUserNames(names)
	a.threadPanel.SetUserNames(names)

	// Build user list for mention picker
	users := make([]mentionpicker.User, 0, len(names))
	for id, displayName := range names {
		users = append(users, mentionpicker.User{
			ID:          id,
			DisplayName: displayName,
			Username:    "", // username not available from this map
		})
	}
	a.compose.SetUsers(users)
	a.threadCompose.SetUsers(users)
}
```

Add the `mentionpicker` import to app.go:

```go
"github.com/gammons/slk/internal/ui/mentionpicker"
```

- [ ] **Step 3: Modify View to render mention picker above compose boxes**

In `internal/ui/app.go`, modify the compose view rendering section (around line 1329). Replace the compose view lines:

```go
// Current (line 1329-1331):
a.compose.SetWidth(msgWidth - 2)
composeView := a.compose.View(msgWidth-2, a.mode == ModeInsert && a.focusedPanel != PanelThread)
composeHeight := lipgloss.Height(composeView)

// New:
a.compose.SetWidth(msgWidth - 2)
composeView := a.compose.View(msgWidth-2, a.mode == ModeInsert && a.focusedPanel != PanelThread)
mentionView := a.compose.MentionPickerView(msgWidth - 2)
if mentionView != "" {
	composeView = mentionView + "\n" + composeView
}
composeHeight := lipgloss.Height(composeView)
```

Do the same for thread compose (around line 1350):

```go
// Current (line 1350-1352):
a.threadCompose.SetWidth(threadWidth - 2)
threadComposeView := a.threadCompose.View(threadWidth-2, a.mode == ModeInsert && a.focusedPanel == PanelThread)
threadComposeHeight := lipgloss.Height(threadComposeView)

// New:
a.threadCompose.SetWidth(threadWidth - 2)
threadComposeView := a.threadCompose.View(threadWidth-2, a.mode == ModeInsert && a.focusedPanel == PanelThread)
threadMentionView := a.threadCompose.MentionPickerView(threadWidth - 2)
if threadMentionView != "" {
	threadComposeView = threadMentionView + "\n" + threadComposeView
}
threadComposeHeight := lipgloss.Height(threadComposeView)
```

- [ ] **Step 4: Build and verify compilation**

Run: `go build ./...`
Expected: Compiles successfully.

- [ ] **Step 5: Run all tests**

Run: `go test ./...`
Expected: All tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat: integrate mention picker into app key routing and view rendering"
```

---

### Task 6: End-to-end verification and edge case fixes

**Files:**
- Modify: `internal/ui/compose/model_test.go`
- Possibly modify: `internal/ui/compose/model.go`, `internal/ui/mentionpicker/model.go`

- [ ] **Step 1: Add edge case tests**

Append to `internal/ui/compose/model_test.go`:

```go
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
```

- [ ] **Step 2: Run all tests**

Run: `go test ./internal/ui/compose/ ./internal/ui/mentionpicker/ -v`
Expected: All tests PASS.

- [ ] **Step 3: Run full test suite**

Run: `go test ./...`
Expected: All tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/compose/ internal/ui/mentionpicker/
git commit -m "test: add edge case tests for mention translation"
```

---

### Task 7: Update STATUS.md

**Files:**
- Modify: `docs/STATUS.md`

- [ ] **Step 1: Add mention compose to STATUS.md**

In `docs/STATUS.md`, add under the Messages section (after the "Message sending via Slack API" line):

```markdown
- [x] @mention autocomplete in compose (inline picker, translates to <@UserID> on send)
```

- [ ] **Step 2: Commit**

```bash
git add docs/STATUS.md
git commit -m "docs: add mention autocomplete to STATUS.md"
```
