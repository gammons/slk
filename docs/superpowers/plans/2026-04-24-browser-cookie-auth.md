# Browser Cookie Auth (xoxc/xoxd) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Slack App auth (xoxp + xapp tokens, Socket Mode) with browser cookie auth (xoxc token + d cookie, RTM) so any user can connect without workspace admin permissions.

**Architecture:** The `Token` struct drops `AppToken`/`RefreshToken` and adds `Cookie`. The `Client` creates a custom `http.Client` with a cookie jar for the `d` cookie and uses `slack.OptionHTTPClient()`. Real-time events switch from Socket Mode to RTM via `api.NewRTM()` + `rtm.ManageConnection()`.

**Tech Stack:** Go 1.22+, slack-go (RTM + Web API), net/http (cookie jar)

**Spec:** `docs/superpowers/specs/2026-04-24-browser-cookie-auth.md`

---

## File Structure

Files being modified (no new files):

```
slk/
├── internal/slack/
│   ├── auth.go          # Token struct: remove AppToken/RefreshToken, add Cookie
│   ├── auth_test.go     # Update test tokens to use xoxc + cookie
│   ├── client.go        # Replace Socket Mode with RTM, cookie jar HTTP client
│   ├── client_test.go   # Update NewClient test for new signature
│   ├── events.go        # Replace Socket Mode dispatcher with RTM event loop
│   └── events_test.go   # Update mock/test for RTM events
├── cmd/slk/
│   ├── onboarding.go    # Prompt for xoxc token + d cookie instead of xapp/xoxp
│   └── main.go          # Update client wiring: pass cookie, start RTM
└── go.mod               # Remove socketmode dependency (go mod tidy)
```

---

## Task 1: Update Token Struct and Tests

**Files:**
- Modify: `internal/slack/auth.go:14-21`
- Modify: `internal/slack/auth_test.go`

- [ ] **Step 1: Update the Token struct**

In `internal/slack/auth.go`, replace the Token struct:

```go
// Token holds browser session credentials for a single Slack workspace.
type Token struct {
	AccessToken string `json:"access_token"` // xoxc-... token
	Cookie      string `json:"cookie"`       // d cookie value
	TeamID      string `json:"team_id"`
	TeamName    string `json:"team_name"`
}
```

Remove the `RefreshToken` and `AppToken` fields.

- [ ] **Step 2: Update auth tests for new Token struct**

In `internal/slack/auth_test.go`, replace `TestSaveAndLoadToken`:

```go
func TestSaveAndLoadToken(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	token := Token{
		AccessToken: "xoxc-test-token",
		Cookie:      "xoxd-test-cookie",
		TeamID:      "T123",
		TeamName:    "Acme",
	}

	if err := store.Save(token); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load("T123")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "xoxc-test-token" {
		t.Errorf("expected access token 'xoxc-test-token', got %q", got.AccessToken)
	}
	if got.Cookie != "xoxd-test-cookie" {
		t.Errorf("expected cookie 'xoxd-test-cookie', got %q", got.Cookie)
	}
	if got.TeamName != "Acme" {
		t.Errorf("expected team name 'Acme', got %q", got.TeamName)
	}
}
```

Update `TestListTokens` to use new fields:

```go
func TestListTokens(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	store.Save(Token{AccessToken: "t1", Cookie: "c1", TeamID: "T1", TeamName: "Team 1"})
	store.Save(Token{AccessToken: "t2", Cookie: "c2", TeamID: "T2", TeamName: "Team 2"})

	tokens, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(tokens))
	}
}
```

- [ ] **Step 3: Run auth tests**

Run: `go test ./internal/slack/ -run TestSave -v && go test ./internal/slack/ -run TestLoad -v && go test ./internal/slack/ -run TestList -v`
Expected: All PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/slack/auth.go internal/slack/auth_test.go
git commit -m "refactor: update Token struct for browser cookie auth (xoxc/xoxd)"
```

---

## Task 2: Replace Client with RTM and Cookie Jar

**Files:**
- Modify: `internal/slack/client.go`
- Modify: `internal/slack/client_test.go`

- [ ] **Step 1: Update client_test.go for new constructor**

Replace the entire test file:

```go
package slackclient

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient("xoxc-test", "test-cookie-value")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.TeamID() != "" {
		t.Error("expected empty team ID before connecting")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/slack/ -run TestNewClient -v`
Expected: FAIL -- `NewClient` still expects `(userToken, appToken string)` but semantics are changing. The test itself will pass since both are strings, but we need to verify the full rewrite compiles.

- [ ] **Step 3: Rewrite client.go**

Replace the entire `internal/slack/client.go` with:

```go
package slackclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"

	"github.com/slack-go/slack"
)

// SlackAPI defines the subset of the Slack API we use.
// This interface enables mocking in tests.
type SlackAPI interface {
	GetConversations(params *slack.GetConversationsParameters) ([]slack.Channel, string, error)
	GetConversationHistory(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	GetUsersContext(ctx context.Context) ([]slack.User, error)
	PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
	UpdateMessage(channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error)
	DeleteMessage(channelID, timestamp string) (string, string, error)
	AddReaction(name string, item slack.ItemRef) error
	RemoveReaction(name string, item slack.ItemRef) error
	AuthTest() (*slack.AuthTestResponse, error)
}

// Client wraps the slack-go library, providing RTM connectivity
// and a simplified Web API surface for the service layer.
// Uses browser cookie auth (xoxc token + d cookie).
type Client struct {
	api    *slack.Client
	rtm    *slack.RTM
	teamID string
	userID string
	token  string
	cookie string
}

// NewClient creates a new Slack client using browser cookie auth.
// xoxcToken is the xoxc-... token from the browser.
// dCookie is the value of the 'd' cookie from slack.com.
func NewClient(xoxcToken, dCookie string) *Client {
	httpClient := newCookieHTTPClient(dCookie)

	api := slack.New(
		xoxcToken,
		slack.OptionHTTPClient(httpClient),
	)

	return &Client{
		api:    api,
		token:  xoxcToken,
		cookie: dCookie,
	}
}

// newCookieHTTPClient creates an http.Client with the Slack 'd' cookie set.
func newCookieHTTPClient(dCookie string) *http.Client {
	jar, _ := cookiejar.New(nil)

	slackURL, _ := url.Parse("https://slack.com")
	jar.SetCookies(slackURL, []*http.Cookie{
		{
			Name:   "d",
			Value:  dCookie,
			Domain: ".slack.com",
			Path:   "/",
			Secure: true,
		},
	})

	return &http.Client{Jar: jar}
}

// TeamID returns the authenticated workspace's team ID.
// Empty before Connect is called.
func (c *Client) TeamID() string {
	return c.teamID
}

// UserID returns the authenticated user's ID.
// Empty before Connect is called.
func (c *Client) UserID() string {
	return c.userID
}

// Connect authenticates with Slack and populates the team/user IDs.
func (c *Client) Connect(ctx context.Context) error {
	resp, err := c.api.AuthTest()
	if err != nil {
		return fmt.Errorf("auth test failed: %w", err)
	}
	c.teamID = resp.TeamID
	c.userID = resp.UserID
	return nil
}

// StartRTM creates and starts the RTM connection.
// Events are dispatched to the provided handler in a goroutine.
// Call this after Connect.
func (c *Client) StartRTM(handler EventHandler) {
	c.rtm = c.api.NewRTM()
	go c.rtm.ManageConnection()
	go func() {
		for msg := range c.rtm.IncomingEvents {
			dispatchRTMEvent(msg, handler)
		}
	}()
}

// StopRTM disconnects the RTM connection.
func (c *Client) StopRTM() error {
	if c.rtm != nil {
		return c.rtm.Disconnect()
	}
	return nil
}

// GetChannels retrieves all conversations (channels, DMs, group DMs) the
// authenticated user has access to, paginating automatically.
func (c *Client) GetChannels(ctx context.Context) ([]slack.Channel, error) {
	var allChannels []slack.Channel
	cursor := ""

	for {
		params := &slack.GetConversationsParameters{
			Types:           []string{"public_channel", "private_channel", "mpim", "im"},
			Limit:           200,
			Cursor:          cursor,
			ExcludeArchived: true,
		}

		channels, nextCursor, err := c.api.GetConversations(params)
		if err != nil {
			return nil, fmt.Errorf("getting conversations: %w", err)
		}

		allChannels = append(allChannels, channels...)

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return allChannels, nil
}

// GetHistory retrieves message history for a channel.
// If oldest is set, returns messages newer than that timestamp.
func (c *Client) GetHistory(ctx context.Context, channelID string, limit int, oldest string) ([]slack.Message, error) {
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     limit,
	}
	if oldest != "" {
		params.Oldest = oldest
	}

	resp, err := c.api.GetConversationHistory(params)
	if err != nil {
		return nil, fmt.Errorf("getting history: %w", err)
	}

	return resp.Messages, nil
}

// GetOlderHistory retrieves messages older than the given timestamp.
func (c *Client) GetOlderHistory(ctx context.Context, channelID string, limit int, latest string) ([]slack.Message, error) {
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     limit,
		Latest:    latest,
	}

	resp, err := c.api.GetConversationHistory(params)
	if err != nil {
		return nil, fmt.Errorf("getting older history: %w", err)
	}

	return resp.Messages, nil
}

// GetUsers retrieves all users in the workspace.
func (c *Client) GetUsers(ctx context.Context) ([]slack.User, error) {
	users, err := c.api.GetUsersContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting users: %w", err)
	}
	return users, nil
}

// SendMessage posts a new message to the specified channel.
// Returns the timestamp of the sent message.
func (c *Client) SendMessage(ctx context.Context, channelID, text string) (string, error) {
	_, ts, err := c.api.PostMessage(channelID, slack.MsgOptionText(text, false))
	if err != nil {
		return "", fmt.Errorf("sending message: %w", err)
	}
	return ts, nil
}

// SendReply posts a threaded reply to the specified message.
func (c *Client) SendReply(ctx context.Context, channelID, threadTS, text string) (string, error) {
	_, ts, err := c.api.PostMessage(channelID,
		slack.MsgOptionText(text, false),
		slack.MsgOptionTS(threadTS),
	)
	if err != nil {
		return "", fmt.Errorf("sending reply: %w", err)
	}
	return ts, nil
}

// EditMessage updates an existing message's text.
func (c *Client) EditMessage(ctx context.Context, channelID, ts, text string) error {
	_, _, _, err := c.api.UpdateMessage(channelID, ts, slack.MsgOptionText(text, false))
	if err != nil {
		return fmt.Errorf("editing message: %w", err)
	}
	return nil
}

// RemoveMessage deletes a message from the channel.
func (c *Client) RemoveMessage(ctx context.Context, channelID, ts string) error {
	_, _, err := c.api.DeleteMessage(channelID, ts)
	if err != nil {
		return fmt.Errorf("deleting message: %w", err)
	}
	return nil
}

// AddReaction adds an emoji reaction to a message.
func (c *Client) AddReaction(ctx context.Context, channelID, ts, emoji string) error {
	return c.api.AddReaction(emoji, slack.ItemRef{Channel: channelID, Timestamp: ts})
}

// RemoveReaction removes an emoji reaction from a message.
func (c *Client) RemoveReaction(ctx context.Context, channelID, ts, emoji string) error {
	return c.api.RemoveReaction(emoji, slack.ItemRef{Channel: channelID, Timestamp: ts})
}

// ChannelSection represents a user's sidebar section from the undocumented Slack API.
type ChannelSection struct {
	ID         string   `json:"channel_section_id"`
	Name       string   `json:"name"`
	ChannelIDs []string `json:"channel_ids_page"`
	Type       string   `json:"type"`
}

// GetChannelSections calls the undocumented users.channelSections.list API
// to retrieve the user's sidebar sections. This may break if Slack changes the API.
func (c *Client) GetChannelSections(ctx context.Context) ([]ChannelSection, error) {
	endpoint := "https://slack.com/api/users.channelSections.list"

	form := url.Values{}
	form.Set("token", c.token)

	resp, err := http.PostForm(endpoint, form)
	if err != nil {
		return nil, fmt.Errorf("calling channelSections API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var result struct {
		OK              bool             `json:"ok"`
		Error           string           `json:"error"`
		ChannelSections []ChannelSection `json:"channel_sections"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	if !result.OK {
		return nil, fmt.Errorf("API error: %s (response: %s)", result.Error, string(body))
	}

	return result.ChannelSections, nil
}
```

Note: The `log` import is removed since we no longer use `socketmode.OptionLog`. The `socketmode` import is removed entirely.

- [ ] **Step 4: Run client test**

Run: `go test ./internal/slack/ -run TestNewClient -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "refactor: replace Socket Mode with RTM and cookie jar HTTP client"
```

---

## Task 3: Replace Event Dispatcher with RTM Events

**Files:**
- Modify: `internal/slack/events.go`
- Modify: `internal/slack/events_test.go`

- [ ] **Step 1: Rewrite events.go for RTM**

Replace the entire `internal/slack/events.go` with:

```go
package slackclient

import (
	"log"

	"github.com/slack-go/slack"
)

// EventHandler processes real-time events from Slack.
type EventHandler interface {
	OnMessage(channelID, userID, ts, text, threadTS string, edited bool)
	OnMessageDeleted(channelID, ts string)
	OnReactionAdded(channelID, ts, userID, emoji string)
	OnReactionRemoved(channelID, ts, userID, emoji string)
	OnPresenceChange(userID, presence string)
	OnUserTyping(channelID, userID string)
}

// dispatchRTMEvent routes a single RTM event to the appropriate EventHandler method.
func dispatchRTMEvent(msg slack.RTMEvent, handler EventHandler) {
	switch ev := msg.Data.(type) {
	case *slack.MessageEvent:
		switch ev.SubType {
		case "":
			handler.OnMessage(ev.Channel, ev.User, ev.Timestamp, ev.Text, ev.ThreadTimestamp, false)
		case "message_changed":
			if ev.SubMessage != nil {
				handler.OnMessage(ev.Channel, ev.SubMessage.User, ev.SubMessage.Timestamp, ev.SubMessage.Text, ev.SubMessage.ThreadTimestamp, true)
			}
		case "message_deleted":
			handler.OnMessageDeleted(ev.Channel, ev.DeletedTimestamp)
		}

	case *slack.ReactionAddedEvent:
		handler.OnReactionAdded(ev.Item.Channel, ev.Item.Timestamp, ev.User, ev.Reaction)

	case *slack.ReactionRemovedEvent:
		handler.OnReactionRemoved(ev.Item.Channel, ev.Item.Timestamp, ev.User, ev.Reaction)

	case *slack.PresenceChangeEvent:
		handler.OnPresenceChange(ev.User, ev.Presence)

	case *slack.UserTypingEvent:
		handler.OnUserTyping(ev.Channel, ev.User)

	case *slack.ConnectedEvent:
		log.Printf("RTM connected: %s (connection count: %d)", ev.Info.User.Name, ev.ConnectionCount)

	case *slack.InvalidAuthEvent:
		log.Printf("RTM authentication expired -- re-run --add-workspace to re-authenticate")

	case *slack.LatencyReport:
		// Silently ignore latency reports

	case *slack.HelloEvent:
		// Silently ignore hello events

	case *slack.ConnectingEvent:
		log.Printf("RTM connecting (attempt %d)...", ev.Attempt)

	case *slack.ConnectionErrorEvent:
		log.Printf("RTM connection error: %v", ev.Error())

	default:
		// Ignore other event types
	}
}
```

- [ ] **Step 2: Update events_test.go**

Replace the entire `internal/slack/events_test.go` with:

```go
package slackclient

import (
	"testing"

	"github.com/slack-go/slack"
)

type mockEventHandler struct {
	messages        []string
	deletedMessages []string
	reactions       []string
	presenceChanges []string
	typingEvents    []string
}

func (m *mockEventHandler) OnMessage(channelID, userID, ts, text, threadTS string, edited bool) {
	m.messages = append(m.messages, text)
}

func (m *mockEventHandler) OnMessageDeleted(channelID, ts string) {
	m.deletedMessages = append(m.deletedMessages, ts)
}

func (m *mockEventHandler) OnReactionAdded(channelID, ts, userID, emoji string) {
	m.reactions = append(m.reactions, emoji)
}

func (m *mockEventHandler) OnReactionRemoved(channelID, ts, userID, emoji string) {}
func (m *mockEventHandler) OnPresenceChange(userID, presence string) {
	m.presenceChanges = append(m.presenceChanges, userID+":"+presence)
}
func (m *mockEventHandler) OnUserTyping(channelID, userID string) {
	m.typingEvents = append(m.typingEvents, channelID+":"+userID)
}

func TestEventHandlerInterface(t *testing.T) {
	handler := &mockEventHandler{}

	// Verify the interface is satisfied
	var _ EventHandler = handler

	handler.OnMessage("C1", "U1", "123.456", "hello", "", false)
	if len(handler.messages) != 1 || handler.messages[0] != "hello" {
		t.Error("expected message to be recorded")
	}
}

func TestDispatchRTMMessageEvent(t *testing.T) {
	handler := &mockEventHandler{}

	evt := slack.RTMEvent{
		Type: "message",
		Data: &slack.MessageEvent{
			Msg: slack.Msg{
				Channel:   "C1",
				User:      "U1",
				Text:      "hello world",
				Timestamp: "123.456",
			},
		},
	}

	dispatchRTMEvent(evt, handler)

	if len(handler.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(handler.messages))
	}
	if handler.messages[0] != "hello world" {
		t.Errorf("expected 'hello world', got %q", handler.messages[0])
	}
}

func TestDispatchRTMReactionAddedEvent(t *testing.T) {
	handler := &mockEventHandler{}

	evt := slack.RTMEvent{
		Type: "reaction_added",
		Data: &slack.ReactionAddedEvent{
			User:     "U1",
			Reaction: "thumbsup",
			Item: slack.ReactionItem{
				Channel:   "C1",
				Timestamp: "123.456",
			},
		},
	}

	dispatchRTMEvent(evt, handler)

	if len(handler.reactions) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(handler.reactions))
	}
	if handler.reactions[0] != "thumbsup" {
		t.Errorf("expected 'thumbsup', got %q", handler.reactions[0])
	}
}

func TestDispatchRTMPresenceChangeEvent(t *testing.T) {
	handler := &mockEventHandler{}

	evt := slack.RTMEvent{
		Type: "presence_change",
		Data: &slack.PresenceChangeEvent{
			User:     "U1",
			Presence: "active",
		},
	}

	dispatchRTMEvent(evt, handler)

	if len(handler.presenceChanges) != 1 {
		t.Fatalf("expected 1 presence change, got %d", len(handler.presenceChanges))
	}
	if handler.presenceChanges[0] != "U1:active" {
		t.Errorf("expected 'U1:active', got %q", handler.presenceChanges[0])
	}
}

func TestDispatchRTMMessageDeletedEvent(t *testing.T) {
	handler := &mockEventHandler{}

	evt := slack.RTMEvent{
		Type: "message",
		Data: &slack.MessageEvent{
			Msg: slack.Msg{
				Channel:          "C1",
				SubType:          "message_deleted",
				DeletedTimestamp: "123.456",
			},
		},
	}

	dispatchRTMEvent(evt, handler)

	if len(handler.deletedMessages) != 1 {
		t.Fatalf("expected 1 deleted message, got %d", len(handler.deletedMessages))
	}
	if handler.deletedMessages[0] != "123.456" {
		t.Errorf("expected '123.456', got %q", handler.deletedMessages[0])
	}
}
```

- [ ] **Step 3: Run event tests**

Run: `go test ./internal/slack/ -run TestEvent -v && go test ./internal/slack/ -run TestDispatch -v`
Expected: All PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/slack/events.go internal/slack/events_test.go
git commit -m "refactor: replace Socket Mode event dispatcher with RTM event loop"
```

---

## Task 4: Update Onboarding Flow

**Files:**
- Modify: `cmd/slk/onboarding.go`

- [ ] **Step 1: Rewrite onboarding.go**

Replace the entire `cmd/slk/onboarding.go` with:

```go
package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	slackclient "github.com/gammons/slk/internal/slack"
)

func addWorkspace() error {
	dataDir := xdgData()
	tokenDir := filepath.Join(dataDir, "tokens")
	tokenStore := slackclient.NewTokenStore(tokenDir)

	// Styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#4A9EFF")).
		MarginBottom(1)

	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		MarginBottom(1)

	stepStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#50C878"))

	successStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#50C878")).
		MarginTop(1)

	errorStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#E04040"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666"))

	// Welcome
	fmt.Println()
	fmt.Println(titleStyle.Render("slk -- Add Workspace"))
	fmt.Println(subtitleStyle.Render("Connect a Slack workspace using your browser session."))
	fmt.Println()

	// Step 1: Instructions
	fmt.Println(stepStyle.Render("Step 1: Get your browser tokens"))
	fmt.Println()
	fmt.Println(dimStyle.Render("  a. Open Slack in your browser and log into your workspace"))
	fmt.Println(dimStyle.Render("  b. Open DevTools (F12 or Cmd+Option+I)"))
	fmt.Println(dimStyle.Render("  c. Go to Application > Cookies > https://app.slack.com"))
	fmt.Println(dimStyle.Render("     Find the cookie named 'd' and copy its value"))
	fmt.Println(dimStyle.Render("  d. Go to the Console tab and run:"))
	fmt.Println(dimStyle.Render("     JSON.parse(localStorage.localConfig_v2).teams[Object.keys(JSON.parse(localStorage.localConfig_v2).teams)[0]].token"))
	fmt.Println(dimStyle.Render("     Copy the xoxc-... token"))
	fmt.Println()

	// Step 2: Enter tokens via huh form
	fmt.Println(stepStyle.Render("Step 2: Enter your tokens"))
	fmt.Println()

	var xoxcToken, dCookie string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Token (xoxc)").
				Description("The xoxc-... token from your browser console").
				Placeholder("xoxc-...").
				Value(&xoxcToken).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return fmt.Errorf("token is required")
					}
					if !strings.HasPrefix(s, "xoxc-") {
						return fmt.Errorf("must start with xoxc-")
					}
					return nil
				}),

			huh.NewInput().
				Title("Cookie (d)").
				Description("The 'd' cookie value from Application > Cookies").
				Placeholder("xoxd-...").
				Value(&dCookie).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return fmt.Errorf("cookie is required")
					}
					return nil
				}),
		),
	).WithTheme(huh.ThemeDracula())

	err := form.Run()
	if err != nil {
		return fmt.Errorf("form cancelled")
	}

	xoxcToken = strings.TrimSpace(xoxcToken)
	dCookie = strings.TrimSpace(dCookie)

	// Step 3: Validate tokens with spinner
	fmt.Println()
	fmt.Println(stepStyle.Render("Step 3: Validating tokens..."))

	var client *slackclient.Client
	var connectErr error

	spinErr := spinner.New().
		Title("Connecting to Slack...").
		Action(func() {
			client = slackclient.NewClient(xoxcToken, dCookie)
			connectErr = client.Connect(context.Background())
		}).
		Run()

	if spinErr != nil {
		return fmt.Errorf("spinner error: %w", spinErr)
	}

	if connectErr != nil {
		fmt.Println(errorStyle.Render("  Authentication failed: " + connectErr.Error()))
		fmt.Println()
		fmt.Println(dimStyle.Render("  Make sure you're logged into Slack in your browser"))
		fmt.Println(dimStyle.Render("  and that you copied the correct token and cookie values."))
		return fmt.Errorf("authentication failed: %w", connectErr)
	}

	teamID := client.TeamID()
	fmt.Println(successStyle.Render("  Connected!") + dimStyle.Render(fmt.Sprintf(" (team: %s)", teamID)))
	fmt.Println()

	// Step 4: Workspace name
	fmt.Println(stepStyle.Render("Step 4: Name your workspace"))
	fmt.Println()

	var wsName string
	nameForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Display Name").
				Description("A friendly name for this workspace (e.g., 'Acme Corp')").
				Placeholder(teamID).
				Value(&wsName),
		),
	).WithTheme(huh.ThemeDracula())

	if err := nameForm.Run(); err != nil {
		wsName = teamID
	}

	wsName = strings.TrimSpace(wsName)
	if wsName == "" {
		wsName = teamID
	}

	// Save
	token := slackclient.Token{
		AccessToken: xoxcToken,
		Cookie:      dCookie,
		TeamID:      teamID,
		TeamName:    wsName,
	}

	if err := tokenStore.Save(token); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}

	fmt.Println()
	fmt.Println(successStyle.Render(fmt.Sprintf("  Workspace '%s' added successfully!", wsName)))
	fmt.Println()
	fmt.Println(dimStyle.Render("  Run ") + lipgloss.NewStyle().Bold(true).Render("slk") + dimStyle.Render(" to start."))
	fmt.Println()

	return nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./cmd/slk/`
Expected: Compiles with no errors.

- [ ] **Step 3: Commit**

```bash
git add cmd/slk/onboarding.go
git commit -m "refactor: update onboarding to prompt for xoxc token and d cookie"
```

---

## Task 5: Update main.go Wiring

**Files:**
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Update client creation in main.go**

In `cmd/slk/main.go`, find the line inside the `for _, token := range tokens` loop:

```go
		client := slackclient.NewClient(token.AccessToken, token.AppToken)
```

Replace with:

```go
		client := slackclient.NewClient(token.AccessToken, token.Cookie)
```

- [ ] **Step 2: Remove the RunSocketMode reference if present**

Search `main.go` for any reference to `RunSocketMode`. Currently there is none in the `run()` function (Socket Mode was set up in the client but not started from main.go), so no change needed here. The `StartRTM` method is available but we won't start it yet -- wiring RTM events to the UI is a separate feature (listed as "Real-time event handling" in STATUS.md).

- [ ] **Step 3: Verify the full build compiles**

Run: `go build ./cmd/slk/`
Expected: Compiles with no errors.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go
git commit -m "refactor: update main.go to pass cookie to client constructor"
```

---

## Task 6: Clean Up Dependencies and Run All Tests

**Files:**
- Modify: `go.mod`, `go.sum` (via go mod tidy)

- [ ] **Step 1: Run go mod tidy**

Run: `go mod tidy`
Expected: Removes any unused dependencies (socketmode may or may not be removed since it's still in go.mod transitively via slack-go).

- [ ] **Step 2: Run all tests**

Run: `go test ./... -v -race`
Expected: All tests PASS.

- [ ] **Step 3: Verify build**

Run: `make build`
Expected: Binary builds successfully at `bin/slk`.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: tidy go modules after Socket Mode removal"
```

---

## Task 7: Update Documentation

**Files:**
- Modify: `docs/STATUS.md`
- Modify: `README.md`

- [ ] **Step 1: Update STATUS.md**

In `docs/STATUS.md`, move the browser cookie auth item from "Not Yet Implemented > High Priority" to "What's Working > Core":

Add to the Core section:
```
- [x] Browser cookie auth (xoxc/xoxd) -- connect using browser session tokens, no Slack App needed
- [x] RTM (Real-Time Messaging) client -- replaces Socket Mode for real-time connectivity
```

Remove from High Priority:
```
- [ ] **Browser cookie auth (xoxc/xoxd)** -- allows any user to connect without admin permissions...
```

- [ ] **Step 2: Update README.md Setup section**

Replace the entire "Setup" section (from `## Setup` through the `--add-workspace` section) with:

```markdown
## Setup

### 1. Log Into Slack in Your Browser

Open [https://app.slack.com](https://app.slack.com) and log into your workspace.

### 2. Get Your Browser Tokens

**Get the `d` cookie:**
- Open DevTools (F12 or Cmd+Option+I)
- Go to Application > Cookies > `https://app.slack.com`
- Find the cookie named `d` and copy its value

**Get the `xoxc` token:**
- Go to the Console tab in DevTools and run:
  ```javascript
  JSON.parse(localStorage.localConfig_v2).teams[Object.keys(JSON.parse(localStorage.localConfig_v2).teams)[0]].token
  ```
- Copy the `xoxc-...` token

### 3. Add Workspace

```bash
./bin/slk --add-workspace
```

This launches an interactive onboarding that prompts for your `xoxc` token and `d` cookie.

Alternatively, just run `./bin/slk` -- it will launch onboarding automatically if no workspaces are configured.
```

Also remove the "Prerequisites" section's reference to "A Slack workspace with a configured Slack App" and replace with:

```markdown
- Go 1.22+
- A Slack workspace (log in via browser to get auth tokens)
```

- [ ] **Step 3: Commit**

```bash
git add docs/STATUS.md README.md
git commit -m "docs: update README and STATUS for browser cookie auth"
```
