# Customizable Themes Design (Revised)

Date: 2026-04-26

## Overview

Full theme system with 12 built-in themes, user-created custom themes loaded from a directory, and a live theme switcher overlay (Ctrl+y). Users can also override individual colors on top of any theme via `[theme]` in config.toml.

## Existing Infrastructure

- **Styles:** `internal/ui/styles/styles.go` defines ~11 color vars and ~25 composed lipgloss styles as package-level `var` declarations. All components reference these vars directly.
- **Config:** `internal/config/config.go` has `[appearance]` section with `theme = "dark"` field (currently unused). `Theme` struct with 10 color fields already added (Task 1 complete).
- **Overlay pattern:** `channelfinder`, `workspacefinder` overlays provide the template: `Model` struct with `Open()`, `Close()`, `IsVisible()`, `HandleKey()`, `ViewOverlay()` methods.
- **Config path:** `~/.config/slk/config.toml`

## Theme Definition

A theme is a named set of 10 semantic colors:

| Color | Purpose |
|-------|---------|
| `primary` | Focused borders, usernames, links, active elements |
| `accent` | Selection indicator, online presence, success states |
| `warning` | Command mode badge |
| `error` | Error states, unread badges, new message separator |
| `background` | Main panel backgrounds |
| `surface` | Slightly elevated surface |
| `surface_dark` | Status bar, compose box backgrounds |
| `text` | Primary text |
| `text_muted` | Timestamps, section headers, muted text |
| `border` | Unfocused panel borders, compose borders |

## Built-in Themes (12)

Embedded in the binary as Go maps. Each maps the 10 color keys to hex values.

1. **Dark** (current default)
2. **Light**
3. **Dracula**
4. **Solarized Dark**
5. **Solarized Light**
6. **Gruvbox Dark**
7. **Gruvbox Light**
8. **Nord**
9. **Tokyo Night**
10. **Catppuccin Mocha**
11. **One Dark**
12. **Rosé Pine**

## Custom Themes

Users place `.toml` files in `~/.config/slk/themes/`. Each file defines one theme:

```toml
name = "My Custom Theme"

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

The `name` field is required. All 10 colors should be specified (missing colors fall back to the dark theme defaults). At startup, the app scans `~/.config/slk/themes/` and loads all `.toml` files. Custom themes with the same name as a built-in override the built-in.

## Config

```toml
[appearance]
theme = "dark"  # name of active theme (case-insensitive match)

# Optional per-color overrides applied on top of the active theme
[theme]
primary = "#FF0000"
```

The `[theme]` section provides inline color overrides applied after the named theme is loaded. This lets users tweak a theme without creating a full custom file.

## Apply Function

`styles.Apply(themeName string, overrides config.Theme)` in the styles package:

1. Looks up the theme by name (built-in map first, then custom themes)
2. Falls back to "dark" if not found
3. Sets all 10 package-level color vars
4. Applies any non-empty overrides from the `config.Theme` struct
5. Calls `buildStyles()` to rebuild all ~25 composed lipgloss styles

Called at startup and whenever the user switches themes via the overlay.

## Theme Switcher Overlay

A filterable overlay triggered by `Ctrl+y`, following the same pattern as the workspace finder:

- **Model:** `internal/ui/themeswitcher/model.go` with `Open()`, `Close()`, `IsVisible()`, `HandleKey() *ThemeResult`, `ViewOverlay()`
- **Items:** List of theme names (built-in + custom), filterable by typing
- **Selection:** Green left-border indicator, j/k or up/down navigation
- **Apply on select:** When the user presses Enter, the selected theme name is returned as a `ThemeResult`. The app handles it by calling `styles.Apply()` and saving the selection to config.
- **Escape:** Closes without changing
- **Live:** Theme applies immediately on selection, no restart needed

### App Integration

- New `ModeThemeSwitcher` mode (or reuse overlay pattern without a dedicated mode)
- `Ctrl+y` opens the overlay
- `ThemeSelectedMsg{Name string}` bubbletea message dispatched on selection
- `App.Update` handles it: calls `styles.Apply(name, cfg.Theme)`, updates `cfg.Appearance.Theme`, saves config

### Saving Theme Selection

When the user selects a theme, update `cfg.Appearance.Theme` and write the config back. Use the existing TOML library to marshal and write to the config path. Only update the `[appearance]` section's `theme` field.

## Files

| File | Purpose |
|------|---------|
| `internal/ui/styles/themes.go` | Built-in theme color maps, `LoadCustomThemes(dir string)`, theme registry |
| `internal/ui/styles/styles.go` | Existing styles + `Apply()` + `buildStyles()` |
| `internal/ui/styles/styles_test.go` | Tests for Apply, theme loading, overrides |
| `internal/config/config.go` | Theme struct (done), add `SaveTheme(path, name string)` |
| `internal/ui/themeswitcher/model.go` | Theme switcher overlay component |
| `internal/ui/themeswitcher/model_test.go` | Tests for the overlay |
| `internal/ui/app.go` | Ctrl+y binding, ThemeSelectedMsg, overlay rendering |
| `internal/ui/keys.go` | Add ThemeSwitcher keybinding |
| `cmd/slk/main.go` | Startup: load custom themes, apply theme; wire theme save callback |

## Testing

- Apply with each built-in theme name produces correct colors
- Apply with unknown name falls back to dark
- Apply with overrides merges correctly
- Custom theme loading from directory
- Custom theme overrides built-in with same name
- Theme switcher: open/close, filter, select
- Config save after theme switch

## Out of Scope

- Individual style property overrides (bold, italic, padding)
- Theme editor/creator UI
- Animated theme transitions
