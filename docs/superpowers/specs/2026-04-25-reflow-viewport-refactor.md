# Reflow + Viewport Refactor Design Spec

Date: 2026-04-25

## Overview

Replace custom text wrapping, padding, truncation, and scroll logic with `muesli/reflow` and `charmbracelet/bubbles/viewport`. This fixes known bugs (ANSI-corrupting truncation, highlight width issues) and eliminates ~210 lines of custom viewport code across 3 components.

## reflow Adoption

### wordwrap — Message Text Wrapping

Replace `styles.MessageText.Width(contentWidth).Render(text)` with `styles.MessageText.Render(wordwrap.String(text, contentWidth))`.

Affected locations:
- `internal/ui/messages/model.go` line 230 — message body text
- `internal/ui/thread/model.go` line 271 — thread reply text
- `internal/ui/messages/model.go` line 311 — channel topic text

This separates the wrapping concern (reflow) from the styling concern (lipgloss). `reflow/wordwrap` is ANSI-aware and provides proper word-boundary wrapping.

### padding — Selection Highlight

Replace the manual per-line ANSI-width-measurement + space-padding in `internal/ui/thread/model.go` (the `applySelection` function, lines 247-259) with `padding.String(content, uint(width))`.

The messages pane's `applySelection` (`internal/ui/messages/model.go` line 299-301) should also be updated to use `reflow/padding` instead of relying on `lipgloss.Width()` + `Padding()`.

### truncate — Channel Name and Finder Truncation

Replace byte-level string slicing with ANSI-aware truncation:

- `internal/ui/channelfinder/model.go` line 222: `line[:innerWidth-1] + "…"` → `truncate.StringWithTail(line, uint(innerWidth), "…")`. This fixes an active bug where truncation corrupts ANSI escape sequences from lipgloss styling.
- `internal/ui/sidebar/model.go` line 175: `name[:maxNameLen-1] + "…"` → `truncate.StringWithTail(name, uint(maxNameLen), "…")`. Fixes potential breakage on multi-byte Unicode channel names.

## bubbles/viewport Adoption

### Approach

Each scrollable panel (messages, thread, sidebar) keeps its item-level `selected` index and navigation methods (`MoveUp`, `MoveDown`, etc.). The viewport is used as a scroll-aware rendering container:

1. Pre-render all items into a single string with selection highlight on the selected item
2. Set that string as viewport content via `viewport.SetContent()`
3. Calculate which line the selected item starts at
4. Call `viewport.SetYOffset()` to ensure the selected item is visible
5. Return `viewport.View()` for the final render

### What Gets Eliminated

- The `offset int` field and all manual offset tracking in messages, thread, and sidebar
- The bottom-up fill algorithm in `messages/model.go` (~120 lines, lines 340-457)
- The viewport fill algorithm in `thread/model.go` (~50 lines, lines 185-241)
- The scroll/offset logic in `sidebar/model.go` (~40 lines, lines 238-277)
- All `lipgloss.Height()` measurement loops for viewport fitting (~12 calls)
- Manual "N more above / N more below" indicators (replaced by viewport position checks via `viewport.AtTop()` / `viewport.AtBottom()` and `viewport.TotalLineCount()` / `viewport.YOffset`)

### What Stays

- `selected int` field and item navigation methods on each component
- The render cache in the messages pane (pre-renders items, feeds result to viewport)
- The `View(height, width int) string` signature on each component
- Chrome rendering (headers, separators) above the viewport area

### Per-Component Viewport Setup

Each component adds a `viewport.Model` field:
- Initialized in constructor with default dimensions
- On dimension change, update `viewport.Width` and `viewport.Height`
- Viewport `KeyMap` is zeroed out (disabled) — the parent App handles j/k navigation, not viewport's built-in key handling
- After navigation or content change, rebuild the viewport content string and set the Y offset

### Scroll Indicators

The messages pane currently shows "-- N more above --" and "-- N more below --" text indicators. These are preserved by checking `viewport.YOffset > 0` (more above) and `viewport.YOffset + viewport.Height < viewport.TotalLineCount()` (more below). The indicator text is rendered outside the viewport, in the chrome area, so the viewport content is purely messages.

## Components Not Changed

- **Compose box** — not scrollable, stays as-is
- **Workspace rail** — not scrollable, stays as-is
- **Status bar** — layout constraints stay as-is, `lipgloss.Width()` is appropriate here
- **Channel finder** — simple fixed-count sliding window, viewport would be overkill. Only truncation is updated via reflow.
- **Border and panel width constraints** in `app.go` — layout-level concerns, stay as-is

## Dependencies Added

- `github.com/muesli/reflow` (wordwrap, padding, truncate subpackages)
- `github.com/charmbracelet/bubbles/viewport` (already available — `bubbles` is a direct dependency)
