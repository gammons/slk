# Desktop Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Send OS-level desktop notifications for mentions, DMs, and keyword matches in incoming Slack messages.

**Architecture:** A thin `internal/notify/` package wraps `beeep.Notify()` for cross-platform desktop notifications. A pure `ShouldNotify()` function encapsulates trigger logic (mentions, DMs, keywords, suppression). The `rtmEventHandler.OnMessage()` in `main.go` calls both to decide and send notifications.

**Tech Stack:** Go, github.com/gen2brain/beeep

---

### Task 1: Add beeep Dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the beeep dependency**

Run:
```bash
go get github.com/gen2brain/beeep@latest
```

- [ ] **Step 2: Verify it resolves**

Run: `go mod tidy`

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add beeep for cross-platform desktop notifications"
```

---

### Task 2: Create Notify Package with ShouldNotify and StripSlackMarkup

**Files:**
- Create: `internal/notify/notifier.go`
- Create: `internal/notify/notifier_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/notify/notifier_test.go`:

```go
package notify

import (
	"testing"
)

func TestShouldNotify_SelfMessage(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C_OTHER",
		IsActiveWS:      true,
		OnMention:       true,
		OnDM:            true,
	}
	// Self-messages never trigger
	if ShouldNotify(ctx, "C1", "U1", "hello", "dm") {
		t.Error("should not notify for self-messages")
	}
}

func TestShouldNotify_DM(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C_OTHER",
		IsActiveWS:      true,
		OnDM:            true,
	}
	if !ShouldNotify(ctx, "C1", "U2", "hello", "dm") {
		t.Error("should notify for DM")
	}
	if !ShouldNotify(ctx, "C1", "U2", "hello", "group_dm") {
		t.Error("should notify for group DM")
	}
}

func TestShouldNotify_DM_Disabled(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C_OTHER",
		IsActiveWS:      true,
		OnDM:            false,
	}
	if ShouldNotify(ctx, "C1", "U2", "hello", "dm") {
		t.Error("should not notify for DM when OnDM is false")
	}
}

func TestShouldNotify_Mention(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C_OTHER",
		IsActiveWS:      true,
		OnMention:       true,
	}
	if !ShouldNotify(ctx, "C1", "U2", "hey <@U1> check this", "channel") {
		t.Error("should notify for mention")
	}
	if ShouldNotify(ctx, "C1", "U2", "hey <@U3> check this", "channel") {
		t.Error("should not notify for mention of another user")
	}
}

func TestShouldNotify_Mention_Disabled(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C_OTHER",
		IsActiveWS:      true,
		OnMention:       false,
	}
	if ShouldNotify(ctx, "C1", "U2", "hey <@U1> check this", "channel") {
		t.Error("should not notify for mention when OnMention is false")
	}
}

func TestShouldNotify_Keyword(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C_OTHER",
		IsActiveWS:      true,
		OnKeyword:       []string{"deploy", "incident"},
	}
	if !ShouldNotify(ctx, "C1", "U2", "starting deploy now", "channel") {
		t.Error("should notify for keyword match")
	}
	if !ShouldNotify(ctx, "C1", "U2", "DEPLOY is done", "channel") {
		t.Error("should notify for case-insensitive keyword match")
	}
	if ShouldNotify(ctx, "C1", "U2", "nothing relevant", "channel") {
		t.Error("should not notify when no keyword matches")
	}
}

func TestShouldNotify_ActiveChannel_Suppressed(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C1",
		IsActiveWS:      true,
		OnDM:            true,
	}
	if ShouldNotify(ctx, "C1", "U2", "hello", "dm") {
		t.Error("should suppress notification for active channel")
	}
}

func TestShouldNotify_InactiveWorkspace_NotSuppressed(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C1",
		IsActiveWS:      false,
		OnDM:            true,
	}
	// Same channel ID but different workspace — should still notify
	if !ShouldNotify(ctx, "C1", "U2", "hello", "dm") {
		t.Error("should notify when workspace is inactive even if channel ID matches")
	}
}

func TestStripSlackMarkup(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello world", "hello world"},
		{"hey <@U123>", "hey @someone"},
		{"see <#C123|general>", "see #general"},
		{"visit <https://example.com|Example>", "visit Example"},
		{"visit <https://example.com>", "visit https://example.com"},
		{"*bold* and _italic_ and ~strike~", "bold and italic and strike"},
		{"`code`", "code"},
		{"", ""},
	}
	for _, tt := range tests {
		result := StripSlackMarkup(tt.input)
		if result != tt.expected {
			t.Errorf("StripSlackMarkup(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestStripSlackMarkup_Truncation(t *testing.T) {
	long := ""
	for i := 0; i < 120; i++ {
		long += "a"
	}
	result := StripSlackMarkup(long)
	if len(result) > 103 { // 100 + "..."
		t.Errorf("expected truncation, got length %d", len(result))
	}
	if result[len(result)-3:] != "..." {
		t.Error("expected ... suffix")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/notify/ -v`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Create the notify package**

Create `internal/notify/notifier.go`:

```go
// Package notify provides desktop notification support.
package notify

import (
	"regexp"
	"strings"

	"github.com/gen2brain/beeep"
)

// Notifier sends OS-level desktop notifications.
type Notifier struct {
	enabled bool
}

// New creates a Notifier. If enabled is false, Notify is a no-op.
func New(enabled bool) *Notifier {
	return &Notifier{enabled: enabled}
}

// Notify sends a desktop notification with the given title and body.
// Returns nil if notifications are disabled.
func (n *Notifier) Notify(title, body string) error {
	if !n.enabled {
		return nil
	}
	return beeep.Notify(title, body, "")
}

// NotifyContext holds the state needed to evaluate notification triggers.
type NotifyContext struct {
	CurrentUserID   string
	ActiveChannelID string
	IsActiveWS      bool
	OnMention       bool
	OnDM            bool
	OnKeyword       []string
}

// ShouldNotify returns true if a message should trigger a desktop notification.
func ShouldNotify(ctx NotifyContext, channelID, userID, text, channelType string) bool {
	// Never notify for own messages
	if userID == ctx.CurrentUserID {
		return false
	}

	// Suppress if viewing this channel on the active workspace
	if ctx.IsActiveWS && channelID == ctx.ActiveChannelID {
		return false
	}

	// Check DM trigger
	if ctx.OnDM && (channelType == "dm" || channelType == "group_dm") {
		return true
	}

	// Check mention trigger
	if ctx.OnMention && strings.Contains(text, "<@"+ctx.CurrentUserID+">") {
		return true
	}

	// Check keyword triggers
	if len(ctx.OnKeyword) > 0 {
		lower := strings.ToLower(text)
		for _, kw := range ctx.OnKeyword {
			if strings.Contains(lower, strings.ToLower(kw)) {
				return true
			}
		}
	}

	return false
}

var (
	// Match Slack user mentions: <@U123ABC>
	userMentionRe = regexp.MustCompile(`<@[A-Z0-9]+>`)
	// Match Slack channel mentions: <#C123|general>
	channelMentionRe = regexp.MustCompile(`<#[A-Z0-9]+\|([^>]+)>`)
	// Match Slack links: <url|label> or <url>
	linkWithLabelRe = regexp.MustCompile(`<(https?://[^|>]+)\|([^>]+)>`)
	linkBareRe      = regexp.MustCompile(`<(https?://[^>]+)>`)
)

// StripSlackMarkup converts Slack-formatted text to plain text.
// Truncates to 100 characters with "..." suffix.
func StripSlackMarkup(text string) string {
	// Channel mentions: <#C123|general> -> #general
	text = channelMentionRe.ReplaceAllString(text, "#$1")
	// Links with label: <url|label> -> label
	text = linkWithLabelRe.ReplaceAllString(text, "$2")
	// Bare links: <url> -> url
	text = linkBareRe.ReplaceAllString(text, "$1")
	// User mentions: <@U123> -> @someone
	text = userMentionRe.ReplaceAllString(text, "@someone")
	// Bold: *text* -> text
	text = strings.ReplaceAll(text, "*", "")
	// Italic: _text_ -> text
	text = strings.ReplaceAll(text, "_", "")
	// Strikethrough: ~text~ -> text
	text = strings.ReplaceAll(text, "~", "")
	// Inline code: `text` -> text
	text = strings.ReplaceAll(text, "`", "")

	// Truncate
	if len(text) > 100 {
		text = text[:100] + "..."
	}

	return text
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/notify/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/notify/notifier.go internal/notify/notifier_test.go
git commit -m "feat: add notify package with ShouldNotify and StripSlackMarkup"
```

---

### Task 3: Wire Notifications into rtmEventHandler

**Files:**
- Modify: `cmd/slk/main.go:756-764` (rtmEventHandler struct), `cmd/slk/main.go:766-803` (OnMessage), `cmd/slk/main.go:328-340` (handler construction)

- [ ] **Step 1: Add notification fields to rtmEventHandler**

In `cmd/slk/main.go`, modify the `rtmEventHandler` struct (currently at line 756):

```go
type rtmEventHandler struct {
	program     *tea.Program
	userNames   map[string]string
	tsFormat    string
	db          *cache.DB
	workspaceID string
	connected   bool
	isActive    func() bool

	// Notifications
	notifier        *notify.Notifier
	notifyCfg       config.Notifications
	currentUserID   string
	channelNames    map[string]string
	channelTypes    map[string]string
	workspaceName   string
	activeChannelID func() string
}
```

Add the import for the notify package at the top of the file:

```go
	"github.com/gammons/slk/internal/notify"
```

- [ ] **Step 2: Add notification check to OnMessage**

In `cmd/slk/main.go`, modify `OnMessage` to add notification logic. Add the notification check **after** the message is cached to SQLite and **before** the early return for inactive workspaces. This way notifications fire for all workspaces, not just the active one.

Replace the current `OnMessage` method:

```go
func (h *rtmEventHandler) OnMessage(channelID, userID, ts, text, threadTS string, edited bool) {
	// Cache every message to SQLite, regardless of active workspace
	h.db.UpsertMessage(cache.Message{
		TS:          ts,
		ChannelID:   channelID,
		WorkspaceID: h.workspaceID,
		UserID:      userID,
		Text:        text,
		ThreadTS:    threadTS,
		CreatedAt:   time.Now().Unix(),
	})

	// Check if this message should trigger a desktop notification.
	// Do this before the active workspace check so inactive workspaces
	// can still trigger notifications.
	if h.notifier != nil && h.notifyCfg.Enabled {
		isActiveWS := h.isActive != nil && h.isActive()
		activeChID := ""
		if h.activeChannelID != nil {
			activeChID = h.activeChannelID()
		}
		ctx := notify.NotifyContext{
			CurrentUserID:   h.currentUserID,
			ActiveChannelID: activeChID,
			IsActiveWS:      isActiveWS,
			OnMention:       h.notifyCfg.OnMention,
			OnDM:            h.notifyCfg.OnDM,
			OnKeyword:       h.notifyCfg.OnKeyword,
		}
		chType := h.channelTypes[channelID]
		if notify.ShouldNotify(ctx, channelID, userID, text, chType) {
			senderName := userID
			if resolved, ok := h.userNames[userID]; ok {
				senderName = resolved
			}
			chName := h.channelNames[channelID]
			title := h.workspaceName + ": #" + chName
			if chType == "dm" || chType == "group_dm" {
				title = h.workspaceName + ": " + senderName
			}
			body := senderName + ": " + notify.StripSlackMarkup(text)
			go h.notifier.Notify(title, body)
		}
	}

	if h.isActive != nil && !h.isActive() {
		// Inactive workspace — just notify about unread
		h.program.Send(ui.WorkspaceUnreadMsg{
			TeamID:    h.workspaceID,
			ChannelID: channelID,
		})
		return
	}

	userName := userID
	if resolved, ok := h.userNames[userID]; ok {
		userName = resolved
	}
	h.program.Send(ui.NewMessageMsg{
		ChannelID: channelID,
		Message: messages.MessageItem{
			TS:        ts,
			UserID:    userID,
			UserName:  userName,
			Text:      text,
			Timestamp: formatTimestamp(ts, h.tsFormat),
			ThreadTS:  threadTS,
			IsEdited:  edited,
		},
	})
}
```

- [ ] **Step 3: Wire notifier and context at handler construction**

In `cmd/slk/main.go`, find where the handler is constructed (around line 328-340). First, create the notifier once in the `run()` function, near where `cfg` is loaded (around line 67):

```go
	notifier := notify.New(cfg.Notifications.Enabled)
```

Then build channel name/type maps from `wctx.Channels` and pass them to the handler. Modify the handler construction:

```go
			// Build channel lookup maps for notifications
			channelNames := make(map[string]string, len(wctx.Channels))
			channelTypes := make(map[string]string, len(wctx.Channels))
			for _, ch := range wctx.Channels {
				channelNames[ch.ID] = ch.Name
				channelTypes[ch.ID] = ch.Type
			}

			handler := &rtmEventHandler{
				program:         p,
				userNames:       wctx.UserNames,
				tsFormat:        tsFormat,
				db:              db,
				workspaceID:     teamID,
				isActive:        func() bool { return teamID == activeTeamID },
				notifier:        notifier,
				notifyCfg:       cfg.Notifications,
				currentUserID:   wctx.UserID,
				channelNames:    channelNames,
				channelTypes:    channelTypes,
				workspaceName:   wctx.TeamName,
				activeChannelID: func() string { return app.ActiveChannelID() },
			}
```

- [ ] **Step 4: Add ActiveChannelID getter to App**

In `internal/ui/app.go`, add a public getter method:

```go
// ActiveChannelID returns the ID of the currently viewed channel.
func (a *App) ActiveChannelID() string {
	return a.activeChannelID
}
```

- [ ] **Step 5: Verify build**

Run: `go build ./...`
Expected: Compiles successfully.

- [ ] **Step 6: Run all tests**

Run: `go test ./...`
Expected: All tests pass.

- [ ] **Step 7: Commit**

```bash
git add cmd/slk/main.go internal/ui/app.go
git commit -m "feat: wire desktop notifications into message handler"
```

---

### Task 4: Update STATUS.md

**Files:**
- Modify: `docs/STATUS.md`

- [ ] **Step 1: Move notifications from "Not Yet Implemented" to "What's Working"**

In `docs/STATUS.md`, remove this line from the "Medium Priority" section:
```
- [ ] Desktop notifications (OS-level via notify-send/osascript)
```

Add to the "Messages" section under "What's Working":
```
- [x] Desktop notifications (mentions, DMs, keywords via beeep)
```

- [ ] **Step 2: Commit**

```bash
git add docs/STATUS.md
git commit -m "docs: mark desktop notifications as implemented"
```
