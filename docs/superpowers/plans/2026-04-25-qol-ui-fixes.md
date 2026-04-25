# QoL UI Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Four independent UI improvements: better insert mode indication, multi-line compose, workspace rail cleanup, and connection status indicators.

**Architecture:** Each fix is independent and touches a small number of files. The compose box swap (textinput -> textarea) is the most involved change. The connection status requires adding two methods to the EventHandler interface and wiring them through main.go.

**Tech Stack:** Go, bubbletea, bubbles/textarea, lipgloss

---

### Task 1: Workspace Rail -- Remove Border

**Files:**
- Modify: `internal/ui/workspace/model.go`

- [ ] **Step 1: Remove border from rail container style**

In `internal/ui/workspace/model.go`, replace the `rail` style block in `View()` (lines 78-88):

```go
	rail := lipgloss.NewStyle().
		Width(5).
		Height(height).
		MaxHeight(height).
		Background(styles.SurfaceDark).
		Padding(1, 0).
		BorderStyle(lipgloss.Border{Right: "│"}).
		BorderRight(true).
		BorderForeground(styles.Border).
		Align(lipgloss.Center).
		Render(content)
```

With:

```go
	rail := lipgloss.NewStyle().
		Width(5).
		Height(height).
		MaxHeight(height).
		Background(styles.SurfaceDark).
		Padding(1, 0).
		Align(lipgloss.Center).
		Render(content)
```

- [ ] **Step 2: Update Width() return value**

Replace line 94:

```go
	return 7 // 5 content + 1 border + 1 padding
```

With:

```go
	return 5 // 5 content, no border
```

- [ ] **Step 3: Verify it compiles and tests pass**

Run: `go build ./internal/ui/... && go test ./internal/ui/... -v`
Expected: All pass

- [ ] **Step 4: Commit**

```bash
git add internal/ui/workspace/model.go
git commit -m "fix: remove vertical border from workspace rail"
```

---

### Task 2: Insert Mode Indicator -- Compose Box Only

**Files:**
- Modify: `internal/ui/styles/styles.go`
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add ComposeInsert style**

In `internal/ui/styles/styles.go`, add after the `ComposeFocused` style:

```go
	ComposeInsert = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(Primary).
		Background(lipgloss.Color("#111128")).
		Padding(0, 1)
```

- [ ] **Step 2: Update compose View to use ComposeInsert**

In `internal/ui/compose/model.go`, update the `View()` method. Change the `focused` parameter usage:

Replace:
```go
func (m Model) View(width int, focused bool) string {
	m.input.Width = width - 4 // account for padding/border

	var style lipgloss.Style
	if focused {
		style = styles.ComposeFocused.Width(width - 2)
	} else {
		style = styles.ComposeBox.Width(width - 2)
	}

	return style.Render(m.input.View())
}
```

With:
```go
func (m Model) View(width int, focused bool) string {
	m.input.Width = width - 4 // account for padding/border

	var style lipgloss.Style
	if focused {
		style = styles.ComposeInsert.Width(width - 2)
	} else {
		style = styles.ComposeBox.Width(width - 2)
	}

	return style.Render(m.input.View())
}
```

- [ ] **Step 3: Update app.go to not change panel borders in insert mode**

In `internal/ui/app.go`, the `View()` method currently uses `a.focusedPanel` to decide which panel gets the blue border. In insert mode, all panel borders should be gray. The compose box itself provides the visual indicator.

Find the message pane border logic in `View()`:

```go
	msgBorderStyle := styles.UnfocusedBorder.Width(msgWidth)
	if a.focusedPanel == PanelMessages {
		msgBorderStyle = styles.FocusedBorder.Width(msgWidth)
	}
```

Replace with:

```go
	msgBorderStyle := styles.UnfocusedBorder.Width(msgWidth)
	if a.focusedPanel == PanelMessages && a.mode != ModeInsert {
		msgBorderStyle = styles.FocusedBorder.Width(msgWidth)
	}
```

Do the same for the sidebar border:

```go
	borderStyle := styles.UnfocusedBorder.Width(sidebarWidth)
	if a.focusedPanel == PanelSidebar {
		borderStyle = styles.FocusedBorder.Width(sidebarWidth)
	}
```

Replace with:

```go
	borderStyle := styles.UnfocusedBorder.Width(sidebarWidth)
	if a.focusedPanel == PanelSidebar && a.mode != ModeInsert {
		borderStyle = styles.FocusedBorder.Width(sidebarWidth)
	}
```

And for the thread panel border:

```go
	threadBorderStyle := styles.UnfocusedBorder.Width(threadWidth)
	if a.focusedPanel == PanelThread {
		threadBorderStyle = styles.FocusedBorder.Width(threadWidth)
	}
```

Replace with:

```go
	threadBorderStyle := styles.UnfocusedBorder.Width(threadWidth)
	if a.focusedPanel == PanelThread && a.mode != ModeInsert {
		threadBorderStyle = styles.FocusedBorder.Width(threadWidth)
	}
```

- [ ] **Step 4: Verify it compiles and tests pass**

Run: `go build ./cmd/slack-tui/ && go test ./internal/ui/... -v`
Expected: All pass

- [ ] **Step 5: Commit**

```bash
git add internal/ui/styles/styles.go internal/ui/compose/model.go internal/ui/app.go
git commit -m "feat: compose box insert mode indicator with background tint, gray panel borders"
```

---

### Task 3: Multi-line Compose with Shift+Enter

**Files:**
- Modify: `internal/ui/compose/model.go`
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Replace textinput with textarea in compose model**

Replace the entire `internal/ui/compose/model.go` with:

```go
// internal/ui/compose/model.go
package compose

import (
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gammons/slack-tui/internal/ui/styles"
)

type Model struct {
	input       textarea.Model
	channelName string
}

func New(channelName string) Model {
	ta := textarea.New()
	ta.Placeholder = "Message #" + channelName + "... (i to insert)"
	ta.CharLimit = 40000
	ta.MaxHeight = 5
	ta.ShowLineNumbers = false
	ta.Prompt = "> "
	ta.SetWidth(40)

	return Model{
		input:       ta,
		channelName: channelName,
	}
}

func (m *Model) SetChannel(name string) {
	m.channelName = name
	m.input.Placeholder = "Message #" + name + "... (i to insert)"
}

func (m *Model) Focus() tea.Cmd {
	return m.input.Focus()
}

func (m *Model) Blur() {
	m.input.Blur()
}

func (m *Model) Value() string {
	return m.input.Value()
}

func (m *Model) SetValue(s string) {
	m.input.SetValue(s)
}

func (m *Model) Reset() {
	m.input.Reset()
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) View(width int, focused bool) string {
	m.input.SetWidth(width - 4) // account for padding/border

	var style = styles.ComposeBox.Width(width - 2)
	if focused {
		style = styles.ComposeInsert.Width(width - 2)
	}

	return style.Render(m.input.View())
}
```

- [ ] **Step 2: Update handleInsertMode in app.go for Enter vs Shift+Enter**

The textarea widget by default inserts a newline on Enter. We need Enter to send the message and let Shift+Enter (which textarea treats as a newline by default) insert a newline.

In `internal/ui/app.go`, update the `handleInsertMode` method. The key check `msg.Type == tea.KeyEnter` needs to also check that Shift is NOT held:

Replace the thread reply Enter check:
```go
		if msg.Type == tea.KeyEnter {
```

With:
```go
		if msg.Type == tea.KeyEnter && msg.Mod == 0 {
```

Replace the channel message Enter check:
```go
	if msg.Type == tea.KeyEnter {
```

With:
```go
	if msg.Type == tea.KeyEnter && !msg.Alt {
```

**Shift+Enter for newline:** When `msg.Type == tea.KeyEnter` and the shift modifier is NOT held, we send the message. When Shift+Enter is pressed, we pass it through to the textarea which inserts a newline. Check for the shift modifier via bubbletea's key msg API (e.g., `msg.Mod` or checking `msg.String()` for `"shift+enter"`).

- [ ] **Step 3: Verify it compiles and tests pass**

Run: `go build ./cmd/slack-tui/ && go test ./internal/ui/... -v`
Expected: All pass

- [ ] **Step 4: Commit**

```bash
git add internal/ui/compose/model.go internal/ui/app.go
git commit -m "feat: multi-line compose with textarea, Shift+Enter for newline"
```

---

### Task 4: Connection Status Indicators

**Files:**
- Modify: `internal/ui/statusbar/model.go`
- Modify: `internal/slack/events.go`
- Modify: `internal/slack/client.go`
- Modify: `cmd/slack-tui/main.go`

- [ ] **Step 1: Add ConnectionState enum and update statusbar**

In `internal/ui/statusbar/model.go`, add the enum and update the struct:

```go
// ConnectionState represents the WebSocket connection status.
type ConnectionState int

const (
	StateConnecting   ConnectionState = iota
	StateConnected
	StateDisconnected
)
```

Replace the `connected bool` field with `connState ConnectionState` in the struct:

```go
type Model struct {
	mode        string
	channel     string
	workspace   string
	unreadCount int
	connState   ConnectionState
	inThread    bool
}
```

Update `New()` to set initial state:

```go
func New() Model {
	return Model{
		mode:      "NORMAL",
		connState: StateConnecting,
	}
}
```

Replace `SetConnected(bool)` with:

```go
func (m *Model) SetConnectionState(state ConnectionState) {
	m.connState = state
}
```

Update the connection indicator in `View()`. Replace lines 83-88:

```go
	if m.connected {
		rightParts = append(rightParts, styles.PresenceOnline.Render("*"))
	} else {
		disconnectedStyle := lipgloss.NewStyle().Foreground(styles.Error)
		rightParts = append(rightParts, disconnectedStyle.Render("DISCONNECTED"))
	}
```

With:

```go
	switch m.connState {
	case StateConnected:
		rightParts = append(rightParts,
			lipgloss.NewStyle().Foreground(styles.Accent).Render("● Connected"))
	case StateConnecting:
		rightParts = append(rightParts,
			lipgloss.NewStyle().Foreground(styles.Warning).Render("● Connecting"))
	case StateDisconnected:
		rightParts = append(rightParts,
			lipgloss.NewStyle().Foreground(styles.Error).Render("● Disconnected"))
	}
```

- [ ] **Step 2: Add OnConnect and OnDisconnect to EventHandler**

In `internal/slack/events.go`, add to the `EventHandler` interface:

```go
type EventHandler interface {
	OnMessage(channelID, userID, ts, text, threadTS string, edited bool)
	OnMessageDeleted(channelID, ts string)
	OnReactionAdded(channelID, ts, userID, emoji string)
	OnReactionRemoved(channelID, ts, userID, emoji string)
	OnPresenceChange(userID, presence string)
	OnUserTyping(channelID, userID string)
	OnConnect()
	OnDisconnect()
}
```

In `dispatchWebSocketEvent`, update the `"hello"` case:

```go
	case "hello":
		handler.OnConnect()
```

- [ ] **Step 3: Call OnDisconnect when WebSocket goroutine exits**

In `internal/slack/client.go`, update the WebSocket goroutine (inside `StartWebSocket`):

```go
	go func() {
		defer handler.OnDisconnect()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					return
				}
				log.Printf("WebSocket read error: %v", err)
				return
			}
			dispatchWebSocketEvent(message, handler)
		}
	}()
```

- [ ] **Step 4: Wire connection callbacks in main.go**

In `cmd/slack-tui/main.go`, add `OnConnect` and `OnDisconnect` methods to `rtmEventHandler`:

```go
func (h *rtmEventHandler) OnConnect() {
	h.program.Send(connectionStateMsg{state: statusbar.StateConnected})
}

func (h *rtmEventHandler) OnDisconnect() {
	h.program.Send(connectionStateMsg{state: statusbar.StateDisconnected})
}
```

Add the message type at the top of main.go (or near the other msg types):

```go
type connectionStateMsg struct {
	state statusbar.ConnectionState
}
```

- [ ] **Step 5: Handle connectionStateMsg in App.Update**

In `internal/ui/app.go`, add a new message type import if needed, then add a case in `Update()`:

Actually, this message is defined in main.go, not in the ui package. We need to handle it differently. The simplest approach: define a `ConnectionStateMsg` in `internal/ui/app.go`:

```go
	ConnectionStateMsg struct {
		State int // 0=connecting, 1=connected, 2=disconnected
	}
```

Then in `Update()`:

```go
	case ConnectionStateMsg:
		a.statusbar.SetConnectionState(statusbar.ConnectionState(msg.State))
```

And in main.go, update the methods:

```go
func (h *rtmEventHandler) OnConnect() {
	h.program.Send(ui.ConnectionStateMsg{State: int(statusbar.StateConnected)})
}

func (h *rtmEventHandler) OnDisconnect() {
	h.program.Send(ui.ConnectionStateMsg{State: int(statusbar.StateDisconnected)})
}
```

- [ ] **Step 6: Update statusbar test**

In `internal/ui/statusbar/statusbar_test.go`, any test that references `SetConnected` must be updated to use `SetConnectionState`. Read the test file first to see what needs updating.

- [ ] **Step 7: Verify it compiles and all tests pass**

Run: `go build ./cmd/slack-tui/ && go test ./... -v`
Expected: All pass

- [ ] **Step 8: Commit**

```bash
git add internal/ui/statusbar/model.go internal/slack/events.go internal/slack/client.go cmd/slack-tui/main.go internal/ui/app.go
git commit -m "feat: three-state connection status indicator with colored dots"
```

---

### Task 5: Full Build and Test Verification

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v`
Expected: All pass

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 3: Build**

Run: `make build`
Expected: Binary at `bin/slack-tui`
