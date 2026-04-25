# Thread Panel Design Spec

Date: 2026-04-25

## Overview

Add a thread panel to the right side of the message pane for viewing and replying to threaded conversations. The panel opens when the user presses Enter on any message, shows the parent message and all replies, and supports composing thread replies.

## Component Architecture

### New Package: `internal/ui/thread/`

A `Model` struct following the existing component pattern (no `tea.Model` interface, pointer-receiver setter methods, `View(height, width int) string`).

**State:**

| Field | Type | Purpose |
|-------|------|---------|
| `parentMsg` | `messages.MessageItem` | Thread parent, displayed at top of panel |
| `replies` | `[]messages.MessageItem` | Thread replies, ordered by ts ascending |
| `channelID` | `string` | Channel the thread belongs to |
| `threadTS` | `string` | Parent message timestamp (thread identifier) |
| `selected` | `int` | Cursor position for j/k navigation in replies |
| `scrollOffset` | `int` | Viewport scroll position |
| `focused` | `bool` | Whether the panel has keyboard focus |
| `replyCount` | `int` | Reply count for the header display |

**Methods:**

- `SetThread(parent MessageItem, replies []MessageItem, channelID, threadTS string)` -- populate the panel with a thread
- `AddReply(msg MessageItem)` -- append a new reply (from RTM or after sending)
- `MoveUp()` / `MoveDown()` -- cursor navigation within replies
- `ScrollToBottom()` -- jump to newest reply
- `View(height, width int) string` -- render the panel
- `Clear()` -- reset all state (used on channel switch)
- `ThreadTS() string` -- return the currently open thread's timestamp
- `IsEmpty() bool` -- whether a thread is loaded
- `SetFocused(bool)` / `Focused() bool` -- focus state

**Rendering:** Reuses `messages.RenderMessage()` for individual message rendering (markdown, emoji, avatars). No duplication of the rendering pipeline.

### Panel Layout

**Header:** "Thread" label + reply count (e.g., "Thread  4 replies"). Separated from content by a horizontal rule.

**Body:** Parent message at top with a separator below it, then a "N replies" divider, then reply messages rendered with the same format as the main pane.

**Footer:** Reply compose input -- a second `compose.Model` instance owned by the thread panel, same visual style as the main compose box. The App dispatches `tea.Msg` to the thread's compose model when the thread panel is focused and the mode is Insert.

## Layout Integration

### Width Calculation

When `threadVisible == true`, the message area (everything right of the sidebar) splits between the message pane and thread panel:

- Message pane: 65% of message area width
- Thread panel: 35% of message area width
- Rail and sidebar widths are unchanged

**Minimum widths:** Thread panel minimum is 30 columns. Message pane minimum is 40 columns. If the terminal is too narrow to satisfy both minimums, the thread panel auto-hides (sets `threadVisible = false`).

### Focus Cycle

`FocusNext()` / `FocusPrev()` (Tab / Shift+Tab) cycle:

- Without thread: Sidebar -> Messages -> Sidebar
- With thread visible: Sidebar -> Messages -> Thread -> Sidebar

`h/l` directional focus works the same way, moving between adjacent panels including Thread when visible.

The `PanelThread` enum value already exists in `app.go` but is currently unused. It will be wired into the focus management.

### Border Styling

Thread panel uses the same border styling as other panels:
- Focused: `styles.FocusedBorder` (blue rounded border)
- Unfocused: `styles.UnfocusedBorder` (gray rounded border)

### Active Thread Highlight

When a thread is open, the parent message in the main message pane gets a subtle visual indicator (e.g., left border highlight or background tint) to show which message's thread is being viewed.

## Keybindings

| Key | Context | Action |
|-----|---------|--------|
| `Enter` | Normal mode, messages pane focused, message selected | Open thread panel for the selected message |
| `Ctrl+]` | Normal mode | Toggle thread panel closed/open |
| `j/k` | Normal mode, thread panel focused | Scroll through thread replies |
| `i` | Normal mode, thread panel focused | Enter Insert mode for thread reply compose |
| `Enter` | Insert mode, thread compose focused | Send thread reply |
| `Esc` | Insert mode, thread compose | Return to Normal mode, thread panel stays focused |
| `gg/G` | Normal mode, thread panel focused | Jump to first/last reply |

## Data Flow

### Opening a Thread

1. User presses `Enter` on a message in the main pane
2. `App.Update()` emits a `ThreadOpenedMsg{ChannelID, ThreadTS, ParentMsg}`
3. App sets `threadVisible = true`, focuses thread panel
4. Fires a `tea.Cmd` calling the thread fetch function
5. Fetch function: tries `MessageService.GetThreadReplies()` (cache), then calls `Client.GetThreadReplies()` for fresh data from the API
6. Returns `ThreadRepliesLoadedMsg{ThreadTS, Replies []MessageItem}`
7. Thread panel renders parent + replies

### New Slack API Wrapper

`Client.GetThreadReplies(channelID, threadTS string) ([]slack.Message, error)` -- wraps the existing `SlackAPI.GetConversationReplies()` interface method (declared but currently has no wrapper on `Client`).

### Sending a Thread Reply

1. User types in thread compose box, presses Enter
2. `App.Update()` detects focus is on thread panel, dispatches `SendThreadReplyMsg{ChannelID, ThreadTS, Text}`
3. Calls `Client.SendReply()` (already implemented)
4. Reply appears via the RTM WebSocket event flow (same as channel messages)

### Real-Time Updates

Incoming WebSocket messages are routed based on context:

- If `threadVisible && msg.ThreadTS == openThreadTS`: route to thread panel via `AddReply()`
- If `msg.ThreadTS != ""` and it's the parent message: update reply count in the main pane
- Otherwise: route to main message pane as before

The RTM handler in `main.go` performs this check and dispatches accordingly.

### Channel Switch

Switching channels:
1. Calls `thread.Clear()`
2. Sets `threadVisible = false`
3. Resets focus to messages pane

## New Message Types

Added to `app.go`:

```go
type ThreadOpenedMsg struct {
    ChannelID string
    ThreadTS  string
    ParentMsg messages.MessageItem
}

type ThreadRepliesLoadedMsg struct {
    ThreadTS string
    Replies  []messages.MessageItem
}

type SendThreadReplyMsg struct {
    ChannelID string
    ThreadTS  string
    Text      string
}
```

## New Callback Functions

Added to `App`:

```go
type ThreadFetchFunc func(channelID, threadTS string) tea.Msg
type ThreadReplySendFunc func(channelID, threadTS, text string) tea.Msg
```

Set via `SetThreadFetcher()` and `SetThreadReplySender()`, wired in `main.go`.

## Mode Interaction

No new modes are needed. The existing `ModeInsert` is used for both channel compose and thread reply compose. The distinction is contextual -- based on which panel is focused when the user enters Insert mode:

- Thread panel focused + Insert mode = typing in thread reply compose
- Messages panel focused + Insert mode = typing in channel compose

The status bar updates to show context: `NORMAL | #engineering > Thread | myteam` when the thread panel is focused.

## Responsive Behavior

- If the terminal is resized below the minimum width threshold (message pane < 40 cols with thread open), the thread panel auto-hides
- If the terminal is resized back above the threshold, the thread panel does NOT auto-reopen (user must press Ctrl+] or Enter on a message)

## Existing Infrastructure

These pieces are already implemented and ready to use:

| Layer | What Exists | Status |
|-------|------------|--------|
| Cache schema | `messages` table with `thread_ts`, `reply_count` columns | Done |
| Cache index | `idx_messages_thread` on `(thread_ts, channel_id)` | Done |
| Cache query | `GetThreadReplies(channelID, threadTS)` | Done + tested |
| Service | `MessageService.GetThreadReplies()` wrapper | Done |
| Slack API | `SendReply(channelID, threadTS, text)` | Done |
| Slack API | `SlackAPI.GetConversationReplies` interface method | Declared, needs wrapper |
| Events | `thread_ts` parsed from WebSocket messages | Done |
| UI enum | `PanelThread = 3` constant | Defined, unused |
| UI field | `threadVisible bool` on App | Defined, always false |
| Keybinding | `ToggleThread` bound to `ctrl+]` | Defined, unhandled |
| Messages | `ThreadTS`, `ReplyCount` on `MessageItem` | Done |
| Messages | `[N replies ->]` indicator rendering | Done |

## Out of Scope

- "Also send to channel" toggle on thread replies
- Thread-level notifications
- Thread unread indicators
- Infinite scroll within threads (fetch all replies upfront; threads are typically small)
