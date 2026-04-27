# Customizable Themes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to customize the app's color scheme via a `[theme]` section in config.toml, with dark and light presets.

**Architecture:** Add a `Theme` struct to config, add an `Apply()` function to the styles package that rebuilds all color vars and composed styles from a preset + overrides, and call it at startup.

**Tech Stack:** Go, lipgloss, go-toml/v2

---

### Task 1: Add Theme Struct to Config

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write failing test for Theme parsing**

Add to `internal/config/config_test.go`:

```go
func TestThemeParsing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`
[theme]
primary = "#FF0000"
accent = "#00FF00"
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme.Primary != "#FF0000" {
		t.Errorf("expected primary #FF0000, got %q", cfg.Theme.Primary)
	}
	if cfg.Theme.Accent != "#00FF00" {
		t.Errorf("expected accent #00FF00, got %q", cfg.Theme.Accent)
	}
	// Unset fields should be empty
	if cfg.Theme.Background != "" {
		t.Errorf("expected empty background, got %q", cfg.Theme.Background)
	}
}
```

Ensure `"os"` and `"path/filepath"` are imported in the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestThemeParsing -v`
Expected: FAIL — `cfg.Theme` field does not exist.

- [ ] **Step 3: Add Theme struct and wire into Config**

In `internal/config/config.go`, add the struct after `CacheConfig`:

```go
type Theme struct {
	Primary     string `toml:"primary"`
	Accent      string `toml:"accent"`
	Warning     string `toml:"warning"`
	Error       string `toml:"error"`
	Background  string `toml:"background"`
	Surface     string `toml:"surface"`
	SurfaceDark string `toml:"surface_dark"`
	Text        string `toml:"text"`
	TextMuted   string `toml:"text_muted"`
	Border      string `toml:"border"`
}
```

Add `Theme Theme` to the `Config` struct:

```go
type Config struct {
	General       General               `toml:"general"`
	Appearance    Appearance            `toml:"appearance"`
	Animations    Animations            `toml:"animations"`
	Notifications Notifications         `toml:"notifications"`
	Cache         CacheConfig           `toml:"cache"`
	Sections      map[string]SectionDef `toml:"sections"`
	Theme         Theme                 `toml:"theme"`
}
```

No defaults needed — zero-value empty strings mean "use preset."

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestThemeParsing -v`
Expected: PASS

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/ -v`
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: add Theme struct to config for color customization"
```

---

### Task 2: Add Apply Function to Styles Package

**Files:**
- Modify: `internal/ui/styles/styles.go`
- Create: `internal/ui/styles/styles_test.go`

This is the core task. The `Apply` function selects a preset, applies overrides, and rebuilds all styles.

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
	// Restore dark for other tests
	Apply("dark", config.Theme{})
}

func TestApplyOverrides(t *testing.T) {
	Apply("dark", config.Theme{Primary: "#FF0000"})
	if Primary != "#FF0000" {
		t.Errorf("expected overridden primary #FF0000, got %s", string(Primary))
	}
	// Other colors should remain dark defaults
	if Accent != "#50C878" {
		t.Errorf("expected dark accent #50C878, got %s", string(Accent))
	}
	// Restore
	Apply("dark", config.Theme{})
}

func TestApplyUnknownPresetFallsToDark(t *testing.T) {
	Apply("nonexistent", config.Theme{})
	if Primary != "#4A9EFF" {
		t.Errorf("expected dark fallback primary #4A9EFF, got %s", string(Primary))
	}
	// Restore
	Apply("dark", config.Theme{})
}

func TestApplyRebuildsFocusedBorder(t *testing.T) {
	Apply("dark", config.Theme{Primary: "#ABCDEF"})
	// FocusedBorder should use the new Primary color.
	// We can't easily inspect lipgloss style internals, but we can
	// verify it doesn't panic and the style is non-zero.
	rendered := FocusedBorder.Width(10).Render("test")
	if rendered == "" {
		t.Error("expected non-empty rendered output")
	}
	// Restore
	Apply("dark", config.Theme{})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/styles/ -v`
Expected: FAIL — `Apply` function does not exist.

- [ ] **Step 3: Implement Apply function and light preset**

In `internal/ui/styles/styles.go`, add the `Apply` function and a `buildStyles` helper. Add the config import. The full updated file should be:

Add import at the top:
```go
import (
	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slk/internal/config"
)
```

Add the `Apply` function and light preset after the existing `var` block (after the closing `)`):

```go
// Apply sets the color palette from a preset name and optional overrides,
// then rebuilds all composed styles.
func Apply(preset string, overrides config.Theme) {
	// Select base palette
	switch preset {
	case "light":
		Primary = lipgloss.Color("#0366D6")
		Secondary = lipgloss.Color("#586069")
		Accent = lipgloss.Color("#28A745")
		Warning = lipgloss.Color("#D9840D")
		Error = lipgloss.Color("#CB2431")
		Background = lipgloss.Color("#FFFFFF")
		Surface = lipgloss.Color("#F6F8FA")
		SurfaceDark = lipgloss.Color("#EAEEF2")
		TextPrimary = lipgloss.Color("#24292E")
		TextMuted = lipgloss.Color("#6A737D")
		Border = lipgloss.Color("#D1D5DA")
	default: // "dark" or unknown
		Primary = lipgloss.Color("#4A9EFF")
		Secondary = lipgloss.Color("#666666")
		Accent = lipgloss.Color("#50C878")
		Warning = lipgloss.Color("#E0A030")
		Error = lipgloss.Color("#E04040")
		Background = lipgloss.Color("#1A1A2E")
		Surface = lipgloss.Color("#16162B")
		SurfaceDark = lipgloss.Color("#0F0F23")
		TextPrimary = lipgloss.Color("#E0E0E0")
		TextMuted = lipgloss.Color("#888888")
		Border = lipgloss.Color("#333333")
	}

	// Apply overrides
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

	// Rebuild all composed styles
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
		Bold(true).
		Padding(0, 1).
		Align(lipgloss.Center)

	WorkspaceInactive = lipgloss.NewStyle().
		Background(lipgloss.Color("#444444")).
		Foreground(TextPrimary).
		Padding(0, 1).
		Align(lipgloss.Center)

	ChannelSelected = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).
		Padding(0, 1)

	ChannelNormal = lipgloss.NewStyle().
		Foreground(TextPrimary).
		Padding(0, 1)

	ChannelUnread = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).
		Padding(0, 1)

	UnreadBadge = lipgloss.NewStyle().
		Background(Error).
		Foreground(lipgloss.Color("#FFFFFF")).
		Padding(0, 1)

	SectionHeader = lipgloss.NewStyle().
		Foreground(TextMuted).
		Bold(true).
		Padding(0, 1)

	Username = lipgloss.NewStyle().
		Foreground(Primary).
		Bold(true)

	Timestamp = lipgloss.NewStyle().
		Foreground(TextMuted).
		Italic(true)

	MessageText = lipgloss.NewStyle().
		Foreground(TextPrimary)

	ThreadIndicator = lipgloss.NewStyle().
		Foreground(Primary).
		Italic(true)

	StatusBar = lipgloss.NewStyle().
		Background(SurfaceDark).
		Foreground(TextPrimary).
		Padding(0, 1)

	StatusMode = lipgloss.NewStyle().
		Background(Primary).
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).
		Padding(0, 1)

	StatusModeInsert = lipgloss.NewStyle().
		Background(Accent).
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).
		Padding(0, 1)

	StatusModeCommand = lipgloss.NewStyle().
		Background(Warning).
		Foreground(lipgloss.Color("#000000")).
		Bold(true).
		Padding(0, 1)

	ComposeBox = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).
		BorderLeft(true).
		BorderForeground(Border).
		Background(SurfaceDark).
		MarginTop(1).
		Padding(1, 1, 1, 1)

	ComposeFocused = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).
		BorderLeft(true).
		BorderForeground(Primary).
		Background(SurfaceDark).
		MarginTop(1).
		Padding(1, 1, 1, 1)

	ComposeInsert = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).
		BorderLeft(true).
		BorderForeground(Primary).
		Background(SurfaceDark).
		MarginTop(1).
		Padding(1, 1, 1, 1)

	PresenceOnline = lipgloss.NewStyle().Foreground(Accent)
	PresenceAway = lipgloss.NewStyle().Foreground(TextMuted)

	ReactionPillOwn = lipgloss.NewStyle().
		Background(lipgloss.Color("#1a2e1a")).
		Foreground(Accent).
		Padding(0, 1)

	ReactionPillOther = lipgloss.NewStyle().
		Background(lipgloss.Color("#1a1a2e")).
		Foreground(TextMuted).
		Padding(0, 1)

	ReactionPillSelected = lipgloss.NewStyle().
		Background(lipgloss.Color("#252540")).
		Foreground(Primary).
		Padding(0, 1)

	ReactionPillPlus = lipgloss.NewStyle().
		Background(lipgloss.Color("#1a1a2e")).
		Foreground(Primary).
		Padding(0, 1)

	NewMessageSeparator = lipgloss.NewStyle().
		Foreground(Error).
		Bold(true).
		Align(lipgloss.Center)

	TypingIndicator = lipgloss.NewStyle().
		Foreground(TextMuted).
		Italic(true).
		PaddingLeft(2)
}
```

Note: The `thickLeftBorder` var remains defined in the existing `var` block — it's not a color so it doesn't need rebuilding. The reaction pill background colors (`#1a2e1a`, `#1a1a2e`, `#252540`) are kept as fixed values since they are subtle tints that work across themes. Their foreground colors now reference the semantic vars (`Accent`, `TextMuted`, `Primary`) so they adapt to the theme.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/styles/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Run full build**

Run: `go build ./...`
Expected: Compiles successfully.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/styles/styles.go internal/ui/styles/styles_test.go
git commit -m "feat: add Apply function for theme presets and color overrides"
```

---

### Task 3: Wire Theme Application at Startup

**Files:**
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Add styles.Apply call after config load**

In `cmd/slk/main.go`, in the `run()` function, after the config is loaded (line 68: `cfg, err := config.Load(configPath)`) and before the notifier creation, add:

```go
	// Apply theme colors
	styles.Apply(cfg.Appearance.Theme, cfg.Theme)
```

Add the styles import if not already present:
```go
	"github.com/gammons/slk/internal/ui/styles"
```

The full section should look like:

```go
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Apply theme colors
	styles.Apply(cfg.Appearance.Theme, cfg.Theme)

	notifier := notify.New(cfg.Notifications.Enabled)
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: Compiles successfully.

- [ ] **Step 3: Run all tests**

Run: `go test ./...`
Expected: All tests pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat: apply theme colors at startup from config"
```

---

### Task 4: Update STATUS.md and README

**Files:**
- Modify: `docs/STATUS.md`
- Modify: `README.md`

- [ ] **Step 1: Update STATUS.md**

Remove these lines from "Not Yet Implemented":
```
- [ ] Theme support (light mode, custom colors)
- [ ] Custom themes (light mode, custom colors)
```

Add to the "UI" section under "What's Working":
```
- [x] Customizable themes (dark/light presets, per-color overrides in config.toml)
```

- [ ] **Step 2: Add theme config example to README**

In `README.md`, in the Configuration section where the example `config.toml` is shown, add after the `[cache]` section:

```toml
# Custom theme colors (all optional, override preset)
[theme]
primary = "#4A9EFF"
accent = "#50C878"
background = "#1A1A2E"
text = "#E0E0E0"
```

- [ ] **Step 3: Commit**

```bash
git add docs/STATUS.md README.md
git commit -m "docs: document theme customization"
```
