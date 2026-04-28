# Themes Expansion & Per-Workspace Theme Assignment — Design

## Problem

Two related gaps in the theme system:

1. **Theme variety.** The current 21 built-in themes are weighted toward dark variants. Several popular communities (Catppuccin family, GitHub Light, Cobalt2, Iceberg, Cyberpunk Neon, etc.) are missing. Users with multiple workspaces want more options.

2. **One theme for everything.** The theme is currently global. Users with multiple Slack workspaces (work + personal, multiple clients, etc.) often want a different visual identity per workspace as a fast visual cue for which one is active. Today they have to manually re-switch the theme on every workspace change.

## Solution

Two coordinated additions to the existing theme system, sharing the same `ThemeColors` struct and `styles.Apply` plumbing:

**A. Twelve new built-in themes** (purely additive — only `internal/ui/styles/themes.go` changes).

**B. Per-workspace theme assignment** — workspaces can set their own theme; the UI re-themes on workspace switch; the existing Ctrl+Y picker scopes to the active workspace; a new Ctrl+Shift+Y picker sets the global default.

## Architecture

### A. New themes

Twelve entries added to `builtinThemes` in `internal/ui/styles/themes.go`. Each defines all 14 `ThemeColors` fields (the four optional Sidebar*/Rail fields are explicitly set so light themes get appropriately-toned sidebars).

| Theme | Background | Notes |
|------|-----------|-------|
| Catppuccin Latte | `#eff1f5` | Soft pastels on warm cream; blue accent `#1e66f5`. Dark sidebar (Mocha base `#1e1e2e`) for contrast. |
| GitHub Light | `#ffffff` | Clean professional; blue `#0969da`. Gray sidebar `#24292f`. |
| Tokyo Night Light | `#d5d6db` | Light TN variant; blue `#34548a`. Dark sidebar `#1a1b26`. |
| Atom One Light | `#fafafa` | Clean Atom-style; blue `#4078f2`. Dark sidebar `#282c34`. |
| Catppuccin Frappé | `#303446` | Medium-dark; blue `#8caaee`. |
| Catppuccin Macchiato | `#24273a` | Warmer dark; blue `#8aadf4`. |
| Tokyo Night Storm | `#24283b` | Bluer Tokyo Night; `#7aa2f7`. |
| Cobalt2 | `#193549` | Wes Bos signature; orange `#ffc600`. |
| Iceberg | `#161821` | Cool blue Vim theme; `#84a0c6`. |
| Oceanic Next | `#1b2b34` | Sublime/Atom popular; teal `#6699cc`. |
| Cyberpunk Neon | `#000b1e` | Neon vibes; cyan `#0abdc6`, pink `#ea00d9`. |
| Material Palenight | `#292d3e` | Popular VS Code theme; `#82aaff`. |

Existing custom-theme loading, `lookupTheme`, and the switcher UX cover the new themes for free. The `ThemeNames()` function returns them automatically (deduplicated, sorted).

### B. Per-workspace theme

Three coordinated changes:

**1. Config schema.** `~/.config/slk/config.toml` gains an optional `[workspaces.<TeamID>]` map keyed by Slack TeamID. Example:

```toml
[appearance]
theme = "dark"            # global default

[theme]                   # global color overrides (existing)
primary = "#FF0000"

# ACME Corp
[workspaces.T01ABCDEF]
theme = "dracula"

# Personal
[workspaces.T02XYZ123]
theme = "github light"
```

The TeamID is the canonical identifier (already keys the token files, SQLite workspaces table, and runtime context map). Workspace names appear as TOML comments above each section for human readability — written by the saver when it creates a new section.

**Why TeamID over workspace name as the key:** workspace names can be renamed by admins (breaking the mapping), can collide between unrelated workspaces, and need TOML escaping for special characters.

**Helper for manual editing:** add `slk --list-workspaces` CLI subcommand that prints `<TeamID>  <Name>  <Domain>` so users wanting to hand-edit `config.toml` have an easy lookup.

**2. Resolution.** A new method on `Config`:

```go
// ResolveTheme returns the theme name to use for the given workspace,
// falling back to the global default when no per-workspace theme is set.
func (c *Config) ResolveTheme(teamID string) string {
    if ws, ok := c.Workspaces[teamID]; ok && ws.Theme != "" {
        return ws.Theme
    }
    if c.Appearance.Theme != "" {
        return c.Appearance.Theme
    }
    return "dark"
}
```

**3. Switching.** The workspace-switch callback in `cmd/slk/main.go` calls `styles.Apply(cfg.ResolveTheme(newTeamID), cfg.Theme)`. The existing `styles.Version()` counter causes all UI components to invalidate caches and re-render automatically — no per-component changes required.

## Components

### Config (`internal/config/config.go`)

```go
type WorkspaceSettings struct {
    Theme string `toml:"theme"`
}

type Config struct {
    // ... existing fields ...
    Workspaces map[string]WorkspaceSettings `toml:"workspaces"`
}
```

Plus `ResolveTheme(teamID string) string` (above).

### Saver functions (`cmd/slk/main.go`)

Two functions, parallel to the existing textual config rewriter:

- `saveWorkspaceTheme(teamID, name, themeName string) error` — writes/updates `[workspaces.<teamID>] theme = "..."`. If the section is missing, append it preceded by `# <name>` comment.
- `saveGlobalTheme(themeName string) error` — writes/updates `[appearance] theme = "..."`. Renamed from the existing single saver, behavior unchanged.

Textual rewrite (not full TOML re-marshal) preserves user comments and ordering.

### Theme picker (`internal/ui/themeswitcher/model.go`)

Add fields:

```go
type ThemeScope int

const (
    ScopeWorkspace ThemeScope = iota
    ScopeGlobal
)

type Model struct {
    // ... existing fields ...
    scope      ThemeScope
    headerText string
}
```

The header text is set by the caller when opening the picker:
- `"Theme for <Workspace Name>"` for workspace scope (Ctrl+Y).
- `"Default theme for new workspaces"` for global scope (Ctrl+Shift+Y).

Returned `ThemeResult` includes the scope; the app dispatches to the matching saver.

### App-level wiring (`internal/ui/app.go`, `internal/ui/keys.go`, `cmd/slk/main.go`)

- Add `KeyThemePickerGlobal` keybind (Ctrl+Shift+Y) alongside the existing `KeyThemePicker` (Ctrl+Y).
- Both handlers open the same picker model with their respective scope and header text.
- The save callback is parameterized:

```go
type ThemeSaver func(themeName string, scope ThemeScope, teamID string) error
```

`cmd/slk/main.go` provides the implementation, routing to `saveWorkspaceTheme` or `saveGlobalTheme`.

### Workspace-switch callback (`cmd/slk/main.go`)

The existing per-switch hook (`wireCallbacks` / `app.SetWorkspaceSwitcher` callback) gains one line: `styles.Apply(cfg.ResolveTheme(newTeamID), cfg.Theme)` after the workspace becomes active.

### CLI: `slk --list-workspaces`

Prints workspaces from the token store with their TeamID, Name, and Domain. ~20 lines in `cmd/slk/main.go`. Useful for users hand-editing `config.toml` and as a general inspection tool.

## Data Flow

**Startup (active workspace = T01ABCDEF):**
1. Load config (`Config.Workspaces[T01ABCDEF].Theme = "dracula"`).
2. `styles.Apply(cfg.ResolveTheme("T01ABCDEF"), cfg.Theme)` → "dracula".
3. UI renders with Dracula colors.

**Workspace switch (T01 → T02, T02 has no per-workspace theme):**
1. Workspace context becomes T02.
2. Switch callback runs: `cfg.ResolveTheme("T02XYZ123")` → falls through to `cfg.Appearance.Theme` → "dark".
3. `styles.Apply("dark", cfg.Theme)`. Version bumps. All components re-render.

**Theme picker (Ctrl+Y, T02 active):**
1. Picker opens with header "Theme for Personal" (T02's name).
2. User picks "github light".
3. Saver:
   a. `styles.Apply("github light", cfg.Theme)` immediately.
   b. `cfg.Workspaces["T02XYZ123"] = WorkspaceSettings{Theme: "github light"}`.
   c. `saveWorkspaceTheme("T02XYZ123", "Personal", "github light")` writes to disk.

**Theme picker (Ctrl+Shift+Y, T02 active, T02 has no per-workspace theme):**
1. Picker opens with header "Default theme for new workspaces".
2. User picks "tokyo night".
3. Saver:
   a. `cfg.Appearance.Theme = "tokyo night"`.
   b. Because T02 has no per-workspace theme, `styles.Apply("tokyo night", cfg.Theme)` is also called for the active session.
   c. `saveGlobalTheme("tokyo night")` writes to disk.

**Theme picker (Ctrl+Shift+Y, T01 active, T01 has per-workspace theme):**
1. User picks "tokyo night".
2. Saver:
   a. `cfg.Appearance.Theme = "tokyo night"`.
   b. T01 has its own theme set, so the active session keeps Dracula.
   c. `saveGlobalTheme("tokyo night")` writes to disk. Future workspaces without per-workspace themes will get Tokyo Night.

## Edge Cases

| Case | Behavior |
|------|----------|
| Workspace's saved theme no longer exists | `lookupTheme` falls back to "dark" (existing behavior). |
| `cfg.Workspaces` is nil (older config) | `ResolveTheme` handles via map zero-value lookup; falls through to global. |
| Per-workspace `theme = ""` (deleted but kept) | `ResolveTheme` treats empty as unset; falls through to global. |
| Both Ctrl+Y and Ctrl+Shift+Y opened in succession | Each picker opens fresh; no stale state. |
| User adds new workspace mid-session | New workspace has no entry in `cfg.Workspaces`; `ResolveTheme` returns global. Picker on that workspace can save one. |
| User edits config.toml externally during session | Not auto-detected. Existing behavior — change takes effect on next launch. |

## Testing

### Unit tests

`internal/config/config_test.go`:
- `Config.ResolveTheme` — covers workspace has theme; workspace has empty theme; workspace not in map; global theme set; global theme empty (returns "dark"); both nil maps.

`cmd/slk/save_theme_test.go` (or wherever savers live):
- `saveWorkspaceTheme` round-trip — empty config → save → re-read → expected section with comment. Update existing entry. Multiple workspaces. Workspace name with special characters in comment is OK.
- `saveGlobalTheme` round-trip — parallel test.

`internal/ui/styles/themes_test.go`:
- Extend existing table: each of the 12 new themes is registered, has all 14 `ThemeColors` fields populated (no empty strings for Sidebar*/Rail), and resolves via `lookupTheme`.

`internal/ui/themeswitcher/model_test.go`:
- Picker opened with `ScopeWorkspace` → returned result carries scope. Same for global.
- Header text is set correctly for each scope.

### Manual verification

- Switch workspaces with Ctrl+T → UI re-themes.
- Set workspace theme via Ctrl+Y → switch away → switch back → theme persists.
- Set global theme via Ctrl+Shift+Y on a workspace that has no per-workspace theme → theme applies live.
- Set global theme on a workspace that DOES have a per-workspace theme → saved globally but active session keeps the per-workspace theme.
- Run `slk --list-workspaces` → expected output.
- Inspect `config.toml` after first per-workspace save → workspace name comment present.

## Out of Scope

- Auto-import themes from VS Code/iTerm/Alacritty config files. Users have ~33 built-ins plus the existing `~/.config/slk/themes/` TOML loader.
- Theme inheritance/composition (workspace inherits global, then overrides specific colors). Workspace either has a full theme name or falls through.
- Per-workspace `[theme]` color overrides in addition to a per-workspace theme name. YAGNI.
- Live config reload when `config.toml` changes externally.
- Theme preview before commit. Live preview happens immediately upon selection (existing behavior).
- Migration of existing global theme to a per-workspace assignment. The global theme stays the global default until the user explicitly assigns per-workspace.

## File Changes

| File | Change |
|------|--------|
| `internal/ui/styles/themes.go` | Add 12 new theme entries to `builtinThemes`. |
| `internal/ui/styles/themes_test.go` | Extend table to verify new themes. |
| `internal/config/config.go` | Add `WorkspaceSettings`, `Workspaces` field, `ResolveTheme`. |
| `internal/config/config_test.go` | New tests for `ResolveTheme`. |
| `cmd/slk/main.go` | Add `saveWorkspaceTheme`, rename existing saver to `saveGlobalTheme`, parameterize `ThemeSaver`, wire workspace-switch theme apply, add `--list-workspaces`. |
| `internal/ui/themeswitcher/model.go` | Add `Scope`, `headerText`. |
| `internal/ui/keys.go` | Add `KeyThemePickerGlobal` (Ctrl+Shift+Y). |
| `internal/ui/app.go` | Handle the new keybind, parameterize the saver dispatch. |
| `cmd/slk/save_theme_test.go` (new) | Round-trip tests for both savers. |
