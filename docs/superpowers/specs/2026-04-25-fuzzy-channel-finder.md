# Fuzzy Channel Finder Design

## Problem

Navigating to a specific channel requires scrolling through the sidebar with j/k. With many channels and DMs, this is slow. A fuzzy finder overlay (like Slack's Ctrl+K or VS Code's Ctrl+P) lets users jump to any channel instantly.

## Solution

A centered floating overlay triggered by `Ctrl+t` or `Ctrl+p` that filters all channels/DMs by substring match and switches to the selected one.

## Trigger

`Ctrl+t` or `Ctrl+p` in NORMAL mode opens the overlay. Works from any focused panel. `Esc` closes it and returns to NORMAL mode.

## UI Layout

Centered overlay rendered on top of the existing three-panel layout:

```
┌─ Switch Channel ──────────────────┐
│ > mar                             │
│                                   │
│   # marketing                     │
│   ● mpdm-gavin--pran...          │
│   # ext-market-data               │
└───────────────────────────────────┘
```

- Title: "Switch Channel"
- Text input at the top showing the current query
- Filtered results below, max 10 visible, scrollable
- Each result shows the channel type prefix (`#` public, `◆` private, `●`/`○` DM with presence)
- Selected result highlighted with background color
- Overlay width: 50% of terminal width, min 30 cols, max 80 cols
- Overlay height: dynamic based on results, max ~12 rows (title + input + 10 results)

## Keybindings (within finder)

| Key | Action |
|-----|--------|
| Any character | Append to query, re-filter |
| Backspace | Delete last char from query |
| `j` / `Down` / `Ctrl+n` | Move selection down |
| `k` / `Up` / `Ctrl+p` | Move selection up |
| `Enter` | Select highlighted channel, close overlay |
| `Esc` | Cancel, close overlay, return to NORMAL |

Note: `j`/`k` navigate the result list (not typed into the query) because this is a picker, not a text editor. All other characters go into the query.

## Fuzzy Matching

Case-insensitive substring match on channel name. The channel list is small enough (typically <200) that substring matching is fast and simple. No external fuzzy matching library needed.

Results are ordered by match position (earlier match = higher rank), with exact prefix matches first.

## Search Scope

All items in the sidebar: public channels, private channels, DMs, and group DMs.

## Architecture

### New Component

`internal/ui/channelfinder/model.go` -- self-contained bubbletea-style component:

```go
type Model struct {
    items    []ChannelItem   // all channels (copied from sidebar)
    filtered []int           // indices into items matching current query
    query    string
    selected int             // index into filtered
    visible  bool
}
```

Methods:
- `New(items []ChannelItem) Model`
- `SetItems(items []ChannelItem)` -- update channel list
- `Open()` -- show overlay, clear query, reset selection
- `Close()` -- hide overlay
- `IsVisible() bool`
- `Update(msg tea.KeyMsg) (Model, *ChannelSelectedResult)` -- handle input, return selected channel or nil
- `View(width, height int) string` -- render the overlay

`ChannelItem` reuses the same struct from the sidebar package (or a shared type).

### New Mode

Add `ModeChannelFinder` to `internal/ui/mode.go`.

### App Integration

In `app.go`:
- Add `channelFinder channelfinder.Model` to the App struct
- `Ctrl+t`/`Ctrl+p` in NORMAL mode → set `ModeChannelFinder`, call `channelFinder.Open()`
- In `ModeChannelFinder`, route all key events to `channelFinder.Update()`
- If Update returns a selected channel → send `ChannelSelectedMsg`, set mode back to NORMAL
- If `Esc` → call `channelFinder.Close()`, set mode back to NORMAL
- In `View()`, if finder is visible, render it as an overlay on top of the existing layout using `lipgloss.Place()`

### Selection Flow

Selecting a channel from the finder produces the same `ChannelSelectedMsg` that clicking a sidebar channel does. This means:
1. The sidebar selection syncs to the chosen channel
2. The channel fetcher loads messages
3. The message pane updates

The sidebar's `SelectByID(channelID)` method is called to sync the sidebar cursor.

## Files Changed

- Create: `internal/ui/channelfinder/model.go`
- Modify: `internal/ui/mode.go` -- add `ModeChannelFinder`
- Modify: `internal/ui/app.go` -- add finder to App struct, handle mode, render overlay
- Modify: `internal/ui/sidebar/model.go` -- add `SelectByID(id string)` method if not present

## What Stays the Same

- Sidebar component (still works independently)
- Channel fetching flow (reuses existing `ChannelSelectedMsg`)
- Message pane, compose, workspace rail
- All keybindings outside the finder
