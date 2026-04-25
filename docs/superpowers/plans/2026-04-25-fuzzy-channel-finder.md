# Fuzzy Channel Finder Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Ctrl+t/Ctrl+p fuzzy channel finder overlay that lets users quickly jump to any channel or DM by typing a substring.

**Architecture:** New `channelfinder` package with a self-contained model. App gets a new `ModeChannelFinder` mode. When active, keys route to the finder, and the finder overlay renders on top of the existing layout via `lipgloss.Place()`. Selection emits the existing `ChannelSelectedMsg`.

**Tech Stack:** Go, bubbletea, lipgloss, charmbracelet/bubbles/key

**Spec:** `docs/superpowers/specs/2026-04-25-fuzzy-channel-finder.md`

---

## File Structure

```
slack-tui/
├── internal/ui/
│   ├── channelfinder/
│   │   └── model.go          # New: fuzzy finder component
│   ├── mode.go                # Modify: add ModeChannelFinder
│   ├── app.go                 # Modify: integrate finder into Update/View/handleKey
│   └── sidebar/
│       └── model.go           # Modify: add SelectByID method
```

---

## Task 1: Add ModeChannelFinder

**Files:**
- Modify: `internal/ui/mode.go`

- [ ] **Step 1: Add ModeChannelFinder to the Mode enum**

In `internal/ui/mode.go`, add `ModeChannelFinder` after `ModeSearch`:

```go
const (
	ModeNormal Mode = iota
	ModeInsert
	ModeCommand
	ModeSearch
	ModeChannelFinder
)
```

And add the case to `String()`:

```go
func (m Mode) String() string {
	switch m {
	case ModeNormal:
		return "NORMAL"
	case ModeInsert:
		return "INSERT"
	case ModeCommand:
		return "COMMAND"
	case ModeSearch:
		return "SEARCH"
	case ModeChannelFinder:
		return "FIND"
	default:
		return "UNKNOWN"
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/ui/...`
Expected: Compiles with no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/mode.go
git commit -m "feat: add ModeChannelFinder to mode enum"
```

---

## Task 2: Add SelectByID to Sidebar

**Files:**
- Modify: `internal/ui/sidebar/model.go`

- [ ] **Step 1: Add SelectByID method**

Add this method to `internal/ui/sidebar/model.go`:

```go
// SelectByID moves the sidebar selection to the channel with the given ID.
// Returns true if the channel was found, false otherwise.
func (m *Model) SelectByID(id string) bool {
	for i, idx := range m.filtered {
		if m.items[idx].ID == id {
			m.selected = i
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Add Items() accessor for the channel finder**

Add this method to expose the channel list:

```go
// Items returns all channel items.
func (m *Model) Items() []ChannelItem {
	return m.items
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/ui/...`
Expected: Compiles with no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/sidebar/model.go
git commit -m "feat: add SelectByID and Items accessor to sidebar model"
```

---

## Task 3: Create Channel Finder Component

**Files:**
- Create: `internal/ui/channelfinder/model.go`

- [ ] **Step 1: Create the channelfinder package**

Create `internal/ui/channelfinder/model.go`:

```go
package channelfinder

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slack-tui/internal/ui/styles"
)

// ChannelResult is returned when the user selects a channel.
type ChannelResult struct {
	ID   string
	Name string
}

// Item represents a searchable channel/DM entry.
type Item struct {
	ID       string
	Name     string
	Type     string // channel, dm, group_dm, private
	Presence string // for DMs: active, away
}

// Model is the fuzzy channel finder overlay.
type Model struct {
	items    []Item
	filtered []int // indices into items matching query
	query    string
	selected int // index into filtered
	visible  bool
}

// New creates a new channel finder with the given items.
func New() Model {
	return Model{}
}

// SetItems updates the searchable channel list.
func (m *Model) SetItems(items []Item) {
	m.items = items
}

// Open shows the overlay and resets state.
func (m *Model) Open() {
	m.visible = true
	m.query = ""
	m.selected = 0
	m.filter()
}

// Close hides the overlay.
func (m *Model) Close() {
	m.visible = false
}

// IsVisible returns whether the overlay is showing.
func (m Model) IsVisible() bool {
	return m.visible
}

// HandleKey processes a key event and returns a ChannelResult if the user
// selected a channel, or nil otherwise.
func (m *Model) HandleKey(keyStr string, keyType int) *ChannelResult {
	switch keyStr {
	case "enter":
		if len(m.filtered) > 0 {
			idx := m.filtered[m.selected]
			return &ChannelResult{
				ID:   m.items[idx].ID,
				Name: m.items[idx].Name,
			}
		}
		return nil

	case "esc":
		m.Close()
		return nil

	case "down", "ctrl+n":
		if m.selected < len(m.filtered)-1 {
			m.selected++
		}
		return nil

	case "up", "ctrl+p":
		if m.selected > 0 {
			m.selected--
		}
		return nil

	case "backspace":
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
			m.selected = 0
			m.filter()
		}
		return nil
	}

	// If it's a single printable rune, add to query
	if len(keyStr) == 1 && keyStr[0] >= 32 && keyStr[0] <= 126 {
		m.query += keyStr
		m.selected = 0
		m.filter()
	}

	return nil
}

// filter rebuilds the filtered list based on the current query.
func (m *Model) filter() {
	m.filtered = nil
	q := strings.ToLower(m.query)

	if q == "" {
		// Show all items when query is empty
		for i := range m.items {
			m.filtered = append(m.filtered, i)
		}
		return
	}

	// Prefix matches first, then substring matches
	var prefixMatches, substringMatches []int
	for i, item := range m.items {
		name := strings.ToLower(item.Name)
		if strings.HasPrefix(name, q) {
			prefixMatches = append(prefixMatches, i)
		} else if strings.Contains(name, q) {
			substringMatches = append(substringMatches, i)
		}
	}
	m.filtered = append(prefixMatches, substringMatches...)
}

// View renders the overlay. width and height are the terminal dimensions.
func (m Model) View(termWidth, termHeight int) string {
	if !m.visible {
		return ""
	}

	// Overlay dimensions
	overlayWidth := termWidth / 2
	if overlayWidth < 30 {
		overlayWidth = 30
	}
	if overlayWidth > 80 {
		overlayWidth = 80
	}
	innerWidth := overlayWidth - 4 // border + padding

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Primary).
		Render("Switch Channel")

	// Query input
	queryDisplay := m.query
	if queryDisplay == "" {
		queryDisplay = lipgloss.NewStyle().Foreground(styles.TextMuted).Render("Type to filter...")
	}
	input := lipgloss.NewStyle().
		Foreground(styles.TextPrimary).
		Render("> " + queryDisplay + "█")

	// Results (max 10)
	maxResults := 10
	if maxResults > len(m.filtered) {
		maxResults = len(m.filtered)
	}

	// Adjust scroll window for results
	startIdx := 0
	if m.selected >= maxResults {
		startIdx = m.selected - maxResults + 1
	}
	endIdx := startIdx + maxResults
	if endIdx > len(m.filtered) {
		endIdx = len(m.filtered)
		startIdx = endIdx - maxResults
		if startIdx < 0 {
			startIdx = 0
		}
	}

	var resultRows []string
	for i := startIdx; i < endIdx; i++ {
		idx := m.filtered[i]
		item := m.items[idx]

		prefix := channelPrefix(item)
		line := prefix + " " + item.Name

		if len(line) > innerWidth {
			line = line[:innerWidth-1] + "…"
		}

		if i == m.selected {
			row := lipgloss.NewStyle().
				Background(styles.ChannelSelectedBg).
				Foreground(lipgloss.Color("#FFFFFF")).
				Bold(true).
				Width(innerWidth).
				Render(line)
			resultRows = append(resultRows, row)
		} else {
			row := lipgloss.NewStyle().
				Foreground(styles.TextPrimary).
				Width(innerWidth).
				Render(line)
			resultRows = append(resultRows, row)
		}
	}

	if len(m.filtered) == 0 && m.query != "" {
		noResults := lipgloss.NewStyle().
			Foreground(styles.TextMuted).
			Italic(true).
			Render("No matching channels")
		resultRows = append(resultRows, noResults)
	}

	// Compose the overlay content
	content := title + "\n" + input + "\n\n" + strings.Join(resultRows, "\n")

	// Wrap in a bordered box
	overlay := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)

	// Center the overlay on screen
	return lipgloss.Place(termWidth, termHeight,
		lipgloss.Center, lipgloss.Center,
		overlay,
		lipgloss.WithWhitespaceChars(" "),
	)
}

// channelPrefix returns the display prefix for a channel type.
func channelPrefix(item Item) string {
	switch item.Type {
	case "private":
		return lipgloss.NewStyle().Foreground(styles.Warning).Render("◆")
	case "dm":
		if item.Presence == "active" {
			return lipgloss.NewStyle().Foreground(styles.Accent).Render("●")
		}
		return lipgloss.NewStyle().Foreground(styles.TextMuted).Render("○")
	case "group_dm":
		return lipgloss.NewStyle().Foreground(styles.TextMuted).Render("●")
	default:
		return lipgloss.NewStyle().Foreground(styles.TextMuted).Render("#")
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/ui/channelfinder/...`
Expected: Compiles with no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/channelfinder/model.go
git commit -m "feat: add channel finder overlay component"
```

---

## Task 4: Integrate Finder into App

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add finder to App struct and imports**

Add the import:
```go
"github.com/gammons/slack-tui/internal/ui/channelfinder"
```

Add to the App struct:
```go
	channelFinder channelfinder.Model
```

Initialize in `NewApp()`:
```go
	channelFinder: channelfinder.New(),
```

- [ ] **Step 2: Add SetChannelFinderItems method**

Add this method so main.go can populate the finder:

```go
// SetChannelFinderItems sets the items available in the fuzzy channel finder.
func (a *App) SetChannelFinderItems(items []channelfinder.Item) {
	a.channelFinder.SetItems(items)
}
```

- [ ] **Step 3: Handle Ctrl+t/Ctrl+p in handleNormalMode**

In `handleNormalMode`, add a case before the final `}`:

```go
	case key.Matches(msg, a.keys.FuzzyFinder) || key.Matches(msg, a.keys.FuzzyFinderAlt):
		a.channelFinder.Open()
		a.SetMode(ModeChannelFinder)
```

- [ ] **Step 4: Add handleChannelFinderMode method**

Add this new method:

```go
func (a *App) handleChannelFinderMode(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, a.keys.Escape) {
		a.channelFinder.Close()
		a.SetMode(ModeNormal)
		return nil
	}

	// Map tea.KeyMsg to string for the finder
	keyStr := msg.String()
	if msg.Type == tea.KeyEnter {
		keyStr = "enter"
	} else if msg.Type == tea.KeyEsc {
		keyStr = "esc"
	} else if msg.Type == tea.KeyUp {
		keyStr = "up"
	} else if msg.Type == tea.KeyDown {
		keyStr = "down"
	} else if msg.Type == tea.KeyBackspace {
		keyStr = "backspace"
	}

	result := a.channelFinder.HandleKey(keyStr, int(msg.Type))
	if result != nil {
		a.channelFinder.Close()
		a.SetMode(ModeNormal)
		a.sidebar.SelectByID(result.ID)
		return func() tea.Msg {
			return ChannelSelectedMsg{ID: result.ID, Name: result.Name}
		}
	}
	return nil
}
```

- [ ] **Step 5: Route ModeChannelFinder in handleKey**

In `handleKey`, add the `ModeChannelFinder` case to the switch:

```go
	switch a.mode {
	case ModeInsert:
		return a.handleInsertMode(msg)
	case ModeCommand:
		return a.handleCommandMode(msg)
	case ModeChannelFinder:
		return a.handleChannelFinderMode(msg)
	default:
		return a.handleNormalMode(msg)
	}
```

- [ ] **Step 6: Render finder overlay in View**

At the end of the `View()` method, before `return`, add the overlay rendering. Replace:

```go
	return lipgloss.JoinVertical(lipgloss.Left, content, status)
```

with:

```go
	screen := lipgloss.JoinVertical(lipgloss.Left, content, status)

	// Render channel finder overlay on top if visible
	if a.channelFinder.IsVisible() {
		overlay := a.channelFinder.View(a.width, a.height)
		screen = overlay
	}

	return screen
```

- [ ] **Step 7: Verify it compiles**

Run: `go build ./cmd/slack-tui/`
Expected: Compiles with no errors.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat: integrate channel finder overlay into app"
```

---

## Task 5: Wire Finder Items from main.go

**Files:**
- Modify: `cmd/slack-tui/main.go`

- [ ] **Step 1: Add import and populate finder items**

Add the import:
```go
"github.com/gammons/slack-tui/internal/ui/channelfinder"
```

After the line `app.SetUserNames(userNames)`, add:

```go
		// Populate channel finder with all channels/DMs
		var finderItems []channelfinder.Item
		for _, ch := range sidebarItems {
			finderItems = append(finderItems, channelfinder.Item{
				ID:       ch.ID,
				Name:     ch.Name,
				Type:     ch.Type,
				Presence: ch.Presence,
			})
		}
		app.SetChannelFinderItems(finderItems)
```

This uses `sidebarItems` which is the `[]sidebar.ChannelItem` slice already built earlier in main.go for the sidebar.

- [ ] **Step 2: Verify it compiles and builds**

Run: `make build`
Expected: Builds successfully.

- [ ] **Step 3: Run all tests**

Run: `go test ./... -v -race`
Expected: All tests pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/slack-tui/main.go
git commit -m "feat: wire channel finder items from main.go"
```

---

## Task 6: Add Tests

**Files:**
- Create: `internal/ui/channelfinder/model_test.go`

- [ ] **Step 1: Create tests**

Create `internal/ui/channelfinder/model_test.go`:

```go
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

	// Empty query shows all items
	if len(m.filtered) != 6 {
		t.Errorf("expected 6 filtered items, got %d", len(m.filtered))
	}
}

func TestFilterSubstring(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("e", 0)
	m.HandleKey("n", 0)
	m.HandleKey("g", 0)

	// "eng" should match "engineering"
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

	m.HandleKey("A", 0)
	m.HandleKey("l", 0)
	m.HandleKey("i", 0)

	// "Ali" should match "Alice"
	if len(m.filtered) != 1 {
		t.Errorf("expected 1 match for 'Ali', got %d", len(m.filtered))
	}
}

func TestFilterPrefixFirst(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("m", 0)
	m.HandleKey("a", 0)

	// "ma" should match "marketing" (prefix) before any substring matches
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

	result := m.HandleKey("enter", 0)
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

	m.HandleKey("down", 0)
	m.HandleKey("down", 0)

	result := m.HandleKey("enter", 0)
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

	m.HandleKey("esc", 0)
	if m.IsVisible() {
		t.Error("expected not visible after Esc")
	}
}

func TestBackspace(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("x", 0)
	m.HandleKey("y", 0)
	m.HandleKey("z", 0)

	if len(m.filtered) != 0 {
		t.Errorf("expected 0 matches for 'xyz', got %d", len(m.filtered))
	}

	m.HandleKey("backspace", 0)
	m.HandleKey("backspace", 0)
	m.HandleKey("backspace", 0)

	// Back to empty query, all should show
	if len(m.filtered) != 6 {
		t.Errorf("expected 6 matches after clearing query, got %d", len(m.filtered))
	}
}

func TestNoMatchesNoResult(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	m.HandleKey("z", 0)
	m.HandleKey("z", 0)
	m.HandleKey("z", 0)

	result := m.HandleKey("enter", 0)
	if result != nil {
		t.Error("expected nil result when no matches")
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/ui/channelfinder/ -v`
Expected: All tests PASS.

- [ ] **Step 3: Run all tests**

Run: `go test ./... -race`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/channelfinder/model_test.go
git commit -m "test: add channel finder unit tests"
```

---

## Task 7: Update Documentation

**Files:**
- Modify: `docs/STATUS.md`

- [ ] **Step 1: Update STATUS.md**

Move the fuzzy channel finder from "Not Yet Implemented > High Priority" to "What's Working > UI":

Add to the UI section:
```
- [x] Ctrl+t/Ctrl+p fuzzy channel finder overlay
```

Remove from High Priority:
```
- [ ] **Ctrl+t/Ctrl+p fuzzy channel finder** -- floating overlay for quick channel switching
```

- [ ] **Step 2: Commit**

```bash
git add docs/STATUS.md
git commit -m "docs: mark fuzzy channel finder as complete"
```
