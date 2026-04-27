# Lipgloss v2 / Bubbletea v2 Migration Design

Date: 2026-04-27

## Overview

Migrate the entire UI stack from lipgloss v1 + bubbletea v1 + bubbles v1 to lipgloss v2 + bubbletea v2 + bubbles v2. The primary motivation is lipgloss v2's compositor with layer support, which enables a clean "background fill" layer that eliminates the dark-patch issues with light themes.

## Motivation

Light themes currently show dark patches where rendered content doesn't have explicit ANSI background codes. Lipgloss v1 has no concept of layering -- every cell must explicitly set its own background. Lipgloss v2 introduces a cell-based compositor where a background layer fills all uncolored cells, solving this problem architecturally.

## Scope

### Library Upgrades

| Library | From | To |
|---------|------|----|
| lipgloss | `github.com/charmbracelet/lipgloss` | `charm.land/lipgloss/v2` |
| bubbletea | `github.com/charmbracelet/bubbletea` | `charm.land/bubbletea/v2` (if available) |
| bubbles | `github.com/charmbracelet/bubbles` | `charm.land/bubbles/v2` (if available) |

### Key API Changes (from lipgloss v2 upgrade guide)

1. **Import path:** `github.com/charmbracelet/lipgloss` -> `charm.land/lipgloss/v2`
2. **Color type:** `lipgloss.Color` is now a function returning `color.Color`, not a string type. Our `styles.go` color vars change from `lipgloss.Color("#hex")` (string type) to `lipgloss.Color("#hex")` (function call returning `color.Color`).
3. **Renderer removal:** No more `*Renderer`. `Style` is a pure value type.
4. **Whitespace options:** `WithWhitespaceBackground(c)` -> `WithWhitespaceStyle(s)`
5. **Compositor:** New `lipgloss.NewLayer()` with X/Y/Z positioning and `compositor.Compose()` for layered rendering.

### Files to Modify

Every file that imports lipgloss, bubbletea, or bubbles. Approximately:
- `internal/ui/styles/styles.go` and `themes.go` -- color type changes
- `internal/ui/app.go` -- compositor for background layer in View()
- `internal/ui/messages/model.go` -- remove hacky background fills, use compositor
- `internal/ui/sidebar/model.go` -- same
- `internal/ui/statusbar/model.go` -- same
- `internal/ui/thread/model.go` -- same
- `internal/ui/compose/model.go` -- bubbletea/bubbles changes
- `internal/ui/workspace/model.go` -- lipgloss changes
- `internal/ui/workspacefinder/model.go` -- lipgloss changes
- `internal/ui/channelfinder/model.go` -- lipgloss changes
- `internal/ui/themeswitcher/model.go` -- lipgloss changes
- `internal/ui/reactionpicker/model.go` -- lipgloss changes
- `internal/ui/mentionpicker/model.go` -- lipgloss changes
- `internal/ui/keys.go` -- bubbles key changes (if any)
- `internal/ui/mode.go` -- may not need changes
- `cmd/slk/main.go` -- bubbletea program creation changes
- All test files

### Compositor Strategy

In `app.go` View():
1. Render a full-screen background fill with `styles.Background`
2. Render panels (workspace rail, sidebar, messages, thread) as content
3. Use the compositor to layer content over background
4. All the hacky `Background(styles.Background)` additions on individual styles can be removed
5. Overlays (channel finder, theme switcher, etc.) become top layers

### Migration Strategy

1. Upgrade go.mod dependencies
2. Fix import paths (mechanical find-and-replace)
3. Fix color type changes in styles.go and themes.go
4. Fix whitespace option changes
5. Fix any bubbletea/bubbles API changes
6. Add compositor background layer in app.go View()
7. Remove per-element Background hacks
8. Verify all tests pass
9. Visual testing with all themes

## Risk

- Bubbletea v2 and bubbles v2 may have breaking changes beyond lipgloss
- The `bubbles/viewport` and `bubbles/textarea` components may have API changes
- Need to verify the compositor actually solves the background fill problem as expected

## Prerequisites

- Verify bubbletea v2 and bubbles v2 are released and stable
- Read bubbletea v2 migration guide (if available)
- Read bubbles v2 migration guide (if available)
