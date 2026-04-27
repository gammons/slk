# Lipgloss v2 / Bubbletea v2 / Bubbles v2 Migration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate from lipgloss v1 + bubbletea v1 + bubbles v1 to v2, using the compositor for background fills, and remove all hacky space-padding/background workarounds.

**Architecture:** The migration upgrades three Charm libraries simultaneously. The compositor replaces the `lipgloss.Place(WithWhitespaceBackground)` pattern in `app.go` View() with a proper background layer. All per-element `.Background(styles.Background)` hacks in styles.go and across UI components are removed since the compositor fills uncolored cells. The bubbletea v2 declarative View API replaces imperative program options.

**Tech Stack:** Go, charm.land/lipgloss/v2, charm.land/bubbletea/v2, charm.land/bubbles/v2, charm.land/huh/v2

---

## Reference: Upgrade Guides

- Lipgloss v2: https://github.com/charmbracelet/lipgloss/blob/main/UPGRADE_GUIDE_V2.md
- Bubbletea v2: https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md
- Bubbles v2: https://github.com/charmbracelet/bubbles/blob/main/UPGRADE_GUIDE_V2.md

## Reference: Key API Changes Summary

### Lipgloss v2
- Import: `github.com/charmbracelet/lipgloss` -> `charm.land/lipgloss/v2`
- `lipgloss.Color("#hex")` is now a function returning `color.Color`, not a string type -- but call sites look the same
- Color variables typed as `lipgloss.Color` must become `color.Color` (from `image/color`)
- No more Renderer -- `Style` is a pure value type
- `WithWhitespaceBackground(c)` -> `WithWhitespaceStyle(lipgloss.NewStyle().Background(c))`
- New: `Canvas`, `Compositor`, `Layer` for cell-based compositing

### Bubbletea v2
- Import: `github.com/charmbracelet/bubbletea` -> `charm.land/bubbletea/v2`
- `View() string` -> `View() tea.View` (use `tea.NewView(content)`)
- `tea.KeyMsg` (struct) -> `tea.KeyPressMsg` for key presses
- `msg.Type` -> `msg.Code`, `msg.Runes` -> `msg.Text`, `msg.Alt` -> `msg.Mod`
- `tea.KeyRune` -> check `len(msg.Text) > 0`
- `tea.KeyCtrlC` etc. -> `msg.String() == "ctrl+c"` or `msg.Code == 'c' && msg.Mod == tea.ModCtrl`
- `case " ":` -> `case "space":`
- `tea.WithAltScreen()` -> `view.AltScreen = true` in View()
- `tea.WithInputTTY()` -> removed (automatic)
- `tea.WindowSizeMsg` stays the same
- `tea.NewProgram(model)` -- remove options that moved to View

### Bubbles v2
- Import: `github.com/charmbracelet/bubbles/*` -> `charm.land/bubbles/v2/*`
- `viewport.New(w, h)` -> `viewport.New(viewport.WithWidth(w), viewport.WithHeight(h))`
- `vp.Width = w` -> `vp.SetWidth(w)`, `vp.Width` field -> `vp.Width()` method
- `vp.Height = h` -> `vp.SetHeight(h)`, `vp.Height` field -> `vp.Height()` method
- `vp.YOffset` field -> `vp.YOffset()` method, `vp.SetYOffset(n)`
- `vp.TotalLineCount()` -- check if this still exists or renamed
- `textarea.Model` styles: `ta.FocusedStyle` -> `ta.Styles.Focused`, `ta.BlurredStyle` -> `ta.Styles.Blurred`
- `textarea.Style` type -> `textarea.StyleState` type
- `ta.SetCursor(col)` -> `ta.SetCursorColumn(col)`
- `key.Binding`, `key.NewBinding`, `key.Matches` -- unchanged
- `viewport.KeyMap{}` empty struct -- should still work for disabling keys

### Huh v2
- Import: `github.com/charmbracelet/huh` -> `charm.land/huh/v2`
- Import: `github.com/charmbracelet/huh/spinner` -> `charm.land/huh/v2/spinner`

---

### Task 1: Upgrade go.mod Dependencies

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Remove old dependencies and add new ones**

```bash
# Remove old v1 dependencies
go get github.com/charmbracelet/lipgloss@none
go get github.com/charmbracelet/bubbletea@none
go get github.com/charmbracelet/bubbles@none
go get github.com/charmbracelet/huh@none

# Add new v2 dependencies
go get charm.land/lipgloss/v2@latest
go get charm.land/bubbletea/v2@latest
go get charm.land/bubbles/v2@latest
go get charm.land/huh/v2@latest
```

- [ ] **Step 2: Verify go.mod looks correct**

```bash
grep -E "charm.land|charmbracelet" go.mod
```

Expected: Only `charm.land/*` v2 imports remain as direct dependencies. Old `github.com/charmbracelet/{lipgloss,bubbletea,bubbles,huh}` should be gone from the `require` block (they may still appear as indirect if some other dep needs them).

Note: This step will break the build since import paths in source haven't been updated yet. That's expected.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: upgrade to lipgloss/bubbletea/bubbles/huh v2 in go.mod"
```

---

### Task 2: Fix All Import Paths

**Files:**
- Modify: Every `.go` file that imports charmbracelet packages

This is a mechanical find-and-replace across all Go files.

- [ ] **Step 1: Replace lipgloss import path**

Search and replace in all `.go` files:
```
"github.com/charmbracelet/lipgloss" -> "charm.land/lipgloss/v2"
```

- [ ] **Step 2: Replace bubbletea import path**

```
"github.com/charmbracelet/bubbletea" -> "charm.land/bubbletea/v2"
```

- [ ] **Step 3: Replace bubbles import paths**

```
"github.com/charmbracelet/bubbles/viewport" -> "charm.land/bubbles/v2/viewport"
"github.com/charmbracelet/bubbles/textarea" -> "charm.land/bubbles/v2/textarea"
"github.com/charmbracelet/bubbles/key"      -> "charm.land/bubbles/v2/key"
```

- [ ] **Step 4: Replace huh import paths**

```
"github.com/charmbracelet/huh/spinner" -> "charm.land/huh/v2/spinner"
"github.com/charmbracelet/huh"         -> "charm.land/huh/v2"
```

- [ ] **Step 5: Run go mod tidy to clean up**

```bash
go mod tidy
```

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "chore: update all import paths to charm.land v2"
```

---

### Task 3: Fix Lipgloss Color Type Changes in styles.go

**Files:**
- Modify: `internal/ui/styles/styles.go`

In lipgloss v2, `lipgloss.Color("#hex")` is a function that returns `color.Color` (from `image/color`), not a `lipgloss.Color` string type. Our package-level color variables are currently typed implicitly. Since we assign them with `lipgloss.Color(...)` and reassign in `Apply()`, we need to ensure the variable types are `color.Color`.

- [ ] **Step 1: Add `image/color` import**

Add to the import block in `styles.go`:
```go
"image/color"
```

- [ ] **Step 2: Change color variable declarations to use `color.Color` type**

Change the `var` block from implicit typing to explicit `color.Color`:
```go
var (
	// Colors
	Primary     color.Color = lipgloss.Color("#4A9EFF")
	Secondary   color.Color = lipgloss.Color("#666666")
	Accent      color.Color = lipgloss.Color("#50C878")
	Warning     color.Color = lipgloss.Color("#E0A030")
	Error       color.Color = lipgloss.Color("#E04040")
	Background  color.Color = lipgloss.Color("#1A1A2E")
	Surface     color.Color = lipgloss.Color("#16162B")
	SurfaceDark color.Color = lipgloss.Color("#0F0F23")
	TextPrimary color.Color = lipgloss.Color("#E0E0E0")
	TextMuted   color.Color = lipgloss.Color("#888888")
	Border      color.Color = lipgloss.Color("#333333")
```

- [ ] **Step 3: Verify the Apply() function still compiles**

The `Apply()` function reassigns these variables with `lipgloss.Color(...)` which now returns `color.Color`. Since our variables are now `color.Color`, the assignments work.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/styles/styles.go
git commit -m "fix: use color.Color type for style color variables (lipgloss v2)"
```

---

### Task 4: Fix Bubbletea v2 Changes in app.go

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `cmd/slk/main.go`

The biggest bubbletea v2 changes: `View()` returns `tea.View`, `tea.KeyMsg` becomes `tea.KeyPressMsg`, program options move to View fields.

- [ ] **Step 1: Change View() signature in app.go**

Change:
```go
func (a *App) View() string {
```
To:
```go
func (a *App) View() tea.View {
```

At the end of View(), wrap the return with `tea.NewView()` and set `AltScreen`:
```go
	v := tea.NewView(screen)  // where `screen` is the final rendered string
	v.AltScreen = true
	return v
```

The current View() returns `lipgloss.Place(...)`. Change the final return to capture that string, then wrap it.

- [ ] **Step 2: Change tea.KeyMsg to tea.KeyPressMsg in app.go**

Find all `case tea.KeyMsg:` and change to `case tea.KeyPressMsg:`.

Find all references to `msg.Type` and change to `msg.Code`.
Find all references to `msg.Runes` and change to `msg.Text` (note: `msg.Text` is now a string, not `[]rune`).

Key constant changes:
- `tea.KeyEnter` -> `tea.KeyEnter` (check if still exists, or use `msg.Code == tea.KeyEnter`)
- `tea.KeyEsc` -> `tea.KeyEscape` (verify name)
- `tea.KeyCtrlC` -> use `msg.String() == "ctrl+c"`
- `tea.KeyCtrlB` -> use `msg.String() == "ctrl+b"` or `msg.Code == 'b' && msg.Mod == tea.ModCtrl`
- `tea.KeyTab` -> `tea.KeyTab`
- `tea.KeyRunes` -> check `len(msg.Text) > 0`
- `case " ":` -> `case "space":` (when matching `msg.String()`)

For msg.Type comparisons like:
```go
if msg.Type == tea.KeyRunes {
    switch string(msg.Runes) {
```
Change to:
```go
if len(msg.Text) > 0 {
    switch msg.Text {
```

For key type constants:
```go
case tea.KeyUp: -> check if msg.Code == tea.KeyUp
case tea.KeyDown: -> check if msg.Code == tea.KeyDown
```

**Important:** The exact v2 key constant names need to be verified. The upgrade guide says `msg.Type` -> `msg.Code` which is a `rune`. Special keys like Enter, Escape, etc. are rune constants like `tea.KeyEnter`, `tea.KeyEscape`, etc.

- [ ] **Step 3: Update main.go**

Change:
```go
p := tea.NewProgram(app, tea.WithAltScreen())
```
To:
```go
p := tea.NewProgram(app)
```

(AltScreen is now declared in View())

Remove `tea.WithInputTTY()` if present (automatic in v2).

- [ ] **Step 4: Update compose/model.go Update() method**

The compose model also handles `tea.KeyMsg`. Change:
```go
case tea.KeyMsg:
```
To:
```go
case tea.KeyPressMsg:
```

Update any `msg.Type`, `msg.Runes` references similarly.

- [ ] **Step 5: Try to build and note remaining compile errors**

```bash
go build ./...
```

Fix any remaining `tea.KeyMsg`-related compile errors across the codebase.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "fix: migrate to bubbletea v2 API (View, KeyPressMsg, declarative options)"
```

---

### Task 5: Fix Bubbles v2 Changes -- Viewport

**Files:**
- Modify: `internal/ui/messages/model.go`
- Modify: `internal/ui/sidebar/model.go`
- Modify: `internal/ui/thread/model.go`

All three use `viewport.Model` with direct field access.

- [ ] **Step 1: Fix viewport field access in messages/model.go**

Change all occurrences:
```go
m.vp.Width = width       ->  m.vp.SetWidth(width)
m.vp.Height = msgAreaHeight  ->  m.vp.SetHeight(msgAreaHeight)
m.vp.KeyMap = viewport.KeyMap{}  ->  // Check if this pattern still works for disabling keys. If not, we need to explicitly unbind all keys.
m.vp.YOffset             ->  m.vp.YOffset()
m.vp.SetYOffset(n)       ->  m.vp.SetYOffset(n)  (unchanged)
m.vp.TotalLineCount()    ->  verify method name still exists
m.vp.SetContent(s)       ->  m.vp.SetContent(s)  (unchanged)
m.vp.View()              ->  m.vp.View()  (unchanged, but View() now returns tea.View -- need to extract string)
```

**Critical:** In bubbles v2, `vp.View()` likely returns `tea.View` instead of `string`. We need to call `vp.View().Content` or similar to get the string. Check the API.

Actually, looking at the bubbles v2 guide more carefully -- sub-components like viewport may still return string from View() since they're not top-level tea.Model implementations. Need to verify.

If viewport.View() returns tea.View, extract the content string with `.String()` or check API.

- [ ] **Step 2: Fix viewport in sidebar/model.go**

Same pattern as messages. Change:
```go
m.vp.Width = width    ->  m.vp.SetWidth(width)
m.vp.Height = height  ->  m.vp.SetHeight(height)
m.vp.KeyMap = viewport.KeyMap{}  ->  (same approach)
m.vp.YOffset  ->  m.vp.YOffset()
```

- [ ] **Step 3: Fix viewport in thread/model.go**

Same pattern.

- [ ] **Step 4: Fix viewport initialization**

If any file creates viewport with `viewport.New(w, h)`, change to:
```go
viewport.New(viewport.WithWidth(w), viewport.WithHeight(h))
```

Or if created with zero values (`viewport.Model{}`), that should still work with the new API -- just use SetWidth/SetHeight later.

- [ ] **Step 5: Build and fix remaining viewport errors**

```bash
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "fix: migrate viewport usage to bubbles v2 getter/setter API"
```

---

### Task 6: Fix Bubbles v2 Changes -- Textarea

**Files:**
- Modify: `internal/ui/compose/model.go`

- [ ] **Step 1: Fix textarea style access**

Change:
```go
ta.FocusedStyle.Base = bg
ta.FocusedStyle.Text = bg
ta.FocusedStyle.CursorLine = bg
ta.FocusedStyle.EndOfBuffer = bg
ta.FocusedStyle.Prompt = bg
ta.BlurredStyle.Base = bg
ta.BlurredStyle.Text = bg
ta.BlurredStyle.CursorLine = bg
```
To:
```go
ta.Styles.Focused.Base = bg
ta.Styles.Focused.Text = bg
ta.Styles.Focused.CursorLine = bg
ta.Styles.Focused.EndOfBuffer = bg
ta.Styles.Focused.Prompt = bg
ta.Styles.Blurred.Base = bg
ta.Styles.Blurred.Text = bg
ta.Styles.Blurred.CursorLine = bg
```

Note: `EndOfBuffer` may have been renamed or removed in v2. Check the `textarea.StyleState` type.

- [ ] **Step 2: Fix textarea width if using direct field**

If `m.input.Width` is accessed as a field, it may need to change to `m.input.Width()` (getter) and `m.input.SetWidth()` (setter). Check current usage.

- [ ] **Step 3: Fix SetCursor -> SetCursorColumn if used**

```go
ta.SetCursor(col) -> ta.SetCursorColumn(col)
```

- [ ] **Step 4: Build and fix remaining textarea errors**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "fix: migrate textarea usage to bubbles v2 style/API changes"
```

---

### Task 7: Fix Whitespace Options and Overlays

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/channelfinder/model.go`
- Modify: `internal/ui/workspacefinder/model.go`
- Modify: `internal/ui/themeswitcher/model.go`
- Modify: `internal/ui/reactionpicker/model.go`

- [ ] **Step 1: Fix WithWhitespaceBackground in app.go**

Change:
```go
lipgloss.Place(a.width, a.height,
    lipgloss.Left, lipgloss.Top,
    screen,
    lipgloss.WithWhitespaceBackground(styles.Background),
)
```
To:
```go
lipgloss.Place(a.width, a.height,
    lipgloss.Left, lipgloss.Top,
    screen,
    lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(styles.Background)),
)
```

Also fix the loading overlay:
```go
lipgloss.WithWhitespaceBackground(lipgloss.Color("#0F0F1A"))
```
To:
```go
lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(lipgloss.Color("#0F0F1A")))
```

- [ ] **Step 2: Fix WithWhitespaceBackground in overlay files**

In `channelfinder/model.go`, `workspacefinder/model.go`, `themeswitcher/model.go`, `reactionpicker/model.go`:

Change:
```go
lipgloss.WithWhitespaceBackground(lipgloss.Color("#0F0F1A"))
```
To:
```go
lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(lipgloss.Color("#0F0F1A")))
```

- [ ] **Step 3: Build and verify**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "fix: replace WithWhitespaceBackground with WithWhitespaceStyle (lipgloss v2)"
```

---

### Task 8: Fix Huh v2 Changes in Onboarding

**Files:**
- Modify: `cmd/slk/onboarding.go`

- [ ] **Step 1: Update huh API calls if needed**

The huh v2 API may have changed. Check if the `huh.NewForm`, `huh.NewGroup`, `huh.NewInput`, etc. patterns still work. The import path was already changed in Task 2.

If huh v2 has the same API surface (likely just import path change), no code changes needed beyond the import.

If there are compile errors, fix them based on the huh v2 API.

- [ ] **Step 2: Build onboarding**

```bash
go build ./cmd/slk/...
```

- [ ] **Step 3: Commit if changes were needed**

```bash
git add -A
git commit -m "fix: update huh usage for v2 compatibility"
```

---

### Task 9: Get the Build Compiling -- Fix All Remaining Compile Errors

**Files:**
- Potentially any `.go` file

- [ ] **Step 1: Attempt full build**

```bash
go build ./...
```

- [ ] **Step 2: Fix any remaining compile errors**

Common remaining issues:
- `lipgloss.Color` used as a type (not function call) -- e.g. `lipgloss.Color("#fff")` as a function arg is fine, but `var c lipgloss.Color = "#fff"` won't compile
- Any `lipgloss.TerminalColor` references -> `color.Color`
- viewport.View() return type changes
- textarea API changes not caught earlier
- Key constant name changes in bubbletea v2

Fix each error, tracking which files needed changes.

- [ ] **Step 3: Run tests**

```bash
go test ./...
```

Fix any test compilation errors. Tests may reference the old APIs too.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "fix: resolve all remaining v2 migration compile errors"
```

---

### Task 10: Add Compositor Background Layer in app.go

**Files:**
- Modify: `internal/ui/app.go`

This is the key architectural improvement. Instead of the `lipgloss.Place(WithWhitespaceBackground)` approach at the end of View(), use lipgloss v2's compositor to render a background layer.

- [ ] **Step 1: Replace the final screen placement with compositor**

The current pattern at the end of View() is:
```go
return lipgloss.Place(a.width, a.height,
    lipgloss.Left, lipgloss.Top,
    screen,
    lipgloss.WithWhitespaceBackground(styles.Background),
)
```

Replace with compositor:
```go
// Create a background fill layer
bgFill := lipgloss.NewStyle().
    Width(a.width).
    Height(a.height).
    Background(styles.Background).
    Render("")

// Layer the screen content on top of the background
bgLayer := lipgloss.NewLayer(bgFill)
contentLayer := lipgloss.NewLayer(screen).Z(1)

comp := lipgloss.NewCompositor(bgLayer, contentLayer)
return comp.Render()
```

The compositor will fill all uncolored cells from the background layer, which is exactly what `WithWhitespaceBackground` was doing but more powerful.

**Note:** The exact compositor API may need adjustment based on testing. The key is:
- Layer 0 (Z=0): solid background fill
- Layer 1 (Z=1): the actual screen content
- Compositor composites them, with content cells overriding background cells

If overlays (channel finder, etc.) are rendered on top of the screen, they become additional layers or are already composited into the `screen` string.

- [ ] **Step 2: Test visually**

```bash
go build ./cmd/slk && ./bin/slk
```

Check that:
- Dark theme looks correct (backgrounds fill properly)
- Switch to a light theme with Ctrl+y and verify no dark patches appear

- [ ] **Step 3: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat: use lipgloss v2 compositor for background layer"
```

---

### Task 11: Remove Hacky Background Fills from styles.go

**Files:**
- Modify: `internal/ui/styles/styles.go`

With the compositor handling background fills, the `.Background(styles.Background)` on individual text styles is no longer needed. Styles that use a *different* background (like `SurfaceDark` for compose/statusbar, or `Primary` for active workspace) should keep their backgrounds.

- [ ] **Step 1: Remove `.Background(Background)` from text/layout styles in buildStyles()**

Remove `.Background(Background)` from these styles (they should inherit from the compositor background):
- `FocusedBorder` -- remove `.Background(Background)`
- `UnfocusedBorder` -- remove `.Background(Background)`
- `ChannelSelected` -- remove `.Background(Background)`
- `ChannelNormal` -- remove `.Background(Background)`
- `ChannelUnread` -- remove `.Background(Background)`
- `SectionHeader` -- remove `.Background(Background)`
- `Username` -- remove `.Background(Background)`
- `Timestamp` -- remove `.Background(Background)`
- `MessageText` -- remove `.Background(Background)`
- `ThreadIndicator` -- remove `.Background(Background)`
- `PresenceOnline` -- remove `.Background(Background)`
- `PresenceAway` -- remove `.Background(Background)`
- `DateSeparator` -- remove `.Background(Background)`
- `NewMessageSeparator` -- remove `.Background(Background)`
- `TypingIndicator` -- remove `.Background(Background)`

**Keep** `.Background(...)` on these (they intentionally use different backgrounds):
- `WorkspaceActive` -- `.Background(Primary)` -- keep
- `WorkspaceInactive` -- `.Background(Surface)` -- keep
- `StatusBar` -- `.Background(SurfaceDark)` -- keep
- `StatusMode*` -- various backgrounds -- keep
- `ComposeBox/ComposeFocused/ComposeInsert` -- `.Background(SurfaceDark)` -- keep
- `ReactionPill*` -- `.Background(Surface)` -- keep
- `UnreadBadge` -- `.Background(Error)` -- keep

- [ ] **Step 2: Also remove from the initial var declarations (top of file)**

The initial `var` block also has these styles defined. Remove `.Background(Background)` from the same styles there too (those are the default values before `Apply()` is called).

Note: Some initial declarations don't have `.Background()` at all -- those are fine as-is.

- [ ] **Step 3: Build and test visually**

```bash
go build ./cmd/slk && ./bin/slk
```

Verify dark and light themes still look correct with the compositor handling background fills.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/styles/styles.go
git commit -m "refactor: remove redundant .Background() from styles (compositor handles it)"
```

---

### Task 12: Remove Hacky Background Fills from messages/model.go

**Files:**
- Modify: `internal/ui/messages/model.go`

This file has the most workarounds. Remove them one by one.

- [ ] **Step 1: Simplify date separators**

The current pattern manually pads with spaces to fill width:
```go
sep := lipgloss.NewStyle().Background(styles.Background).Foreground(styles.TextMuted).Bold(true).
    Render(strings.Repeat(" ", padLeft) + sepStr + strings.Repeat(" ", padRight))
```

Replace with:
```go
sep := lipgloss.NewStyle().Foreground(styles.TextMuted).Bold(true).
    Width(width).Align(lipgloss.Center).
    Render(sepStr)
```

Same for the "new messages" separator -- replace manual space padding with `.Width(width).Align(lipgloss.Center)`.

- [ ] **Step 2: Simplify avatar spacing**

Change:
```go
left = avatarLines[i] + lipgloss.NewStyle().Background(styles.Background).Render(" ")
```
To:
```go
left = avatarLines[i] + " "
```

Change the empty avatar placeholder:
```go
left = lipgloss.NewStyle().Background(styles.Background).Width(avatarWidth).Render("")
```
To:
```go
left = lipgloss.NewStyle().Width(avatarWidth).Render("")
```

- [ ] **Step 3: Simplify applyLeftBorder and applySelection**

Change:
```go
func applyLeftBorder(content string, width int) string {
    filled := lipgloss.NewStyle().
        Width(width - 1).
        Background(styles.Background).
        Render(content)
    return lipgloss.NewStyle().
        BorderStyle(thickLeftBorder).
        BorderLeft(true).
        BorderForeground(styles.Background).
        Render(filled)
}
```
To:
```go
func applyLeftBorder(content string, width int) string {
    filled := lipgloss.NewStyle().
        Width(width - 1).
        Render(content)
    return lipgloss.NewStyle().
        BorderStyle(thickLeftBorder).
        BorderLeft(true).
        BorderForeground(styles.Background).
        Render(filled)
}
```

Same for `applySelection` -- remove `.Background(styles.Background)` from the inner fill.

- [ ] **Step 4: Simplify spacer()**

Change:
```go
func spacer(width int) string {
    return lipgloss.NewStyle().
        Background(styles.Background).
        Render(strings.Repeat(" ", width))
}
```
To:
```go
func spacer(width int) string {
    return lipgloss.NewStyle().
        Width(width).
        Render("")
}
```

Or even simpler -- just use `strings.Repeat(" ", width)` if the compositor fills background.

- [ ] **Step 5: Simplify separator line**

Change:
```go
separator := lipgloss.NewStyle().Width(width).Foreground(styles.Border).Background(styles.Background).Render(strings.Repeat("-", width))
```
To:
```go
separator := lipgloss.NewStyle().Width(width).Foreground(styles.Border).Render(strings.Repeat("-", width))
```

- [ ] **Step 6: Simplify scroll indicators**

Remove `.Background(styles.Background)` from scroll indicator styles.

- [ ] **Step 7: Remove `.Background(styles.Background)` from final viewport wrapper**

The View() method wraps `vp.View()` in a style with `.Background(styles.Background)` -- remove that.

- [ ] **Step 8: Build and test visually**

```bash
go build ./cmd/slk && ./bin/slk
```

- [ ] **Step 9: Commit**

```bash
git add internal/ui/messages/model.go
git commit -m "refactor: remove background fill hacks from messages pane (compositor handles it)"
```

---

### Task 13: Remove Hacky Background Fills from thread/model.go

**Files:**
- Modify: `internal/ui/thread/model.go`

- [ ] **Step 1: Simplify applyLeftBorder and applySelection**

Same changes as messages/model.go -- remove `.Background(styles.Background)` from the inner fill style.

- [ ] **Step 2: Remove any other `.Background(styles.Background)` on text styles**

Search the file for `.Background(styles.Background)` and remove from text styling (keep on styles that intentionally use a different background).

- [ ] **Step 3: Build and commit**

```bash
go build ./cmd/slk && git add internal/ui/thread/model.go && git commit -m "refactor: remove background fill hacks from thread panel"
```

---

### Task 14: Remove Hacky Background Fills from sidebar/model.go

**Files:**
- Modify: `internal/ui/sidebar/model.go`

- [ ] **Step 1: Remove inline `.Background(styles.Background)` on glyphs**

Change:
```go
cursor = lipgloss.NewStyle().Foreground(styles.Accent).Background(styles.Background).Render("▌")
```
To:
```go
cursor = lipgloss.NewStyle().Foreground(styles.Accent).Render("▌")
```

Same for unread dot, channel prefix, etc. -- remove `.Background(styles.Background)`.

- [ ] **Step 2: Remove `.Background(styles.Background)` from final viewport wrapper**

- [ ] **Step 3: Build and commit**

```bash
go build ./cmd/slk && git add internal/ui/sidebar/model.go && git commit -m "refactor: remove background fill hacks from sidebar"
```

---

### Task 15: Remove Hacky Background Fills from statusbar/model.go

**Files:**
- Modify: `internal/ui/statusbar/model.go`

- [ ] **Step 1: Remove inline `.Background(styles.SurfaceDark)` on connection indicators**

The status bar legitimately uses `SurfaceDark` as its background. These inline `.Background(styles.SurfaceDark)` calls on connection indicator text should be handled by the parent StatusBar style. If the text is rendered inside the StatusBar style, the background is inherited. Remove the inline backgrounds.

Change:
```go
lipgloss.NewStyle().Foreground(styles.Accent).Background(styles.SurfaceDark).Render("● Connected")
```
To:
```go
lipgloss.NewStyle().Foreground(styles.Accent).Render("● Connected")
```

**Caveat:** Test this carefully. If the StatusBar style wraps the whole bar with Background, then the individual items inherit it through the `lipgloss.JoinHorizontal`. If not, we may need to keep these.

- [ ] **Step 2: Simplify the gap filler**

The gap filler creates spaces with StatusBar style to fill the gap. This should still work since StatusBar has its own background. No change needed here.

- [ ] **Step 3: Build and test visually**

```bash
go build ./cmd/slk && ./bin/slk
```

- [ ] **Step 4: Commit**

```bash
git add internal/ui/statusbar/model.go
git commit -m "refactor: simplify statusbar background styling"
```

---

### Task 16: Remove Hacky Background Fills from compose/model.go

**Files:**
- Modify: `internal/ui/compose/model.go`

- [ ] **Step 1: Simplify textarea style overrides**

The compose box intentionally uses SurfaceDark background, which is different from the main background. The textarea style overrides may still be needed to ensure the textarea content has the correct dark background within the compose box.

Review and simplify where possible, but keep the `SurfaceDark` background on compose styles since it's intentionally different.

- [ ] **Step 2: Simplify placeholder rendering**

Remove `.Background(styles.SurfaceDark)` if the parent ComposeBox style already provides it:
```go
placeholder := lipgloss.NewStyle().
    Foreground(styles.TextMuted).
    Width(innerWidth).
    Render(m.input.Placeholder)
```

- [ ] **Step 3: Simplify textarea wrapper**

The full-width wrapper may still be needed for the compose box since it has a different background:
```go
content := lipgloss.NewStyle().
    Background(styles.SurfaceDark).
    Width(innerWidth).
    Render(m.input.View())
```

Keep the `.Background(styles.SurfaceDark)` here since it's intentionally different from the compositor background.

- [ ] **Step 4: Build and commit**

```bash
go build ./cmd/slk && git add internal/ui/compose/model.go && git commit -m "refactor: simplify compose box background styling"
```

---

### Task 17: Remove Hacky Background from app.go Helpers

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Simplify exactHeight helper**

Change:
```go
exactHeight := func(s string, h int) string {
    return lipgloss.NewStyle().Width(lipgloss.Width(s)).Height(h).MaxHeight(h).Background(styles.Background).Render(s)
}
```
To:
```go
exactHeight := func(s string, h int) string {
    return lipgloss.NewStyle().Width(lipgloss.Width(s)).Height(h).MaxHeight(h).Render(s)
}
```

- [ ] **Step 2: Simplify loading overlay backdrop**

Change the hardcoded `#0F0F1A` to use the theme's SurfaceDark color:
```go
lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(styles.SurfaceDark))
```

- [ ] **Step 3: Build and commit**

```bash
go build ./cmd/slk && git add internal/ui/app.go && git commit -m "refactor: remove background fill hacks from app.go layout helpers"
```

---

### Task 18: Fix Test Files

**Files:**
- Modify: All `*_test.go` files under `internal/`

- [ ] **Step 1: Run tests and identify failures**

```bash
go test ./... 2>&1
```

- [ ] **Step 2: Fix import paths in test files**

The import path replacements from Task 2 should have already covered test files. If any were missed, fix them.

- [ ] **Step 3: Fix any API changes in tests**

Tests may reference:
- `tea.KeyMsg` -> `tea.KeyPressMsg`
- `viewport.New(w, h)` -> `viewport.New(viewport.WithWidth(w), viewport.WithHeight(h))`
- `vp.Width` field -> `vp.Width()` method
- Direct style comparisons that might break with color type changes

- [ ] **Step 4: Run tests and verify all pass**

```bash
go test ./...
```

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "fix: update test files for v2 API changes"
```

---

### Task 19: Final Verification

- [ ] **Step 1: Clean build**

```bash
go clean -cache && go build ./...
```

- [ ] **Step 2: Run all tests**

```bash
go test ./...
```

- [ ] **Step 3: Run the application and test visually**

```bash
./bin/slk
```

Test:
- Dark theme: all backgrounds consistent, no default-terminal-color patches
- Light theme (Ctrl+y): no dark patches, backgrounds fill properly
- All overlays: channel finder (Ctrl+t), workspace finder (Ctrl+w), theme switcher (Ctrl+y), reaction picker (r)
- Thread panel: Enter on a message, verify thread renders correctly
- Compose: i to enter insert mode, type a message, verify compose box styling
- Scrolling: j/k through messages, verify no rendering artifacts

- [ ] **Step 4: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: final v2 migration polish"
```
