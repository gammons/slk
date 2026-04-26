# Typing Indicators Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show typing indicators when others are typing in the current channel, and broadcast typing events when the user composes messages.

**Architecture:** Inbound typing events flow from the existing WebSocket dispatch through a new `UserTypingMsg` bubbletea message into a `typingUsers` map on `App` with 5-second TTL expiry. Outbound typing sends a JSON frame over the WebSocket with 3-second throttling. A single muted line renders between the message viewport and compose box.

**Tech Stack:** Go, bubbletea, lipgloss, gorilla/websocket

---

### Task 1: Add WebSocket Write Mutex and SendTyping Method

**Files:**
- Modify: `internal/slack/client.go:38-46` (Client struct), add new method
- Test: `internal/slack/client_test.go` (create)

- [ ] **Step 1: Write the failing test for SendTyping**

Create `internal/slack/client_test.go`:

```go
package slackclient

import (
	"testing"
)

func TestSendTypingReturnsErrorWhenNotConnected(t *testing.T) {
	c := &Client{}
	err := c.SendTyping("C123")
	if err == nil {
		t.Error("expected error when wsConn is nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/slack/ -run TestSendTyping -v`
Expected: FAIL — `SendTyping` method does not exist.

- [ ] **Step 3: Add wsMu mutex to Client struct and implement SendTyping**

In `internal/slack/client.go`, add `sync` import and modify the Client struct:

```go
import (
	"sync"
	// ... existing imports
)

type Client struct {
	api    SlackAPI
	wsConn *websocket.Conn
	wsMu   sync.Mutex
	wsDone chan struct{}
	teamID string
	userID string
	token  string
	cookie string
}
```

Add the `SendTyping` method:

```go
// SendTyping sends a typing indicator to the given channel via WebSocket.
func (c *Client) SendTyping(channelID string) error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	if c.wsConn == nil {
		return fmt.Errorf("websocket not connected")
	}
	msg := map[string]string{
		"type":    "typing",
		"channel": channelID,
	}
	return c.wsConn.WriteJSON(msg)
}
```

Also wrap the existing `WriteControl` call in the ping handler (inside `StartWebSocket`) with the mutex. Change the `SetPingHandler` closure:

```go
	conn.SetPingHandler(func(msg string) error {
		conn.SetReadDeadline(time.Now().Add(wsTimeout))
		c.wsMu.Lock()
		defer c.wsMu.Unlock()
		return conn.WriteControl(websocket.PongMessage, []byte(msg), time.Now().Add(10*time.Second))
	})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/slack/ -run TestSendTyping -v`
Expected: PASS

- [ ] **Step 5: Run all existing slack tests**

Run: `go test ./internal/slack/ -v`
Expected: All tests pass (existing event tests still work).

- [ ] **Step 6: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "feat: add SendTyping method with write mutex on WebSocket"
```

---

### Task 2: Add Typing Indicator Style

**Files:**
- Modify: `internal/ui/styles/styles.go:167` (add style at end of var block)

- [ ] **Step 1: Add TypingIndicator style**

In `internal/ui/styles/styles.go`, add before the closing `)` of the `var` block:

```go
	// Typing indicator
	TypingIndicator = lipgloss.NewStyle().
		Foreground(TextMuted).
		Italic(true).
		PaddingLeft(2)
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: Compiles successfully.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/styles/styles.go
git commit -m "feat: add typing indicator style"
```

---

### Task 3: Add Typing State Management and Inbound Message Handling

**Files:**
- Modify: `internal/ui/app.go:33-120` (message types), `internal/ui/app.go:156-204` (App struct), `internal/ui/app.go:239-433` (Update method)
- Test: `internal/ui/app_test.go` (add tests)

- [ ] **Step 1: Write failing tests for typing state management**

Add to `internal/ui/app_test.go`:

```go
func TestTypingStateAddAndExpire(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"

	// Simulate receiving a typing event
	app.addTypingUser("C1", "U1")

	users := app.getTypingUsers("C1")
	if len(users) != 1 || users[0] != "U1" {
		t.Errorf("expected [U1], got %v", users)
	}

	// Add another user
	app.addTypingUser("C1", "U2")
	users = app.getTypingUsers("C1")
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}

	// Expire all
	app.expireTypingUsers()
	// They shouldn't be expired yet (TTL is 5 seconds)
	users = app.getTypingUsers("C1")
	if len(users) != 2 {
		t.Errorf("expected 2 users still active, got %d", len(users))
	}
}

func TestTypingStateFiltersSelf(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.currentUserID = "U1"

	app.addTypingUser("C1", "U1")
	app.addTypingUser("C1", "U2")

	users := app.getTypingUsersFiltered("C1")
	if len(users) != 1 || users[0] != "U2" {
		t.Errorf("expected [U2] (self filtered), got %v", users)
	}
}

func TestTypingIndicatorText(t *testing.T) {
	app := NewApp()

	text := app.typingIndicatorText(nil)
	if text != "" {
		t.Errorf("expected empty for nil, got %q", text)
	}

	text = app.typingIndicatorText([]string{"Alice"})
	if text != "Alice is typing..." {
		t.Errorf("expected 'Alice is typing...', got %q", text)
	}

	text = app.typingIndicatorText([]string{"Alice", "Bob"})
	if text != "Alice and Bob are typing..." {
		t.Errorf("expected 'Alice and Bob are typing...', got %q", text)
	}

	text = app.typingIndicatorText([]string{"Alice", "Bob", "Charlie"})
	if text != "Several people are typing..." {
		t.Errorf("expected 'Several people are typing...', got %q", text)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run "TestTyping" -v`
Expected: FAIL — methods do not exist.

- [ ] **Step 3: Add message types**

In `internal/ui/app.go`, add inside the type block (after `LoadingTimeoutMsg`):

```go
	UserTypingMsg struct {
		ChannelID   string
		UserID      string
		WorkspaceID string
	}
	TypingExpiredMsg struct{}
```

- [ ] **Step 4: Add typing state fields to App struct**

In `internal/ui/app.go`, add to the App struct (in the "State" section):

```go
	// Typing indicators
	typingUsers    map[string]map[string]time.Time // channelID -> userID -> expiresAt
	typingTickerOn bool
	typingEnabled  bool
```

Add `"time"` to the import block if not already present.

- [ ] **Step 5: Initialize typingUsers in NewApp and add SetTypingEnabled setter**

In `NewApp()`, add after the existing field initializations:

```go
		typingUsers: make(map[string]map[string]time.Time),
```

Add a setter method:

```go
// SetTypingEnabled controls whether typing indicators are shown and sent.
func (a *App) SetTypingEnabled(enabled bool) {
	a.typingEnabled = enabled
}
```

- [ ] **Step 6: Implement typing state helper methods**

Add to `internal/ui/app.go`:

```go
// addTypingUser records that a user is typing in a channel.
func (a *App) addTypingUser(channelID, userID string) {
	if a.typingUsers[channelID] == nil {
		a.typingUsers[channelID] = make(map[string]time.Time)
	}
	a.typingUsers[channelID][userID] = time.Now().Add(5 * time.Second)
}

// expireTypingUsers removes expired typing entries.
func (a *App) expireTypingUsers() {
	now := time.Now()
	for ch, users := range a.typingUsers {
		for uid, expires := range users {
			if now.After(expires) {
				delete(users, uid)
			}
		}
		if len(users) == 0 {
			delete(a.typingUsers, ch)
		}
	}
}

// getTypingUsers returns user IDs currently typing in the given channel.
func (a *App) getTypingUsers(channelID string) []string {
	users := a.typingUsers[channelID]
	if len(users) == 0 {
		return nil
	}
	now := time.Now()
	var result []string
	for uid, expires := range users {
		if now.Before(expires) {
			result = append(result, uid)
		}
	}
	return result
}

// getTypingUsersFiltered returns typing user IDs excluding the current user.
func (a *App) getTypingUsersFiltered(channelID string) []string {
	all := a.getTypingUsers(channelID)
	var filtered []string
	for _, uid := range all {
		if uid != a.currentUserID {
			filtered = append(filtered, uid)
		}
	}
	return filtered
}

// typingIndicatorText formats the typing indicator string from display names.
func (a *App) typingIndicatorText(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0] + " is typing..."
	case 2:
		return names[0] + " and " + names[1] + " are typing..."
	default:
		return "Several people are typing..."
	}
}
```

- [ ] **Step 7: Handle UserTypingMsg and TypingExpiredMsg in Update()**

In `internal/ui/app.go`, add cases to the `switch msg := msg.(type)` block in `Update()`:

```go
	case UserTypingMsg:
		if !a.typingEnabled {
			return a, nil
		}
		a.addTypingUser(msg.ChannelID, msg.UserID)
		if !a.typingTickerOn {
			a.typingTickerOn = true
			cmds = append(cmds, tea.Tick(time.Second, func(time.Time) tea.Msg {
				return TypingExpiredMsg{}
			}))
		}

	case TypingExpiredMsg:
		a.expireTypingUsers()
		// Continue ticking if there are still active typers
		hasTypers := len(a.typingUsers) > 0
		a.typingTickerOn = hasTypers
		if hasTypers {
			cmds = append(cmds, tea.Tick(time.Second, func(time.Time) tea.Msg {
				return TypingExpiredMsg{}
			}))
		}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run "TestTyping" -v`
Expected: PASS

- [ ] **Step 9: Run all UI tests**

Run: `go test ./internal/ui/ -v`
Expected: All tests pass.

- [ ] **Step 10: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "feat: add typing state management with TTL expiry"
```

---

### Task 4: Render Typing Indicator in View

**Files:**
- Modify: `internal/ui/app.go:1329-1338` (View method, message panel rendering)

- [ ] **Step 1: Write a test for typing indicator rendering**

Add to `internal/ui/app_test.go`:

```go
func TestRenderTypingIndicator(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.typingEnabled = true
	app.currentUserID = "U_SELF"

	// Set up user names
	app.messagepane.SetUserNames(map[string]string{"U1": "Alice", "U2": "Bob"})

	// No one typing — should return empty
	line := app.renderTypingLine()
	if line != "" {
		t.Errorf("expected empty, got %q", line)
	}

	// One person typing
	app.addTypingUser("C1", "U1")
	line = app.renderTypingLine()
	if line == "" {
		t.Error("expected typing indicator, got empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestRenderTypingIndicator -v`
Expected: FAIL — `renderTypingLine` does not exist.

- [ ] **Step 3: Implement renderTypingLine**

Add to `internal/ui/app.go`:

```go
// renderTypingLine returns the styled typing indicator for the current channel,
// or an empty string if no one is typing.
func (a *App) renderTypingLine() string {
	if !a.typingEnabled {
		return ""
	}
	userIDs := a.getTypingUsersFiltered(a.activeChannelID)
	if len(userIDs) == 0 {
		return ""
	}

	// Resolve user IDs to display names
	names := make([]string, 0, len(userIDs))
	for _, uid := range userIDs {
		name := a.messagepane.ResolveUserName(uid)
		if name == "" {
			name = uid
		}
		names = append(names, name)
	}

	text := a.typingIndicatorText(names)
	return styles.TypingIndicator.Render(text)
}
```

- [ ] **Step 4: Check if messagepane has a ResolveUserName method; if not, add it**

The `messages.Model` has a `userNames` map set via `SetUserNames`. We need a getter. Add to `internal/ui/messages/model.go`:

```go
// ResolveUserName returns the display name for a user ID, or empty string if unknown.
func (m *Model) ResolveUserName(userID string) string {
	if m.userNames == nil {
		return ""
	}
	return m.userNames[userID]
}
```

- [ ] **Step 5: Insert typing line into View()**

In `internal/ui/app.go`, modify the message panel rendering section. Change:

```go
	msgView := a.messagepane.View(msgContentHeight, msgWidth-2)
	msgInner := lipgloss.JoinVertical(lipgloss.Left, msgView, composeView)
```

To:

```go
	typingLine := a.renderTypingLine()
	typingHeight := 0
	if typingLine != "" {
		typingHeight = 1
	}
	msgContentHeight := contentHeight - 2 - composeHeight - typingHeight
	if msgContentHeight < 3 {
		msgContentHeight = 3
	}
	msgView := a.messagepane.View(msgContentHeight, msgWidth-2)
	if typingLine != "" {
		msgInner = lipgloss.JoinVertical(lipgloss.Left, msgView, typingLine, composeView)
	} else {
		msgInner = lipgloss.JoinVertical(lipgloss.Left, msgView, composeView)
	}
```

Note: The existing `msgContentHeight` calculation (lines 1333-1336) needs to be moved after the `typingHeight` calculation. The full block should be:

```go
	a.compose.SetWidth(msgWidth - 2)
	composeView := a.compose.View(msgWidth-2, a.mode == ModeInsert && a.focusedPanel != PanelThread)
	composeHeight := lipgloss.Height(composeView)
	typingLine := a.renderTypingLine()
	typingHeight := 0
	if typingLine != "" {
		typingHeight = 1
	}
	msgContentHeight := contentHeight - 2 - composeHeight - typingHeight
	if msgContentHeight < 3 {
		msgContentHeight = 3
	}
	msgView := a.messagepane.View(msgContentHeight, msgWidth-2)
	var msgInner string
	if typingLine != "" {
		msgInner = lipgloss.JoinVertical(lipgloss.Left, msgView, typingLine, composeView)
	} else {
		msgInner = lipgloss.JoinVertical(lipgloss.Left, msgView, composeView)
	}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestRenderTypingIndicator -v`
Expected: PASS

- [ ] **Step 7: Run full build**

Run: `go build ./...`
Expected: Compiles successfully.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go internal/ui/messages/model.go
git commit -m "feat: render typing indicator line between messages and compose"
```

---

### Task 5: Wire Inbound Typing Events from WebSocket to UI

**Files:**
- Modify: `cmd/slk/main.go:872-874` (OnUserTyping stub)

- [ ] **Step 1: Fill in OnUserTyping to send UserTypingMsg**

In `cmd/slk/main.go`, replace the stub:

```go
func (h *rtmEventHandler) OnUserTyping(channelID, userID string) {
	// TODO: implement typing indicators in UI
}
```

With:

```go
func (h *rtmEventHandler) OnUserTyping(channelID, userID string) {
	if h.program == nil {
		return
	}
	h.program.Send(ui.UserTypingMsg{
		ChannelID:   channelID,
		UserID:      userID,
		WorkspaceID: h.workspaceID,
	})
}
```

- [ ] **Step 2: Wire typing enabled from config**

In `cmd/slk/main.go`, in the section where `app` is configured (near where other setters are called), add:

```go
app.SetTypingEnabled(cfg.Animations.TypingIndicators)
```

This should go near the other `app.Set*` calls, before the bubbletea program is started.

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: Compiles successfully.

- [ ] **Step 4: Run all tests**

Run: `go test ./...`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat: wire inbound typing events from WebSocket to UI"
```

---

### Task 6: Add Outbound Typing with Throttling

**Files:**
- Modify: `internal/ui/app.go` (App struct, handleInsertMode, callback type)
- Modify: `cmd/slk/main.go` (wireCallbacks)
- Test: `internal/ui/app_test.go`

- [ ] **Step 1: Write failing test for typing throttle**

Add to `internal/ui/app_test.go`:

```go
func TestTypingThrottle(t *testing.T) {
	app := NewApp()
	app.typingEnabled = true
	app.activeChannelID = "C1"

	// First call should allow sending
	if !app.shouldSendTyping() {
		t.Error("expected first typing send to be allowed")
	}

	// Mark as just sent
	app.lastTypingSent = time.Now()

	// Immediate second call should be throttled
	if app.shouldSendTyping() {
		t.Error("expected typing send to be throttled")
	}

	// After 3 seconds, should allow again
	app.lastTypingSent = time.Now().Add(-4 * time.Second)
	if !app.shouldSendTyping() {
		t.Error("expected typing send to be allowed after 3s")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestTypingThrottle -v`
Expected: FAIL — `shouldSendTyping` and `lastTypingSent` do not exist.

- [ ] **Step 3: Add outbound typing fields and callback type**

In `internal/ui/app.go`, add to the App struct:

```go
	// Outbound typing
	typingSendFn   TypingSendFunc
	lastTypingSent time.Time
```

Add the callback type (near other func types):

```go
// TypingSendFunc is called to broadcast a typing indicator.
type TypingSendFunc func(channelID string)
```

Add the setter:

```go
// SetTypingSender sets the callback for sending typing indicators.
func (a *App) SetTypingSender(fn TypingSendFunc) {
	a.typingSendFn = fn
}
```

- [ ] **Step 4: Implement shouldSendTyping**

```go
// shouldSendTyping returns true if enough time has passed since the last typing send.
func (a *App) shouldSendTyping() bool {
	if !a.typingEnabled {
		return false
	}
	return time.Since(a.lastTypingSent) >= 3*time.Second
}
```

- [ ] **Step 5: Hook typing send into handleInsertMode**

In `internal/ui/app.go`, in the `handleInsertMode` method, add typing send logic. At the end of the method, just before the final `return cmd` (the section that forwards keys to the compose box), change:

```go
	// Forward other keys to compose box
	var cmd tea.Cmd
	a.compose, cmd = a.compose.Update(msg)
	return cmd
```

To:

```go
	// Forward other keys to compose box
	var cmd tea.Cmd
	a.compose, cmd = a.compose.Update(msg)
	a.maybeSendTyping()
	return cmd
```

Similarly, in the thread compose section, after `a.threadCompose, cmd = a.threadCompose.Update(msg)` (the last else branch for thread compose), add `a.maybeSendTyping()` before `return cmd`.

Add the helper:

```go
// maybeSendTyping sends a typing indicator if the throttle allows it.
func (a *App) maybeSendTyping() {
	if a.typingSendFn == nil || !a.shouldSendTyping() {
		return
	}
	a.lastTypingSent = time.Now()
	channelID := a.activeChannelID
	if a.focusedPanel == PanelThread && a.threadVisible {
		channelID = a.threadPanel.ChannelID()
	}
	go a.typingSendFn(channelID)
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestTypingThrottle -v`
Expected: PASS

- [ ] **Step 7: Wire the typing sender in main.go**

In `cmd/slk/main.go`, inside `wireCallbacks`, add after the existing setter calls:

```go
		app.SetTypingSender(func(channelID string) {
			_ = client.SendTyping(channelID)
		})
```

- [ ] **Step 8: Verify build**

Run: `go build ./...`
Expected: Compiles successfully.

- [ ] **Step 9: Run all tests**

Run: `go test ./...`
Expected: All tests pass.

- [ ] **Step 10: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go cmd/slk/main.go
git commit -m "feat: add outbound typing indicators with 3-second throttle"
```

---

### Task 7: Clear Typing State on Channel Switch

**Files:**
- Modify: `internal/ui/app.go` (ChannelSelectedMsg handler)
- Test: `internal/ui/app_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/ui/app_test.go`:

```go
func TestTypingClearedOnChannelSwitch(t *testing.T) {
	app := NewApp()
	app.typingEnabled = true
	app.activeChannelID = "C1"

	app.addTypingUser("C1", "U1")
	app.addTypingUser("C2", "U2")

	// Typing indicator should show for C1
	users := app.getTypingUsersFiltered("C1")
	if len(users) != 1 {
		t.Errorf("expected 1 user typing in C1, got %d", len(users))
	}

	// After "switching" to C2, reset throttle
	app.activeChannelID = "C2"
	app.lastTypingSent = time.Time{} // reset throttle on switch

	// C2 should show its typers
	users = app.getTypingUsersFiltered("C2")
	if len(users) != 1 {
		t.Errorf("expected 1 user typing in C2, got %d", len(users))
	}
}
```

- [ ] **Step 2: Run test to verify it passes (this should already work)**

Run: `go test ./internal/ui/ -run TestTypingClearedOnChannelSwitch -v`
Expected: PASS — the typing state is per-channel, so switching `activeChannelID` naturally shows the right channel's typers.

- [ ] **Step 3: Reset outbound typing throttle on channel switch**

In `internal/ui/app.go`, in the `ChannelSelectedMsg` handler, add after `a.activeChannelID = msg.ID`:

```go
		a.lastTypingSent = time.Time{} // reset typing throttle for new channel
```

- [ ] **Step 4: Run all tests**

Run: `go test ./internal/ui/ -v`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "feat: reset typing throttle on channel switch"
```

---

### Task 8: Update STATUS.md

**Files:**
- Modify: `docs/STATUS.md`

- [ ] **Step 1: Move typing indicators from "Not Yet Implemented" to "What's Working"**

In `docs/STATUS.md`, remove this line from the "Low Priority" section:
```
- [ ] Typing indicators
```

Add to the "Messages" section under "What's Working":
```
- [x] Typing indicators (show who's typing, broadcast your own typing)
```

- [ ] **Step 2: Commit**

```bash
git add docs/STATUS.md
git commit -m "docs: mark typing indicators as implemented"
```
