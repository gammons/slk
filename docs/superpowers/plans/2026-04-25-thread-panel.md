# Thread Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a side panel for viewing and replying to threaded conversations, opening when the user presses Enter on a message.

**Architecture:** New `internal/ui/thread/` package with a `Model` that holds thread state (parent message, replies, scroll position) and a dedicated `compose.Model` instance for thread replies. The App splits the message area 65/35 when the thread is visible, routes keyboard input based on focused panel, and wires thread fetch/send callbacks in main.go. A new `GetReplies` method on the Slack client wraps the existing `GetConversationReplies` interface method.

**Tech Stack:** Go, bubbletea, lipgloss, slack-go

---

### Task 1: Add `GetReplies` to Slack Client

**Files:**
- Modify: `internal/slack/client.go:240` (after SendReply)
- Test: `internal/slack/client_test.go`

- [ ] **Step 1: Write the test for GetReplies**

```go
// In internal/slack/client_test.go, add to the mockSlackAPI struct:

func (m *mockSlackAPI) GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
	return []slack.Message{
		{Msg: slack.Msg{Timestamp: "1700000001.000000", Text: "parent msg", User: "U1"}},
		{Msg: slack.Msg{Timestamp: "1700000002.000000", Text: "reply 1", User: "U2"}},
	}, false, "", nil
}
```

Then add the test:

```go
func TestGetReplies(t *testing.T) {
	mock := &mockSlackAPI{}
	client := &Client{api: mock}

	msgs, err := client.GetReplies(context.Background(), "C123", "1700000001.000000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Text != "parent msg" {
		t.Errorf("expected parent msg, got %s", msgs[0].Text)
	}
	if msgs[1].Text != "reply 1" {
		t.Errorf("expected reply 1, got %s", msgs[1].Text)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/slack/ -run TestGetReplies -v`
Expected: FAIL — `client.GetReplies` undefined

- [ ] **Step 3: Implement GetReplies**

Add after `SendReply` in `internal/slack/client.go`:

```go
// GetReplies retrieves all replies in a thread.
// The first message in the returned slice is the parent message.
func (c *Client) GetReplies(ctx context.Context, channelID, threadTS string) ([]slack.Message, error) {
	var allMessages []slack.Message
	cursor := ""

	for {
		msgs, hasMore, nextCursor, err := c.api.GetConversationReplies(&slack.GetConversationRepliesParameters{
			ChannelID: channelID,
			Timestamp: threadTS,
			Cursor:    cursor,
		})
		if err != nil {
			return nil, fmt.Errorf("getting thread replies: %w", err)
		}
		allMessages = append(allMessages, msgs...)
		if !hasMore || nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return allMessages, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/slack/ -run TestGetReplies -v`
Expected: PASS

- [ ] **Step 5: Run all existing tests to verify no regressions**

Run: `go test ./internal/slack/ -v`
Expected: All tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "feat: add GetReplies to Slack client for fetching thread replies"
```

---

### Task 2: Add Thread Message Types and Callback Types to App

**Files:**
- Modify: `internal/ui/app.go:27-63` (message types and callback types section)

- [ ] **Step 1: Add thread-related message types and callback types**

In `internal/ui/app.go`, add to the message types block (after `SendMessageMsg`):

```go
	ThreadOpenedMsg struct {
		ChannelID string
		ThreadTS  string
		ParentMsg messages.MessageItem
	}
	ThreadRepliesLoadedMsg struct {
		ThreadTS string
		Replies  []messages.MessageItem
	}
	SendThreadReplyMsg struct {
		ChannelID string
		ThreadTS  string
		Text      string
	}
	ThreadReplySentMsg struct {
		ChannelID string
		ThreadTS  string
		Message   messages.MessageItem
	}
```

After the existing callback types, add:

```go
// ThreadFetchFunc is called when the user opens a thread.
type ThreadFetchFunc func(channelID, threadTS string) tea.Msg

// ThreadReplySendFunc is called when the user sends a thread reply.
type ThreadReplySendFunc func(channelID, threadTS, text string) tea.Msg
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/ui/`
Expected: Success (no errors)

- [ ] **Step 3: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat: add thread message types and callback types"
```

---

### Task 3: Create Thread Panel Component

**Files:**
- Create: `internal/ui/thread/model.go`
- Create: `internal/ui/thread/model_test.go`

- [ ] **Step 1: Write tests for the thread panel model**

Create `internal/ui/thread/model_test.go`:

```go
package thread

import (
	"strings"
	"testing"

	"github.com/gammons/slack-tui/internal/ui/messages"
)

func TestSetThread(t *testing.T) {
	m := New()

	parent := messages.MessageItem{
		TS:       "1700000001.000000",
		UserName: "alice",
		Text:     "parent message",
	}
	replies := []messages.MessageItem{
		{TS: "1700000002.000000", UserName: "bob", Text: "reply 1"},
		{TS: "1700000003.000000", UserName: "charlie", Text: "reply 2"},
	}

	m.SetThread(parent, replies, "C123", "1700000001.000000")

	if m.ThreadTS() != "1700000001.000000" {
		t.Errorf("expected thread ts 1700000001.000000, got %s", m.ThreadTS())
	}
	if m.IsEmpty() {
		t.Error("expected thread to not be empty after SetThread")
	}
	if m.ReplyCount() != 2 {
		t.Errorf("expected 2 replies, got %d", m.ReplyCount())
	}
}

func TestClear(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1700000001.000000", UserName: "alice", Text: "hi"}
	m.SetThread(parent, nil, "C123", "1700000001.000000")

	m.Clear()

	if !m.IsEmpty() {
		t.Error("expected thread to be empty after Clear")
	}
	if m.ThreadTS() != "" {
		t.Errorf("expected empty thread ts after Clear, got %s", m.ThreadTS())
	}
}

func TestAddReply(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1700000001.000000", UserName: "alice", Text: "hi"}
	m.SetThread(parent, nil, "C123", "1700000001.000000")

	m.AddReply(messages.MessageItem{TS: "1700000002.000000", UserName: "bob", Text: "hey"})

	if m.ReplyCount() != 1 {
		t.Errorf("expected 1 reply, got %d", m.ReplyCount())
	}
}

func TestNavigation(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1700000001.000000", UserName: "alice", Text: "hi"}
	replies := []messages.MessageItem{
		{TS: "1700000002.000000", UserName: "bob", Text: "r1"},
		{TS: "1700000003.000000", UserName: "charlie", Text: "r2"},
		{TS: "1700000004.000000", UserName: "dave", Text: "r3"},
	}
	m.SetThread(parent, replies, "C123", "1700000001.000000")

	// Should start at bottom (newest reply)
	if m.selected != 2 {
		t.Errorf("expected selected=2, got %d", m.selected)
	}

	m.MoveUp()
	if m.selected != 1 {
		t.Errorf("expected selected=1, got %d", m.selected)
	}

	m.MoveUp()
	m.MoveUp() // should not go below 0
	if m.selected != 0 {
		t.Errorf("expected selected=0, got %d", m.selected)
	}

	m.GoToBottom()
	if m.selected != 2 {
		t.Errorf("expected selected=2 after GoToBottom, got %d", m.selected)
	}
}

func TestViewRendersContent(t *testing.T) {
	m := New()
	parent := messages.MessageItem{
		TS:        "1700000001.000000",
		UserName:  "alice",
		Text:      "parent message",
		Timestamp: "10:30 AM",
	}
	replies := []messages.MessageItem{
		{TS: "1700000002.000000", UserName: "bob", Text: "reply one", Timestamp: "10:31 AM"},
	}
	m.SetThread(parent, replies, "C123", "1700000001.000000")

	view := m.View(20, 40)

	if !strings.Contains(view, "Thread") {
		t.Error("expected view to contain 'Thread'")
	}
	if !strings.Contains(view, "alice") {
		t.Error("expected view to contain parent username 'alice'")
	}
	if !strings.Contains(view, "bob") {
		t.Error("expected view to contain reply username 'bob'")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/thread/ -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Implement the thread panel model**

Create `internal/ui/thread/model.go`:

```go
package thread

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slack-tui/internal/ui/messages"
	"github.com/gammons/slack-tui/internal/ui/styles"
)

var (
	threadHeaderStyle = lipgloss.NewStyle().
		Foreground(styles.TextPrimary).
		Bold(true)

	replyCountStyle = lipgloss.NewStyle().
		Foreground(styles.TextMuted)

	separatorStyle = lipgloss.NewStyle().
		Foreground(styles.Border)

	selectedBg = lipgloss.NewStyle().
		Background(lipgloss.Color("#222233"))
)

// Model is the thread panel component. It displays a parent message
// and its replies, with cursor navigation and scroll support.
type Model struct {
	parentMsg messages.MessageItem
	replies   []messages.MessageItem
	channelID string
	threadTS  string
	selected  int
	offset    int // scroll offset for viewport
	focused   bool
	avatarFn  messages.AvatarFunc
	userNames map[string]string
}

// New creates an empty thread panel.
func New() *Model {
	return &Model{}
}

// SetThread populates the panel with a thread's parent and replies.
func (m *Model) SetThread(parent messages.MessageItem, replies []messages.MessageItem, channelID, threadTS string) {
	m.parentMsg = parent
	m.replies = replies
	m.channelID = channelID
	m.threadTS = threadTS
	// Start at the bottom (newest reply)
	if len(replies) > 0 {
		m.selected = len(replies) - 1
	} else {
		m.selected = 0
	}
	m.offset = 0
}

// AddReply appends a new reply to the thread.
func (m *Model) AddReply(msg messages.MessageItem) {
	wasAtBottom := len(m.replies) == 0 || m.selected >= len(m.replies)-1
	m.replies = append(m.replies, msg)
	if wasAtBottom {
		m.selected = len(m.replies) - 1
	}
}

// Clear resets the thread panel to empty state.
func (m *Model) Clear() {
	m.parentMsg = messages.MessageItem{}
	m.replies = nil
	m.channelID = ""
	m.threadTS = ""
	m.selected = 0
	m.offset = 0
}

// ThreadTS returns the timestamp of the currently open thread's parent.
func (m *Model) ThreadTS() string {
	return m.threadTS
}

// ChannelID returns the channel of the currently open thread.
func (m *Model) ChannelID() string {
	return m.channelID
}

// IsEmpty returns true if no thread is loaded.
func (m *Model) IsEmpty() bool {
	return m.threadTS == ""
}

// ReplyCount returns the number of replies.
func (m *Model) ReplyCount() int {
	return len(m.replies)
}

// SetFocused sets whether the panel has keyboard focus.
func (m *Model) SetFocused(focused bool) {
	m.focused = focused
}

// Focused returns whether the panel has keyboard focus.
func (m *Model) Focused() bool {
	return m.focused
}

// SetAvatarFunc sets the avatar rendering function.
func (m *Model) SetAvatarFunc(fn messages.AvatarFunc) {
	m.avatarFn = fn
}

// SetUserNames sets the user ID -> display name map for mention resolution.
func (m *Model) SetUserNames(names map[string]string) {
	m.userNames = names
}

// MoveUp moves the selection cursor up.
func (m *Model) MoveUp() {
	if m.selected > 0 {
		m.selected--
	}
}

// MoveDown moves the selection cursor down.
func (m *Model) MoveDown() {
	if m.selected < len(m.replies)-1 {
		m.selected++
	}
}

// GoToTop moves to the first reply.
func (m *Model) GoToTop() {
	m.selected = 0
	m.offset = 0
}

// GoToBottom moves to the last reply.
func (m *Model) GoToBottom() {
	if len(m.replies) > 0 {
		m.selected = len(m.replies) - 1
	}
}

// View renders the thread panel content (without border -- the parent App adds the border).
func (m *Model) View(height, width int) string {
	if m.IsEmpty() {
		return lipgloss.NewStyle().
			Width(width).
			Height(height).
			Foreground(styles.TextMuted).
			Render("No thread selected")
	}

	// Header: "Thread  N replies"
	replyText := fmt.Sprintf("%d replies", len(m.replies))
	if len(m.replies) == 1 {
		replyText = "1 reply"
	} else if len(m.replies) == 0 {
		replyText = "No replies yet"
	}
	header := threadHeaderStyle.Render("Thread") + "  " + replyCountStyle.Render(replyText)

	sep := separatorStyle.Width(width).Render(strings.Repeat("─", width))

	// Parent message
	parentRendered := renderThreadMessage(m.parentMsg, width, m.userNames)
	parentSection := parentRendered + "\n" + sep

	chrome := header + "\n" + sep + "\n" + parentSection + "\n"
	chromeHeight := lipgloss.Height(chrome)

	replyAreaHeight := height - chromeHeight
	if replyAreaHeight < 1 {
		replyAreaHeight = 1
	}

	if len(m.replies) == 0 {
		empty := lipgloss.NewStyle().
			Width(width).
			Height(replyAreaHeight).
			Foreground(styles.TextMuted).
			Render("  Start a thread...")
		return chrome + empty
	}

	// Render replies with simple viewport
	var renderedReplies []string
	for i, reply := range m.replies {
		rendered := renderThreadMessage(reply, width, m.userNames)
		if i == m.selected {
			rendered = selectedBg.Width(width - 2).Padding(0, 1).Render(rendered)
		}
		renderedReplies = append(renderedReplies, rendered)
	}

	// Simple viewport: ensure selected reply is visible
	// Build from selected entry, fitting as many as possible
	var visible []string
	usedHeight := 0

	// Add selected entry first
	selHeight := lipgloss.Height(renderedReplies[m.selected])
	visible = append(visible, renderedReplies[m.selected])
	usedHeight += selHeight

	// Add entries above selected
	for i := m.selected - 1; i >= 0; i-- {
		h := lipgloss.Height(renderedReplies[i])
		if usedHeight+h > replyAreaHeight {
			break
		}
		visible = append([]string{renderedReplies[i]}, visible...)
		usedHeight += h
	}

	// Add entries below selected
	for i := m.selected + 1; i < len(renderedReplies); i++ {
		h := lipgloss.Height(renderedReplies[i])
		if usedHeight+h > replyAreaHeight {
			break
		}
		visible = append(visible, renderedReplies[i])
		usedHeight += h
	}

	replyContent := strings.Join(visible, "\n")
	replySection := lipgloss.NewStyle().
		Width(width).
		Height(replyAreaHeight).
		MaxHeight(replyAreaHeight).
		Render(replyContent)

	return chrome + replySection
}

// renderThreadMessage renders a single message for the thread panel.
// Simpler than main pane: no avatars, no day separators.
func renderThreadMessage(msg messages.MessageItem, width int, userNames map[string]string) string {
	line := styles.Username.Render(msg.UserName) + "  " + styles.Timestamp.Render(msg.Timestamp)
	contentWidth := width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}
	text := styles.MessageText.Width(contentWidth).Render(messages.RenderSlackMarkdown(msg.Text, userNames))
	return line + "\n" + text
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/thread/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/thread/
git commit -m "feat: add thread panel UI component"
```

---

### Task 4: Wire Thread Panel into App Struct and Setters

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add thread panel and callbacks to App struct**

Add import for the thread package in the imports:

```go
"github.com/gammons/slack-tui/internal/ui/thread"
```

Add to the App struct (after `channelFinder channelfinder.Model`):

```go
	threadPanel *thread.Model
	threadCompose compose.Model
```

Add to callback fields (after `messageSender MessageSendFunc`):

```go
	threadFetcher     ThreadFetchFunc
	threadReplySender ThreadReplySendFunc
```

- [ ] **Step 2: Initialize thread panel in NewApp**

In `NewApp()`, add after `channelFinder: channelfinder.New(),`:

```go
		threadPanel:  thread.New(),
		threadCompose: compose.New("thread"),
```

- [ ] **Step 3: Add setter methods**

Add after `SetMessageSender`:

```go
// SetThreadFetcher sets the callback used to load thread replies.
func (a *App) SetThreadFetcher(fn ThreadFetchFunc) {
	a.threadFetcher = fn
}

// SetThreadReplySender sets the callback used to send thread replies.
func (a *App) SetThreadReplySender(fn ThreadReplySendFunc) {
	a.threadReplySender = fn
}
```

- [ ] **Step 4: Wire avatar and userNames to thread panel**

In `SetAvatarFunc`, add:

```go
	a.threadPanel.SetAvatarFunc(fn)
```

In `SetUserNames`, add:

```go
	a.threadPanel.SetUserNames(names)
```

- [ ] **Step 5: Verify it compiles**

Run: `go build ./internal/ui/`
Expected: Success

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat: wire thread panel into App struct with setters"
```

---

### Task 5: Update Focus Management to Include Thread Panel

**Files:**
- Modify: `internal/ui/app.go` (FocusNext, FocusPrev, handleDown, handleUp, handleGoToBottom)

- [ ] **Step 1: Update FocusNext**

Replace `FocusNext()`:

```go
func (a *App) FocusNext() {
	if !a.sidebarVisible {
		if a.threadVisible {
			if a.focusedPanel == PanelMessages {
				a.focusedPanel = PanelThread
			} else {
				a.focusedPanel = PanelMessages
			}
		}
		return
	}
	switch a.focusedPanel {
	case PanelSidebar:
		a.focusedPanel = PanelMessages
	case PanelMessages:
		if a.threadVisible {
			a.focusedPanel = PanelThread
		} else {
			a.focusedPanel = PanelSidebar
		}
	case PanelThread:
		a.focusedPanel = PanelSidebar
	}
}
```

- [ ] **Step 2: Update FocusPrev**

Replace `FocusPrev()`:

```go
func (a *App) FocusPrev() {
	if !a.sidebarVisible {
		if a.threadVisible {
			if a.focusedPanel == PanelThread {
				a.focusedPanel = PanelMessages
			} else {
				a.focusedPanel = PanelThread
			}
		}
		return
	}
	switch a.focusedPanel {
	case PanelSidebar:
		if a.threadVisible {
			a.focusedPanel = PanelThread
		} else {
			a.focusedPanel = PanelMessages
		}
	case PanelMessages:
		a.focusedPanel = PanelSidebar
	case PanelThread:
		a.focusedPanel = PanelMessages
	}
}
```

- [ ] **Step 3: Update handleDown to include thread panel**

Add a case to `handleDown()`:

```go
	case PanelThread:
		a.threadPanel.MoveDown()
```

- [ ] **Step 4: Update handleUp to include thread panel**

Add a case to `handleUp()`:

```go
	case PanelThread:
		a.threadPanel.MoveUp()
```

- [ ] **Step 5: Update handleGoToBottom to include thread panel**

Add a case to `handleGoToBottom()`:

```go
	case PanelThread:
		a.threadPanel.GoToBottom()
```

- [ ] **Step 6: Verify it compiles**

Run: `go build ./internal/ui/`
Expected: Success

- [ ] **Step 7: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat: include thread panel in focus cycle and navigation"
```

---

### Task 6: Handle Thread Open, Close, and Toggle

**Files:**
- Modify: `internal/ui/app.go` (handleNormalMode, handleEnter, Update, new methods)

- [ ] **Step 1: Add ToggleThread handler in handleNormalMode**

In `handleNormalMode()`, add a case after `ToggleSidebar`:

```go
	case key.Matches(msg, a.keys.ToggleThread):
		a.ToggleThread()
```

- [ ] **Step 2: Implement ToggleThread method**

Add after `ToggleSidebar()`:

```go
func (a *App) ToggleThread() {
	if a.threadVisible {
		a.CloseThread()
	}
	// Don't open on toggle if no thread is loaded -- use Enter for that
}

func (a *App) CloseThread() {
	a.threadVisible = false
	a.threadPanel.Clear()
	a.threadCompose.Blur()
	if a.focusedPanel == PanelThread {
		a.focusedPanel = PanelMessages
	}
}
```

- [ ] **Step 3: Update handleEnter to open thread from messages pane**

Replace `handleEnter()`:

```go
func (a *App) handleEnter() tea.Cmd {
	if a.focusedPanel == PanelSidebar {
		item, ok := a.sidebar.SelectedItem()
		if ok {
			return func() tea.Msg {
				return ChannelSelectedMsg{ID: item.ID, Name: item.Name}
			}
		}
	}

	if a.focusedPanel == PanelMessages {
		msg, ok := a.messagepane.SelectedMessage()
		if ok {
			// Use the message's own TS as the thread parent.
			// If it's already a thread reply, use its ThreadTS instead.
			threadTS := msg.TS
			if msg.ThreadTS != "" && msg.ThreadTS != msg.TS {
				threadTS = msg.ThreadTS
			}
			a.threadVisible = true
			a.focusedPanel = PanelThread
			a.threadPanel.SetThread(msg, nil, a.activeChannelID, threadTS)
			a.threadCompose.SetChannel("thread")

			if a.threadFetcher != nil {
				fetcher := a.threadFetcher
				chID := a.activeChannelID
				ts := threadTS
				return func() tea.Msg {
					return fetcher(chID, ts)
				}
			}
		}
	}

	return nil
}
```

- [ ] **Step 4: Handle ThreadRepliesLoadedMsg in Update**

Add a case in `Update()` after `MessageSentMsg`:

```go
	case ThreadOpenedMsg:
		// Handled inline in handleEnter -- this msg type exists for external use
		// if needed in the future.

	case ThreadRepliesLoadedMsg:
		if a.threadVisible && msg.ThreadTS == a.threadPanel.ThreadTS() {
			a.threadPanel.SetThread(a.threadPanel.ParentMsg(), msg.Replies, a.threadPanel.ChannelID(), msg.ThreadTS)
		}

	case SendThreadReplyMsg:
		if a.threadReplySender != nil {
			sender := a.threadReplySender
			chID, ts, text := msg.ChannelID, msg.ThreadTS, msg.Text
			cmds = append(cmds, func() tea.Msg {
				return sender(chID, ts, text)
			})
		}

	case ThreadReplySentMsg:
		if a.threadVisible && msg.ThreadTS == a.threadPanel.ThreadTS() {
			a.threadPanel.AddReply(msg.Message)
		}
```

- [ ] **Step 5: Add ParentMsg getter to thread model**

In `internal/ui/thread/model.go`, add:

```go
// ParentMsg returns the parent message of the current thread.
func (m *Model) ParentMsg() messages.MessageItem {
	return m.parentMsg
}
```

- [ ] **Step 6: Close thread on channel switch**

In the `ChannelSelectedMsg` case in `Update()`, add before the fetcher call:

```go
		// Close thread panel when switching channels
		a.CloseThread()
```

- [ ] **Step 7: Route incoming real-time messages to thread panel**

In the `NewMessageMsg` case in `Update()`, update to:

```go
	case NewMessageMsg:
		if msg.ChannelID == a.activeChannelID {
			// Route thread replies to the thread panel if it matches the open thread
			if a.threadVisible && msg.Message.ThreadTS == a.threadPanel.ThreadTS() {
				a.threadPanel.AddReply(msg.Message)
			}
			// Always add to main pane if it's a top-level message (no ThreadTS or is the parent)
			if msg.Message.ThreadTS == "" || msg.Message.ThreadTS == msg.Message.TS {
				a.messagepane.AppendMessage(msg.Message)
			}
		}
```

- [ ] **Step 8: Verify it compiles**

Run: `go build ./internal/ui/`
Expected: Success

- [ ] **Step 9: Commit**

```bash
git add internal/ui/app.go internal/ui/thread/model.go
git commit -m "feat: handle thread open/close/toggle and real-time routing"
```

---

### Task 7: Handle Insert Mode for Thread Reply Compose

**Files:**
- Modify: `internal/ui/app.go` (handleNormalMode, handleInsertMode)

- [ ] **Step 1: Update InsertMode entry to be context-aware**

In `handleNormalMode()`, replace the `InsertMode` case:

```go
	case key.Matches(msg, a.keys.InsertMode):
		a.SetMode(ModeInsert)
		if a.focusedPanel == PanelThread {
			return a.threadCompose.Focus()
		}
		a.focusedPanel = PanelMessages
		return a.compose.Focus()
```

- [ ] **Step 2: Update handleInsertMode to be context-aware**

Replace `handleInsertMode()`:

```go
func (a *App) handleInsertMode(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, a.keys.Escape) {
		a.SetMode(ModeNormal)
		a.compose.Blur()
		a.threadCompose.Blur()
		return nil
	}

	// Determine which compose box is active based on focused panel
	if a.focusedPanel == PanelThread && a.threadVisible {
		// Thread reply compose
		if msg.Type == tea.KeyEnter {
			text := a.threadCompose.Value()
			if text != "" {
				a.threadCompose.Reset()
				threadTS := a.threadPanel.ThreadTS()
				channelID := a.threadPanel.ChannelID()
				return func() tea.Msg {
					return SendThreadReplyMsg{
						ChannelID: channelID,
						ThreadTS:  threadTS,
						Text:      text,
					}
				}
			}
			return nil
		}
		var cmd tea.Cmd
		a.threadCompose, cmd = a.threadCompose.Update(msg)
		return cmd
	}

	// Channel message compose (existing behavior)
	if msg.Type == tea.KeyEnter {
		text := a.compose.Value()
		if text != "" {
			a.compose.Reset()
			return func() tea.Msg {
				return SendMessageMsg{
					ChannelID: a.activeChannelID,
					Text:      text,
				}
			}
		}
		return nil
	}

	var cmd tea.Cmd
	a.compose, cmd = a.compose.Update(msg)
	return cmd
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/ui/`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat: context-aware insert mode for thread reply compose"
```

---

### Task 8: Update View to Render Thread Panel

**Files:**
- Modify: `internal/ui/app.go` (View method)

- [ ] **Step 1: Update View to split message area when thread is visible**

Replace the `View()` method:

```go
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "Initializing..."
	}

	statusHeight := 1
	contentHeight := a.height - statusHeight

	// Calculate widths, accounting for borders (2 cols each for left+right)
	railWidth := a.workspaceRail.Width()
	sidebarWidth := 0
	sidebarBorder := 0
	if a.sidebarVisible {
		sidebarWidth = a.sidebar.Width()
		sidebarBorder = 2 // left + right border
	}

	// Calculate the message area (everything right of sidebar)
	msgAreaWidth := a.width - railWidth - sidebarWidth - sidebarBorder

	// Determine thread and message pane widths
	msgBorder := 2
	threadWidth := 0
	threadBorder := 0
	if a.threadVisible {
		threadBorder = 2
		// 35% of message area for thread, but enforce minimums
		threadWidth = msgAreaWidth * 35 / 100
		msgPaneWidth := msgAreaWidth - threadWidth - msgBorder - threadBorder
		// Enforce minimum widths
		if msgPaneWidth < 40 || threadWidth < 30 {
			// Too narrow -- auto-hide thread
			a.threadVisible = false
			threadWidth = 0
			threadBorder = 0
			if a.focusedPanel == PanelThread {
				a.focusedPanel = PanelMessages
			}
		}
	}

	msgWidth := msgAreaWidth - msgBorder - threadWidth - threadBorder
	if msgWidth < 10 {
		msgWidth = 10
	}

	// Helper to force a panel to an exact height
	exactHeight := func(s string, h int) string {
		return lipgloss.NewStyle().Width(lipgloss.Width(s)).Height(h).MaxHeight(h).Render(s)
	}

	// Render workspace rail
	rail := exactHeight(a.workspaceRail.View(contentHeight), contentHeight)

	var panels []string
	panels = append(panels, rail)

	// Render sidebar
	if a.sidebarVisible {
		borderStyle := styles.UnfocusedBorder.Width(sidebarWidth)
		if a.focusedPanel == PanelSidebar {
			borderStyle = styles.FocusedBorder.Width(sidebarWidth)
		}
		sidebarView := a.sidebar.View(contentHeight-2, sidebarWidth)
		sidebarView = borderStyle.Render(sidebarView)
		panels = append(panels, exactHeight(sidebarView, contentHeight))
	}

	// Render message pane with border
	msgBorderStyle := styles.UnfocusedBorder.Width(msgWidth)
	if a.focusedPanel == PanelMessages {
		msgBorderStyle = styles.FocusedBorder.Width(msgWidth)
	}
	composeView := a.compose.View(msgWidth-2, a.mode == ModeInsert && a.focusedPanel != PanelThread)
	composeHeight := lipgloss.Height(composeView)
	msgContentHeight := contentHeight - 2 - composeHeight
	if msgContentHeight < 3 {
		msgContentHeight = 3
	}
	msgView := a.messagepane.View(msgContentHeight, msgWidth-2)
	msgInner := lipgloss.JoinVertical(lipgloss.Left, msgView, composeView)
	msgPanel := exactHeight(
		msgBorderStyle.Render(msgInner),
		contentHeight,
	)
	panels = append(panels, msgPanel)

	// Render thread panel if visible
	if a.threadVisible && threadWidth > 0 {
		threadBorderStyle := styles.UnfocusedBorder.Width(threadWidth)
		if a.focusedPanel == PanelThread {
			threadBorderStyle = styles.FocusedBorder.Width(threadWidth)
		}
		threadComposeView := a.threadCompose.View(threadWidth-2, a.mode == ModeInsert && a.focusedPanel == PanelThread)
		threadComposeHeight := lipgloss.Height(threadComposeView)
		threadContentHeight := contentHeight - 2 - threadComposeHeight
		if threadContentHeight < 3 {
			threadContentHeight = 3
		}
		threadView := a.threadPanel.View(threadContentHeight, threadWidth-2)
		threadInner := lipgloss.JoinVertical(lipgloss.Left, threadView, threadComposeView)
		threadPanel := exactHeight(
			threadBorderStyle.Render(threadInner),
			contentHeight,
		)
		panels = append(panels, threadPanel)
	}

	content := lipgloss.JoinHorizontal(lipgloss.Top, panels...)
	status := a.statusbar.View(a.width)

	screen := lipgloss.JoinVertical(lipgloss.Left, content, status)

	// Render channel finder overlay on top of existing layout
	if a.channelFinder.IsVisible() {
		screen = a.channelFinder.ViewOverlay(a.width, a.height, screen)
	}

	return screen
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/ui/`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat: render thread panel in View with 65/35 width split"
```

---

### Task 9: Wire Thread Fetcher and Reply Sender in main.go

**Files:**
- Modify: `cmd/slack-tui/main.go`

- [ ] **Step 1: Add fetchThreadReplies function**

Add after `fetchChannelMessages`:

```go
func fetchThreadReplies(client *slackclient.Client, channelID, threadTS string, db *cache.DB, userNames map[string]string, tsFormat string) []messages.MessageItem {
	ctx := context.Background()
	history, err := client.GetReplies(ctx, channelID, threadTS)
	if err != nil {
		log.Printf("Warning: failed to fetch thread replies: %v", err)
		return nil
	}

	var msgItems []messages.MessageItem
	for _, m := range history {
		db.UpsertMessage(cache.Message{
			TS:          m.Timestamp,
			ChannelID:   channelID,
			WorkspaceID: client.TeamID(),
			UserID:      m.User,
			Text:        m.Text,
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			CreatedAt:   time.Now().Unix(),
		})

		userName := m.User
		if resolved, ok := userNames[m.User]; ok {
			userName = resolved
		}

		msgItems = append(msgItems, messages.MessageItem{
			TS:         m.Timestamp,
			UserID:     m.User,
			UserName:   userName,
			Text:       m.Text,
			Timestamp:  formatTimestamp(m.Timestamp, tsFormat),
			ThreadTS:   m.ThreadTimestamp,
			ReplyCount: m.ReplyCount,
		})
	}

	// First message from GetConversationReplies is the parent -- skip it for the replies list
	if len(msgItems) > 1 {
		return msgItems[1:]
	}
	return nil
}
```

- [ ] **Step 2: Wire thread fetcher callback**

In the `if activeClient != nil` block, after `SetOlderMessagesFetcher`, add:

```go
		// Wire up thread fetcher
		app.SetThreadFetcher(func(channelID, threadTS string) tea.Msg {
			replies := fetchThreadReplies(client, channelID, threadTS, db, userNames, tsFormat)
			return ui.ThreadRepliesLoadedMsg{
				ThreadTS: threadTS,
				Replies:  replies,
			}
		})

		// Wire up thread reply sender
		app.SetThreadReplySender(func(channelID, threadTS, text string) tea.Msg {
			ctx := context.Background()
			ts, err := client.SendReply(ctx, channelID, threadTS, text)
			if err != nil {
				log.Printf("Warning: failed to send thread reply: %v", err)
				return nil
			}
			userName := "you"
			if resolved, ok := userNames[client.UserID()]; ok {
				userName = resolved
			}
			return ui.ThreadReplySentMsg{
				ChannelID: channelID,
				ThreadTS:  threadTS,
				Message: messages.MessageItem{
					TS:        ts,
					UserID:    client.UserID(),
					UserName:  userName,
					Text:      text,
					Timestamp: formatTimestamp(ts, tsFormat),
					ThreadTS:  threadTS,
				},
			}
		})
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./cmd/slack-tui/`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add cmd/slack-tui/main.go
git commit -m "feat: wire thread fetcher and reply sender in main.go"
```

---

### Task 10: Update Status Bar for Thread Context

**Files:**
- Modify: `internal/ui/statusbar/model.go`
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add inThread field and setter to statusbar**

In `internal/ui/statusbar/model.go`, add `inThread bool` to the `Model` struct:

```go
type Model struct {
	mode        string
	channel     string
	workspace   string
	unreadCount int
	connected   bool
	inThread    bool
}
```

Add the setter method:

```go
func (m *Model) SetInThread(inThread bool) {
	m.inThread = inThread
}
```

- [ ] **Step 2: Update statusbar View to show thread context**

In the `View()` method at line 61, change the channel info rendering:

Replace:
```go
	channelInfo := styles.StatusBar.Render(fmt.Sprintf(" #%s ", m.channel))
```

With:
```go
	channelLabel := fmt.Sprintf(" #%s ", m.channel)
	if m.inThread {
		channelLabel = fmt.Sprintf(" #%s > Thread ", m.channel)
	}
	channelInfo := styles.StatusBar.Render(channelLabel)
```

- [ ] **Step 3: Wire status bar thread state in App**

In `app.go`, wherever `a.threadVisible` is set to `true`, also call `a.statusbar.SetInThread(true)`. Wherever thread is closed (in `CloseThread()`), call `a.statusbar.SetInThread(false)`.

- [ ] **Step 4: Verify it compiles**

Run: `go build ./cmd/slack-tui/`
Expected: Success

- [ ] **Step 5: Commit**

```bash
git add internal/ui/statusbar/model.go internal/ui/app.go
git commit -m "feat: show thread context in status bar"
```

---

### Task 11: Full Build and Test Verification

**Files:** None (verification only)

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v`
Expected: All PASS

- [ ] **Step 2: Run the build**

Run: `make build`
Expected: Binary produced at `bin/slack-tui`

- [ ] **Step 3: Run vet and check for issues**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 4: Final commit if any fixes were needed**

Only commit if test/vet failures required code changes.
