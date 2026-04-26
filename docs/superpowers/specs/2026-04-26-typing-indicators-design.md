# Typing Indicators Design

Date: 2026-04-26

## Overview

Add typing indicators to slk: show when other users are typing in the current channel, and broadcast your own typing to others. Displayed as a single muted line between the message viewport and compose box.

## Existing Infrastructure

The following pieces are already in place:

- **WebSocket event parsing:** `user_typing` events are parsed into `wsTypingEvent{Channel, User}` and dispatched via `handler.OnUserTyping(channelID, userID)` (`internal/slack/events.go:120-125`)
- **EventHandler interface:** `OnUserTyping(channelID, userID string)` is defined (`internal/slack/events.go:14`)
- **Stub implementation:** `rtmEventHandler.OnUserTyping()` exists with a TODO comment (`cmd/slk/main.go:872-874`)
- **Config toggle:** `Config.Animations.TypingIndicators` exists and defaults to `true` (`internal/config/config.go:42,69`)
- **Test coverage:** `TestDispatchWebSocketUserTypingEvent` verifies event dispatch (`internal/slack/events_test.go:131-143`)

## Inbound: Receiving Typing Indicators

### State Tracking

`App` gets a new field:

```go
typingUsers map[string]map[string]time.Time // channelID -> userID -> expiresAt
```

When a `UserTypingMsg` arrives, set the expiry to `now + 5s`. A background ticker fires `TypingExpiredMsg` every 1 second to prune stale entries. The ticker starts when the first typing event arrives and stops when the map is empty.

### Message Types

Two new bubbletea message types in `app.go`:

```go
type UserTypingMsg struct {
    ChannelID   string
    UserID      string
    WorkspaceID string
}

type TypingExpiredMsg struct{}
```

### Message Flow

1. WebSocket receives `user_typing` JSON
2. `dispatchWebSocketEvent` parses it and calls `handler.OnUserTyping(channelID, userID)`
3. `rtmEventHandler.OnUserTyping()` calls `h.program.Send(ui.UserTypingMsg{...})`
4. `App.Update` handles `UserTypingMsg`: updates `typingUsers[channelID][userID] = now + 5s`, starts expiry ticker if not running
5. `App.Update` handles `TypingExpiredMsg`: prunes entries where `expiresAt < now`, stops ticker if map is empty

### Rendering

In `App.View()`, between `msgView` and `composeView`, render a typing indicator line when there are active typers for the current channel.

Format:
- 1 user: `Alice is typing...`
- 2 users: `Alice and Bob are typing...`
- 3+ users: `Several people are typing...`

Style: dim/muted text, left-aligned. Resolve user IDs to display names using the existing `userNames` map.

Filter out the current user's own ID so "You are typing..." never appears.

When nobody is typing, the line is not rendered (the space is reclaimed by the message viewport). This means the message viewport height changes by 1 line when typing starts/stops, but this is a minor shift and matches Slack's behavior.

### Multi-workspace

The `UserTypingMsg` includes `WorkspaceID`. Typing state is only shown when the typing event's workspace matches the active workspace. When switching workspaces, the typing display automatically changes to show the new workspace's typing state (the map already tracks by channel ID, and channels are workspace-scoped).

## Outbound: Sending Typing Indicators

### WebSocket Write Method

Add to `Client`:

```go
func (c *Client) SendTyping(channelID string) error
```

This writes `{"type":"typing","channel":"<channelID>"}` to the WebSocket connection.

### Concurrency Protection

The WebSocket connection (`wsConn`) is currently only used for reads. Adding writes requires a `sync.Mutex` to protect against concurrent access. Add `wsMu sync.Mutex` to `Client` and lock it around `wsConn.WriteJSON()`.

### Throttling

Track `lastTypingSent time.Time` in `App` (per-channel is unnecessary; Slack only cares about the active channel). Only send a typing indicator if 3+ seconds have elapsed since the last send.

### Integration Point

In `App.handleInsertMode()`, when a keypress is forwarded to the compose component (i.e., the user is actively typing a message), check the throttle and fire a `tea.Cmd` that calls the typing send function. Same pattern for thread compose.

### Callback Wiring

Add `typingSendFunc func(channelID string)` to `App`, wired in `wireCallbacks()` in `main.go`. This follows the existing pattern used by `sendFunc`, `reactionFunc`, etc.

## Thread Support

The Slack browser WebSocket `user_typing` event does not include a `thread_ts` field, so thread-level typing is indistinguishable from channel-level typing. Typing indicators are shown only in the main channel area (between message viewport and compose box), not in the thread panel.

Outbound typing is sent when composing in either the main compose or thread compose, using the channel ID in both cases (Slack's typing API only accepts channel ID).

## Config Gating

Check `cfg.Animations.TypingIndicators` before:
- Rendering inbound typing indicators
- Sending outbound typing events

When disabled, `OnUserTyping` still receives events (to keep the interface simple) but `App.Update` skips processing them.

## Files to Modify

| File | Changes |
|------|---------|
| `internal/slack/client.go` | Add `SendTyping()`, add `wsMu sync.Mutex`, lock around WebSocket writes |
| `internal/ui/app.go` | Add `UserTypingMsg`, `TypingExpiredMsg`, `typingUsers` state, expiry ticker, rendering in `View()`, outbound throttle in `handleInsertMode()`, `typingSendFunc` callback |
| `cmd/slk/main.go` | Fill in `OnUserTyping()` to send `UserTypingMsg`, wire `typingSendFunc` in `wireCallbacks()` |
| `internal/ui/styles/styles.go` | Add `TypingIndicator` style (dim, italic) |

## Testing

- Unit test typing state management: add/expire/prune entries
- Unit test rendering: 0, 1, 2, 3+ typers produce correct output
- Unit test throttle: verify sends are suppressed within 3-second window
- Existing `TestDispatchWebSocketUserTypingEvent` already covers event parsing
