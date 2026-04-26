# Reconnection & New Message Landmark Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add automatic WebSocket reconnection with exponential backoff, and a red "new" message landmark showing where unread messages begin with mark-as-read synced to Slack.

**Architecture:** A `ConnectionManager` in `internal/slack/` owns the WebSocket lifecycle with backoff retry. The new-message landmark follows the existing day-separator pattern in `buildCache()`, driven by `last_read_ts` extracted from Slack's `client.counts` API. Mark-as-read calls `conversations.mark` on channel entry.

**Tech Stack:** Go, gorilla/websocket, bubbletea, lipgloss, SQLite

---

## File Structure

### New Files
| File | Responsibility |
|------|---------------|
| `internal/slack/connection.go` | ConnectionManager with exponential backoff reconnection |
| `internal/slack/connection_test.go` | Tests for backoff logic |

### Modified Files
| File | Changes |
|------|---------|
| `internal/slack/client.go` | Add `WsDone()` channel, `MarkChannel()` method, update `UnreadInfo` with `LastRead`, update `StartWebSocket` to manage done channel |
| `internal/cache/channels.go` | Add `UpdateLastReadTS()` method |
| `internal/ui/styles/styles.go` | Add `NewMessageSeparator` style |
| `internal/ui/messages/model.go` | Add `lastReadTS` field, landmark insertion in `buildCache()` |
| `internal/ui/sidebar/model.go` | Add `ClearUnread()` method |
| `internal/ui/app.go` | Add `ChannelMarkedReadMsg`, `ReconnectedMsg` handling |
| `cmd/slk/main.go` | Wire ConnectionManager, extract `last_read_ts`, call `MarkChannel`, pass `lastReadTS` to messages |

---

### Task 1: WsDone Channel on Client

**Files:**
- Modify: `internal/slack/client.go`

- [ ] **Step 1: Add wsDone channel to Client struct**

In `internal/slack/client.go`, add a `wsDone` field to the `Client` struct:

```go
type Client struct {
	api     SlackAPI
	wsConn  *websocket.Conn
	wsDone  chan struct{}
	teamID  string
	userID  string
	token   string
	cookie  string
}
```

Add accessor method:

```go
// WsDone returns a channel that is closed when the WebSocket read loop exits.
func (c *Client) WsDone() <-chan struct{} {
	return c.wsDone
}
```

- [ ] **Step 2: Create and close wsDone in StartWebSocket**

In `StartWebSocket`, create a fresh `wsDone` channel before launching the goroutine, and close it when the goroutine exits. Find the current goroutine launch (approximately line 135):

Change from:
```go
	go func() {
		defer handler.OnDisconnect()
		for {
```

To:
```go
	c.wsDone = make(chan struct{})
	go func() {
		defer handler.OnDisconnect()
		defer close(c.wsDone)
		for {
```

- [ ] **Step 3: Build to verify**

Run: `go build ./...`
Expected: Compiles

- [ ] **Step 4: Commit**

```bash
git add internal/slack/client.go
git commit -m "feat: add WsDone channel to Client for disconnect detection"
```

---

### Task 2: ConnectionManager

**Files:**
- Create: `internal/slack/connection.go`
- Create: `internal/slack/connection_test.go`

- [ ] **Step 1: Write tests for backoff logic**

Create `internal/slack/connection_test.go`:

```go
package slack

import (
	"testing"
	"time"
)

func TestBackoffSequence(t *testing.T) {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	expected := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		30 * time.Second, // should cap at max
	}

	for i, want := range expected {
		if backoff != want {
			t.Errorf("step %d: expected %v, got %v", i, want, backoff)
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func TestBackoffReset(t *testing.T) {
	backoff := 16 * time.Second
	// Simulate successful connection — reset
	backoff = 1 * time.Second
	if backoff != 1*time.Second {
		t.Errorf("expected 1s after reset, got %v", backoff)
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/slack/ -run TestBackoff -v`
Expected: PASS

- [ ] **Step 3: Implement ConnectionManager**

Create `internal/slack/connection.go`:

```go
package slack

import (
	"context"
	"log"
	"time"
)

// ConnectionManager manages the WebSocket connection lifecycle with
// automatic reconnection using exponential backoff.
type ConnectionManager struct {
	client  *Client
	handler EventHandler
	cancel  context.CancelFunc
}

// NewConnectionManager creates a new connection manager.
func NewConnectionManager(client *Client, handler EventHandler) *ConnectionManager {
	return &ConnectionManager{
		client:  client,
		handler: handler,
	}
}

// Run starts the connection loop. It connects, waits for disconnect,
// and reconnects with exponential backoff. Blocks until ctx is cancelled.
func (cm *ConnectionManager) Run(ctx context.Context) {
	ctx, cm.cancel = context.WithCancel(ctx)

	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Signal "Connecting" state
		cm.handler.OnDisconnect() // shows "Disconnected" briefly, then...
		log.Printf("WebSocket: connecting...")

		err := cm.client.StartWebSocket(cm.handler)
		if err != nil {
			log.Printf("WebSocket: connection failed: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}

		// Connected — reset backoff
		backoff = 1 * time.Second

		// Wait for disconnect
		select {
		case <-ctx.Done():
			cm.client.StopWebSocket()
			return
		case <-cm.client.WsDone():
			log.Printf("WebSocket: disconnected, will reconnect...")
		}

		// Brief pause before reconnect
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = nextBackoff(backoff, maxBackoff)
	}
}

// Stop cancels the connection loop and closes the WebSocket.
func (cm *ConnectionManager) Stop() {
	if cm.cancel != nil {
		cm.cancel()
	}
	cm.client.StopWebSocket()
}

func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}
```

- [ ] **Step 4: Build to verify**

Run: `go build ./...`
Expected: Compiles

- [ ] **Step 5: Commit**

```bash
git add internal/slack/connection.go internal/slack/connection_test.go
git commit -m "feat: add ConnectionManager with exponential backoff reconnection"
```

---

### Task 3: Wire ConnectionManager in main.go

**Files:**
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Add reconnect tracking to rtmEventHandler**

Add a `connected bool` field to `rtmEventHandler`:

```go
type rtmEventHandler struct {
	program     *tea.Program
	userNames   map[string]string
	tsFormat    string
	db          *cache.DB
	workspaceID string
	connected   bool
}
```

Update `OnConnect` to track first connect vs reconnect:

```go
func (h *rtmEventHandler) OnConnect() {
	if h.connected {
		// This is a reconnection
		log.Printf("WebSocket: reconnected")
	}
	h.connected = true
	h.program.Send(ui.ConnectionStateMsg{State: int(statusbar.StateConnected)})
}
```

- [ ] **Step 2: Replace StartWebSocket with ConnectionManager**

In `run()`, find the WebSocket setup block (approximately lines 366-380). Replace:

```go
	if activeClient != nil {
		handler := &rtmEventHandler{
			program:     p,
			userNames:   userNames,
			tsFormat:    tsFormat,
			db:          db,
			workspaceID: activeClient.TeamID(),
		}
		if err := activeClient.StartWebSocket(handler); err != nil {
			log.Printf("Warning: failed to start WebSocket: %v", err)
		} else {
			defer activeClient.StopWebSocket()
		}
	}
```

With:

```go
	if activeClient != nil {
		handler := &rtmEventHandler{
			program:     p,
			userNames:   userNames,
			tsFormat:    tsFormat,
			db:          db,
			workspaceID: activeClient.TeamID(),
		}
		connMgr := slackclient.NewConnectionManager(activeClient, handler)
		go connMgr.Run(ctx)
		defer connMgr.Stop()
	}
```

- [ ] **Step 3: Build and test**

Run: `go build ./...`
Expected: Compiles

Run: `go test ./...`
Expected: All pass

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat: wire ConnectionManager for automatic WebSocket reconnection"
```

---

### Task 4: Extract last_read_ts from Unread Counts API

**Files:**
- Modify: `internal/slack/client.go`
- Modify: `internal/cache/channels.go`
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Add LastRead field to UnreadInfo**

In `internal/slack/client.go`, update the `UnreadInfo` struct:

```go
type UnreadInfo struct {
	ChannelID string
	Count     int
	HasUnread bool
	LastRead  string // Slack message timestamp, e.g. "1700000001.000000"
}
```

- [ ] **Step 2: Extract last_read from API response**

In `GetUnreadCounts()`, the JSON response has per-channel objects. Each channel/mpim/im entry has a `last_read` field. Update the JSON parsing structs to include it.

Find the anonymous structs used for JSON parsing in `GetUnreadCounts()` and add `LastRead string` to each. These are approximately:

For the channels array items, add:
```go
LastRead string `json:"last_read"`
```

Then when building `UnreadInfo`, set:
```go
LastRead: ch.LastRead,
```

Do the same for `mpims` and `ims` entries.

- [ ] **Step 3: Add UpdateLastReadTS to cache**

In `internal/cache/channels.go`, add:

```go
// UpdateLastReadTS sets the last read timestamp for a channel.
func (db *DB) UpdateLastReadTS(channelID, ts string) error {
	_, err := db.conn.Exec(
		`UPDATE channels SET last_read_ts = ? WHERE id = ?`,
		ts, channelID,
	)
	if err != nil {
		return fmt.Errorf("updating last_read_ts: %w", err)
	}
	return nil
}

// GetLastReadTS returns the last read timestamp for a channel.
func (db *DB) GetLastReadTS(channelID string) (string, error) {
	var ts string
	err := db.conn.QueryRow(
		`SELECT last_read_ts FROM channels WHERE id = ?`,
		channelID,
	).Scan(&ts)
	if err != nil {
		return "", err
	}
	return ts, nil
}
```

Add `"fmt"` to the imports if not already present.

- [ ] **Step 4: Store last_read_ts in main.go during startup**

In `main.go`, after the unread count processing block (where `unreadMap` is built), also build a `lastReadMap`:

```go
		unreadMap := make(map[string]int)
		lastReadMap := make(map[string]string)
		for _, u := range unreadCounts {
			unreadMap[u.ChannelID] = u.Count
			if u.LastRead != "" {
				lastReadMap[u.ChannelID] = u.LastRead
				_ = db.UpdateLastReadTS(u.ChannelID, u.LastRead)
			}
		}
```

Store `lastReadMap` in a variable accessible to the channel fetch callback (it's already in the same closure scope).

- [ ] **Step 5: Build and test**

Run: `go build ./...`
Expected: Compiles

Run: `go test ./...`
Expected: All pass

- [ ] **Step 6: Commit**

```bash
git add internal/slack/client.go internal/cache/channels.go cmd/slk/main.go
git commit -m "feat: extract last_read_ts from Slack unread counts API"
```

---

### Task 5: New Message Landmark in Messages

**Files:**
- Modify: `internal/ui/styles/styles.go`
- Modify: `internal/ui/messages/model.go`

- [ ] **Step 1: Add new message separator style**

In `internal/ui/styles/styles.go`, add after the existing reaction pill styles (before the closing `)`):

```go
	// New message landmark
	NewMessageSeparator = lipgloss.NewStyle().
		Foreground(Error).
		Bold(true).
		Align(lipgloss.Center)
```

- [ ] **Step 2: Add lastReadTS field and setter to messages Model**

In `internal/ui/messages/model.go`, add to the `Model` struct:

```go
	lastReadTS string
```

Add setter method:

```go
// SetLastReadTS sets the timestamp of the last read message.
// Messages with TS > lastReadTS are considered unread.
func (m *Model) SetLastReadTS(ts string) {
	m.lastReadTS = ts
	m.cache = nil // invalidate render cache
}
```

- [ ] **Step 3: Insert landmark in buildCache**

In `buildCache()`, after the day separator insertion and before the message rendering, add the new-message landmark check. Find the loop in `buildCache()` — after the day separator block and before the `avatarStr` / `renderMessagePlain` call:

```go
	var lastDate string
	newMsgLandmarkInserted := false
	for i, msg := range m.messages {
		msgDate := dateFromTS(msg.TS)
		if msgDate != "" && msgDate != lastDate {
			label := dateSeparatorStyle.Width(width).Render("── " + formatDateSeparator(msgDate) + " ──")
			m.cache = append(m.cache, viewEntry{
				content: label,
				height:  lipgloss.Height(label),
				msgIdx:  -1,
			})
			lastDate = msgDate
		}

		// New message landmark: insert before the first unread message
		if m.lastReadTS != "" && !newMsgLandmarkInserted && msg.TS > m.lastReadTS {
			label := styles.NewMessageSeparator.Width(width).Render("── new ──")
			m.cache = append(m.cache, viewEntry{
				content: label,
				height:  lipgloss.Height(label),
				msgIdx:  -1,
			})
			newMsgLandmarkInserted = true
		}

		avatarStr := ""
```

Note: The landmark should be inserted AFTER the day separator (if any) for the same date, but BEFORE the message itself. This ordering is natural since the day separator check comes first.

- [ ] **Step 4: Build and test**

Run: `go build ./...`
Expected: Compiles

Run: `go test ./internal/ui/messages/ -v`
Expected: All pass

- [ ] **Step 5: Commit**

```bash
git add internal/ui/styles/styles.go internal/ui/messages/model.go
git commit -m "feat: add red 'new' message landmark for unread messages"
```

---

### Task 6: Sidebar ClearUnread and ChannelMarkedReadMsg

**Files:**
- Modify: `internal/ui/sidebar/model.go`
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add ClearUnread to sidebar Model**

In `internal/ui/sidebar/model.go`, add:

```go
// ClearUnread sets the unread count to 0 for the given channel.
func (m *Model) ClearUnread(channelID string) {
	for i := range m.items {
		if m.items[i].ID == channelID {
			m.items[i].UnreadCount = 0
			return
		}
	}
}
```

- [ ] **Step 2: Add ChannelMarkedReadMsg to app**

In `internal/ui/app.go`, add a new message type after the existing ones:

```go
	ChannelMarkedReadMsg struct {
		ChannelID string
	}
```

Handle it in `Update`:

```go
	case ChannelMarkedReadMsg:
		a.sidebar.ClearUnread(msg.ChannelID)
```

- [ ] **Step 3: Build and test**

Run: `go build ./...`
Expected: Compiles

Run: `go test ./...`
Expected: All pass

- [ ] **Step 4: Commit**

```bash
git add internal/ui/sidebar/model.go internal/ui/app.go
git commit -m "feat: add ClearUnread and ChannelMarkedReadMsg"
```

---

### Task 7: MarkChannel API and Wiring

**Files:**
- Modify: `internal/slack/client.go`
- Modify: `cmd/slk/main.go`
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add MarkChannel to Slack client**

In `internal/slack/client.go`, add:

```go
// MarkChannel marks a channel as read up to the given timestamp.
// Uses raw HTTP POST since slack-go doesn't expose conversations.mark.
func (c *Client) MarkChannel(ctx context.Context, channelID, ts string) error {
	data := url.Values{
		"channel": {channelID},
		"ts":      {ts},
	}

	req, err := http.NewRequest("POST", "https://slack.com/api/conversations.mark", strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("creating mark request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Cookie", "d="+c.cookie)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("marking channel: %w", err)
	}
	defer resp.Body.Close()
	return nil
}
```

Note: Check if `net/http`, `net/url`, `strings` are already imported; add them if not. Follow the same raw HTTP pattern used in `GetUnreadCounts()`.

- [ ] **Step 2: Pass lastReadTS in MessagesLoadedMsg**

In `internal/ui/app.go`, add `LastReadTS` to `MessagesLoadedMsg`:

```go
	MessagesLoadedMsg struct {
		ChannelID  string
		Messages   []messages.MessageItem
		LastReadTS string
	}
```

In the `MessagesLoadedMsg` handler, set it before loading messages:

```go
	case MessagesLoadedMsg:
		if msg.ChannelID == a.activeChannelID {
			a.messagepane.SetLastReadTS(msg.LastReadTS)
			a.messagepane.SetMessages(msg.Messages)
		}
```

- [ ] **Step 3: Wire channel fetch to include lastReadTS and mark-as-read**

In `cmd/slk/main.go`, update the channel fetcher callback. The `lastReadMap` is already in scope (from Task 4). Update the channel fetcher:

```go
		app.SetChannelFetcher(func(channelID, channelName string) tea.Msg {
			msgItems := fetchChannelMessages(client, channelID, db, userNames, tsFormat)

			lastReadTS := lastReadMap[channelID]

			// Mark channel as read up to the latest message
			if len(msgItems) > 0 {
				latestTS := msgItems[len(msgItems)-1].TS
				go func() {
					_ = client.MarkChannel(ctx, channelID, latestTS)
					_ = db.UpdateLastReadTS(channelID, latestTS)
					lastReadMap[channelID] = latestTS
					p.Send(ui.ChannelMarkedReadMsg{ChannelID: channelID})
				}()
			}

			return ui.MessagesLoadedMsg{
				ChannelID:  channelID,
				Messages:   msgItems,
				LastReadTS: lastReadTS,
			}
		})
```

Note: `p` (the `tea.Program`) needs to be accessible in this callback. Currently the channel fetcher is wired before `p` is created. You'll need to restructure slightly — either wire the fetcher after `p := tea.NewProgram(app, ...)`, or store `p` on a variable that's set after creation and captured by the closure. Check the current code flow and adjust. The simplest approach: declare `var p *tea.Program` before the callback wiring, then assign `p = tea.NewProgram(...)` later. The closure captures the variable, not the value.

Also update the initial channel load to pass lastReadTS:

```go
		if len(sidebarItems) > 0 {
			firstCh := sidebarItems[0]
			msgItems := fetchChannelMessages(client, firstCh.ID, db, userNames, tsFormat)
			lastReadTS := lastReadMap[firstCh.ID]
			if len(msgItems) > 0 {
				app.SetInitialChannel(firstCh.ID, firstCh.Name, msgItems)
				app.SetInitialLastReadTS(lastReadTS)
			}
		}
```

Add `SetInitialLastReadTS` to `App`:

```go
func (a *App) SetInitialLastReadTS(ts string) {
	a.messagepane.SetLastReadTS(ts)
}
```

- [ ] **Step 4: Build and test**

Run: `go build ./...`
Expected: Compiles

Run: `go test ./...`
Expected: All pass

- [ ] **Step 5: Commit**

```bash
git add internal/slack/client.go internal/ui/app.go cmd/slk/main.go
git commit -m "feat: wire mark-as-read and lastReadTS to message landmark"
```

---

### Task 8: Final Verification and STATUS.md

**Files:**
- Modify: `docs/STATUS.md`

- [ ] **Step 1: Full build and test**

Run: `go build ./...`
Expected: Clean compile

Run: `go test ./... -v`
Expected: All tests pass

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 2: Build binary and test manually**

Run: `make build`

Verify:
1. App connects and shows "Connected" in status bar
2. Kill network briefly — status bar shows "Disconnected", then "Connecting", then "Connected" on recovery
3. Switch to a channel with unread messages — red "── new ──" landmark appears between read and unread messages
4. After viewing a channel, switch away and back — landmark should not appear (channel was marked as read)

- [ ] **Step 3: Update STATUS.md**

Add under "### Core":
```
- [x] Automatic WebSocket reconnection with exponential backoff (1s-30s)
```

Add under "### Messages":
```
- [x] New message landmark ("── new ──" in red, marking unread boundary)
- [x] Mark-as-read synced to Slack via conversations.mark API
```

Remove from "Not Yet Implemented" if any of these were listed.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: complete reconnection and new message landmark"
```
