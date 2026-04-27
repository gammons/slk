# Customizable Themes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Full theme system with 12 built-in themes, custom themes from a directory, and a live theme switcher overlay (Ctrl+y).

**Architecture:** Built-in themes are Go maps in `styles/themes.go`. Custom themes are TOML files in `~/.config/slk/themes/`. `styles.Apply()` sets color vars and rebuilds all composed styles. A theme switcher overlay (modeled on workspace finder) lets users browse and apply themes live.

**Tech Stack:** Go, lipgloss, go-toml/v2

**Note:** Task 1 (Theme struct in config) is already complete from the prior plan.

---

### Task 2: Built-in Theme Definitions and Apply Function

**Files:**
- Create: `internal/ui/styles/themes.go`
- Modify: `internal/ui/styles/styles.go`
- Create: `internal/ui/styles/styles_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/ui/styles/styles_test.go`:

```go
package styles

import (
	"testing"

	"github.com/gammons/slk/internal/config"
)

func TestApplyDarkDefaults(t *testing.T) {
	Apply("dark", config.Theme{})
	if Primary != "#4A9EFF" {
		t.Errorf("expected dark primary #4A9EFF, got %s", string(Primary))
	}
	if Background != "#1A1A2E" {
		t.Errorf("expected dark background #1A1A2E, got %s", string(Background))
	}
}

func TestApplyLightDefaults(t *testing.T) {
	Apply("light", config.Theme{})
	if Primary != "#0366D6" {
		t.Errorf("expected light primary #0366D6, got %s", string(Primary))
	}
	if Background != "#FFFFFF" {
		t.Errorf("expected light background #FFFFFF, got %s", string(Background))
	}
	Apply("dark", config.Theme{})
}

func TestApplyDracula(t *testing.T) {
	Apply("dracula", config.Theme{})
	if Primary != "#BD93F9" {
		t.Errorf("expected dracula primary #BD93F9, got %s", string(Primary))
	}
	Apply("dark", config.Theme{})
}

func TestApplyOverrides(t *testing.T) {
	Apply("dark", config.Theme{Primary: "#FF0000"})
	if Primary != "#FF0000" {
		t.Errorf("expected overridden primary #FF0000, got %s", string(Primary))
	}
	if Accent != "#50C878" {
		t.Errorf("expected dark accent #50C878, got %s", string(Accent))
	}
	Apply("dark", config.Theme{})
}

func TestApplyUnknownPresetFallsToDark(t *testing.T) {
	Apply("nonexistent", config.Theme{})
	if Primary != "#4A9EFF" {
		t.Errorf("expected dark fallback primary #4A9EFF, got %s", string(Primary))
	}
}

func TestApplyCaseInsensitive(t *testing.T) {
	Apply("Dracula", config.Theme{})
	if Primary != "#BD93F9" {
		t.Errorf("expected dracula primary #BD93F9, got %s", string(Primary))
	}
	Apply("dark", config.Theme{})
}

func TestThemeNames(t *testing.T) {
	names := ThemeNames()
	if len(names) < 12 {
		t.Errorf("expected at least 12 built-in themes, got %d", len(names))
	}
	// Check a few known names
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	for _, expected := range []string{"Dark", "Light", "Dracula", "Nord"} {
		if !found[expected] {
			t.Errorf("expected theme %q in list", expected)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/styles/ -v`
Expected: FAIL — `Apply` and `ThemeNames` do not exist.

- [ ] **Step 3: Create themes.go with built-in theme definitions**

Create `internal/ui/styles/themes.go`:

```go
package styles

import (
	"sort"
	"strings"

	"github.com/gammons/slk/internal/config"
)

// ThemeColors holds the 10 semantic colors for a theme.
type ThemeColors struct {
	Primary     string
	Accent      string
	Warning     string
	Error       string
	Background  string
	Surface     string
	SurfaceDark string
	Text        string
	TextMuted   string
	Border      string
}

// builtinThemes maps lowercase theme names to their display name and colors.
var builtinThemes = map[string]struct {
	Name   string
	Colors ThemeColors
}{
	"dark": {"Dark", ThemeColors{
		Primary: "#4A9EFF", Accent: "#50C878", Warning: "#E0A030", Error: "#E04040",
		Background: "#1A1A2E", Surface: "#16162B", SurfaceDark: "#0F0F23",
		Text: "#E0E0E0", TextMuted: "#888888", Border: "#333333",
	}},
	"light": {"Light", ThemeColors{
		Primary: "#0366D6", Accent: "#28A745", Warning: "#D9840D", Error: "#CB2431",
		Background: "#FFFFFF", Surface: "#F6F8FA", SurfaceDark: "#EAEEF2",
		Text: "#24292E", TextMuted: "#6A737D", Border: "#D1D5DA",
	}},
	"dracula": {"Dracula", ThemeColors{
		Primary: "#BD93F9", Accent: "#50FA7B", Warning: "#FFB86C", Error: "#FF5555",
		Background: "#282A36", Surface: "#343746", SurfaceDark: "#21222C",
		Text: "#F8F8F2", TextMuted: "#6272A4", Border: "#44475A",
	}},
	"solarized dark": {"Solarized Dark", ThemeColors{
		Primary: "#268BD2", Accent: "#859900", Warning: "#B58900", Error: "#DC322F",
		Background: "#002B36", Surface: "#073642", SurfaceDark: "#001E26",
		Text: "#839496", TextMuted: "#586E75", Border: "#073642",
	}},
	"solarized light": {"Solarized Light", ThemeColors{
		Primary: "#268BD2", Accent: "#859900", Warning: "#B58900", Error: "#DC322F",
		Background: "#FDF6E3", Surface: "#EEE8D5", SurfaceDark: "#E4DCCA",
		Text: "#657B83", TextMuted: "#93A1A1", Border: "#EEE8D5",
	}},
	"gruvbox dark": {"Gruvbox Dark", ThemeColors{
		Primary: "#83A598", Accent: "#B8BB26", Warning: "#FABD2F", Error: "#FB4934",
		Background: "#282828", Surface: "#3C3836", SurfaceDark: "#1D2021",
		Text: "#EBDBB2", TextMuted: "#928374", Border: "#504945",
	}},
	"gruvbox light": {"Gruvbox Light", ThemeColors{
		Primary: "#076678", Accent: "#79740E", Warning: "#B57614", Error: "#9D0006",
		Background: "#FBF1C7", Surface: "#EBDBB2", SurfaceDark: "#D5C4A1",
		Text: "#3C3836", TextMuted: "#928374", Border: "#BDAE93",
	}},
	"nord": {"Nord", ThemeColors{
		Primary: "#88C0D0", Accent: "#A3BE8C", Warning: "#EBCB8B", Error: "#BF616A",
		Background: "#2E3440", Surface: "#3B4252", SurfaceDark: "#242933",
		Text: "#ECEFF4", TextMuted: "#7B88A1", Border: "#4C566A",
	}},
	"tokyo night": {"Tokyo Night", ThemeColors{
		Primary: "#7AA2F7", Accent: "#9ECE6A", Warning: "#E0AF68", Error: "#F7768E",
		Background: "#1A1B26", Surface: "#24283B", SurfaceDark: "#16161E",
		Text: "#C0CAF5", TextMuted: "#565F89", Border: "#3B4261",
	}},
	"catppuccin mocha": {"Catppuccin Mocha", ThemeColors{
		Primary: "#89B4FA", Accent: "#A6E3A1", Warning: "#F9E2AF", Error: "#F38BA8",
		Background: "#1E1E2E", Surface: "#313244", SurfaceDark: "#181825",
		Text: "#CDD6F4", TextMuted: "#6C7086", Border: "#45475A",
	}},
	"one dark": {"One Dark", ThemeColors{
		Primary: "#61AFEF", Accent: "#98C379", Warning: "#E5C07B", Error: "#E06C75",
		Background: "#282C34", Surface: "#2C313C", SurfaceDark: "#21252B",
		Text: "#ABB2BF", TextMuted: "#636D83", Border: "#3E4452",
	}},
	"rosé pine": {"Rosé Pine", ThemeColors{
		Primary: "#C4A7E7", Accent: "#9CCFD8", Warning: "#F6C177", Error: "#EB6F92",
		Background: "#191724", Surface: "#1F1D2E", SurfaceDark: "#16141F",
		Text: "#E0DEF4", TextMuted: "#6E6A86", Border: "#26233A",
	}},
}

// customThemes stores themes loaded from the user's themes directory.
var customThemes = map[string]struct {
	Name   string
	Colors ThemeColors
}{}

// RegisterCustomTheme adds a custom theme to the registry.
func RegisterCustomTheme(name string, colors ThemeColors) {
	customThemes[strings.ToLower(name)] = struct {
		Name   string
		Colors ThemeColors
	}{Name: name, Colors: colors}
}

// ThemeNames returns the display names of all available themes (built-in + custom),
// sorted alphabetically.
func ThemeNames() []string {
	seen := map[string]string{} // lowercase -> display name
	for _, t := range builtinThemes {
		seen[strings.ToLower(t.Name)] = t.Name
	}
	for _, t := range customThemes {
		seen[strings.ToLower(t.Name)] = t.Name // custom overrides built-in
	}
	var names []string
	for _, name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// lookupTheme finds a theme by name (case-insensitive). Custom themes take
// priority over built-in. Returns dark theme if not found.
func lookupTheme(name string) ThemeColors {
	key := strings.ToLower(name)
	if t, ok := customThemes[key]; ok {
		return t.Colors
	}
	if t, ok := builtinThemes[key]; ok {
		return t.Colors
	}
	return builtinThemes["dark"].Colors
}
```

- [ ] **Step 4: Add Apply function and buildStyles to styles.go**

In `internal/ui/styles/styles.go`, add the config import:

```go
import (
	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slk/internal/config"
)
```

Add the `Apply` function after the closing `)` of the `var` block:

```go
// Apply sets the color palette from a named theme with optional overrides,
// then rebuilds all composed styles.
func Apply(themeName string, overrides config.Theme) {
	colors := lookupTheme(themeName)

	Primary = lipgloss.Color(colors.Primary)
	Secondary = lipgloss.Color("#666666")
	Accent = lipgloss.Color(colors.Accent)
	Warning = lipgloss.Color(colors.Warning)
	Error = lipgloss.Color(colors.Error)
	Background = lipgloss.Color(colors.Background)
	Surface = lipgloss.Color(colors.Surface)
	SurfaceDark = lipgloss.Color(colors.SurfaceDark)
	TextPrimary = lipgloss.Color(colors.Text)
	TextMuted = lipgloss.Color(colors.TextMuted)
	Border = lipgloss.Color(colors.Border)

	// Apply per-color overrides
	if overrides.Primary != "" {
		Primary = lipgloss.Color(overrides.Primary)
	}
	if overrides.Accent != "" {
		Accent = lipgloss.Color(overrides.Accent)
	}
	if overrides.Warning != "" {
		Warning = lipgloss.Color(overrides.Warning)
	}
	if overrides.Error != "" {
		Error = lipgloss.Color(overrides.Error)
	}
	if overrides.Background != "" {
		Background = lipgloss.Color(overrides.Background)
	}
	if overrides.Surface != "" {
		Surface = lipgloss.Color(overrides.Surface)
	}
	if overrides.SurfaceDark != "" {
		SurfaceDark = lipgloss.Color(overrides.SurfaceDark)
	}
	if overrides.Text != "" {
		TextPrimary = lipgloss.Color(overrides.Text)
	}
	if overrides.TextMuted != "" {
		TextMuted = lipgloss.Color(overrides.TextMuted)
	}
	if overrides.Border != "" {
		Border = lipgloss.Color(overrides.Border)
	}

	buildStyles()
}

func buildStyles() {
	FocusedBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(Primary)
	UnfocusedBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(Border)
	WorkspaceActive = lipgloss.NewStyle().
		Background(Primary).
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).Padding(0, 1).Align(lipgloss.Center)
	WorkspaceInactive = lipgloss.NewStyle().
		Background(lipgloss.Color("#444444")).
		Foreground(TextPrimary).
		Padding(0, 1).Align(lipgloss.Center)
	ChannelSelected = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Padding(0, 1)
	ChannelNormal = lipgloss.NewStyle().
		Foreground(TextPrimary).Padding(0, 1)
	ChannelUnread = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Padding(0, 1)
	UnreadBadge = lipgloss.NewStyle().
		Background(Error).Foreground(lipgloss.Color("#FFFFFF")).Padding(0, 1)
	SectionHeader = lipgloss.NewStyle().
		Foreground(TextMuted).Bold(true).Padding(0, 1)
	Username = lipgloss.NewStyle().
		Foreground(Primary).Bold(true)
	Timestamp = lipgloss.NewStyle().
		Foreground(TextMuted).Italic(true)
	MessageText = lipgloss.NewStyle().
		Foreground(TextPrimary)
	ThreadIndicator = lipgloss.NewStyle().
		Foreground(Primary).Italic(true)
	StatusBar = lipgloss.NewStyle().
		Background(SurfaceDark).Foreground(TextPrimary).Padding(0, 1)
	StatusMode = lipgloss.NewStyle().
		Background(Primary).Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Padding(0, 1)
	StatusModeInsert = lipgloss.NewStyle().
		Background(Accent).Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Padding(0, 1)
	StatusModeCommand = lipgloss.NewStyle().
		Background(Warning).Foreground(lipgloss.Color("#000000")).Bold(true).Padding(0, 1)
	ComposeBox = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(Border).
		Background(SurfaceDark).MarginTop(1).Padding(1, 1, 1, 1)
	ComposeFocused = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(Primary).
		Background(SurfaceDark).MarginTop(1).Padding(1, 1, 1, 1)
	ComposeInsert = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(Primary).
		Background(SurfaceDark).MarginTop(1).Padding(1, 1, 1, 1)
	PresenceOnline = lipgloss.NewStyle().Foreground(Accent)
	PresenceAway = lipgloss.NewStyle().Foreground(TextMuted)
	ReactionPillOwn = lipgloss.NewStyle().
		Background(lipgloss.Color("#1a2e1a")).Foreground(Accent).Padding(0, 1)
	ReactionPillOther = lipgloss.NewStyle().
		Background(lipgloss.Color("#1a1a2e")).Foreground(TextMuted).Padding(0, 1)
	ReactionPillSelected = lipgloss.NewStyle().
		Background(lipgloss.Color("#252540")).Foreground(Primary).Padding(0, 1)
	ReactionPillPlus = lipgloss.NewStyle().
		Background(lipgloss.Color("#1a1a2e")).Foreground(Primary).Padding(0, 1)
	NewMessageSeparator = lipgloss.NewStyle().
		Foreground(Error).Bold(true).Align(lipgloss.Center)
	TypingIndicator = lipgloss.NewStyle().
		Foreground(TextMuted).Italic(true).PaddingLeft(2)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/styles/ -v`
Expected: All tests PASS.

- [ ] **Step 6: Run full build**

Run: `go build ./...`

- [ ] **Step 7: Commit**

```bash
git add internal/ui/styles/themes.go internal/ui/styles/styles.go internal/ui/styles/styles_test.go
git commit -m "feat: add 12 built-in themes with Apply function"
```

---

### Task 3: Custom Theme Loading from Directory

**Files:**
- Modify: `internal/ui/styles/themes.go` (add LoadCustomThemes)
- Create: `internal/ui/styles/themes_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/ui/styles/themes_test.go`:

```go
package styles

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCustomThemes(t *testing.T) {
	dir := t.TempDir()

	themeData := []byte(`
name = "My Custom"

[colors]
primary = "#AABBCC"
accent = "#112233"
warning = "#445566"
error = "#778899"
background = "#000000"
surface = "#111111"
surface_dark = "#222222"
text = "#FFFFFF"
text_muted = "#999999"
border = "#555555"
`)
	if err := os.WriteFile(filepath.Join(dir, "mycustom.toml"), themeData, 0644); err != nil {
		t.Fatal(err)
	}

	// Also write a non-toml file that should be ignored
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a theme"), 0644); err != nil {
		t.Fatal(err)
	}

	LoadCustomThemes(dir)

	// Verify the custom theme was loaded
	names := ThemeNames()
	found := false
	for _, n := range names {
		if n == "My Custom" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'My Custom' in theme names, got %v", names)
	}

	// Verify it can be applied
	Apply("my custom", config.Theme{})
	if Primary != "#AABBCC" {
		t.Errorf("expected custom primary #AABBCC, got %s", string(Primary))
	}

	// Clean up custom themes for other tests
	customThemes = map[string]struct {
		Name   string
		Colors ThemeColors
	}{}
	Apply("dark", config.Theme{})
}

func TestLoadCustomThemesMissingDir(t *testing.T) {
	// Should not panic on non-existent directory
	LoadCustomThemes("/tmp/nonexistent-theme-dir-12345")
}
```

Add the config import to the test file:
```go
import (
	"github.com/gammons/slk/internal/config"
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/styles/ -run TestLoadCustom -v`
Expected: FAIL — `LoadCustomThemes` does not exist.

- [ ] **Step 3: Implement LoadCustomThemes**

Add to `internal/ui/styles/themes.go`:

```go
import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/gammons/slk/internal/config"
)
```

Add the function:

```go
// customThemeFile is the TOML structure for a custom theme file.
type customThemeFile struct {
	Name   string      `toml:"name"`
	Colors ThemeColors `toml:"colors"`
}

// LoadCustomThemes scans a directory for .toml theme files and registers them.
func LoadCustomThemes(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // directory doesn't exist or can't be read — silently skip
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var tf customThemeFile
		if err := toml.Unmarshal(data, &tf); err != nil {
			continue
		}
		if tf.Name == "" {
			continue
		}
		// Fill missing colors from dark defaults
		dark := builtinThemes["dark"].Colors
		if tf.Colors.Primary == "" {
			tf.Colors.Primary = dark.Primary
		}
		if tf.Colors.Accent == "" {
			tf.Colors.Accent = dark.Accent
		}
		if tf.Colors.Warning == "" {
			tf.Colors.Warning = dark.Warning
		}
		if tf.Colors.Error == "" {
			tf.Colors.Error = dark.Error
		}
		if tf.Colors.Background == "" {
			tf.Colors.Background = dark.Background
		}
		if tf.Colors.Surface == "" {
			tf.Colors.Surface = dark.Surface
		}
		if tf.Colors.SurfaceDark == "" {
			tf.Colors.SurfaceDark = dark.SurfaceDark
		}
		if tf.Colors.Text == "" {
			tf.Colors.Text = dark.Text
		}
		if tf.Colors.TextMuted == "" {
			tf.Colors.TextMuted = dark.TextMuted
		}
		if tf.Colors.Border == "" {
			tf.Colors.Border = dark.Border
		}
		RegisterCustomTheme(tf.Name, tf.Colors)
	}
}
```

Note: The `config` import is needed for the `ThemeColors` TOML tag support. Actually, `ThemeColors` already has its own struct, so the `config` import is only needed if used elsewhere. Check — the `Apply` function uses `config.Theme` so the import is already needed in `themes.go`. Add it to the import block.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/styles/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/styles/themes.go internal/ui/styles/themes_test.go
git commit -m "feat: load custom themes from ~/.config/slk/themes/"
```

---

### Task 4: Wire Theme Application at Startup

**Files:**
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Add theme loading and application after config load**

In `cmd/slk/main.go`, after the config is loaded (line 71) and before the notifier (line 73), add:

```go
	// Load custom themes and apply the active theme
	themesDir := filepath.Join(configDir, "themes")
	styles.LoadCustomThemes(themesDir)
	styles.Apply(cfg.Appearance.Theme, cfg.Theme)
```

Add the styles import:
```go
	"github.com/gammons/slk/internal/ui/styles"
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`

- [ ] **Step 3: Run all tests**

Run: `go test ./...`

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat: apply theme at startup from config"
```

---

### Task 5: Theme Switcher Overlay Component

**Files:**
- Create: `internal/ui/themeswitcher/model.go`
- Create: `internal/ui/themeswitcher/model_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/ui/themeswitcher/model_test.go`:

```go
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

	// First item selected by default
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
	// Should match "Dark" and "Dracula"
	result := m.HandleKey("enter")
	if result == nil || (result.Name != "Dark" && result.Name != "Dracula") {
		t.Errorf("expected Dark or Dracula, got %v", result)
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/themeswitcher/ -v`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement theme switcher model**

Create `internal/ui/themeswitcher/model.go`:

```go
package themeswitcher

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// ThemeResult is returned when the user selects a theme.
type ThemeResult struct {
	Name string
}

// Model is the theme switcher overlay.
type Model struct {
	items    []string // theme display names
	filtered []int    // indices into items matching query
	query    string
	selected int // index into filtered
	visible  bool
}

// New creates a new theme switcher.
func New() Model {
	return Model{}
}

// SetItems updates the list of available theme names.
func (m *Model) SetItems(items []string) {
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

// HandleKey processes a key event and returns a ThemeResult if the user
// selected a theme, or nil otherwise.
func (m *Model) HandleKey(keyStr string) *ThemeResult {
	switch keyStr {
	case "enter":
		if len(m.filtered) > 0 {
			idx := m.filtered[m.selected]
			return &ThemeResult{Name: m.items[idx]}
		}
		return nil

	case "esc":
		m.Close()
		return nil

	case "down", "ctrl+n", "j":
		if m.selected < len(m.filtered)-1 {
			m.selected++
		}
		return nil

	case "up", "ctrl+p", "k":
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

	// Single printable rune — add to query
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
		for i := range m.items {
			m.filtered = append(m.filtered, i)
		}
		return
	}

	var prefixMatches, substringMatches []int
	for i, item := range m.items {
		name := strings.ToLower(item)
		if strings.HasPrefix(name, q) {
			prefixMatches = append(prefixMatches, i)
		} else if strings.Contains(name, q) {
			substringMatches = append(substringMatches, i)
		}
	}
	m.filtered = append(prefixMatches, substringMatches...)
}

// ViewOverlay renders the overlay as a centered modal with a dark backdrop.
func (m Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}

	box := m.renderBox(termWidth)
	if box == "" {
		return background
	}

	return lipgloss.Place(termWidth, termHeight,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#0F0F1A")),
	)
}

func (m Model) renderBox(termWidth int) string {
	if !m.visible {
		return ""
	}

	overlayWidth := termWidth / 2
	if overlayWidth < 30 {
		overlayWidth = 30
	}
	if overlayWidth > 60 {
		overlayWidth = 60
	}
	innerWidth := overlayWidth - 4

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Primary).
		Render("Switch Theme")

	var inputText string
	if m.query == "" {
		placeholder := lipgloss.NewStyle().Foreground(styles.TextMuted).Render("Type to filter...")
		inputText = "\u2588 " + placeholder
	} else {
		inputText = m.query + "\u2588"
	}
	input := lipgloss.NewStyle().
		BorderStyle(lipgloss.Border{Left: "\u258c"}).
		BorderLeft(true).
		BorderForeground(styles.Primary).
		PaddingLeft(1).
		Foreground(styles.TextPrimary).
		Render(inputText)

	maxVisible := 12
	if maxVisible > len(m.filtered) {
		maxVisible = len(m.filtered)
	}

	startIdx := 0
	if m.selected >= maxVisible {
		startIdx = m.selected - maxVisible + 1
	}
	endIdx := startIdx + maxVisible
	if endIdx > len(m.filtered) {
		endIdx = len(m.filtered)
		startIdx = endIdx - maxVisible
		if startIdx < 0 {
			startIdx = 0
		}
	}

	var resultRows []string
	for i := startIdx; i < endIdx; i++ {
		idx := m.filtered[i]
		line := m.items[idx]

		if lipgloss.Width(line) > innerWidth {
			line = truncate.StringWithTail(line, uint(innerWidth), "\u2026")
		}

		if i == m.selected {
			indicator := lipgloss.NewStyle().Foreground(styles.Accent).Render("\u258c")
			row := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Bold(true).
				Width(innerWidth - 1).
				Render(line)
			resultRows = append(resultRows, indicator+row)
		} else {
			row := lipgloss.NewStyle().
				Foreground(styles.TextPrimary).
				Width(innerWidth - 1).
				Render(line)
			resultRows = append(resultRows, " "+row)
		}
	}

	if len(m.filtered) == 0 && m.query != "" {
		noResults := lipgloss.NewStyle().
			Foreground(styles.TextMuted).
			Italic(true).
			Render("No matching themes")
		resultRows = append(resultRows, noResults)
	}

	content := title + "\n" + input + "\n\n" + strings.Join(resultRows, "\n")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/themeswitcher/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Run full build**

Run: `go build ./...`

- [ ] **Step 6: Commit**

```bash
git add internal/ui/themeswitcher/model.go internal/ui/themeswitcher/model_test.go
git commit -m "feat: add theme switcher overlay component"
```

---

### Task 6: Integrate Theme Switcher into App

**Files:**
- Modify: `internal/ui/keys.go` (add ThemeSwitcher binding)
- Modify: `internal/ui/mode.go` (add ModeThemeSwitcher)
- Modify: `internal/ui/app.go` (add theme switcher field, keybinding, mode handler, overlay rendering, ThemeSelectedMsg)

- [ ] **Step 1: Add ThemeSwitcher keybinding**

In `internal/ui/keys.go`, add to the `KeyMap` struct:

```go
	ThemeSwitcher   key.Binding
```

Add to `DefaultKeyMap()`:

```go
		ThemeSwitcher:   key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "switch theme")),
```

- [ ] **Step 2: Add ModeThemeSwitcher mode**

In `internal/ui/mode.go`, add to the const block:

```go
	ModeThemeSwitcher
```

Add to `String()`:

```go
	case ModeThemeSwitcher:
		return "THEME"
```

- [ ] **Step 3: Add ThemeSelectedMsg and theme switcher field to App**

In `internal/ui/app.go`, add the message type in the type block:

```go
	ThemeSelectedMsg struct {
		Name string
	}
```

Add the import:
```go
	"github.com/gammons/slk/internal/ui/themeswitcher"
```

Add to the App struct:

```go
	themeSwitcher    themeswitcher.Model
```

Add callback fields:

```go
	// Theme switching
	themeSaveFn      func(name string)
	themeOverrides   config.Theme
```

Initialize in `NewApp()`:

```go
		themeSwitcher:   themeswitcher.New(),
```

Add setters:

```go
// SetThemeItems sets the available themes for the switcher.
func (a *App) SetThemeItems(names []string) {
	a.themeSwitcher.SetItems(names)
}

// SetThemeSaver sets the callback for saving the theme selection.
func (a *App) SetThemeSaver(fn func(name string)) {
	a.themeSaveFn = fn
}

// SetThemeOverrides stores the config theme overrides for applying on switch.
func (a *App) SetThemeOverrides(overrides config.Theme) {
	a.themeOverrides = overrides
}
```

Add config import if not present:
```go
	"github.com/gammons/slk/internal/config"
```

- [ ] **Step 4: Add Ctrl+y handler in handleNormalMode**

In `handleNormalMode`, add a case for the theme switcher keybinding (near the workspace finder handler):

```go
	case key.Matches(msg, a.keys.ThemeSwitcher):
		a.themeSwitcher.Open()
		a.SetMode(ModeThemeSwitcher)
```

- [ ] **Step 5: Add mode dispatch in handleKey**

In the `handleKey` method's mode switch, add:

```go
	case ModeThemeSwitcher:
		return a.handleThemeSwitcherMode(msg)
```

- [ ] **Step 6: Implement handleThemeSwitcherMode**

Add to `app.go`:

```go
func (a *App) handleThemeSwitcherMode(msg tea.KeyMsg) tea.Cmd {
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

	result := a.themeSwitcher.HandleKey(keyStr)
	if result != nil {
		a.themeSwitcher.Close()
		a.SetMode(ModeNormal)
		// Apply theme immediately
		styles.Apply(result.Name, a.themeOverrides)
		// Save selection
		if a.themeSaveFn != nil {
			go a.themeSaveFn(result.Name)
		}
		return nil
	}
	if !a.themeSwitcher.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return nil
}
```

Add the styles import:
```go
	"github.com/gammons/slk/internal/ui/styles"
```

- [ ] **Step 7: Add overlay rendering in View()**

In the `View()` method, after the workspace finder overlay check, add:

```go
	if a.themeSwitcher.IsVisible() {
		screen = a.themeSwitcher.ViewOverlay(a.width, a.height, screen)
	}
```

- [ ] **Step 8: Verify build**

Run: `go build ./...`

- [ ] **Step 9: Run all tests**

Run: `go test ./...`

- [ ] **Step 10: Commit**

```bash
git add internal/ui/keys.go internal/ui/mode.go internal/ui/app.go
git commit -m "feat: integrate theme switcher overlay with Ctrl+y"
```

---

### Task 7: Wire Theme Switcher in main.go

**Files:**
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Wire theme items and save callback**

In `cmd/slk/main.go`, after `styles.Apply(...)` and before the bubbletea program starts, add:

```go
	app.SetThemeItems(styles.ThemeNames())
	app.SetThemeOverrides(cfg.Theme)
	app.SetThemeSaver(func(name string) {
		cfg.Appearance.Theme = name
		// Write updated theme to config file
		data, err := os.ReadFile(configPath)
		if err != nil {
			return
		}
		// Simple string replacement for theme field
		lines := strings.Split(string(data), "\n")
		found := false
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "theme") && strings.Contains(trimmed, "=") {
				// Check this is in [appearance] section
				lines[i] = "theme = \"" + name + "\""
				found = true
				break
			}
		}
		if !found {
			// Append [appearance] section with theme
			lines = append(lines, "", "[appearance]", "theme = \""+name+"\"")
		}
		os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
	})
```

Add the `strings` import if not present.

Note: This is a simple config save approach. It finds the `theme = ` line and replaces it. For a more robust approach, the entire config could be re-marshaled, but that risks losing comments and formatting. The simple approach is good enough.

- [ ] **Step 2: Verify build**

Run: `go build ./...`

- [ ] **Step 3: Run all tests**

Run: `go test ./...`

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat: wire theme switcher save callback"
```

---

### Task 8: Update STATUS.md and README

**Files:**
- Modify: `docs/STATUS.md`
- Modify: `README.md`

- [ ] **Step 1: Update STATUS.md**

Remove from "Not Yet Implemented":
```
- [ ] Theme support (light mode, custom colors)
- [ ] Custom themes (light mode, custom colors)
```

Add to the "UI" section under "What's Working":
```
- [x] Customizable themes (12 built-in themes, custom theme files, Ctrl+y theme switcher)
```

- [ ] **Step 2: Update README**

In the key bindings table, add:
```
| `Ctrl+y` | Normal | Switch theme |
```

In the configuration section, add a theme example:

```toml
# Custom theme colors (override active theme)
[theme]
primary = "#FF0000"
accent = "#00FF00"
```

Add a section about custom themes:

```markdown
### Custom Themes

Place `.toml` theme files in `~/.config/slk/themes/`:

```toml
name = "My Theme"

[colors]
primary = "#BD93F9"
accent = "#50FA7B"
warning = "#FFB86C"
error = "#FF5555"
background = "#282A36"
surface = "#343746"
surface_dark = "#21222C"
text = "#F8F8F2"
text_muted = "#6272A4"
border = "#44475A"
```
```

- [ ] **Step 3: Commit**

```bash
git add docs/STATUS.md README.md
git commit -m "docs: document theme customization and switcher"
```
