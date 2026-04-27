# UI Fixes Batch — Design Spec

**Date:** 2026-04-27

Seven UI fixes and enhancements for the TUI layout, unread indicators, presence data, DM name resolution, and mouse interaction.

---

## Fix 1: Right-align "Connected" indicator with inner panel edge

**Problem:** The status bar spans the full terminal width (`a.width`). The "Connected" indicator is right-aligned to the terminal edge, but visually it should align with the inner-right edge of the rightmost content panel (message or thread).

**Change:** In `app.go`, compute the status bar width as the total width minus the workspace rail width, so the status bar starts at the sidebar's left edge and extends to the right edge of the content area. Render the status bar at this narrower width, joined horizontally with a rail-width spacer (or just offset it). The mode badge, channel name, and connection indicator all render within this narrower span.

**Files:** `internal/ui/app.go` (View method, around line 1674).

---

## Fix 2: Widen sidebar to 30 columns

**Problem:** Sidebar is 25 columns. Channel names with unread dots overflow to the next line because the dot doesn't fit on the same line as the name.

**Changes:**
1. `sidebar/model.go`: Change `Width()` to return 30.
2. `sidebar/model.go`: In `View()`, when rendering a channel line with an unread dot, calculate the available width for the channel name as: `total line width - prefix width - dot suffix width`. Truncate the channel name with `…` if it exceeds this width, ensuring `name + " ●"` always fits on one line.

**Files:** `internal/ui/sidebar/model.go`.

---

## Fix 3: Widen workspace rail to 6 columns, use `●` instead of `*`

**Problem:** The workspace rail is 5 columns wide. The unread indicator is a `*` character rendered on the line below the workspace initials. It should be a `●` dot on the same line as the initials.

**Changes:**
1. `workspace/model.go`: Change `Width()` to return 6.
2. `workspace/model.go`: In `View()`, replace the `*` indicator with `●`. Render it on the same line as the workspace initials (to the right of the initials text) instead of on a separate line below.

**Files:** `internal/ui/workspace/model.go`.

---

## Fix 4: Fix external guest DM name resolution

**Problem:** External guests (shared channel users, Slack Connect) are not in the initial user list fetch. Their DM channels show the raw user ID (e.g., `U01ABC123`) instead of their display name.

**Change:** During channel list construction in `main.go`, when building DM channel items and `wctx.UserNames[ch.User]` returns empty, trigger an async user profile fetch via the existing `resolveUser()` function. Store the resolved name in the cache and update the channel item's `Name` field. Since this is async, the channel list may initially show the ID and update once the fetch completes (send a bubbletea message to refresh the sidebar).

**Approach:**
1. During initial channel load, collect DM user IDs not found in `wctx.UserNames`.
2. Dispatch background goroutines (batched) to fetch their profiles via `client.GetUserInfo()`.
3. On resolution, send a new `UserResolvedMsg` to the bubbletea program, which updates `wctx.UserNames` and refreshes the sidebar items.

**Files:** `cmd/slk/main.go`, `internal/ui/app.go` (new message type).

---

## Fix 5: Wire up presence / online status

**Problem:** The rendering code for presence indicators (green `●` for active, gray `○` for away) exists in the sidebar, but the data pipeline from Slack's WebSocket presence events is stubbed out. The `OnPresenceChange` handler in `main.go:966-968` is a no-op.

**Changes:**
1. **New message type:** Add `PresenceChangeMsg{UserID, Presence string}` in `app.go`.
2. **Wire RTM handler:** In `main.go`, implement `OnPresenceChange` to send `PresenceChangeMsg` to the bubbletea program.
3. **Handle in Update:** In `app.go`, on `PresenceChangeMsg`, iterate sidebar items and update `Presence` for any DM whose user matches the changed user ID.
4. **Initial presence:** During channel loading, set each DM's `Presence` field from the cached user data (if available). Also consider calling `users.getPresence` for DM users on initial load, or subscribing to presence via the RTM `presence_sub` event.
5. **Cache update:** Update `cache.UpdatePresence()` when presence changes arrive.

**Files:** `cmd/slk/main.go`, `internal/ui/app.go`, `internal/ui/sidebar/model.go` (may need `UpdatePresence(userID, presence)` method).

---

## Fix 6: Mouse click to select panels and highlight messages

**Problem:** No mouse support exists. Clicking in a panel should focus it and select the item under the cursor.

**Changes:**
1. **Enable mouse:** Add `tea.WithMouseCellMotion()` option in the bubbletea program initialization (`main.go` or wherever `tea.NewProgram` is called).
2. **Handle MouseMsg:** Add a `tea.MouseMsg` case in `app.go`'s `Update()` method.
3. **Panel detection:** On left-click, use the x-coordinate to determine which panel was clicked:
   - `x < railWidth` → workspace rail (ignore or switch workspace)
   - `x < railWidth + sidebarWidth + sidebarBorder` → sidebar panel
   - `x < railWidth + sidebarWidth + sidebarBorder + msgWidth + msgBorder` → message panel
   - Otherwise → thread panel
4. **Set focus:** Update `focusedPanel` to the detected panel.
5. **Message selection:** For the message panel, translate the click's y-coordinate relative to the panel's viewport to determine which message was clicked. Update `messagepane.selected` accordingly. Same logic for thread panel.
6. **Store panel layout widths:** The `App` struct needs to store the computed panel widths (rail, sidebar, msg, thread) from `View()` so they're available in `Update()`. Currently they're local variables in `View()`.

**Files:** `cmd/slk/main.go` (tea options), `internal/ui/app.go` (Update, View, stored widths), `internal/ui/messages/model.go` (click-to-select method).

---

## Fix 7: Mouse click on channels/DMs to open them

**Problem:** Clicking a channel in the sidebar should select and open it.

**Changes:**
1. When a mouse click lands in the sidebar panel, calculate which channel item was clicked based on the y-coordinate relative to the sidebar viewport.
2. Account for section headers (which are not selectable items) when mapping y to item index.
3. Update `sidebar.selected` to the clicked item index.
4. Trigger the same channel-open flow as pressing Enter: emit `ChannelSelectedMsg` with the selected channel's ID and name.

**Complexity note:** Mapping y-coordinate to sidebar item is non-trivial because of section headers, viewport scrolling, and the selected-item indicator. The sidebar may need to expose a `ClickAt(y int)` method that returns the item index, accounting for its internal layout.

**Files:** `internal/ui/sidebar/model.go` (new `ClickAt` method), `internal/ui/app.go` (mouse handler calls sidebar click).

---

## Out of Scope

- Mouse drag/scroll (just click selection for now)
- Right-click context menus
- Mouse hover highlighting
- Workspace rail mouse click (can be added later)
