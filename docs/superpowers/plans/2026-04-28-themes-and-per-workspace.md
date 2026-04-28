# Themes Expansion & Per-Workspace Theme — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add 12 new built-in themes and let users assign a theme per workspace via the existing Ctrl+Y picker (per-workspace) plus a new Ctrl+Shift+Y picker (global default).

**Architecture:** Twelve entries appended to `builtinThemes` (purely additive). Per-workspace theme stored as `[workspaces.<TeamID>]` in `~/.config/slk/config.toml`; resolution at workspace-switch time via a new `Config.ResolveTheme(teamID)` method; the existing theme picker overlay gains a scope (workspace vs. global) that drives a parameterized save callback.

**Tech Stack:** Go, `github.com/pelletier/go-toml/v2` (already a dep), `charm.land/lipgloss/v2`, existing `internal/ui/styles` and `internal/ui/themeswitcher` packages.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/ui/styles/themes.go` | Add 12 entries to the existing `builtinThemes` map. |
| `internal/ui/styles/themes_test.go` | Verify each new theme registers, resolves, has all 14 fields. |
| `internal/config/config.go` | Add `WorkspaceSettings`, `Workspaces` field, `ResolveTheme`. |
| `internal/config/config_test.go` (new) | Tests for `ResolveTheme`. |
| `internal/ui/themeswitcher/model.go` | Add `Scope`, header text plumbing, return scope in `ThemeResult`. |
| `internal/ui/themeswitcher/model_test.go` (new) | Tests for scope behavior. |
| `internal/ui/keys.go` | Add `ThemeSwitcherGlobal` keybind for Ctrl+Shift+Y. |
| `internal/ui/app.go` | Handle the new keybind, parameterize the save callback by scope. |
| `cmd/slk/save_theme.go` (new) | `saveWorkspaceTheme` and `saveGlobalTheme` functions, replacing the inline saver in `main.go`. |
| `cmd/slk/save_theme_test.go` (new) | Round-trip tests for both savers. |
| `cmd/slk/main.go` | Wire in the new save dispatch, apply theme on workspace switch, add `--list-workspaces`. |

---

## Task 1: Add 12 new built-in themes

**Files:**
- Modify: `internal/ui/styles/themes.go`
- Modify: `internal/ui/styles/themes_test.go`

- [ ] **Step 1: Read the existing themes file to confirm map structure**

Run: `head -60 internal/ui/styles/themes.go`
Confirm the `builtinThemes` map starts at around line 36 and uses the format:
```go
"theme name": {"Display Name", ThemeColors{ ... }},
```

- [ ] **Step 2: Append 12 entries before the closing `}` of `builtinThemes`**

Open `internal/ui/styles/themes.go`. Find the `builtinThemes` map. Just before the closing `}` (currently line 150 after the `synthwave` entry), insert these 12 entries:

```go
	"catppuccin latte": {"Catppuccin Latte", ThemeColors{
		Primary: "#1E66F5", Accent: "#40A02B", Warning: "#DF8E1D", Error: "#D20F39",
		Background: "#EFF1F5", Surface: "#E6E9EF", SurfaceDark: "#DCE0E8",
		Text: "#4C4F69", TextMuted: "#6C6F85", Border: "#BCC0CC",
		SidebarBackground: "#1E1E2E", SidebarText: "#CDD6F4", SidebarTextMuted: "#9399B2",
		RailBackground: "#181825",
	}},
	"github light": {"GitHub Light", ThemeColors{
		Primary: "#0969DA", Accent: "#1A7F37", Warning: "#9A6700", Error: "#CF222E",
		Background: "#FFFFFF", Surface: "#F6F8FA", SurfaceDark: "#EAEEF2",
		Text: "#1F2328", TextMuted: "#656D76", Border: "#D0D7DE",
		SidebarBackground: "#24292F", SidebarText: "#F6F8FA", SidebarTextMuted: "#8C959F",
		RailBackground: "#1B1F23",
	}},
	"tokyo night light": {"Tokyo Night Light", ThemeColors{
		Primary: "#34548A", Accent: "#485E30", Warning: "#8F5E15", Error: "#8C4351",
		Background: "#D5D6DB", Surface: "#CBCCD1", SurfaceDark: "#C4C8DA",
		Text: "#343B58", TextMuted: "#6172B0", Border: "#9699A8",
		SidebarBackground: "#1A1B26", SidebarText: "#A9B1D6", SidebarTextMuted: "#565F89",
		RailBackground: "#16161E",
	}},
	"atom one light": {"Atom One Light", ThemeColors{
		Primary: "#4078F2", Accent: "#50A14F", Warning: "#C18401", Error: "#E45649",
		Background: "#FAFAFA", Surface: "#F0F0F0", SurfaceDark: "#E5E5E6",
		Text: "#383A42", TextMuted: "#A0A1A7", Border: "#D3D3D3",
		SidebarBackground: "#282C34", SidebarText: "#ABB2BF", SidebarTextMuted: "#5C6370",
		RailBackground: "#21252B",
	}},
	"catppuccin frappé": {"Catppuccin Frappé", ThemeColors{
		Primary: "#8CAAEE", Accent: "#A6D189", Warning: "#E5C890", Error: "#E78284",
		Background: "#303446", Surface: "#414559", SurfaceDark: "#292C3C",
		Text: "#C6D0F5", TextMuted: "#838BA7", Border: "#51576D",
	}},
	"catppuccin macchiato": {"Catppuccin Macchiato", ThemeColors{
		Primary: "#8AADF4", Accent: "#A6DA95", Warning: "#EED49F", Error: "#ED8796",
		Background: "#24273A", Surface: "#363A4F", SurfaceDark: "#1E2030",
		Text: "#CAD3F5", TextMuted: "#6E738D", Border: "#494D64",
	}},
	"tokyo night storm": {"Tokyo Night Storm", ThemeColors{
		Primary: "#7AA2F7", Accent: "#9ECE6A", Warning: "#E0AF68", Error: "#F7768E",
		Background: "#24283B", Surface: "#2F334D", SurfaceDark: "#1F2335",
		Text: "#C0CAF5", TextMuted: "#565F89", Border: "#3B4261",
	}},
	"cobalt2": {"Cobalt2", ThemeColors{
		Primary: "#FFC600", Accent: "#3AD900", Warning: "#FF9D00", Error: "#FF628C",
		Background: "#193549", Surface: "#1F4662", SurfaceDark: "#15232D",
		Text: "#E1EFFF", TextMuted: "#6E96B5", Border: "#0D3A58",
	}},
	"iceberg": {"Iceberg", ThemeColors{
		Primary: "#84A0C6", Accent: "#B4BE82", Warning: "#E2A478", Error: "#E27878",
		Background: "#161821", Surface: "#1E2132", SurfaceDark: "#0F1117",
		Text: "#C6C8D1", TextMuted: "#6B7089", Border: "#2E313F",
	}},
	"oceanic next": {"Oceanic Next", ThemeColors{
		Primary: "#6699CC", Accent: "#99C794", Warning: "#FAC863", Error: "#EC5F67",
		Background: "#1B2B34", Surface: "#343D46", SurfaceDark: "#16232B",
		Text: "#CDD3DE", TextMuted: "#65737E", Border: "#4F5B66",
	}},
	"cyberpunk neon": {"Cyberpunk Neon", ThemeColors{
		Primary: "#0ABDC6", Accent: "#00FF9C", Warning: "#FCEE0C", Error: "#EA00D9",
		Background: "#000B1E", Surface: "#0D1B2A", SurfaceDark: "#000814",
		Text: "#D7D7D7", TextMuted: "#7E7E8E", Border: "#133E7C",
	}},
	"material palenight": {"Material Palenight", ThemeColors{
		Primary: "#82AAFF", Accent: "#C3E88D", Warning: "#FFCB6B", Error: "#FF5370",
		Background: "#292D3E", Surface: "#34324A", SurfaceDark: "#1F1F2E",
		Text: "#A6ACCD", TextMuted: "#676E95", Border: "#3A3F58",
	}},
```

- [ ] **Step 3: Add the test for new themes**

Open `internal/ui/styles/themes_test.go` and append a new test function:

```go
func TestNewBuiltinThemesRegistered(t *testing.T) {
	newThemes := []string{
		"Catppuccin Latte",
		"GitHub Light",
		"Tokyo Night Light",
		"Atom One Light",
		"Catppuccin Frappé",
		"Catppuccin Macchiato",
		"Tokyo Night Storm",
		"Cobalt2",
		"Iceberg",
		"Oceanic Next",
		"Cyberpunk Neon",
		"Material Palenight",
	}

	names := ThemeNames()
	have := make(map[string]bool, len(names))
	for _, n := range names {
		have[n] = true
	}

	for _, want := range newThemes {
		if !have[want] {
			t.Errorf("new built-in theme %q not registered (ThemeNames: %v)", want, names)
		}
	}
}

func TestNewThemesHaveRequiredColors(t *testing.T) {
	newThemes := []string{
		"catppuccin latte",
		"github light",
		"tokyo night light",
		"atom one light",
		"catppuccin frappé",
		"catppuccin macchiato",
		"tokyo night storm",
		"cobalt2",
		"iceberg",
		"oceanic next",
		"cyberpunk neon",
		"material palenight",
	}
	for _, key := range newThemes {
		c := lookupTheme(key)
		if c.Primary == "" || c.Accent == "" || c.Warning == "" || c.Error == "" ||
			c.Background == "" || c.Surface == "" || c.SurfaceDark == "" ||
			c.Text == "" || c.TextMuted == "" || c.Border == "" {
			t.Errorf("theme %q is missing one or more required color fields: %+v", key, c)
		}
	}
}

func TestLightThemesHaveDarkSidebars(t *testing.T) {
	// Light themes should set SidebarBackground/etc explicitly so the
	// sidebar/rail aren't washed out against the light message pane.
	lightThemes := []string{
		"catppuccin latte",
		"github light",
		"tokyo night light",
		"atom one light",
	}
	for _, key := range lightThemes {
		c := lookupTheme(key)
		if c.SidebarBackground == "" {
			t.Errorf("light theme %q must set SidebarBackground", key)
		}
		if c.RailBackground == "" {
			t.Errorf("light theme %q must set RailBackground", key)
		}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/styles/ -run "TestNewBuiltin|TestNewThemes|TestLightThemes" -v`
Expected: 3 tests PASS.

- [ ] **Step 5: Run the full styles test suite for regressions**

Run: `go test ./internal/ui/styles/ -count=1`
Expected: ok, no failures.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/styles/themes.go internal/ui/styles/themes_test.go
git commit -m "feat(themes): add 12 new built-in themes

Adds Catppuccin Latte, GitHub Light, Tokyo Night Light, Atom One Light,
Catppuccin Frappé, Catppuccin Macchiato, Tokyo Night Storm, Cobalt2,
Iceberg, Oceanic Next, Cyberpunk Neon, and Material Palenight to the
built-in theme registry. Light themes set SidebarBackground/RailBackground
explicitly so the sidebar contrasts with the light message pane."
```

---

## Task 2: Add `WorkspaceSettings` and `ResolveTheme` to config

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/config/config_test.go`:

```go
package config

import "testing"

func TestResolveThemeWorkspaceWins(t *testing.T) {
	c := Config{
		Appearance: Appearance{Theme: "dark"},
		Workspaces: map[string]WorkspaceSettings{
			"T01": {Theme: "dracula"},
		},
	}
	if got := c.ResolveTheme("T01"); got != "dracula" {
		t.Errorf("ResolveTheme(T01) = %q, want dracula", got)
	}
}

func TestResolveThemeWorkspaceMissing(t *testing.T) {
	c := Config{
		Appearance: Appearance{Theme: "tokyo night"},
		Workspaces: map[string]WorkspaceSettings{
			"T01": {Theme: "dracula"},
		},
	}
	if got := c.ResolveTheme("T99"); got != "tokyo night" {
		t.Errorf("ResolveTheme(T99) = %q, want tokyo night (global)", got)
	}
}

func TestResolveThemeWorkspaceEmpty(t *testing.T) {
	// Workspace exists in map but has empty Theme.
	c := Config{
		Appearance: Appearance{Theme: "tokyo night"},
		Workspaces: map[string]WorkspaceSettings{
			"T01": {Theme: ""},
		},
	}
	if got := c.ResolveTheme("T01"); got != "tokyo night" {
		t.Errorf("ResolveTheme empty ws theme = %q, want tokyo night", got)
	}
}

func TestResolveThemeNoGlobal(t *testing.T) {
	c := Config{
		Appearance: Appearance{Theme: ""},
		Workspaces: map[string]WorkspaceSettings{},
	}
	if got := c.ResolveTheme("T01"); got != "dark" {
		t.Errorf("ResolveTheme no global = %q, want dark", got)
	}
}

func TestResolveThemeNilWorkspaces(t *testing.T) {
	// A config loaded from a file that has no [workspaces] section
	// will have a nil Workspaces map. ResolveTheme must not panic.
	c := Config{
		Appearance: Appearance{Theme: "nord"},
	}
	if got := c.ResolveTheme("T01"); got != "nord" {
		t.Errorf("ResolveTheme nil workspaces = %q, want nord", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestResolveTheme -v`
Expected: build error or fail with "undefined: WorkspaceSettings" / "undefined: ResolveTheme".

- [ ] **Step 3: Implement `WorkspaceSettings` and `ResolveTheme`**

Open `internal/config/config.go`. After the `CacheConfig` struct (around line 60), add:

```go
// WorkspaceSettings holds per-workspace user preferences. Currently
// only Theme is configurable; future per-workspace settings (notification
// rules, default channel, etc.) belong here.
type WorkspaceSettings struct {
	Theme string `toml:"theme"`
}
```

In the `Config` struct (lines 12-20), add the new field:

```go
type Config struct {
	General       General                       `toml:"general"`
	Appearance    Appearance                    `toml:"appearance"`
	Animations    Animations                    `toml:"animations"`
	Notifications Notifications                 `toml:"notifications"`
	Cache         CacheConfig                   `toml:"cache"`
	Sections      map[string]SectionDef         `toml:"sections"`
	Theme         Theme                         `toml:"theme"`
	Workspaces    map[string]WorkspaceSettings  `toml:"workspaces"`
}
```

At the end of the file, add the `ResolveTheme` method:

```go
// ResolveTheme returns the theme name to use for the given workspace,
// falling back to the global Appearance.Theme when no per-workspace theme
// is set, and to "dark" when no global theme is set either.
func (c Config) ResolveTheme(teamID string) string {
	if ws, ok := c.Workspaces[teamID]; ok && ws.Theme != "" {
		return ws.Theme
	}
	if c.Appearance.Theme != "" {
		return c.Appearance.Theme
	}
	return "dark"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestResolveTheme -v`
Expected: 5 PASS.

- [ ] **Step 5: Run all config tests**

Run: `go test ./internal/config/ -count=1`
Expected: ok.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add per-workspace settings and ResolveTheme

Adds WorkspaceSettings struct and Workspaces map to Config, plus
ResolveTheme(teamID) which falls through workspace -> global -> 'dark'."
```

---

## Task 3: Add `ThemeScope` and `headerText` to themeswitcher

**Files:**
- Modify: `internal/ui/themeswitcher/model.go`
- Create: `internal/ui/themeswitcher/model_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ui/themeswitcher/model_test.go`:

```go
package themeswitcher

import "testing"

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/themeswitcher/ -v`
Expected: build error or fail with "undefined: ScopeWorkspace" etc.

- [ ] **Step 3: Add scope plumbing to the model**

Open `internal/ui/themeswitcher/model.go`.

Replace the `ThemeResult` struct (line 13-16):

```go
// ThemeScope identifies whether a theme selection should be saved to
// the active workspace or to the global default.
type ThemeScope int

const (
	ScopeGlobal ThemeScope = iota
	ScopeWorkspace
)

// ThemeResult is returned when the user selects a theme.
type ThemeResult struct {
	Name  string
	Scope ThemeScope
}
```

Add new fields to the `Model` struct (line 19-25):

```go
type Model struct {
	items      []string // theme display names
	filtered   []int    // indices into items matching query
	query      string
	selected   int // index into filtered
	visible    bool
	scope      ThemeScope
	headerText string
}
```

Replace the `Open` method and add new accessor methods (after line 48 where `Close` is):

```go
// Open shows the overlay and resets state. Defaults to ScopeGlobal with no
// custom header text. Use OpenWithScope to set a scope and header.
func (m *Model) Open() {
	m.OpenWithScope(ScopeGlobal, "")
}

// OpenWithScope shows the overlay scoped to either the active workspace or
// the global default. headerText, if non-empty, replaces the default
// "Switch Theme" title in the rendered overlay.
func (m *Model) OpenWithScope(scope ThemeScope, headerText string) {
	m.visible = true
	m.query = ""
	m.selected = 0
	m.scope = scope
	m.headerText = headerText
	m.filter()
}

// Scope returns the scope the picker was last opened with.
func (m Model) Scope() ThemeScope { return m.scope }

// HeaderText returns the header text the picker was last opened with.
func (m Model) HeaderText() string { return m.headerText }
```

In `HandleKey`, update the `enter` case (line 59-63) to include scope in the result:

```go
	case "enter":
		if len(m.filtered) > 0 {
			idx := m.filtered[m.selected]
			return &ThemeResult{Name: m.items[idx], Scope: m.scope}
		}
		return nil
```

In `renderBox`, update the `title` line (currently around line 157-161) to use the dynamic header when set:

```go
	titleText := "Switch Theme"
	if m.headerText != "" {
		titleText = m.headerText
	}
	title := lipgloss.NewStyle().
		Bold(true).
		Background(bg).
		Foreground(styles.Primary).
		Render(titleText)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/themeswitcher/ -v`
Expected: 3 PASS.

- [ ] **Step 5: Run the broader UI tests for regressions**

Run: `go test ./internal/ui/... -count=1`
Expected: ok across all UI packages.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/themeswitcher/model.go internal/ui/themeswitcher/model_test.go
git commit -m "feat(themeswitcher): add ThemeScope and OpenWithScope

ThemeScope distinguishes workspace-scoped selections (Ctrl+Y) from
global-default selections (Ctrl+Shift+Y). The model accepts a header
text override so callers can label which scope is active."
```

---

## Task 4: Add Ctrl+Shift+Y keybind

**Files:**
- Modify: `internal/ui/keys.go`

- [ ] **Step 1: Add the new binding**

Open `internal/ui/keys.go`. Add a new field to the `KeyMap` struct (after line 34 `ThemeSwitcher`):

```go
	ThemeSwitcherGlobal key.Binding
```

In `DefaultKeyMap()`, after the `ThemeSwitcher` binding (line 66), add:

```go
		ThemeSwitcherGlobal: key.NewBinding(key.WithKeys("ctrl+shift+y"), key.WithHelp("ctrl+shift+y", "set default theme")),
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/keys.go
git commit -m "feat(keys): add Ctrl+Shift+Y for global theme picker"
```

---

## Task 5: Update app to dispatch keybind and parameterize the saver

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Find the existing theme saver type**

Run: `grep -n "themeSaveFn\|SetThemeSaver" internal/ui/app.go`
Expected: a `themeSaveFn func(name string)` field and a `SetThemeSaver` setter (around lines 1759-1761).

- [ ] **Step 2: Change the saver type to include scope**

Open `internal/ui/app.go`. Find the `themeSaveFn` field (in the App struct around line ~318). Change its type:

```go
	themeSaveFn     func(name string, scope themeswitcher.ThemeScope)
```

Find the `SetThemeSaver` method (around line 1759-1761). Update it to match:

```go
// SetThemeSaver sets the callback for saving the theme selection. The
// callback receives the chosen theme name and the scope (workspace vs.
// global) so the implementation can route to the correct save target.
func (a *App) SetThemeSaver(fn func(name string, scope themeswitcher.ThemeScope)) {
	a.themeSaveFn = fn
}
```

In `handleThemeSwitcherMode` (around line 1080-1118), update the call to `themeSaveFn` (line 1110):

```go
		if a.themeSaveFn != nil {
			go a.themeSaveFn(result.Name, result.Scope)
		}
```

- [ ] **Step 3: Wire the new keybind**

Find the existing `case key.Matches(msg, a.keys.ThemeSwitcher)` handler (around line 845-848). Replace that entire case (and add a new one) with:

```go
	case key.Matches(msg, a.keys.ThemeSwitcher):
		// Per-workspace scope. Header text shows the current workspace name.
		header := "Theme for " + a.activeTeamName()
		a.themeSwitcher.OpenWithScope(themeswitcher.ScopeWorkspace, header)
		a.SetMode(ModeThemeSwitcher)
		return nil
	case key.Matches(msg, a.keys.ThemeSwitcherGlobal):
		a.themeSwitcher.OpenWithScope(themeswitcher.ScopeGlobal, "Default theme for new workspaces")
		a.SetMode(ModeThemeSwitcher)
		return nil
```

- [ ] **Step 4: Add the `activeTeamName` helper**

Find the existing methods on `*App`. Near the top of the App methods (around line 1700, before `SetWorkspaces` or similar), add:

```go
// activeTeamName returns the human-readable name of the active workspace,
// falling back to the team ID if no name is known. Used as a label in the
// theme picker header.
func (a *App) activeTeamName() string {
	for _, w := range a.workspaces {
		if w.ID == a.activeTeamID {
			if w.Name != "" {
				return w.Name
			}
			return w.ID
		}
	}
	if a.activeTeamID != "" {
		return a.activeTeamID
	}
	return "this workspace"
}
```

Note: this assumes `a.workspaces` is a `[]workspace.WorkspaceItem` slice and `a.activeTeamID` is the active team ID string. Verify these field names exist; if they're named differently, adapt the helper accordingly. Run `grep -n "workspaces \[\]" internal/ui/app.go` and `grep -n "activeTeamID" internal/ui/app.go` to confirm.

- [ ] **Step 5: Build and run tests**

Run: `go build ./...`
Expected: clean build.

Run: `go test ./internal/ui/... -count=1`
Expected: ok.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat(app): dispatch Ctrl+Shift+Y, parameterize theme saver by scope

Ctrl+Y opens the picker with workspace scope and a header labeling the
active workspace. Ctrl+Shift+Y opens with global scope. The save
callback now receives both the chosen theme and the scope."
```

---

## Task 6: Extract savers into `cmd/slk/save_theme.go`

**Files:**
- Create: `cmd/slk/save_theme.go`
- Create: `cmd/slk/save_theme_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/slk/save_theme_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveGlobalThemeNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[appearance]\ntheme = \"dark\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := saveGlobalTheme(path, "dracula"); err != nil {
		t.Fatalf("saveGlobalTheme: %v", err)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `theme = "dracula"`) {
		t.Errorf("expected theme = \"dracula\", got:\n%s", data)
	}
}

func TestSaveGlobalThemeAddsAppearanceWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[general]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := saveGlobalTheme(path, "dracula"); err != nil {
		t.Fatalf("saveGlobalTheme: %v", err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "[appearance]") || !strings.Contains(got, `theme = "dracula"`) {
		t.Errorf("expected appended [appearance] section, got:\n%s", got)
	}
}

func TestSaveWorkspaceThemeNewSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[appearance]\ntheme = \"dark\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := saveWorkspaceTheme(path, "T01ABCDEF", "ACME Corp", "dracula"); err != nil {
		t.Fatalf("saveWorkspaceTheme: %v", err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "# ACME Corp") {
		t.Errorf("expected '# ACME Corp' comment in:\n%s", got)
	}
	if !strings.Contains(got, "[workspaces.T01ABCDEF]") {
		t.Errorf("expected [workspaces.T01ABCDEF] section in:\n%s", got)
	}
	if !strings.Contains(got, `theme = "dracula"`) {
		t.Errorf("expected theme = \"dracula\" in:\n%s", got)
	}
	// Global theme should be untouched.
	if !strings.Contains(got, `[appearance]`) {
		t.Errorf("global [appearance] section was lost:\n%s", got)
	}
}

func TestSaveWorkspaceThemeUpdatesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := `[appearance]
theme = "dark"

# ACME Corp
[workspaces.T01ABCDEF]
theme = "dracula"
`
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	if err := saveWorkspaceTheme(path, "T01ABCDEF", "ACME Corp", "tokyo night"); err != nil {
		t.Fatalf("saveWorkspaceTheme: %v", err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, `theme = "tokyo night"`) {
		t.Errorf("expected updated theme, got:\n%s", got)
	}
	// The old "dracula" should be gone.
	if strings.Contains(got, `theme = "dracula"`) {
		t.Errorf("old theme still present:\n%s", got)
	}
	// Comment should remain.
	if !strings.Contains(got, "# ACME Corp") {
		t.Errorf("comment was lost:\n%s", got)
	}
}

func TestSaveWorkspaceThemeMultipleWorkspaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[appearance]\ntheme = \"dark\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := saveWorkspaceTheme(path, "T01", "ACME", "dracula"); err != nil {
		t.Fatal(err)
	}
	if err := saveWorkspaceTheme(path, "T02", "Personal", "tokyo night"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "[workspaces.T01]") || !strings.Contains(got, "[workspaces.T02]") {
		t.Errorf("expected both workspace sections, got:\n%s", got)
	}
	if !strings.Contains(got, `theme = "dracula"`) || !strings.Contains(got, `theme = "tokyo night"`) {
		t.Errorf("expected both themes, got:\n%s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/slk/ -run TestSave -v`
Expected: build error or fail with "undefined: saveGlobalTheme" / "undefined: saveWorkspaceTheme".

- [ ] **Step 3: Implement the savers**

Create `cmd/slk/save_theme.go`:

```go
package main

import (
	"fmt"
	"os"
	"strings"
)

// saveGlobalTheme rewrites the [appearance] theme line in config.toml.
// If the file has no theme line, it appends a new [appearance] section.
// Existing comments and ordering are preserved (textual rewrite, not
// TOML re-marshal).
func saveGlobalTheme(configPath, themeName string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")

	// Look for the first top-level `theme = ...` line. We can't
	// distinguish [appearance] theme from [theme.colors] context here, but
	// the existing implementation has the same limitation and it has been
	// adequate. The Workspaces section is always written below the
	// [appearance] block by saveWorkspaceTheme, so the first theme line
	// will be the [appearance] one in practice.
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "theme") && strings.Contains(trimmed, "=") &&
			!strings.HasPrefix(trimmed, "theme.") {
			lines[i] = `theme = "` + themeName + `"`
			return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
		}
	}
	// No theme line found — append a new [appearance] section.
	lines = append(lines, "", "[appearance]", `theme = "`+themeName+`"`)
	return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
}

// saveWorkspaceTheme rewrites or appends a [workspaces.<TeamID>] theme
// entry. If the section already exists the theme line is updated in
// place; otherwise a new section is appended at the end of the file
// preceded by a "# <name>" comment for human readability.
func saveWorkspaceTheme(configPath, teamID, teamName, themeName string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")

	header := fmt.Sprintf("[workspaces.%s]", teamID)

	// Find the section header.
	sectionStart := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			sectionStart = i
			break
		}
	}

	if sectionStart >= 0 {
		// Find the next blank line or section header — that's the end of
		// our section. Update the theme line within.
		end := len(lines)
		for j := sectionStart + 1; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if t == "" || strings.HasPrefix(t, "[") {
				end = j
				break
			}
		}
		updated := false
		for j := sectionStart + 1; j < end; j++ {
			t := strings.TrimSpace(lines[j])
			if strings.HasPrefix(t, "theme") && strings.Contains(t, "=") {
				lines[j] = `theme = "` + themeName + `"`
				updated = true
				break
			}
		}
		if !updated {
			// Insert theme line right after the header.
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:sectionStart+1]...)
			newLines = append(newLines, `theme = "`+themeName+`"`)
			newLines = append(newLines, lines[sectionStart+1:]...)
			lines = newLines
		}
		return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
	}

	// No existing section — append at end.
	// Ensure the file ends with a blank line before our new section.
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}
	commentLine := "# " + teamName
	if teamName == "" {
		commentLine = "# " + teamID
	}
	lines = append(lines, commentLine, header, `theme = "`+themeName+`"`)
	return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/slk/ -run TestSave -v`
Expected: 5 PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/save_theme.go cmd/slk/save_theme_test.go
git commit -m "feat(slk): saveGlobalTheme and saveWorkspaceTheme

Extracts the theme saver from main.go into testable functions.
saveGlobalTheme rewrites or appends the [appearance] theme line.
saveWorkspaceTheme rewrites or appends a [workspaces.<TeamID>] section,
prepending a '# <name>' comment when creating the section so users
can identify workspaces by name in config.toml."
```

---

## Task 7: Wire savers into main.go and apply theme on workspace switch

**Files:**
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Replace the inline theme saver with the new dispatch**

Open `cmd/slk/main.go`. Find the existing `app.SetThemeSaver` block (lines 235-257). Replace the entire block with:

```go
	// Wire theme switcher: dispatch to the appropriate saver based on scope.
	app.SetThemeSaver(func(name string, scope themeswitcher.ThemeScope) {
		switch scope {
		case themeswitcher.ScopeWorkspace:
			if activeTeamID == "" {
				return // shouldn't happen, but guard against it
			}
			// Find the team name for the comment.
			teamName := activeTeamID
			if wctx, ok := workspaces[activeTeamID]; ok && wctx.TeamName != "" {
				teamName = wctx.TeamName
			}
			// Update in-memory config.
			if cfg.Workspaces == nil {
				cfg.Workspaces = make(map[string]config.WorkspaceSettings)
			}
			ws := cfg.Workspaces[activeTeamID]
			ws.Theme = name
			cfg.Workspaces[activeTeamID] = ws
			// Persist.
			if err := saveWorkspaceTheme(configPath, activeTeamID, teamName, name); err != nil {
				log.Printf("save workspace theme: %v", err)
			}
		case themeswitcher.ScopeGlobal:
			cfg.Appearance.Theme = name
			if err := saveGlobalTheme(configPath, name); err != nil {
				log.Printf("save global theme: %v", err)
			}
		}
	})
```

This requires importing `"github.com/gammons/slk/internal/ui/themeswitcher"` and `"github.com/gammons/slk/internal/config"` in main.go. Run `grep -n "themeswitcher\\|gammons/slk/internal/config" cmd/slk/main.go` to confirm; add to imports if missing.

- [ ] **Step 2: Apply per-workspace theme on workspace switch**

Find the `app.SetWorkspaceSwitcher` callback (lines 412-434). After `activeTeamID = teamID` (line 420), add:

```go
		// Apply this workspace's theme (or fall back to global / dark).
		styles.Apply(cfg.ResolveTheme(teamID), cfg.Theme)
		// Force a re-render so the new theme is visible across all panels.
		// (The styles.Version mechanism will invalidate caches automatically
		// on next render, but explicit invalidation makes the switch snappy.)
```

- [ ] **Step 3: Apply theme at startup based on the initially-active workspace**

Find the existing `styles.Apply(cfg.Appearance.Theme, cfg.Theme)` call (around line 161). Note that at startup the active workspace isn't known yet — the switch callback runs later. So change the startup call to use the global default explicitly (which is what `Appearance.Theme` is). Leave it as-is, but add a comment:

```go
	// At startup we apply the global default; per-workspace themes are
	// applied later when the workspace switcher fires for the initial
	// active workspace.
	styles.Apply(cfg.Appearance.Theme, cfg.Theme)
```

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 5: Run all tests**

Run: `go test ./... -count=1 -timeout 60s`
Expected: ok across all packages.

- [ ] **Step 6: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat(slk): apply per-workspace theme on workspace switch

The theme saver now dispatches by scope to saveWorkspaceTheme or
saveGlobalTheme. The workspace-switch callback calls
styles.Apply(cfg.ResolveTheme(teamID), cfg.Theme) so each workspace
re-themes the entire UI when activated."
```

---

## Task 8: Add `slk --list-workspaces` CLI subcommand

**Files:**
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Find the existing CLI flag handling**

Run: `grep -n "add-workspace" cmd/slk/main.go | head -5`
Expected: a section near `func main()` that handles `--add-workspace`.

- [ ] **Step 2: Add `--list-workspaces` handling**

Open `cmd/slk/main.go`. After the existing `--add-workspace` early-return block (around line 58-65), add:

```go
	if len(os.Args) > 1 && os.Args[1] == "--list-workspaces" {
		if err := listWorkspaces(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
```

- [ ] **Step 3: Implement `listWorkspaces`**

At the end of `cmd/slk/main.go` (or in a sensible adjacent spot), add:

```go
// listWorkspaces prints the configured workspaces with their TeamID,
// Name, and Domain, one per line. Useful for users who want to hand-edit
// per-workspace settings in config.toml.
func listWorkspaces() error {
	store, err := slackclient.NewTokenStore()
	if err != nil {
		return fmt.Errorf("token store: %w", err)
	}
	tokens, err := store.List()
	if err != nil {
		return fmt.Errorf("list tokens: %w", err)
	}
	if len(tokens) == 0 {
		fmt.Println("No workspaces configured. Run 'slk --add-workspace' first.")
		return nil
	}
	// Compute column widths for tidy output.
	idW, nameW := len("TEAM ID"), len("NAME")
	for _, t := range tokens {
		if len(t.TeamID) > idW {
			idW = len(t.TeamID)
		}
		if len(t.TeamName) > nameW {
			nameW = len(t.TeamName)
		}
	}
	fmt.Printf("%-*s  %-*s  %s\n", idW, "TEAM ID", nameW, "NAME", "DOMAIN")
	fmt.Printf("%-*s  %-*s  %s\n", idW, strings.Repeat("-", idW), nameW, strings.Repeat("-", nameW), strings.Repeat("-", 6))
	for _, t := range tokens {
		fmt.Printf("%-*s  %-*s  %s\n", idW, t.TeamID, nameW, t.TeamName, "")
	}
	return nil
}
```

Note: `Token` doesn't currently carry a Domain field. The third column is left blank — this is fine; users primarily need TeamID + Name. (Adding Domain to the token struct is out of scope.)

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 5: Manual smoke test**

Run: `./bin/slk --list-workspaces` (after `make build`).
Expected: tabular output of configured workspaces, or the "no workspaces configured" message.

- [ ] **Step 6: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat(slk): add --list-workspaces CLI subcommand

Prints configured workspaces with TeamID and Name so users hand-editing
config.toml can identify which [workspaces.<TeamID>] section is which."
```

---

## Task 9: Manual end-to-end verification

**No new files. Verification only.**

- [ ] **Step 1: Build**

Run: `make build`
Expected: clean build, binary at `bin/slk`.

- [ ] **Step 2: List workspaces**

Run: `./bin/slk --list-workspaces`
Expected: at least one workspace listed with TeamID and Name.

- [ ] **Step 3: Launch with a single workspace**

Run: `./bin/slk`
Expected: TUI launches normally; theme is the global default from `~/.config/slk/config.toml`.

- [ ] **Step 4: Open the per-workspace picker**

Press: `Ctrl+Y`
Expected: picker shows header `Theme for <Workspace Name>`. Pick a different theme. UI re-themes immediately.

- [ ] **Step 5: Verify the per-workspace setting persisted**

Run: `cat ~/.config/slk/config.toml | grep -A2 workspaces`
Expected: a `[workspaces.<TeamID>]` section with a `# <Name>` comment above it and the chosen theme.

- [ ] **Step 6: Switch to another workspace**

Press: `Ctrl+W` and pick a different workspace.
Expected: theme changes (to that workspace's per-workspace theme, or to the global default if none).

- [ ] **Step 7: Switch back**

Switch back to the first workspace.
Expected: it remembers its per-workspace theme.

- [ ] **Step 8: Test the global picker**

On a workspace that has no per-workspace theme set, press `Ctrl+Shift+Y`. Pick a theme.
Expected: header reads "Default theme for new workspaces". After selection, UI re-themes (because this workspace has no per-workspace theme so falls through to the new global). `~/.config/slk/config.toml` shows updated `[appearance] theme`.

- [ ] **Step 9: Test global picker on a configured workspace**

On a workspace with a per-workspace theme, press `Ctrl+Shift+Y` and pick a different theme.
Expected: `[appearance] theme` updates in `config.toml`, but the active session retains the per-workspace theme.

- [ ] **Step 10: Test all 12 new themes are listed**

Press `Ctrl+Y`. Type "catpp", "github", "tokyo", "cobalt", "iceberg", "oceanic", "cyber", "material" — each filter should show the matching new theme. Pick each in turn and visually confirm colors look reasonable.

- [ ] **Step 11: Edge case — light theme on a single workspace**

Pick "Catppuccin Latte" or "GitHub Light" via Ctrl+Y. Verify the sidebar and rail are dark (don't blend into the light message pane), reaction pills are legible, and message borders align.

If any of these checks fail, file a follow-up issue. The plan ends here.
