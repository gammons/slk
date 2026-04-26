# Reaction Picker Design

Date: 2026-04-26

## Overview

Add emoji reaction support to slack-tui: a search-first reaction picker overlay, quick-toggle navigation on existing reactions, pill-style reaction display, and full real-time reaction sync. The picker follows the channel finder's overlay pattern -- a compact, keyboard-driven list with fuzzy search and frecent emoji at the top.

This feature spans six areas: picker overlay UI, quick-toggle mode, pill-style rendering, data wiring (cache + API population), real-time WebSocket updates, and app integration.

## 1. Reaction Picker Overlay

### New Package: `internal/ui/reactionpicker/`

Self-contained overlay component following the channel finder pattern (`internal/ui/channelfinder/`).

### Model

| Field | Type | Purpose |
|-------|------|---------|
| `allEmoji` | `[]emojiEntry` | Full emoji list from `emoji.CodeMap()`, built at startup |
| `query` | `string` | Current search input |
| `filtered` | `[]emojiEntry` | Emoji matching current query |
| `frecent` | `[]emojiEntry` | Recently/frequently used emoji (top 8-10) |
| `selected` | `int` | Cursor index in displayed list |
| `visible` | `bool` | Whether overlay is shown |
| `messageTS` | `string` | TS of message being reacted to |
| `channelID` | `string` | Channel of the target message |
| `existingReactions` | `[]string` | Emoji names the current user already has on this message |

Where `emojiEntry` is:

```go
type emojiEntry struct {
    Name    string // e.g. "thumbsup"
    Unicode string // e.g. "👍"
}
```

### Methods

| Method | Signature | Purpose |
|--------|-----------|---------|
| `Open` | `(channelID, messageTS string, existingReactions []string)` | Show picker, load frecent, set target message |
| `Close` | `()` | Hide picker, clear query |
| `IsVisible` | `() bool` | Check visibility |
| `HandleKey` | `(keyStr string) *ReactionResult` | Process keyboard input, return result or nil |
| `SetFrecentEmoji` | `(emoji []emojiEntry)` | Set the frecent list |
| `View` | `(termWidth int) string` | Render the picker box content |
| `ViewOverlay` | `(termWidth, termHeight int, background string) string` | Composite overlay onto background |

`ReactionResult` returned by `HandleKey`:

```go
type ReactionResult struct {
    Emoji  string // emoji name without colons
    Remove bool   // true if toggling off an existing reaction
}
```

### Emoji Data Source

Built at startup from `emoji.CodeMap()` (from `kyokomi/emoji/v2`). Each entry stripped of surrounding colons, sorted alphabetically. Stored as `allEmoji` on the model.

### Filtering

When `query` is empty, display the frecent list. When the user types, fuzzy-filter `allEmoji` by name substring match against `query`. Cap visible results at 10 rows.

### Display Layout

```
┌─ Add Reaction ──────────────┐
│ ▌ thumbs|                   │
│─────────────────────────────│
│ ▌ 👍 thumbsup         ✓    │
│   👎 thumbsdown             │
│   🫡 saluting_face          │
└─────────────────────────────┘
```

- Title: "Add Reaction" in primary color
- Input with primary-color left border `▌` (matches channel finder)
- Before typing: frecent list shown
- While typing: filtered results
- `✓` marker on emoji the user already has on this message
- Green `▌` on selected row

### Overlay Rendering

`ViewOverlay` uses `lipgloss.Place()` with centered positioning and dark backdrop (`#0F0F1A`), same as channel finder. Box uses `lipgloss.RoundedBorder()` with `styles.Primary` color. Width: max(35 chars, 30% of terminal width). Max 10 visible rows.

### Key Handling

| Key | Action |
|-----|--------|
| Any printable char | Append to query, re-filter |
| Backspace | Remove last char from query, re-filter |
| `j` / Down | Move selection down |
| `k` / Up | Move selection up |
| Enter | Select emoji: if `✓` (already reacted), return `ReactionResult{Remove: true}`; otherwise return `ReactionResult{Remove: false}` |
| Esc | Close picker, return nil |

## 2. Quick-Toggle Mode

Lightweight reaction navigation on the selected message's existing reactions. Lives in `internal/ui/messages/model.go`, not a separate package.

### New State on `messages.Model`

| Field | Type | Purpose |
|-------|------|---------|
| `reactionNavActive` | `bool` | Whether reaction-nav mode is active |
| `reactionNavIndex` | `int` | Which reaction pill is selected (0-indexed) |

### Activation

Press `R` (shift-r) on a selected message that has reactions. If the selected message has no reactions, `R` does nothing (does not open the picker -- use `r` for that).

Note: `R` is not currently defined in `keys.go`. Add a new `ReactionNav` binding: `key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "navigate reactions"))`.

### Methods

| Method | Signature | Purpose |
|--------|-----------|---------|
| `EnterReactionNav` | `()` | Set `reactionNavActive = true`, `reactionNavIndex = 0` |
| `ExitReactionNav` | `()` | Set `reactionNavActive = false` |
| `ReactionNavActive` | `() bool` | Check state |
| `ReactionNavLeft` | `()` | Move index left (wraps around, including `[+]` at end) |
| `ReactionNavRight` | `()` | Move index right (wraps around) |
| `SelectedReaction` | `() (emoji string, isPlus bool)` | Return the emoji at current index, or `isPlus=true` for the `[+]` position |

### Key Handling (routed by App in ModeNormal when `ReactionNavActive()`)

| Key | Action |
|-----|--------|
| `h` / Left | Move selection left |
| `l` / Right | Move selection right |
| Enter | If on `[+]`, open full picker. Otherwise toggle selected reaction (add/remove via callback) |
| `r` | Open full search picker |
| Esc | Exit reaction-nav, return to normal message navigation |

### Navigation

`l` past the `[+]` pill wraps to the first reaction. `h` past the first reaction wraps to `[+]`. The `[+]` pseudo-pill is always the last position.

### Edge Cases

- Real-time event removes the reaction at `reactionNavIndex`: clamp index to new length.
- All reactions removed while in reaction-nav: exit reaction-nav automatically.
- Message selection changes while reaction-nav is active: exit reaction-nav.

## 3. Pill-Style Reaction Display

### ReactionItem Enhancement

```go
type ReactionItem struct {
    Emoji      string // emoji name without colons, e.g. "thumbsup"
    Count      int
    HasReacted bool   // whether the current user has reacted with this emoji
}
```

### Pill Rendering

Each reaction renders as a lipgloss-styled block:

- **Your reaction:** green-tinted background (`#1a2e1a`), green text accent
- **Others' reaction:** dim background (`#1a1a2e`), subtle gray text
- **Selected pill (in reaction-nav):** primary color border, slightly lighter background
- **Format:** `emoji count` with `Padding(0, 1)`, e.g. ` 🎉 3 `
- **Separator:** single space between pills

### Emoji Rendering Fix

The `Emoji` field stores the name without colons (Slack API format). Before rendering, convert to Unicode: `emoji.Sprint(":" + r.Emoji + ":")`.

### Width Handling

If the total reaction line exceeds available message width, wrap to the next line. Each pill is roughly 6-8 chars wide.

### `[+]` Pill

Only rendered when reaction-nav is active. Styled with primary color, contains `+`. When selected and Enter is pressed, opens the full picker.

### Render Cache

Adding/removing reactions or entering/exiting reaction-nav must invalidate the render cache for the affected message. The existing cache invalidation pattern (set `m.cache = nil`) can be extended with a targeted invalidation for single messages, or the full cache can be rebuilt (simpler, acceptable since it's already fast).

## 4. Data Wiring

### Cache CRUD -- `internal/cache/reactions.go`

| Function | Signature | Purpose |
|----------|-----------|---------|
| `UpsertReaction` | `(messageTS, channelID, emoji string, userIDs []string, count int) error` | Insert or update a reaction row. `userIDs` serialized as JSON into the `user_ids` column. |
| `GetReactions` | `(messageTS, channelID string) ([]ReactionRow, error)` | Get all reactions for a message |
| `DeleteReaction` | `(messageTS, channelID, emoji string) error` | Remove a reaction row (count dropped to 0) |

```go
type ReactionRow struct {
    Emoji   string
    UserIDs []string
    Count   int
}
```

Uses the existing `reactions` table schema:

```sql
CREATE TABLE IF NOT EXISTS reactions (
    message_ts TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    emoji TEXT NOT NULL,
    user_ids TEXT NOT NULL DEFAULT '[]',
    count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (message_ts, channel_id, emoji)
);
```

### Frecent Tracking -- `internal/cache/frecent.go`

New table (added via migration in `db.go`):

```sql
CREATE TABLE IF NOT EXISTS frecent_emoji (
    emoji TEXT PRIMARY KEY,
    use_count INTEGER NOT NULL DEFAULT 0,
    last_used INTEGER NOT NULL DEFAULT 0
);
```

| Function | Signature | Purpose |
|----------|-----------|---------|
| `RecordEmojiUse` | `(emoji string) error` | Upsert: increment `use_count`, set `last_used` to `time.Now().Unix()` |
| `GetFrecentEmoji` | `(limit int) ([]string, error)` | Top N emoji sorted by `use_count * recency_weight`. Recency weight: `1.0 / (1 + days_since_last_use)`. Returns emoji names. |

### Populating Reactions from API Responses

In `main.go`'s `fetchChannelMessages`, when converting `slack.Message` to `MessageItem`:

1. Read `msg.Reactions` (type `[]slack.ItemReaction`, each with `Name string`, `Users []string`, `Count int`)
2. Convert each to `ReactionItem{ Emoji: r.Name, Count: r.Count, HasReacted: contains(r.Users, currentUserID) }`
3. Cache each reaction via `UpsertReaction(ts, channelID, r.Name, r.Users, r.Count)`

### Callback Functions

```go
type ReactionSendFunc func(channelID, messageTS, emoji string) error
type ReactionRemoveFunc func(channelID, messageTS, emoji string) error
```

Set via `SetReactionSender(add ReactionSendFunc, remove ReactionRemoveFunc)` on the App, same pattern as the existing `SetMessageSender`. Wired in `main.go` to `Client.AddReaction` and `Client.RemoveReaction`.

## 5. Real-Time Updates

### New Message Types

```go
type ReactionAddedMsg struct {
    ChannelID string
    MessageTS string
    UserID    string
    Emoji     string
}

type ReactionRemovedMsg struct {
    ChannelID string
    MessageTS string
    UserID    string
    Emoji     string
}
```

### RTM Handler Implementation

Replace the TODO stubs in `main.go`:

**`OnReactionAdded(channelID, ts, userID, emoji)`:**
1. Update cache: get existing reaction row, append userID to user_ids, increment count. If no row exists, create with count=1.
2. Send `ReactionAddedMsg` to the bubbletea program via `p.Send()`.

**`OnReactionRemoved(channelID, ts, userID, emoji)`:**
1. Update cache: remove userID from user_ids, decrement count. If count hits 0, delete the row.
2. Send `ReactionRemovedMsg` to the program.

### App.Update Handling

**On `ReactionAddedMsg`:**
- Find the message in `messages.Model` by TS. If found, update its `Reactions` slice: increment existing `ReactionItem` count or append a new one. Set `HasReacted = true` if userID matches current user.
- Check `thread.Model` too -- if the message is displayed there, update it as well.
- Invalidate render cache.

**On `ReactionRemovedMsg`:**
- Find the message, decrement matching reaction's count. If count hits 0, remove from slice. Clear `HasReacted` if userID was current user.
- Check thread panel as well.
- Invalidate render cache.

### Optimistic UI

When the user adds/removes a reaction via the picker or quick-toggle:
1. Update the local `MessageItem.Reactions` immediately.
2. Invalidate render cache so the pill appears/disappears instantly.
3. Fire the API call asynchronously.
4. When the WebSocket echo arrives, it's a no-op (state already matches).
5. If the API call fails, roll back the optimistic update and show an error in the status bar.

### Reaction-Nav Edge Case

If a real-time removal deletes the reaction at `reactionNavIndex`, clamp the index to the new length. If all reactions are removed, exit reaction-nav automatically.

## 6. App Integration

### New Mode

Add `ModeReactionPicker` to `internal/ui/mode.go`:

```go
const (
    ModeNormal Mode = iota
    ModeInsert
    ModeCommand
    ModeSearch
    ModeChannelFinder
    ModeReactionPicker
)
```

### Key Routing in `handleNormalMode`

| Key | Condition | Action |
|-----|-----------|--------|
| `r` | Message pane or thread panel focused, message selected | Set `ModeReactionPicker`, call `reactionPicker.Open(channelID, messageTS, existingReactions)` |
| `R` | Message pane focused, selected message has reactions | Call `messages.EnterReactionNav()` |
| `R` | Thread panel focused, selected reply has reactions | Call `thread.EnterReactionNav()` (same logic) |

### Reaction-Nav Routing (within ModeNormal)

When `messages.ReactionNavActive()` is true, intercept keys before normal dispatch:

- `h/l/Enter/r/Esc` routed to reaction-nav methods
- All other keys suppressed (or exit reaction-nav first)

This is a sub-state within `ModeNormal`, not a separate mode. Checked at the top of `handleNormalMode`.

### New Mode Handler: `handleReactionPickerMode`

Translates `tea.KeyMsg` to string, delegates to `reactionPicker.HandleKey()`:
- If result returned: close picker, restore `ModeNormal`, fire optimistic update + API call
- If Esc: close picker, restore `ModeNormal`
- Record frecent usage on successful add via `cache.RecordEmojiUse()`

### View Compositing

In `App.View()`, after all panels are rendered:

```go
if a.reactionPicker.IsVisible() {
    screen = a.reactionPicker.ViewOverlay(a.width, a.height, screen)
}
```

Rendered after the channel finder check (both should never be visible simultaneously).

### Initialization

- `reactionPicker` created in `NewApp()` with emoji list from `emoji.CodeMap()`
- Frecent list loaded from cache on picker open (not startup -- keeps startup fast)
- `SetReactionSender(add, remove)` called in `main.go` during wiring

### Component Ownership

| Component | Owner | Location |
|-----------|-------|----------|
| Reaction picker overlay | `App` | `internal/ui/reactionpicker/` |
| Reaction-nav state | `messages.Model` | `internal/ui/messages/model.go` |
| Reaction-nav state (threads) | `thread.Model` | `internal/ui/thread/model.go` |
| Pill rendering | `messages.Model` + `thread.Model` | Respective `model.go` files |
| Cache CRUD | Cache layer | `internal/cache/reactions.go` |
| Frecent tracking | Cache layer | `internal/cache/frecent.go` |
| API callbacks | Wired in `main.go` | `cmd/slack-tui/main.go` |
| Real-time events | RTM handler | `cmd/slack-tui/main.go` |
| Message types | App | `internal/ui/app.go` |

## Existing Infrastructure

| Layer | What Exists | File |
|-------|------------|------|
| Slack API | `Client.AddReaction(ctx, channelID, ts, emoji)` | `internal/slack/client.go:390` |
| Slack API | `Client.RemoveReaction(ctx, channelID, ts, emoji)` | `internal/slack/client.go:395` |
| Slack API Interface | `AddReaction` + `RemoveReaction` on `SlackAPI` | `internal/slack/client.go:27-28` |
| WebSocket Events | `OnReactionAdded` + `OnReactionRemoved` dispatch | `internal/slack/events.go:99-111` |
| Event Handler Interface | `OnReactionAdded` + `OnReactionRemoved` methods | `internal/slack/events.go:11-12` |
| Cache Schema | `reactions` table with `(message_ts, channel_id, emoji)` PK | `internal/cache/db.go:95-102` |
| UI Model | `MessageItem.Reactions []ReactionItem` field | `internal/ui/messages/model.go` |
| UI Rendering | Basic reaction display (muted text) | `internal/ui/messages/model.go:236-243` |
| Key Binding | `r` bound to `Reaction` | `internal/ui/keys.go:50` |
| Emoji Library | `kyokomi/emoji/v2` for shortcode-to-Unicode | `go.mod` + `render.go` |
| Overlay Pattern | Channel finder as reference | `internal/ui/channelfinder/model.go` |
| Mode System | Extensible mode enum | `internal/ui/mode.go` |

## Out of Scope

- Custom/workspace emoji (Slack workspaces can add custom emoji -- not supported in this iteration)
- Skin tone variants (emoji skin tone modifiers)
- Emoji categories/tabs (search-first design makes these unnecessary)
- Reaction counts in sidebar/unread indicators
- Animated reaction effects
