# WebSocket Reconnection & New Message Landmark

Date: 2026-04-26

## Overview

Two independent improvements: (1) automatic WebSocket reconnection with exponential backoff so slk survives network interruptions, and (2) a red "new" landmark in the message list showing where unread messages begin, with mark-as-read synced back to Slack.

---

## 1. WebSocket Reconnection

### Problem

When the WebSocket disconnects (network blip, Slack server restart, laptop sleep), slk shows "Disconnected" in the status bar and never recovers. The user must restart the app.

### Design

#### ConnectionManager

New struct in `internal/slack/connection.go`:

```go
type ConnectionManager struct {
    client  *Client
    handler EventHandler
    ctx     context.Context
    cancel  context.CancelFunc
}
```

**`Run(ctx context.Context)`** — long-running method, called in a goroutine from `main.go`. Loop:

1. Call `client.StartWebSocket(handler)` to connect
2. If connect fails, enter backoff loop
3. If connect succeeds, wait for the WebSocket goroutine to exit (disconnect detected)
4. On disconnect, enter backoff loop
5. Repeat until `ctx.Done()`

#### Exponential Backoff

Delays: 1s, 2s, 4s, 8s, 16s, 30s (max). Reset to 1s on successful connection (when `OnConnect` fires via the `"hello"` event).

```go
backoff := 1 * time.Second
maxBackoff := 30 * time.Second

for {
    err := cm.client.StartWebSocket(cm.handler)
    if err != nil {
        // Connection failed — backoff and retry
        handler.OnDisconnect()
        select {
        case <-ctx.Done():
            return
        case <-time.After(backoff):
        }
        backoff = min(backoff*2, maxBackoff)
        continue
    }

    // Connected — reset backoff and wait for disconnect
    backoff = 1 * time.Second
    <-cm.client.WsDone()

    // Disconnected — backoff and retry
    select {
    case <-ctx.Done():
        return
    case <-time.After(backoff):
    }
    backoff = min(backoff*2, maxBackoff)
}
```

#### WsDone Channel

Add a `wsDone chan struct{}` to `Client`. The WebSocket read goroutine closes it when it exits. `ConnectionManager.Run()` blocks on `<-cm.client.WsDone()` to detect disconnect. `StartWebSocket` creates a fresh channel each time.

#### Status Bar Transitions

The existing `OnConnect`/`OnDisconnect` callbacks handle this:
- On disconnect: status bar shows red "Disconnected"
- On reconnect attempt: send `ConnectionStateMsg{State: StateConnecting}` before calling `StartWebSocket`
- On successful reconnect: `OnConnect` fires via the `"hello"` event, status bar shows green "Connected"

Add a `handler.OnReconnecting()` call (or send `StateConnecting` from the manager) before each retry so the status bar shows yellow "Connecting" during backoff.

#### Reconnect Signal to App

Add a new message type:

```go
type ReconnectedMsg struct{}
```

The `OnConnect` callback in the RTM handler distinguishes first connect from reconnect (track with a `connected bool` on the handler). On reconnect, send `ReconnectedMsg` to the app. The app can optionally refresh the current channel's messages to catch any missed during the disconnect.

#### Integration in main.go

Replace:
```go
if err := activeClient.StartWebSocket(handler); err != nil {
    log.Printf("Warning: failed to start WebSocket: %v", err)
}
```

With:
```go
connMgr := slackclient.NewConnectionManager(activeClient, handler)
go connMgr.Run(ctx)
defer connMgr.Stop()
```

Remove `defer activeClient.StopWebSocket()` — the connection manager owns the lifecycle now.

---

## 2. New Message Landmark

### Problem

When switching to a channel with unread messages, there's no visual indicator of where new (unread) messages begin.

### Design

#### Data: Extract last_read_ts from Slack API

The `client.counts` API response (already fetched at startup for unread counts) includes per-channel `last_read` timestamps. Currently these are ignored.

**Changes to `GetUnreadCounts()` in `client.go`:**

Update `UnreadInfo` to include `LastRead`:

```go
type UnreadInfo struct {
    ChannelID string
    Count     int
    LastRead  string // Slack message timestamp, e.g. "1700000001.000000"
}
```

Extract `last_read` from the API response JSON for each channel/mpim/im entry.

**Changes to cache:**

The `channels` table already has a `last_read_ts` column. When processing unread counts in `main.go`, call:

```go
db.UpdateLastReadTS(channelID, unreadInfo.LastRead)
```

Add this method to `internal/cache/channels.go`:

```go
func (db *DB) UpdateLastReadTS(channelID, ts string) error {
    _, err := db.conn.Exec(
        `UPDATE channels SET last_read_ts = ? WHERE id = ?`,
        ts, channelID,
    )
    return err
}
```

#### UI: Render the Landmark

**New separator style** in `internal/ui/styles/styles.go`:

```go
NewMessageSeparator = lipgloss.NewStyle().
    Foreground(Error).
    Bold(true).
    Align(lipgloss.Center)
```

**In `messages.Model`:**

Add a `lastReadTS string` field. Set via `SetLastReadTS(ts string)`.

**In `buildCache()`**, after the day separator check and before rendering the message, insert a new-message landmark:

```go
if m.lastReadTS != "" && !newMsgLandmarkInserted {
    if msg.TS > m.lastReadTS {
        label := newMessageSeparatorStyle.Width(width).Render("── new ──")
        m.cache = append(m.cache, viewEntry{
            content: label,
            height:  lipgloss.Height(label),
            msgIdx:  -1,
        })
        newMsgLandmarkInserted = true
    }
}
```

The `newMsgLandmarkInserted` flag ensures only one landmark is inserted per render.

**Edge cases:**
- No unread messages (all TS <= lastReadTS): no landmark
- All messages unread (lastReadTS is empty or before all messages): landmark before the first message
- Real-time messages arrive after landmark: they naturally appear after it (correct)

#### Wiring: Pass lastReadTS When Loading Messages

In `main.go`'s channel fetch callback, after loading messages:

1. Look up `lastReadTS` from the unread map (built from `GetUnreadCounts()`)
2. Call `app.SetLastReadTS(channelID, lastReadTS)` before setting messages

In `app.go`, the `MessagesLoadedMsg` handler calls `m.messagepane.SetLastReadTS(ts)` before `SetMessages()`.

Alternatively, add `LastReadTS` to `MessagesLoadedMsg` and handle it in one place.

#### Mark as Read

**New Slack API method** in `client.go`:

```go
func (c *Client) MarkChannel(ctx context.Context, channelID, ts string) error {
    // POST https://slack.com/api/conversations.mark
    // params: channel=channelID, ts=ts
    // Uses xoxc token + d cookie auth (same as other API calls)
}
```

**When to call:**

In the channel fetch callback in `main.go`, after messages are loaded successfully:

```go
// Mark channel as read up to the latest message
if len(msgItems) > 0 {
    latestTS := msgItems[len(msgItems)-1].TS
    go func() {
        _ = client.MarkChannel(ctx, channelID, latestTS)
        _ = db.UpdateLastReadTS(channelID, latestTS)
    }()
}
```

Fire-and-forget in a goroutine — don't block the UI. Also update the local cache so the landmark won't show next time.

**Clear sidebar unread:** After marking, send a message to clear the unread indicator on the sidebar item for that channel:

```go
type ChannelMarkedReadMsg struct {
    ChannelID string
}
```

Handle in `App.Update` by calling `a.sidebar.ClearUnread(channelID)`.

Add `ClearUnread(channelID string)` to the sidebar model.

**When NOT to mark:**
- Don't mark on reconnect message refresh unless the user is actively viewing that channel
- Don't mark on every scroll or real-time message — only on channel entry

---

## Existing Infrastructure

| Layer | What Exists | Relevant To |
|-------|------------|-------------|
| WebSocket | `StartWebSocket`, `StopWebSocket`, `EventHandler` interface | Reconnection |
| Status bar | Three-state connection indicator (green/yellow/red) | Reconnection |
| `OnConnect`/`OnDisconnect` | Already fire on connect/disconnect | Reconnection |
| `GetUnreadCounts()` | Fetches from `client.counts` API | Landmark |
| `channels.last_read_ts` | Column exists in cache schema, unused | Landmark |
| Day separator pattern | `viewEntry{msgIdx: -1}` insertion in `buildCache()` | Landmark |
| `UnreadInfo` struct | Has `ChannelID` and `Count` | Landmark |

## Out of Scope

- Periodic unread count refresh (only at startup + reconnect)
- Storing `reconnect_url` from WebSocket events (unnecessary with full reconnect)
- Typing indicators / presence updates (separate features)
- Mark-as-read on individual message scroll (only on channel entry)
