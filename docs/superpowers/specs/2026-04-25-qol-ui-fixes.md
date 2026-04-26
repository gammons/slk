# QoL UI Fixes Design Spec

Date: 2026-04-25

## Overview

Four independent UI improvements: better insert mode indication, multi-line compose, workspace rail cleanup, and connection status indicators.

## 1. Insert Mode Indicator

**Problem:** When entering insert mode, the entire panel border turns blue, which is visually noisy and doesn't clearly communicate that the compose box is the active element.

**Fix:** Only the compose box changes appearance in insert mode. Panel borders stay gray (unfocused style) regardless of mode.

**Compose box in insert mode:**
- Blue border (`styles.Primary` / `#4A9EFF`)
- Subtle dark-blue background tint (e.g., `#111128` or similar dark shade)
- Blinking cursor from the textarea widget

**Compose box when not in insert mode:**
- Gray border (`styles.Border` / `#333333`)
- No background tint (transparent / default)

**Changes required:**
- `internal/ui/styles/styles.go`: Add `ComposeInsert` style with blue border + background tint
- `internal/ui/compose/model.go`: Update `View()` to use the new style when `focused` is true
- `internal/ui/app.go`: Stop changing panel border styles based on insert mode. The `focusedPanel` border logic stays for normal mode panel focus (Tab/h/l navigation), but insert mode should NOT change any panel borders -- the compose box highlight is sufficient.

**Clarification on panel focus borders:** Panel focus borders (blue = focused, gray = unfocused) are a separate concept from insert mode. In normal mode, the focused panel gets a blue border to show which panel receives j/k navigation. In insert mode, panel borders should all stay gray, and only the compose box indicates activity. This means: when entering insert mode, the previously-focused panel's border goes gray; when leaving insert mode, the focused panel's border returns to blue.

## 2. Multi-line Compose (Shift+Enter)

**Problem:** The compose box uses `bubbles/textinput` which is single-line only. No way to compose multi-line messages.

**Fix:** Replace `bubbles/textinput` with `bubbles/textarea`.

**Behavior:**
- `Enter` sends the message (same as today)
- `Shift+Enter` inserts a newline
- The textarea grows up to a maximum of 5 visible lines, then scrolls internally
- No line numbers, no character counter
- Same placeholder text pattern: `Message #channel... (i to insert)`

**Changes required:**
- `internal/ui/compose/model.go`: Replace `textinput.Model` with `textarea.Model`. Update `New()`, `Focus()`, `Blur()`, `Value()`, `Reset()`, `Update()`, and `View()` accordingly.
- `internal/ui/app.go`: Update `handleInsertMode()` to intercept `Enter` before passing to textarea (textarea's default Enter inserts a newline; we want Enter to send and Shift+Enter to insert newline).

**textarea configuration:**
- `ShowLineNumbers = false`
- `CharLimit = 40000`
- `MaxHeight = 5` (lines)
- `Prompt = "> "`
- `SetWidth(width)` called each render
- `Placeholder` set to channel/thread context

## 3. Workspace Rail -- Remove Border

**Problem:** The workspace rail has a vertical `│` border on the right side that looks awkward.

**Fix:** Remove the right border entirely. The rail is a dark background strip (`SurfaceDark` / `#0F0F23`) with centered workspace initials. The sidebar's own rounded border provides visual separation.

**Changes required:**
- `internal/ui/workspace/model.go`: Remove `BorderStyle`, `BorderRight`, and `BorderForeground` from the container style in `View()`. Keep `Background(styles.SurfaceDark)`, `Width`, `Height`, `Padding`, `Align`.
- `internal/ui/workspace/model.go`: Update `Width()` return value from 7 to 6 (no longer includes border character width).

## 4. Connection Status Indicators

**Problem:** The status bar shows a green `*` when connected and red `DISCONNECTED` text when not. There's no "connecting" state, and the indicators are not visually clear.

**Fix:** Three-state connection indicator in the bottom-right of the status bar with colored dot emoji.

**States:**
- `Connected`: `🟢 Connected` (green dot)
- `Connecting`: `🟡 Connecting` (yellow dot)
- `Disconnected`: `🔴 Disconnected` (red dot)

**Changes required:**

**Status bar (`internal/ui/statusbar/model.go`):**
- Replace `connected bool` with `connState ConnectionState` (new enum: `StateConnected`, `StateConnecting`, `StateDisconnected`)
- Replace `SetConnected(bool)` with `SetConnectionState(ConnectionState)`
- Update `View()` to render the three-state indicator in the right section

**WebSocket event wiring (`internal/slack/events.go` + `internal/slack/client.go`):**
- Add `OnConnect()` and `OnDisconnect()` methods to the `EventHandler` interface
- In `client.go`, call `OnConnect()` when the `"hello"` WebSocket message is received (currently ignored)
- In `client.go`, call `OnDisconnect()` when the WebSocket read loop exits (on error or clean close)
- In `cmd/slk/main.go`, wire the new callbacks to update `statusbar.SetConnectionState()`
- Set initial state to `StateConnecting` before WebSocket connection starts

## Out of Scope

- Markdown preview in compose box (deferred)
- Sidebar section spacing changes (already working correctly)
