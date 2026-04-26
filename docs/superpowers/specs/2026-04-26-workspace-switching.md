# Multi-Workspace Runtime Switching

Date: 2026-04-26

## Overview

Enable switching between multiple Slack workspaces at runtime. All workspaces maintain live WebSocket connections for real-time unread badges. Users switch via number keys (`1`/`2`/`3`) or a `Ctrl+w` workspace picker overlay. A loading overlay with spinner shows during startup while workspaces connect in parallel.

---

## 1. WorkspaceContext

Central data structure holding everything needed for one workspace:

```go
type WorkspaceContext struct {
    Client      *slackclient.Client
    ConnMgr     *slackclient.ConnectionManager
    RTMHandler  *rtmEventHandler
    UserNames   map[string]string
    LastReadMap map[string]string
    Channels    []sidebar.ChannelItem
    FinderItems []channelfinder.Item
    TeamID      string
    TeamName    string
    UserID      string
    Ready       bool   // true once channels + unread counts are fetched
    Failed      bool   // true if connection failed
}
```

Stored in `main.go` as `workspaces map[string]*WorkspaceContext` keyed by team ID. An `activeTeamID string` tracks which workspace is currently displayed.

---

## 2. Startup Flow

### Parallel Workspace Connection

Replace the current sequential single-workspace connection with parallel goroutines:

1. Load all tokens from `tokenStore.List()`
2. Build workspace rail items for all tokens (existing code)
3. For each token, launch a goroutine that:
   a. Creates `Client`, calls `Connect(ctx)` to get team/user IDs
   b. Seeds `userNames` from SQLite cache (`db.ListUsers`)
   c. Fetches channels (`client.GetChannels`) -- sync, needed for sidebar
   d. Fetches unread counts (`client.GetUnreadCounts`) -- sync, needed for badges
   e. Launches background `GetUsers` fetch (existing pattern)
   f. Creates `rtmEventHandler` + `ConnectionManager`, starts both
   g. Builds `WorkspaceContext`, stores in `workspaces` map
   h. Sends `WorkspaceReadyMsg{TeamID}` to the bubbletea program
4. Start the TUI immediately with the loading overlay visible
5. When the first `WorkspaceReadyMsg` arrives, set it as active, wire callbacks, load channels/messages behind the overlay
6. When all workspaces are ready (or 15s timeout), dismiss the overlay

### Thread Safety

The `workspaces` map is written by goroutines and read by the main thread. Options:
- Build each `WorkspaceContext` fully in the goroutine, then send the entire struct via `WorkspaceReadyMsg`. The main thread only reads from it. No concurrent writes.
- This is the cleanest approach -- the goroutine owns the struct until it's sent.

```go
type WorkspaceReadyMsg struct {
    Context *WorkspaceContext
}

type WorkspaceFailedMsg struct {
    TeamID   string
    TeamName string
    Err      error
}
```

---

## 3. Loading Overlay

### Display

Centered overlay box (same pattern as channel finder / reaction picker), shown immediately on TUI start.

Content: a list of workspaces with status indicators:

```
  ⠹ Connecting to Truelist...
```

Updates as each workspace connects:

```
  ✓ Truelist
  ⠹ Connecting to Rands...
```

If a workspace fails:

```
  ✓ Truelist
  ✗ Rands (connection failed)
```

### Implementation

- New `loading bool` field on `App`, starts as `true`
- Spinner animation via `tea.Tick` at 100ms interval, cycling through braille characters: `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`
- New `loadingState` struct tracking per-workspace status: `{TeamID, TeamName, Status}` where Status is `connecting`/`ready`/`failed`
- Rendered in `App.View()` last (after all other overlays)
- Dismissed when all workspaces are ready or 15s timeout fires

### Timeout

After 15s, any workspace still connecting is marked as failed. The overlay dismisses. If no workspace connected, show an error and exit.

```go
type LoadingTimeoutMsg struct{}
```

Sent via `tea.Tick(15 * time.Second, ...)` at app start.

---

## 4. Switching Mechanics

### What Happens on Switch

When the user switches from workspace A to workspace B:

1. Set `activeTeamID = B.TeamID`
2. Update workspace rail selection (`workspaceRail.Select(index)`)
3. Re-wire all App callbacks to B's `Client`:
   - `SetChannelFetcher` -- uses B's client
   - `SetMessageSender` -- uses B's client
   - `SetOlderMessagesFetcher` -- uses B's client
   - `SetThreadFetcher` -- uses B's client
   - `SetThreadReplySender` -- uses B's client
   - `SetReactionSender` -- uses B's client
   - `SetCurrentUserID` -- B's user ID
4. Call `app.SetChannels(B.Channels)` to replace sidebar
5. Call `app.SetChannelFinderItems(B.FinderItems)` to replace finder
6. Call `app.SetUserNames(B.UserNames)` to replace name lookup
7. Close any open thread panel
8. Load the first channel of workspace B

### Callback Re-wiring

Currently all callbacks are closures over `client`. To make them switchable, introduce a `SwitchWorkspaceFunc` callback on the App:

```go
type SwitchWorkspaceFunc func(teamID string) tea.Msg
```

Set via `app.SetWorkspaceSwitcher(fn)`. The App calls this when the user triggers a switch. The function in `main.go`:

1. Looks up the `WorkspaceContext` by team ID
2. Re-wires all the callback closures to the new client
3. Returns a `WorkspaceSwitchedMsg` with the new channels, finder items, user names, etc.

```go
type WorkspaceSwitchedMsg struct {
    TeamID      string
    TeamName    string
    Channels    []sidebar.ChannelItem
    FinderItems []channelfinder.Item
    UserNames   map[string]string
    UserID      string
    LastReadMap map[string]string
}
```

The App's `Update` handler processes this message by resetting the UI state.

### State Reset on Switch

- Messages pane: cleared, first channel loaded
- Thread panel: closed
- Compose box: cleared
- Sidebar: replaced with new channels, selection reset to 0
- Channel finder: replaced with new items
- Status bar: updated with new workspace name
- Mode: reset to `ModeNormal`

---

## 5. WebSocket Events for Inactive Workspaces

Each workspace has its own `rtmEventHandler` sending to the same `tea.Program`. For inactive workspaces, the handler behavior changes:

### Messages from Inactive Workspaces

The `rtmEventHandler` already has a `workspaceID` field. Add an `isActive func() bool` callback:

```go
type rtmEventHandler struct {
    program     *tea.Program
    userNames   map[string]string
    tsFormat    string
    db          *cache.DB
    workspaceID string
    connected   bool
    isActive    func() bool
}
```

On `OnMessage` for an inactive workspace:
- Still cache to SQLite (existing behavior)
- Send `WorkspaceUnreadMsg{TeamID, ChannelID}` instead of `NewMessageMsg`

The App handles `WorkspaceUnreadMsg` by setting `HasUnread = true` on the workspace rail item and incrementing the cached unread count for that channel in the `WorkspaceContext`.

For the active workspace, behavior is unchanged -- full `NewMessageMsg` with message content.

### Reactions/Presence on Inactive Workspaces

- Reaction events: cache to SQLite but don't send UI messages
- Presence events: ignore for now (already TODO)

---

## 6. Switching UX

### Number Keys (`1`/`2`/`3`...)

In `handleNormalMode`, add key handlers for digit keys:

```go
case "1", "2", "3", "4", "5", "6", "7", "8", "9":
    idx, _ := strconv.Atoi(keyStr)
    idx-- // 0-indexed
    if idx < len(workspaceItems) {
        return a.switchWorkspace(workspaceItems[idx].ID)
    }
```

These work from any panel in normal mode. The workspace rail highlights the selected workspace.

### Workspace Picker (`Ctrl+w`)

New overlay following the channel finder pattern. Package: `internal/ui/workspacefinder/`.

**Model:**

```go
type Model struct {
    items    []Item
    filtered []int
    query    string
    selected int
    visible  bool
}

type Item struct {
    ID       string
    Name     string
    Initials string
}

type WorkspaceResult struct {
    ID   string
    Name string
}
```

**Behavior:**
- `Ctrl+w` opens the overlay, sets `ModeWorkspaceFinder`
- Type to filter by workspace name
- `j/k` or arrows to navigate
- `Enter` to select and switch
- `Esc` to cancel
- Same overlay rendering as channel finder (`lipgloss.Place` centered)

Since there are usually 2-5 workspaces, the list is short. No scroll needed.

### New Mode

Add `ModeWorkspaceFinder` to the mode enum. The `Ctrl+w` key binding is added to `keys.go`.

---

## 7. Workspace Rail Updates

### Selection Highlight

The rail already has `WorkspaceActive` / `WorkspaceInactive` styles. The `selected` index is updated when switching workspaces. No changes needed to the rail rendering.

### Unread Badge

The rail already renders an unread `*` indicator when `HasUnread` is true. When a `WorkspaceUnreadMsg` arrives for an inactive workspace, set `HasUnread = true` on that workspace's rail item. When the user switches to that workspace, clear `HasUnread`.

Add `SetUnread(teamID string, hasUnread bool)` method to the workspace rail model.

---

## Existing Infrastructure

| Layer | What Exists | How It's Used |
|-------|------------|---------------|
| `WorkspaceItem` struct | Has `ID`, `Name`, `Initials`, `HasUnread` | Rail rendering + unread badges |
| `workspace.Model.Select(idx)` | Sets selected workspace | Called on switch |
| `PanelWorkspace` enum | Declared but never used for focus | Stays unused -- rail is not focusable |
| `ConnectionManager` | One per client, runs in goroutine | One per workspace |
| `rtmEventHandler` | One per workspace, sends to same `tea.Program` | Active handler sends full msgs, inactive sends unread-only |
| `sidebar.SetItems()` | Replaces all channels | Called on switch |
| Channel finder overlay pattern | `channelfinder/model.go` | Template for workspace finder |

## Out of Scope

- Per-workspace config/theme customization
- Workspace reordering in the rail
- Adding/removing workspaces at runtime (requires restart)
- Cross-workspace search
- Remembering last-viewed channel per workspace (start from first channel on switch)
