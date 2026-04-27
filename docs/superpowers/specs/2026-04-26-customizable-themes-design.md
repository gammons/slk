# Customizable Themes Design

Date: 2026-04-26

## Overview

Allow users to customize the app's color scheme via a `[theme]` section in `config.toml`. Users override semantic colors (primary, accent, background, etc.) and all styles automatically adapt. Ships with dark (default) and light presets.

## Existing Infrastructure

- **Styles:** `internal/ui/styles/styles.go` defines ~11 color vars and ~25 composed lipgloss styles as package-level `var` declarations. All components reference these vars directly.
- **Config:** `internal/config/config.go` has `[appearance]` section with `theme = "dark"` field that is currently unused.
- **Config path:** `~/.config/slk/config.toml`

## Theme Config

A `[theme]` section in `config.toml` with optional hex color overrides. Any color not specified falls back to the active preset (dark or light).

```toml
[appearance]
theme = "dark"  # "dark" (default) or "light"

[theme]
primary = "#4A9EFF"
accent = "#50C878"
warning = "#E0A030"
error = "#E04040"
background = "#1A1A2E"
surface = "#16162B"
surface_dark = "#0F0F23"
text = "#E0E0E0"
text_muted = "#888888"
border = "#333333"
```

All fields are optional strings. Empty string or missing field means "use preset default."

## Config Struct

Add to `internal/config/config.go`:

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

Add `Theme Theme` field to the `Config` struct. No defaults needed -- zero-value (empty strings) means "use preset."

## Built-in Presets

### Dark (default)

The current hardcoded colors:

| Color | Hex |
|-------|-----|
| Primary | `#4A9EFF` |
| Accent | `#50C878` |
| Warning | `#E0A030` |
| Error | `#E04040` |
| Background | `#1A1A2E` |
| Surface | `#16162B` |
| SurfaceDark | `#0F0F23` |
| Text | `#E0E0E0` |
| TextMuted | `#888888` |
| Border | `#333333` |

### Light

| Color | Hex |
|-------|-----|
| Primary | `#0366D6` |
| Accent | `#28A745` |
| Warning | `#D9840D` |
| Error | `#CB2431` |
| Background | `#FFFFFF` |
| Surface | `#F6F8FA` |
| SurfaceDark | `#EAEEF2` |
| Text | `#24292E` |
| TextMuted | `#6A737D` |
| Border | `#D1D5DA` |

## Apply Function

Add `Apply(preset string, overrides config.Theme)` to `internal/ui/styles/styles.go`. This function:

1. Selects a base color palette from the preset name (`"dark"` or `"light"`)
2. Applies any non-empty overrides from the `Theme` struct on top
3. Overwrites the package-level color vars (`Primary`, `Accent`, etc.)
4. Rebuilds all composed styles (`FocusedBorder`, `Username`, `StatusBar`, etc.) from the updated colors

This must be called before any UI rendering. Styles that use inline hex colors (e.g., `lipgloss.Color("#FFFFFF")` for white text in status mode badges) are not affected by theming -- they remain fixed. This is acceptable since they are contrast colors tied to specific backgrounds.

## Integration

In `cmd/slk/main.go`, after `config.Load()`:

```go
styles.Apply(cfg.Appearance.Theme, cfg.Theme)
```

This runs before `NewApp()` or any bubbletea rendering, so all components pick up the themed colors from the package-level vars.

## Files to Modify

| File | Changes |
|------|---------|
| `internal/config/config.go` | Add `Theme` struct, add `Theme Theme` to `Config` |
| `internal/ui/styles/styles.go` | Add `Apply(preset string, overrides config.Theme)`, add light preset colors, refactor style construction into a `buildStyles()` helper called by both init and Apply |
| `cmd/slk/main.go` | Call `styles.Apply(cfg.Appearance.Theme, cfg.Theme)` after config load |

## Testing

- Test that `Apply("dark", Theme{})` produces the default dark colors
- Test that `Apply("light", Theme{})` produces light colors
- Test that `Apply("dark", Theme{Primary: "#FF0000"})` overrides only Primary, keeps other dark defaults
- Test that unknown preset name falls back to dark
- Config tests for TOML parsing of the `[theme]` section

## Out of Scope

- Runtime theme switching (themes are applied at startup only)
- Individual style property overrides (bold, italic, padding)
- Theme file sharing/importing from separate files
