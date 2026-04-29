# Presence & DND Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user set their own Slack presence (active/away) and DND/snooze state from a `Ctrl+S` picker, show the live state in the status bar (reflecting external changes from other clients or the user's own API scripts), and suppress OS notifications while DND is active.

**Architecture:** Read path uses three new WebSocket events (`manual_presence_change`, `dnd_updated`, `dnd_updated_user`) plus a one-shot `presence_sub` frame and an initial REST fetch on connect. State lives per `WorkspaceContext`; the active workspace's state drives the status bar. Write path goes through new pass-through wrappers around `slack-go`'s `SetUserPresence`, `SetSnooze`, `EndSnooze`, `EndDND`. UI is a new `internal/ui/presencemenu` package modeled on `internal/ui/themeswitcher`.

**Tech Stack:** Go 1.22+, `github.com/slack-go/slack` v0.23.0, bubbletea, lipgloss, `gorilla/websocket`.

**Spec:** [`docs/superpowers/specs/2026-04-29-presence-and-dnd-design.md`](../specs/2026-04-29-presence-and-dnd-design.md)

---

## File Structure

| File | Role |
|---|---|
| `internal/slack/client.go` | New `SlackAPI` methods + `Client` wrappers for presence + DND; new `SubscribePresence` WS-frame helper |
| `internal/slack/client_test.go` | Tests for the new wrappers (mock-based) |
| `internal/slack/events.go` | Three new WS event cases; `EventHandler` gains `OnSelfPresenceChange` + `OnDNDChange` |
| `internal/slack/events_test.go` | Dispatch tests for the new events; `mockEventHandler` extended |
| `internal/notify/notifier.go` | `NotifyContext.IsDND` field; early-return in `ShouldNotify` |
| `internal/notify/notifier_test.go` | `TestShouldNotify_SuppressedByDND` |
| `internal/ui/statusbar/model.go` | New `presence`, `dndEnabled`, `dndEndTS` fields; `SetStatus` setter; new right-side segment; `DNDTickMsg` |
| `internal/ui/statusbar/model_test.go` | Format tests for each presence/DND state |
| `internal/ui/presencemenu/model.go` | NEW package: picker overlay model |
| `internal/ui/presencemenu/model_test.go` | Picker unit tests |
| `internal/ui/keys.go` | New `PresenceMenu` binding |
| `internal/ui/mode.go` | New `ModePresenceMenu`, `ModePresenceCustomSnooze` |
| `internal/ui/app.go` | Wire `Ctrl+S`; `handlePresenceMenuMode`; `handlePresenceCustomSnoozeMode`; `StatusChangeMsg` handler + per-workspace cache; trigger optimistic API calls |
| `cmd/slk/main.go` | New `WorkspaceContext` fields; `bootstrapPresenceAndDND` goroutine post-connect; `rtmEventHandler` implements new `EventHandler` methods; populates `notify.NotifyContext.IsDND` |
| `docs/STATUS.md` | Mark presence/DND items done |
| `README.md` | Update roadmap and keybinding table |

Each task below is self-contained, follows TDD where it makes sense, and ends in a single git commit.

---

## Task 1: SlackAPI presence methods (interface + wrappers + tests)

**Files:**
- Modify: `internal/slack/client.go:21-37` (interface), append new methods near line 547
- Modify: `internal/slack/client_test.go` (find existing mock and extend)

- [ ] **Step 1.1: Inspect the existing test mock**

```bash
grep -n "type mockSlackAPI\|mockSlackAPI struct" internal/slack/client_test.go
```

Note: the file defines a mock implementing the `SlackAPI` interface. New interface methods must be added to the mock to keep tests compiling. Read the relevant section before editing.

- [ ] **Step 1.2: Write the failing test for `SetUserPresence`**

Add to `internal/slack/client_test.go`:

```go
func TestClient_SetUserPresence(t *testing.T) {
	mock := &mockSlackAPI{}
	c := &Client{api: mock}
	if err := c.SetUserPresence(context.Background(), "away"); err != nil {
		t.Fatalf("SetUserPresence: %v", err)
	}
	if mock.setUserPresenceCalls != 1 {
		t.Errorf("expected 1 SetUserPresence call, got %d", mock.setUserPresenceCalls)
	}
	if mock.lastPresence != "away" {
		t.Errorf("expected presence 'away', got %q", mock.lastPresence)
	}
}

func TestClient_GetUserPresence(t *testing.T) {
	mock := &mockSlackAPI{
		userPresence: &slack.UserPresence{Presence: "active"},
	}
	c := &Client{api: mock}
	got, err := c.GetUserPresence(context.Background(), "U1")
	if err != nil {
		t.Fatalf("GetUserPresence: %v", err)
	}
	if got.Presence != "active" {
		t.Errorf("expected 'active', got %q", got.Presence)
	}
	if mock.lastUserPresenceUser != "U1" {
		t.Errorf("expected user 'U1', got %q", mock.lastUserPresenceUser)
	}
}
```

Add corresponding fields to the `mockSlackAPI` struct and stub methods (search for an existing similar mock method like `AddReaction` to match the style):

```go
// On mockSlackAPI struct, add fields:
setUserPresenceCalls   int
lastPresence           string
userPresence           *slack.UserPresence
lastUserPresenceUser   string

// Methods:
func (m *mockSlackAPI) SetUserPresenceContext(ctx context.Context, presence string) error {
	m.setUserPresenceCalls++
	m.lastPresence = presence
	return nil
}
func (m *mockSlackAPI) GetUserPresenceContext(ctx context.Context, user string) (*slack.UserPresence, error) {
	m.lastUserPresenceUser = user
	if m.userPresence != nil {
		return m.userPresence, nil
	}
	return &slack.UserPresence{}, nil
}
```

- [ ] **Step 1.3: Verify the test fails**

```bash
go test ./internal/slack/ -run TestClient_SetUserPresence -v
```

Expected: build failure or `Client.SetUserPresence undefined`.

- [ ] **Step 1.4: Extend the `SlackAPI` interface and add wrappers**

In `internal/slack/client.go:21-37`, add to the interface:

```go
SetUserPresenceContext(ctx context.Context, presence string) error
GetUserPresenceContext(ctx context.Context, user string) (*slack.UserPresence, error)
```

Append wrapper methods near line 547 (after `RemoveReaction`):

```go
// SetUserPresence sets the authenticated user's presence to "auto" or "away".
func (c *Client) SetUserPresence(ctx context.Context, presence string) error {
	if err := c.api.SetUserPresenceContext(ctx, presence); err != nil {
		return fmt.Errorf("setting presence: %w", err)
	}
	return nil
}

// GetUserPresence fetches a user's current presence ("active" or "away").
// Pass the authenticated user's ID to read your own state.
func (c *Client) GetUserPresence(ctx context.Context, userID string) (*slack.UserPresence, error) {
	p, err := c.api.GetUserPresenceContext(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("getting presence: %w", err)
	}
	return p, nil
}
```

- [ ] **Step 1.5: Verify the tests pass**

```bash
go test ./internal/slack/ -run TestClient_SetUserPresence -v
go test ./internal/slack/ -run TestClient_GetUserPresence -v
go build ./...
```

Expected: PASS, clean build.

- [ ] **Step 1.6: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "feat(slack): add SetUserPresence / GetUserPresence wrappers"
```

---

## Task 2: SlackAPI DND methods (interface + wrappers + tests)

**Files:**
- Modify: `internal/slack/client.go` (interface and wrappers)
- Modify: `internal/slack/client_test.go` (mock + tests)

- [ ] **Step 2.1: Write failing tests**

Append to `internal/slack/client_test.go`:

```go
func TestClient_SetSnooze(t *testing.T) {
	mock := &mockSlackAPI{
		dndStatus: &slack.DNDStatus{
			SnoozeInfo: slack.SnoozeInfo{SnoozeEnabled: true, SnoozeEndTime: 1700000000},
		},
	}
	c := &Client{api: mock}
	got, err := c.SetSnooze(context.Background(), 60)
	if err != nil {
		t.Fatalf("SetSnooze: %v", err)
	}
	if !got.SnoozeEnabled {
		t.Error("expected snooze enabled")
	}
	if mock.lastSnoozeMinutes != 60 {
		t.Errorf("expected 60 minutes, got %d", mock.lastSnoozeMinutes)
	}
}

func TestClient_EndSnooze(t *testing.T) {
	mock := &mockSlackAPI{
		dndStatus: &slack.DNDStatus{},
	}
	c := &Client{api: mock}
	if _, err := c.EndSnooze(context.Background()); err != nil {
		t.Fatalf("EndSnooze: %v", err)
	}
	if mock.endSnoozeCalls != 1 {
		t.Errorf("expected 1 EndSnooze call, got %d", mock.endSnoozeCalls)
	}
}

func TestClient_EndDND(t *testing.T) {
	mock := &mockSlackAPI{}
	c := &Client{api: mock}
	if err := c.EndDND(context.Background()); err != nil {
		t.Fatalf("EndDND: %v", err)
	}
	if mock.endDNDCalls != 1 {
		t.Errorf("expected 1 EndDND call, got %d", mock.endDNDCalls)
	}
}

func TestClient_GetDNDInfo(t *testing.T) {
	mock := &mockSlackAPI{
		dndStatus: &slack.DNDStatus{
			Enabled: true,
			SnoozeInfo: slack.SnoozeInfo{SnoozeEnabled: true, SnoozeEndTime: 1700000000},
		},
	}
	c := &Client{api: mock}
	got, err := c.GetDNDInfo(context.Background(), "U1")
	if err != nil {
		t.Fatalf("GetDNDInfo: %v", err)
	}
	if !got.SnoozeEnabled || got.SnoozeEndTime != 1700000000 {
		t.Errorf("unexpected DND status: %+v", got)
	}
	if mock.lastDNDInfoUser != "U1" {
		t.Errorf("expected user 'U1', got %q", mock.lastDNDInfoUser)
	}
}
```

Extend `mockSlackAPI` with:

```go
// Fields:
dndStatus           *slack.DNDStatus
lastSnoozeMinutes   int
endSnoozeCalls      int
endDNDCalls         int
lastDNDInfoUser     string

// Methods:
func (m *mockSlackAPI) SetSnoozeContext(ctx context.Context, minutes int) (*slack.DNDStatus, error) {
	m.lastSnoozeMinutes = minutes
	if m.dndStatus != nil {
		return m.dndStatus, nil
	}
	return &slack.DNDStatus{}, nil
}
func (m *mockSlackAPI) EndSnoozeContext(ctx context.Context) (*slack.DNDStatus, error) {
	m.endSnoozeCalls++
	if m.dndStatus != nil {
		return m.dndStatus, nil
	}
	return &slack.DNDStatus{}, nil
}
func (m *mockSlackAPI) EndDNDContext(ctx context.Context) error {
	m.endDNDCalls++
	return nil
}
func (m *mockSlackAPI) GetDNDInfoContext(ctx context.Context, user *string, options ...slack.ParamOption) (*slack.DNDStatus, error) {
	if user != nil {
		m.lastDNDInfoUser = *user
	}
	if m.dndStatus != nil {
		return m.dndStatus, nil
	}
	return &slack.DNDStatus{}, nil
}
```

- [ ] **Step 2.2: Verify tests fail to compile**

```bash
go test ./internal/slack/ 2>&1 | head -20
```

Expected: build failures referencing missing `Client.SetSnooze`, etc.

- [ ] **Step 2.3: Extend the `SlackAPI` interface**

Add to the interface in `internal/slack/client.go:21-37`:

```go
SetSnoozeContext(ctx context.Context, minutes int) (*slack.DNDStatus, error)
EndSnoozeContext(ctx context.Context) (*slack.DNDStatus, error)
EndDNDContext(ctx context.Context) error
GetDNDInfoContext(ctx context.Context, user *string, options ...slack.ParamOption) (*slack.DNDStatus, error)
```

- [ ] **Step 2.4: Add `Client` wrappers**

Append after `GetUserPresence` (added in Task 1):

```go
// SetSnooze enables Do-Not-Disturb for `minutes` minutes.
func (c *Client) SetSnooze(ctx context.Context, minutes int) (*slack.DNDStatus, error) {
	st, err := c.api.SetSnoozeContext(ctx, minutes)
	if err != nil {
		return nil, fmt.Errorf("setting snooze: %w", err)
	}
	return st, nil
}

// EndSnooze ends the current snooze window. Does NOT end admin-scheduled DND.
func (c *Client) EndSnooze(ctx context.Context) (*slack.DNDStatus, error) {
	st, err := c.api.EndSnoozeContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("ending snooze: %w", err)
	}
	return st, nil
}

// EndDND ends the user's current scheduled DND session.
func (c *Client) EndDND(ctx context.Context) error {
	if err := c.api.EndDNDContext(ctx); err != nil {
		return fmt.Errorf("ending DND: %w", err)
	}
	return nil
}

// GetDNDInfo fetches DND/snooze status for a user.
func (c *Client) GetDNDInfo(ctx context.Context, userID string) (*slack.DNDStatus, error) {
	u := userID
	st, err := c.api.GetDNDInfoContext(ctx, &u)
	if err != nil {
		return nil, fmt.Errorf("getting DND info: %w", err)
	}
	return st, nil
}
```

- [ ] **Step 2.5: Verify tests pass**

```bash
go test ./internal/slack/ -run TestClient_ -v
go build ./...
```

Expected: PASS.

- [ ] **Step 2.6: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "feat(slack): add SetSnooze / EndSnooze / EndDND / GetDNDInfo wrappers"
```

---

## Task 3: SubscribePresence WebSocket frame helper

**Files:**
- Modify: `internal/slack/client.go` (append after `SendTyping`)

This sends `{"type":"presence_sub","ids":[...]}` on the existing WS connection so Slack delivers `presence_change` events for our own user (and any others passed in). It mirrors `SendTyping`. There is no easy unit test for it (requires a real WS); we verify by integration when the bootstrap goroutine runs in Task 7. Keep it minimal.

- [ ] **Step 3.1: Add the method**

Append after `SendTyping` (around line 204):

```go
// SubscribePresence asks Slack to deliver presence_change events for the
// given user IDs. Sent over the existing WebSocket connection. Slack only
// emits presence_change for users you've explicitly subscribed to.
func (c *Client) SubscribePresence(userIDs []string) error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	if c.wsConn == nil {
		return fmt.Errorf("websocket not connected")
	}
	msg := map[string]interface{}{
		"type": "presence_sub",
		"ids":  userIDs,
	}
	return c.wsConn.WriteJSON(msg)
}
```

- [ ] **Step 3.2: Verify the build is clean**

```bash
go build ./...
go test ./internal/slack/
```

Expected: clean build, all tests pass.

- [ ] **Step 3.3: Commit**

```bash
git add internal/slack/client.go
git commit -m "feat(slack): add SubscribePresence WS frame helper"
```

---

## Task 4: WS event dispatch for self-presence and DND

**Files:**
- Modify: `internal/slack/events.go`
- Modify: `internal/slack/events_test.go`

- [ ] **Step 4.1: Write failing tests**

Append to `internal/slack/events_test.go`:

```go
func TestDispatchWebSocketManualPresenceChangeEvent(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"manual_presence_change","presence":"away"}`)
	dispatchWebSocketEvent(data, handler)
	if len(handler.selfPresenceChanges) != 1 {
		t.Fatalf("expected 1 self presence change, got %d", len(handler.selfPresenceChanges))
	}
	if handler.selfPresenceChanges[0] != "away" {
		t.Errorf("expected 'away', got %q", handler.selfPresenceChanges[0])
	}
}

func TestDispatchWebSocketDNDUpdatedEvent(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"dnd_updated","dnd_status":{"dnd_enabled":true,"snooze_enabled":true,"snooze_endtime":1700000000,"next_dnd_start_ts":0,"next_dnd_end_ts":0}}`)
	dispatchWebSocketEvent(data, handler)
	if len(handler.dndChanges) != 1 {
		t.Fatalf("expected 1 dnd change, got %d", len(handler.dndChanges))
	}
	got := handler.dndChanges[0]
	if !got.enabled {
		t.Error("expected enabled=true")
	}
	if got.endUnix != 1700000000 {
		t.Errorf("expected endUnix=1700000000, got %d", got.endUnix)
	}
}

func TestDispatchWebSocketDNDUpdatedUserEvent(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"dnd_updated_user","dnd_status":{"dnd_enabled":false,"snooze_enabled":false,"next_dnd_start_ts":0,"next_dnd_end_ts":0}}`)
	dispatchWebSocketEvent(data, handler)
	if len(handler.dndChanges) != 1 {
		t.Fatalf("expected 1 dnd change, got %d", len(handler.dndChanges))
	}
	got := handler.dndChanges[0]
	if got.enabled {
		t.Error("expected enabled=false")
	}
	if got.endUnix != 0 {
		t.Errorf("expected endUnix=0, got %d", got.endUnix)
	}
}

func TestDispatchWebSocketDNDUpdatedEvent_NextDNDEndFallback(t *testing.T) {
	// When snooze is not enabled but admin DND has next_dnd_end_ts in the
	// future, that's the relevant end timestamp.
	handler := &mockEventHandler{}
	data := []byte(`{"type":"dnd_updated","dnd_status":{"dnd_enabled":true,"snooze_enabled":false,"snooze_endtime":0,"next_dnd_start_ts":1699000000,"next_dnd_end_ts":1700000000}}`)
	dispatchWebSocketEvent(data, handler)
	if got := handler.dndChanges[0].endUnix; got != 1700000000 {
		t.Errorf("expected endUnix=1700000000 (next_dnd_end_ts), got %d", got)
	}
}
```

Extend `mockEventHandler` (top of the file) — add fields and methods:

```go
// Fields to add:
selfPresenceChanges []string
dndChanges          []dndChangeRecord

// New helper struct (declared at top of file, near mockEventHandler):
type dndChangeRecord struct {
	enabled bool
	endUnix int64
}

// New methods:
func (m *mockEventHandler) OnSelfPresenceChange(presence string) {
	m.selfPresenceChanges = append(m.selfPresenceChanges, presence)
}
func (m *mockEventHandler) OnDNDChange(enabled bool, endUnix int64) {
	m.dndChanges = append(m.dndChanges, dndChangeRecord{enabled, endUnix})
}
```

- [ ] **Step 4.2: Verify tests fail to compile**

```bash
go test ./internal/slack/
```

Expected: build error referencing missing `EventHandler` methods.

- [ ] **Step 4.3: Extend the `EventHandler` interface**

In `internal/slack/events.go:8-21`, add to the interface:

```go
OnSelfPresenceChange(presence string)
OnDNDChange(enabled bool, endUnix int64)
```

- [ ] **Step 4.4: Add event types and dispatch cases**

After the `wsTypingEvent` struct (around line 74), add:

```go
// wsManualPresenceEvent represents a manual_presence_change event,
// emitted when the authenticated user's own presence flips.
type wsManualPresenceEvent struct {
	Type     string `json:"type"`
	Presence string `json:"presence"`
}

// wsDNDStatusInner mirrors the dnd_status payload Slack ships with
// dnd_updated and dnd_updated_user events.
type wsDNDStatusInner struct {
	Enabled            bool  `json:"dnd_enabled"`
	SnoozeEnabled      bool  `json:"snooze_enabled"`
	SnoozeEndTime      int64 `json:"snooze_endtime"`
	NextDNDStartTS     int64 `json:"next_dnd_start_ts"`
	NextDNDEndTS       int64 `json:"next_dnd_end_ts"`
}

// wsDNDUpdatedEvent represents a dnd_updated or dnd_updated_user event.
type wsDNDUpdatedEvent struct {
	Type      string           `json:"type"`
	DNDStatus wsDNDStatusInner `json:"dnd_status"`
}
```

In the `switch evt.Type` block in `dispatchWebSocketEvent`, add cases (place them near the `presence_change` case):

```go
case "manual_presence_change":
	var evt wsManualPresenceEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	handler.OnSelfPresenceChange(evt.Presence)

case "dnd_updated", "dnd_updated_user":
	var evt wsDNDUpdatedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}
	end := pickDNDEnd(evt.DNDStatus)
	handler.OnDNDChange(evt.DNDStatus.Enabled, end)
```

Append a small helper at the bottom of the file:

```go
// pickDNDEnd unifies snooze and admin-DND end timestamps. Slack delivers
// either snooze_endtime (when the user has a manual snooze) or
// next_dnd_end_ts (when an admin DND schedule is active). Returns 0 when
// neither is in the future.
func pickDNDEnd(s wsDNDStatusInner) int64 {
	if s.SnoozeEnabled && s.SnoozeEndTime > 0 {
		return s.SnoozeEndTime
	}
	if s.NextDNDEndTS > 0 {
		return s.NextDNDEndTS
	}
	return 0
}
```

- [ ] **Step 4.5: Verify tests pass**

```bash
go test ./internal/slack/ -v
go build ./...
```

Expected: all tests PASS, build clean. Note the build will fail in `cmd/slk/main.go` because `rtmEventHandler` does not yet implement the new interface methods. That gets fixed in Task 6, but the slack package alone should compile cleanly.

If the whole-program build fails on `rtmEventHandler` not implementing the new methods, **also add stub no-op methods to `rtmEventHandler`** at the end of `cmd/slk/main.go` so the build is green:

```go
func (h *rtmEventHandler) OnSelfPresenceChange(presence string) {
	// implemented in Task 6
}
func (h *rtmEventHandler) OnDNDChange(enabled bool, endUnix int64) {
	// implemented in Task 6
}
```

- [ ] **Step 4.6: Commit**

```bash
git add internal/slack/events.go internal/slack/events_test.go cmd/slk/main.go
git commit -m "feat(slack): dispatch manual_presence_change and dnd_updated events"
```

---

## Task 5: WorkspaceContext fields + StatusChangeMsg type

**Files:**
- Modify: `cmd/slk/main.go:48-69` (`WorkspaceContext`)
- Modify: `internal/ui/app.go` (add `StatusChangeMsg` near other message types around line 115-200)

- [ ] **Step 5.1: Add fields to `WorkspaceContext`**

In `cmd/slk/main.go:48-69`, after the `CustomEmoji` line, add:

```go
// Self presence and DND state for this workspace. Populated on connect
// and updated by manual_presence_change / dnd_updated WS events plus
// optimistic writes from the presence menu.
Presence   string    // "auto" or "away"; "" until first fetch
DNDEnabled bool      // true if either snooze or admin-DND is active
DNDEndTS   time.Time // unified end timestamp; zero if not in DND
```

- [ ] **Step 5.2: Add `StatusChangeMsg` to the UI package**

In `internal/ui/app.go`, find the block of message types near `PresenceChangeMsg` (around line 191) and add:

```go
// StatusChangeMsg is sent when the authenticated user's own presence
// or DND state changes for any workspace. The App routes it to the
// status bar only when TeamID matches the active workspace; otherwise
// it just updates the App's per-workspace status cache.
StatusChangeMsg struct {
	TeamID     string
	Presence   string    // "active" or "away"; "" means unknown/unchanged
	DNDEnabled bool
	DNDEndTS   time.Time
}
```

If the existing type block doesn't fit a multi-field struct, declare it as a top-level type in the same file. Match the surrounding style.

- [ ] **Step 5.3: Verify build**

```bash
go build ./...
```

Expected: clean build (no behavior yet — fields and the type are unused).

- [ ] **Step 5.4: Commit**

```bash
git add cmd/slk/main.go internal/ui/app.go
git commit -m "feat: add per-workspace presence/DND state and StatusChangeMsg"
```

---

## Task 6: rtmEventHandler implements new EventHandler methods

**Files:**
- Modify: `cmd/slk/main.go` (`rtmEventHandler` struct around line 1119, plus the stub methods added in Task 4)

The handler needs a reference back to its `WorkspaceContext` to mutate `Presence`, `DNDEnabled`, `DNDEndTS`, and a `teamID` to embed in the outgoing `StatusChangeMsg`. It already has `workspaceID` (which IS the team ID).

- [ ] **Step 6.1: Add a context pointer field to rtmEventHandler**

In `cmd/slk/main.go:1121-1138` (the `rtmEventHandler` struct), add:

```go
wsCtx *WorkspaceContext
```

- [ ] **Step 6.2: Wire it where the handler is constructed**

Search for the construction site:

```bash
grep -n "rtmEventHandler{" cmd/slk/main.go
```

It's around line 506-520. Add `wsCtx: wctx,` to the literal alongside the existing fields. The handler is created after the `WorkspaceContext` itself, so the pointer is valid.

- [ ] **Step 6.3: Replace the stub methods from Task 4 with real implementations**

Find the stubs added at the bottom of `cmd/slk/main.go` and replace with:

```go
func (h *rtmEventHandler) OnSelfPresenceChange(presence string) {
	if h.wsCtx == nil {
		return
	}
	// Slack uses "active"/"away" in events but "auto"/"away" in setPresence.
	// Normalize to the event vocabulary for storage; renderer translates.
	h.wsCtx.Presence = presence
	if h.program == nil {
		return
	}
	h.program.Send(ui.StatusChangeMsg{
		TeamID:     h.workspaceID,
		Presence:   presence,
		DNDEnabled: h.wsCtx.DNDEnabled,
		DNDEndTS:   h.wsCtx.DNDEndTS,
	})
}

func (h *rtmEventHandler) OnDNDChange(enabled bool, endUnix int64) {
	if h.wsCtx == nil {
		return
	}
	h.wsCtx.DNDEnabled = enabled
	if endUnix > 0 {
		h.wsCtx.DNDEndTS = time.Unix(endUnix, 0)
	} else {
		h.wsCtx.DNDEndTS = time.Time{}
	}
	if h.program == nil {
		return
	}
	h.program.Send(ui.StatusChangeMsg{
		TeamID:     h.workspaceID,
		Presence:   h.wsCtx.Presence,
		DNDEnabled: h.wsCtx.DNDEnabled,
		DNDEndTS:   h.wsCtx.DNDEndTS,
	})
}
```

- [ ] **Step 6.4: Verify build**

```bash
go build ./...
go test ./...
```

Expected: clean build, all tests still pass (no test asserts on the new behavior yet — App-level wiring of `StatusChangeMsg` happens in Task 9).

- [ ] **Step 6.5: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat: route self presence/DND events into StatusChangeMsg"
```

---

## Task 7: Bootstrap presence + DND on connect

**Files:**
- Modify: `cmd/slk/main.go` (add a `bootstrapPresenceAndDND` function and call it after each workspace connects)

- [ ] **Step 7.1: Find the post-connect callsite**

```bash
grep -n "client.counts\|GetUnreadCounts\|workspace .* connect\|firstWorkspaceReady" cmd/slk/main.go | head -20
```

Look around line 480-520. There's a goroutine spawned per workspace that begins with the connection lifecycle. Add the bootstrap call near where the workspace is fully connected and the WS is up.

- [ ] **Step 7.2: Add the bootstrap function**

Add near other helper functions (e.g., after `xdgCache` around line 1117):

```go
// bootstrapPresenceAndDND fetches the user's current presence and DND
// state from Slack, populates the WorkspaceContext, and sends an initial
// StatusChangeMsg. Also subscribes to presence_change events for the
// self user so external state changes arrive over the WS.
func bootstrapPresenceAndDND(ctx context.Context, wctx *WorkspaceContext, program *tea.Program) {
	if wctx == nil || wctx.Client == nil {
		return
	}

	// Subscribe so future presence_change events for our own user arrive.
	// Failure is non-fatal — manual_presence_change and dnd_updated work
	// without an explicit subscription.
	_ = wctx.Client.SubscribePresence([]string{wctx.UserID})

	// Initial presence fetch
	if p, err := wctx.Client.GetUserPresence(ctx, wctx.UserID); err == nil && p != nil {
		wctx.Presence = p.Presence
	}

	// Initial DND fetch
	if st, err := wctx.Client.GetDNDInfo(ctx, wctx.UserID); err == nil && st != nil {
		// Mirror the same end-timestamp picking logic used in events.go.
		var endUnix int64
		switch {
		case st.SnoozeEnabled && st.SnoozeEndTime > 0:
			endUnix = int64(st.SnoozeEndTime)
		case st.NextEndTimestamp > 0:
			endUnix = int64(st.NextEndTimestamp)
		}
		wctx.DNDEnabled = st.Enabled || st.SnoozeEnabled
		if endUnix > 0 {
			wctx.DNDEndTS = time.Unix(endUnix, 0)
		}
	}

	if program != nil {
		program.Send(ui.StatusChangeMsg{
			TeamID:     wctx.TeamID,
			Presence:   wctx.Presence,
			DNDEnabled: wctx.DNDEnabled,
			DNDEndTS:   wctx.DNDEndTS,
		})
	}
}
```

- [ ] **Step 7.3: Call it after each workspace's WS comes up**

Search for the place where the `OnConnect` flow finishes, or where `program.Send(ui.WorkspaceReadyMsg{...})` is sent:

```bash
grep -n "WorkspaceReadyMsg\|OnConnect\|StateConnected" cmd/slk/main.go | head -20
```

In whichever post-connect goroutine is the natural fit (typically right after the workspace finishes initial bootstrap — channels list, user list, unread counts), add:

```go
go bootstrapPresenceAndDND(ctx, wctx, program)
```

If multiple connect flows exist (initial connect + reconnect), call it on both. Reconnect handler is in `internal/slack/connection.go` invoked via the rtm handler's `OnConnect`; the simplest place is to also call it from `rtmEventHandler.OnConnect`:

In `cmd/slk/main.go:1304-1307`:

```go
func (h *rtmEventHandler) OnConnect() {
	h.connected = true
	h.program.Send(ui.ConnectionStateMsg{State: int(statusbar.StateConnected)})
	if h.wsCtx != nil {
		go bootstrapPresenceAndDND(context.Background(), h.wsCtx, h.program)
	}
}
```

(This also covers the initial connect, so the explicit call elsewhere is optional — pick one path. The `OnConnect` path is preferred because it also handles reconnects.)

- [ ] **Step 7.4: Verify build and tests**

```bash
go build ./...
go test ./...
```

Expected: clean. The new behavior isn't yet observable end-to-end (status bar doesn't render yet), but nothing should be broken.

- [ ] **Step 7.5: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat: bootstrap self presence and DND on workspace connect"
```

---

## Task 8: Status bar segment (fields, setter, format, tests)

**Files:**
- Modify: `internal/ui/statusbar/model.go`
- Modify: `internal/ui/statusbar/model_test.go` (create if absent)

- [ ] **Step 8.1: Check whether the test file exists**

```bash
ls internal/ui/statusbar/model_test.go 2>&1
```

If absent, create it in step 8.3.

- [ ] **Step 8.2: Write failing tests**

Create or append to `internal/ui/statusbar/model_test.go`:

```go
package statusbar

import (
	"strings"
	"testing"
	"time"
)

func TestStatusBar_PresenceSegmentActive(t *testing.T) {
	m := New()
	m.SetStatus("active", false, time.Time{})
	out := stripANSI(m.View(120))
	if !strings.Contains(out, "● Active") {
		t.Errorf("expected '● Active', got: %q", out)
	}
}

func TestStatusBar_PresenceSegmentAway(t *testing.T) {
	m := New()
	m.SetStatus("away", false, time.Time{})
	out := stripANSI(m.View(120))
	if !strings.Contains(out, "○ Away") {
		t.Errorf("expected '○ Away', got: %q", out)
	}
}

func TestStatusBar_DNDSegmentWithCountdown(t *testing.T) {
	m := New()
	end := time.Now().Add(83 * time.Minute) // 1h 23m
	m.SetStatus("active", true, end)
	out := stripANSI(m.View(120))
	if !strings.Contains(out, "🌙 DND") {
		t.Errorf("expected '🌙 DND' prefix, got: %q", out)
	}
	if !strings.Contains(out, "1h 23m") && !strings.Contains(out, "1h 22m") {
		t.Errorf("expected ~1h 23m countdown, got: %q", out)
	}
}

func TestStatusBar_DNDLessThanOneMinute(t *testing.T) {
	m := New()
	end := time.Now().Add(20 * time.Second)
	m.SetStatus("active", true, end)
	out := stripANSI(m.View(120))
	if !strings.Contains(out, "<1m") {
		t.Errorf("expected '<1m', got: %q", out)
	}
}

func TestStatusBar_DNDNoEndTimestamp(t *testing.T) {
	m := New()
	m.SetStatus("active", true, time.Time{})
	out := stripANSI(m.View(120))
	if !strings.Contains(out, "🌙 DND") {
		t.Errorf("expected '🌙 DND', got: %q", out)
	}
	if strings.Contains(out, "DND ") && strings.Contains(out, "m") {
		// hard to write an exact assertion; just make sure no countdown number is present
		// by ensuring no "h " or "m" follows DND directly
	}
}

func TestStatusBar_PresenceUnknown_NoSegment(t *testing.T) {
	m := New()
	// Default state — no SetStatus call. Status segment should not appear.
	out := stripANSI(m.View(120))
	if strings.Contains(out, "Active") || strings.Contains(out, "Away") || strings.Contains(out, "DND") {
		t.Errorf("expected no presence/DND segment when unset, got: %q", out)
	}
}

// stripANSI removes ANSI escape sequences for substring assertions.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
```

- [ ] **Step 8.3: Verify tests fail to build**

```bash
go test ./internal/ui/statusbar/ -v
```

Expected: build error referencing `m.SetStatus undefined`.

- [ ] **Step 8.4: Add fields and setter to `Model`**

In `internal/ui/statusbar/model.go:20-30`, extend the struct:

```go
type Model struct {
	mode        string
	channel     string
	channelType string
	workspace   string
	unreadCount int
	connState   ConnectionState
	inThread    bool
	toast       string
	presence    string    // "active", "away", or "" (unknown — segment hidden)
	dndEnabled  bool
	dndEndTS    time.Time // zero if not in DND
	version     int64
}
```

Add `import "time"` if not already present.

Add the setter near `SetConnectionState`:

```go
// SetStatus updates the self-presence and DND segment. presence is one of
// "active", "away", or "" (segment hidden). dndEnabled with a zero or
// future dndEndTS toggles the DND glyph and countdown.
func (m *Model) SetStatus(presence string, dndEnabled bool, dndEndTS time.Time) {
	if m.presence == presence && m.dndEnabled == dndEnabled && m.dndEndTS.Equal(dndEndTS) {
		return
	}
	m.presence = presence
	m.dndEnabled = dndEnabled
	m.dndEndTS = dndEndTS
	m.dirty()
}
```

- [ ] **Step 8.5: Render the segment**

In the `View` function, add a new block in `rightParts` _before_ the connection indicator (around line 175):

```go
// Presence + DND segment
if m.dndEnabled {
	rightParts = append(rightParts,
		lipgloss.NewStyle().
			Foreground(styles.Warning).
			Background(styles.SurfaceDark).
			Render(formatDND(m.dndEndTS)))
} else if m.presence == "active" {
	rightParts = append(rightParts,
		lipgloss.NewStyle().
			Foreground(styles.Accent).
			Background(styles.SurfaceDark).
			Render("● Active"))
} else if m.presence == "away" {
	rightParts = append(rightParts,
		lipgloss.NewStyle().
			Foreground(styles.TextMuted).
			Background(styles.SurfaceDark).
			Render("○ Away"))
}
```

Add a helper at the bottom of the file:

```go
// formatDND renders the DND segment with an optional countdown to the
// snooze end. Zero endTS or past endTS produces a bare "🌙 DND".
func formatDND(endTS time.Time) string {
	if endTS.IsZero() {
		return "🌙 DND"
	}
	d := time.Until(endTS)
	if d <= 0 {
		return "🌙 DND"
	}
	if d < time.Minute {
		return "🌙 DND <1m"
	}
	hours := int(d / time.Hour)
	minutes := int(d % time.Hour / time.Minute)
	if hours > 0 {
		return fmt.Sprintf("🌙 DND %dh %dm", hours, minutes)
	}
	return fmt.Sprintf("🌙 DND %dm", minutes)
}
```

- [ ] **Step 8.6: Verify tests pass**

```bash
go test ./internal/ui/statusbar/ -v
go build ./...
```

Expected: PASS.

- [ ] **Step 8.7: Commit**

```bash
git add internal/ui/statusbar/model.go internal/ui/statusbar/model_test.go
git commit -m "feat(statusbar): render presence and DND segment"
```

---

## Task 9: Status bar countdown ticker

**Files:**
- Modify: `internal/ui/statusbar/model.go` (add a tick message type)
- Modify: `internal/ui/app.go` (start the tick on `StatusChangeMsg`, re-render on each tick)

The countdown updates once a minute while DND is active. We use `tea.Tick` driven by a new `DNDTickMsg`. The tick reschedules itself for as long as DND is active.

- [ ] **Step 9.1: Add the tick message type**

Append to the bottom of `internal/ui/statusbar/model.go`:

```go
// DNDTickMsg is delivered once a minute while DND is active so the
// status bar can refresh its countdown segment.
type DNDTickMsg struct{}
```

- [ ] **Step 9.2: Wire the tick in app.go**

This is implemented as part of Task 10's `StatusChangeMsg` handler. The plan defers the actual code until then to avoid duplicating context. Just verify the type compiles:

```bash
go build ./...
```

- [ ] **Step 9.3: Commit**

```bash
git add internal/ui/statusbar/model.go
git commit -m "feat(statusbar): add DNDTickMsg for countdown refresh"
```

---

## Task 10: notify.IsDND suppression

**Files:**
- Modify: `internal/notify/notifier.go`
- Modify: `internal/notify/notifier_test.go`
- Modify: `cmd/slk/main.go` (populate `IsDND` in the notify caller)

- [ ] **Step 10.1: Write the failing test**

Append to `internal/notify/notifier_test.go`:

```go
func TestShouldNotify_SuppressedByDND(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C_OTHER",
		IsActiveWS:      false, // would otherwise notify
		OnDM:            true,
		OnMention:       true,
		OnKeyword:       []string{"deploy"},
		IsDND:           true,
	}
	if ShouldNotify(ctx, "C1", "U2", "hey <@U1> deploy", "dm") {
		t.Error("DND should suppress notifications regardless of triggers")
	}
}
```

- [ ] **Step 10.2: Verify the test fails**

```bash
go test ./internal/notify/ -run TestShouldNotify_SuppressedByDND -v
```

Expected: build error — `IsDND` field unknown.

- [ ] **Step 10.3: Add `IsDND` to `NotifyContext` and the early-return**

In `internal/notify/notifier.go:31-38`, add the field:

```go
type NotifyContext struct {
	CurrentUserID   string
	ActiveChannelID string
	IsActiveWS      bool
	OnMention       bool
	OnDM            bool
	OnKeyword       []string
	IsDND           bool // when true, ShouldNotify always returns false
}
```

In `ShouldNotify` (line 41-73), insert after the self-message check (after line 45):

```go
// Suppress entirely while DND/snoozed.
if ctx.IsDND {
	return false
}
```

- [ ] **Step 10.4: Populate `IsDND` at the caller**

In `cmd/slk/main.go:1162-1169`, change the `NotifyContext` literal:

```go
ctx := notify.NotifyContext{
	CurrentUserID:   h.currentUserID,
	ActiveChannelID: activeChID,
	IsActiveWS:      isActiveWS,
	OnMention:       h.notifyCfg.OnMention,
	OnDM:            h.notifyCfg.OnDM,
	OnKeyword:       h.notifyCfg.OnKeyword,
	IsDND:           h.wsCtx != nil && h.wsCtx.DNDEnabled && (h.wsCtx.DNDEndTS.IsZero() || time.Now().Before(h.wsCtx.DNDEndTS)),
}
```

(`time` is already imported in `main.go`.)

- [ ] **Step 10.5: Verify**

```bash
go test ./internal/notify/ -v
go build ./...
go test ./...
```

Expected: PASS.

- [ ] **Step 10.6: Commit**

```bash
git add internal/notify/notifier.go internal/notify/notifier_test.go cmd/slk/main.go
git commit -m "feat(notify): suppress notifications while DND is active"
```

---

## Task 11: presencemenu package

**Files:**
- Create: `internal/ui/presencemenu/model.go`
- Create: `internal/ui/presencemenu/model_test.go`

This is a new package modeled tightly on `internal/ui/themeswitcher`. It returns a `Result` discriminating between actions (set active, set away, snooze N minutes, end DND, open custom-snooze input).

- [ ] **Step 11.1: Write the failing tests first**

Create `internal/ui/presencemenu/model_test.go`:

```go
package presencemenu

import (
	"testing"
	"time"
)

func TestModel_SelectActive(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	if !m.IsVisible() {
		t.Fatal("expected visible")
	}
	r := m.HandleKey("enter")
	if r == nil || r.Action != ActionSetActive {
		t.Fatalf("expected ActionSetActive, got %+v", r)
	}
}

func TestModel_SelectAway(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	m.HandleKey("j") // step to Away
	r := m.HandleKey("enter")
	if r == nil || r.Action != ActionSetAway {
		t.Fatalf("expected ActionSetAway, got %+v", r)
	}
}

func TestModel_SnoozeOption(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	// Active(0), Away(1), Snooze 20m(2) — third item.
	m.HandleKey("j")
	m.HandleKey("j")
	r := m.HandleKey("enter")
	if r == nil || r.Action != ActionSnooze || r.SnoozeMinutes != 20 {
		t.Fatalf("expected 20m snooze, got %+v", r)
	}
}

func TestModel_EndDNDOnlyVisibleWhenSnoozed(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	if m.hasEndDNDItem() {
		t.Error("End DND should not appear when not snoozed")
	}

	m2 := New()
	m2.OpenWith("Workspace", "active", true, time.Now().Add(time.Hour))
	if !m2.hasEndDNDItem() {
		t.Error("End DND should appear when snoozed")
	}
}

func TestModel_CustomSnoozeAction(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	// Walk down past Active, Away, and the 7 snooze durations to "Snooze custom..."
	for i := 0; i < 9; i++ {
		m.HandleKey("j")
	}
	r := m.HandleKey("enter")
	if r == nil || r.Action != ActionCustomSnooze {
		t.Fatalf("expected ActionCustomSnooze, got %+v", r)
	}
}

func TestModel_FilterByQuery(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	m.HandleKey("a")
	m.HandleKey("w")
	m.HandleKey("a")
	m.HandleKey("y")
	r := m.HandleKey("enter")
	if r == nil || r.Action != ActionSetAway {
		t.Fatalf("expected filtered to Away, got %+v", r)
	}
}

func TestModel_EscapeCloses(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	if r := m.HandleKey("esc"); r != nil {
		t.Errorf("expected nil result on esc, got %+v", r)
	}
	if m.IsVisible() {
		t.Error("expected closed after esc")
	}
}
```

- [ ] **Step 11.2: Verify the tests fail**

```bash
go test ./internal/ui/presencemenu/ -v
```

Expected: package not found / build error.

- [ ] **Step 11.3: Implement the model**

Create `internal/ui/presencemenu/model.go`:

```go
// Package presencemenu provides the Ctrl+S overlay for setting
// presence (active/away) and DND snooze state on the active workspace.
package presencemenu

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// Action is the high-level operation the user picked.
type Action int

const (
	ActionSetActive Action = iota
	ActionSetAway
	ActionSnooze       // SnoozeMinutes is set
	ActionCustomSnooze // open the custom-snooze input
	ActionEndDND
)

// Result is returned when the user commits a selection.
type Result struct {
	Action        Action
	SnoozeMinutes int // populated when Action == ActionSnooze
}

// item is a single row in the menu.
type item struct {
	label   string
	action  Action
	minutes int  // for ActionSnooze
	current bool // currently-active row (decorated, still selectable)
}

// Model is the picker overlay.
type Model struct {
	items         []item
	filtered      []int // indices into items matching query
	query         string
	selected      int // index into filtered
	visible       bool
	workspaceName string
	currentPres   string // "active" / "away" / ""
	dndActive     bool
}

func New() Model {
	return Model{}
}

// OpenWith shows the overlay populated for the current workspace state.
func (m *Model) OpenWith(workspaceName, presence string, dndEnabled bool, dndEnd time.Time) {
	m.visible = true
	m.query = ""
	m.selected = 0
	m.workspaceName = workspaceName
	m.currentPres = presence
	m.dndActive = dndEnabled && (dndEnd.IsZero() || time.Now().Before(dndEnd))
	m.items = buildItems(presence, m.dndActive)
	m.filter()
}

// hasEndDNDItem is exposed for tests.
func (m Model) hasEndDNDItem() bool {
	for _, it := range m.items {
		if it.action == ActionEndDND {
			return true
		}
	}
	return false
}

// buildItems composes the menu rows based on current state.
func buildItems(presence string, dndActive bool) []item {
	rows := []item{
		{label: "● Active", action: ActionSetActive, current: presence == "active" && !dndActive},
		{label: "○ Away", action: ActionSetAway, current: presence == "away" && !dndActive},
		{label: "🌙 Snooze for 20 minutes", action: ActionSnooze, minutes: 20},
		{label: "🌙 Snooze for 1 hour", action: ActionSnooze, minutes: 60},
		{label: "🌙 Snooze for 2 hours", action: ActionSnooze, minutes: 120},
		{label: "🌙 Snooze for 4 hours", action: ActionSnooze, minutes: 240},
		{label: "🌙 Snooze for 8 hours", action: ActionSnooze, minutes: 480},
		{label: "🌙 Snooze for 24 hours", action: ActionSnooze, minutes: 1440},
		{label: "🌙 Snooze until tomorrow morning", action: ActionSnooze, minutes: minutesUntilTomorrowMorning(time.Now())},
		{label: "🌙 Snooze custom…", action: ActionCustomSnooze},
	}
	if dndActive {
		rows = append(rows, item{label: "End snooze / DND", action: ActionEndDND})
	}
	return rows
}

// minutesUntilTomorrowMorning returns the number of minutes from now until
// 09:00 local time on the next weekday (Mon–Thu → tomorrow; Fri/Sat/Sun → Monday).
// Always >= 1.
func minutesUntilTomorrowMorning(now time.Time) int {
	loc := now.Location()
	target := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, loc).AddDate(0, 0, 1)
	for target.Weekday() == time.Saturday || target.Weekday() == time.Sunday {
		target = target.AddDate(0, 0, 1)
	}
	d := target.Sub(now)
	mins := int(d.Minutes())
	if mins < 1 {
		mins = 1
	}
	return mins
}

func (m *Model) Close() {
	m.visible = false
}

func (m Model) IsVisible() bool { return m.visible }

// HandleKey processes a key event and returns a non-nil Result on selection.
func (m *Model) HandleKey(keyStr string) *Result {
	switch keyStr {
	case "enter":
		if len(m.filtered) == 0 {
			return nil
		}
		it := m.items[m.filtered[m.selected]]
		r := &Result{Action: it.action, SnoozeMinutes: it.minutes}
		return r
	case "esc":
		m.Close()
		return nil
	case "down", "ctrl+n", "j":
		if m.selected < len(m.filtered)-1 {
			m.selected++
		}
		return nil
	case "up", "ctrl+p", "k":
		if m.selected > 0 {
			m.selected--
		}
		return nil
	case "backspace":
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
			m.selected = 0
			m.filter()
		}
		return nil
	}
	if len(keyStr) == 1 && keyStr[0] >= 32 && keyStr[0] <= 126 {
		m.query += keyStr
		m.selected = 0
		m.filter()
	}
	return nil
}

func (m *Model) filter() {
	m.filtered = nil
	q := strings.ToLower(m.query)
	if q == "" {
		for i := range m.items {
			m.filtered = append(m.filtered, i)
		}
		return
	}
	var prefix, sub []int
	for i, it := range m.items {
		name := strings.ToLower(it.label)
		switch {
		case strings.HasPrefix(name, q):
			prefix = append(prefix, i)
		case strings.Contains(name, q):
			sub = append(sub, i)
		}
	}
	m.filtered = append(prefix, sub...)
}

// ViewOverlay renders the dimmed centered modal.
func (m Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}
	box := m.renderBox(termWidth)
	if box == "" {
		return background
	}
	return overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
}

func (m Model) renderBox(termWidth int) string {
	overlayWidth := termWidth / 2
	if overlayWidth < 36 {
		overlayWidth = 36
	}
	if overlayWidth > 60 {
		overlayWidth = 60
	}
	innerWidth := overlayWidth - 4
	bg := styles.Background

	titleText := "Status"
	if m.workspaceName != "" {
		titleText = "Status — " + m.workspaceName
	}
	title := lipgloss.NewStyle().
		Bold(true).
		Background(bg).
		Foreground(styles.Primary).
		Render(titleText)

	var inputText string
	if m.query == "" {
		placeholder := lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted).Render("Type to filter…")
		inputText = "█ " + placeholder
	} else {
		inputText = m.query + "█"
	}
	input := lipgloss.NewStyle().
		BorderStyle(lipgloss.Border{Left: "▌"}).
		BorderLeft(true).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		PaddingLeft(1).
		Background(bg).
		Foreground(styles.TextPrimary).
		Render(inputText)

	var rows []string
	for i, idx := range m.filtered {
		it := m.items[idx]
		line := it.label
		if it.current {
			line = "✓ " + line
		}
		if lipgloss.Width(line) > innerWidth-1 {
			line = truncate.StringWithTail(line, uint(innerWidth-1), "…")
		}
		var row string
		if i == m.selected {
			indicator := lipgloss.NewStyle().Background(bg).Foreground(styles.Accent).Render("▌")
			label := lipgloss.NewStyle().
				Background(bg).
				Foreground(styles.Primary).
				Bold(true).
				Width(innerWidth - 1).
				Render(line)
			row = indicator + label
		} else {
			fg := styles.TextPrimary
			if it.current {
				fg = styles.Accent
			}
			label := lipgloss.NewStyle().
				Background(bg).
				Foreground(fg).
				Width(innerWidth - 1).
				Render(line)
			row = " " + label
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		rows = append(rows, lipgloss.NewStyle().
			Background(bg).
			Foreground(styles.TextMuted).
			Italic(true).
			Render("No matching options"))
	}

	content := title + "\n" + input + "\n\n" + strings.Join(rows, "\n")
	content = messages.ReapplyBgAfterResets(content, messages.BgANSI()+messages.FgANSI())

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		Background(bg).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)
}

// CustomSnoozeView returns a simple input box used by the
// ModePresenceCustomSnooze sub-mode in app.go. The App tracks the input
// state itself; this helper just renders the box.
func CustomSnoozeView(termWidth, termHeight int, background, query string) string {
	overlayWidth := termWidth / 2
	if overlayWidth < 36 {
		overlayWidth = 36
	}
	if overlayWidth > 60 {
		overlayWidth = 60
	}
	bg := styles.Background

	title := lipgloss.NewStyle().Bold(true).Background(bg).Foreground(styles.Primary).
		Render("Snooze for how many minutes?")

	cursor := query + "█"
	input := lipgloss.NewStyle().
		BorderStyle(lipgloss.Border{Left: "▌"}).
		BorderLeft(true).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		PaddingLeft(1).
		Background(bg).
		Foreground(styles.TextPrimary).
		Render(cursor)

	hint := lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted).Italic(true).
		Render(fmt.Sprintf("Enter to snooze · Esc to cancel"))

	content := title + "\n\n" + input + "\n\n" + hint
	content = messages.ReapplyBgAfterResets(content, messages.BgANSI()+messages.FgANSI())

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		Background(bg).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)

	return overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
}
```

- [ ] **Step 11.4: Verify tests pass**

```bash
go test ./internal/ui/presencemenu/ -v
go build ./...
```

Expected: PASS.

- [ ] **Step 11.5: Commit**

```bash
git add internal/ui/presencemenu/
git commit -m "feat(ui): add presencemenu package for Ctrl+S overlay"
```

---

## Task 12: App wiring — keybinding, mode, picker integration, optimistic updates

**Files:**
- Modify: `internal/ui/keys.go`
- Modify: `internal/ui/mode.go`
- Modify: `internal/ui/app.go`
- Modify: `cmd/slk/main.go` (provide a `setStatusFn` callback to the App, similar to `themeSaveFn`)

This is the largest task; it stitches everything together. Sub-steps decompose it.

- [ ] **Step 12.1: Add the keybinding**

In `internal/ui/keys.go:6-36`:

```go
// In the KeyMap struct, alongside ThemeSwitcher:
PresenceMenu key.Binding
```

In `DefaultKeyMap()`:

```go
PresenceMenu: key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "set status")),
```

- [ ] **Step 12.2: Add new mode constants**

In `internal/ui/mode.go:6-15`:

```go
// Add to the Mode constant block:
ModePresenceMenu
ModePresenceCustomSnooze
```

In `String()`:

```go
case ModePresenceMenu:
	return "STATUS"
case ModePresenceCustomSnooze:
	return "STATUS-INPUT"
```

- [ ] **Step 12.3: Add the picker, per-workspace status cache, and the dispatch hooks to App**

Find the App struct (search `type App struct`):

```bash
grep -n "type App struct" internal/ui/app.go
grep -n "themeSwitcher  " internal/ui/app.go
```

Add fields alongside `themeSwitcher`:

```go
presenceMenu       presencemenu.Model
presenceCustomBuf  string                 // numeric input buffer for custom snooze
statusByTeam       map[string]workspaceStatus
setStatusFn        func(action presencemenu.Action, snoozeMinutes int)
```

Add a small helper type at file scope (near other types):

```go
// workspaceStatus caches the latest StatusChangeMsg per team so the
// status bar can refresh on workspace switch without round-tripping.
type workspaceStatus struct {
	Presence   string
	DNDEnabled bool
	DNDEndTS   time.Time
}
```

Add the import `"github.com/gammons/slk/internal/ui/presencemenu"` and ensure `"time"` is imported.

In the App constructor (search `themeSwitcher: themeswitcher.New(),`):

```go
presenceMenu: presencemenu.New(),
statusByTeam: map[string]workspaceStatus{},
```

- [ ] **Step 12.4: Add a setter for the status callback**

Below other `Set*Fn` setters in app.go, add:

```go
// SetSetStatusFn registers a callback the App invokes when the user picks
// a status action from the presence menu. The callback runs the appropriate
// Slack API call (typically asynchronously) for the active workspace.
func (a *App) SetSetStatusFn(fn func(action presencemenu.Action, snoozeMinutes int)) {
	a.setStatusFn = fn
}
```

- [ ] **Step 12.5: Wire the keybinding in handleNormalMode**

Find the theme switcher case (around line 1223-1227) in `handleNormalMode`:

```go
case key.Matches(msg, a.keys.PresenceMenu):
	header := a.workspaceNameForActive() // see Step 12.6
	pres, dndEnabled, dndEnd := a.activeWorkspaceStatus()
	a.presenceMenu.OpenWith(header, pres, dndEnabled, dndEnd)
	a.SetMode(ModePresenceMenu)
```

- [ ] **Step 12.6: Add helpers used above**

Inside the App, add (placed near other small helpers):

```go
func (a *App) workspaceNameForActive() string {
	for _, ws := range a.workspaces {
		if ws.ID == a.activeTeamID {
			return ws.Name
		}
	}
	return ""
}

func (a *App) activeWorkspaceStatus() (string, bool, time.Time) {
	st, ok := a.statusByTeam[a.activeTeamID]
	if !ok {
		return "", false, time.Time{}
	}
	return st.Presence, st.DNDEnabled, st.DNDEndTS
}
```

(`a.workspaces` is the existing slice of workspaces displayed in the rail; verify the field name. If it differs, adapt accordingly.)

- [ ] **Step 12.7: Dispatch ModePresenceMenu in handleKey**

In the mode dispatch switch (around line 1113-1128):

```go
case ModePresenceMenu:
	return a.handlePresenceMenuMode(msg)
case ModePresenceCustomSnooze:
	return a.handlePresenceCustomSnoozeMode(msg)
```

- [ ] **Step 12.8: Implement handlePresenceMenuMode**

Place near `handleThemeSwitcherMode` (around line 1468):

```go
func (a *App) handlePresenceMenuMode(msg tea.KeyMsg) tea.Cmd {
	keyStr := msg.String()
	switch msg.Key().Code {
	case tea.KeyEnter:
		keyStr = "enter"
	case tea.KeyEscape:
		keyStr = "esc"
	case tea.KeyUp:
		keyStr = "up"
	case tea.KeyDown:
		keyStr = "down"
	case tea.KeyBackspace:
		keyStr = "backspace"
	}

	result := a.presenceMenu.HandleKey(keyStr)
	if result != nil {
		a.presenceMenu.Close()
		// Custom snooze opens a sub-mode instead of firing immediately.
		if result.Action == presencemenu.ActionCustomSnooze {
			a.presenceCustomBuf = ""
			a.SetMode(ModePresenceCustomSnooze)
			return nil
		}
		a.SetMode(ModeNormal)
		// Optimistic UI: update local state + status bar before the API
		// call returns. The WS echo will reaffirm it.
		a.applyOptimisticStatus(result.Action, result.SnoozeMinutes)
		if a.setStatusFn != nil {
			a.setStatusFn(result.Action, result.SnoozeMinutes)
		}
		return nil
	}
	if !a.presenceMenu.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return nil
}

// applyOptimisticStatus updates the App's status cache and status bar
// based on the picked action, before the API round-trip completes.
func (a *App) applyOptimisticStatus(action presencemenu.Action, snoozeMinutes int) {
	st := a.statusByTeam[a.activeTeamID]
	switch action {
	case presencemenu.ActionSetActive:
		st.Presence = "active"
	case presencemenu.ActionSetAway:
		st.Presence = "away"
	case presencemenu.ActionSnooze:
		st.DNDEnabled = true
		st.DNDEndTS = time.Now().Add(time.Duration(snoozeMinutes) * time.Minute)
	case presencemenu.ActionEndDND:
		st.DNDEnabled = false
		st.DNDEndTS = time.Time{}
	}
	a.statusByTeam[a.activeTeamID] = st
	a.statusbar.SetStatus(st.Presence, st.DNDEnabled, st.DNDEndTS)
}
```

- [ ] **Step 12.9: Implement handlePresenceCustomSnoozeMode**

```go
func (a *App) handlePresenceCustomSnoozeMode(msg tea.KeyMsg) tea.Cmd {
	switch msg.Key().Code {
	case tea.KeyEscape:
		a.presenceCustomBuf = ""
		a.SetMode(ModeNormal)
		return nil
	case tea.KeyEnter:
		// Parse and dispatch
		mins, err := strconv.Atoi(a.presenceCustomBuf)
		a.presenceCustomBuf = ""
		a.SetMode(ModeNormal)
		if err != nil || mins <= 0 {
			a.statusbar.SetToast("Invalid snooze duration")
			return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return statusbar.CopiedClearMsg{} })
		}
		a.applyOptimisticStatus(presencemenu.ActionSnooze, mins)
		if a.setStatusFn != nil {
			a.setStatusFn(presencemenu.ActionSnooze, mins)
		}
		return nil
	case tea.KeyBackspace:
		if len(a.presenceCustomBuf) > 0 {
			a.presenceCustomBuf = a.presenceCustomBuf[:len(a.presenceCustomBuf)-1]
		}
		return nil
	}
	// Accept digits only
	r := msg.String()
	if len(r) == 1 && r[0] >= '0' && r[0] <= '9' {
		// cap at 6 digits to avoid runaway input
		if len(a.presenceCustomBuf) < 6 {
			a.presenceCustomBuf += r
		}
	}
	return nil
}
```

Add `"strconv"` to imports if absent.

- [ ] **Step 12.10: Render the overlays in View**

Find the section where `themeSwitcher.ViewOverlay` is composited (around line 2771):

```go
if a.presenceMenu.IsVisible() {
	screen = a.presenceMenu.ViewOverlay(a.width, a.height, screen)
}
if a.mode == ModePresenceCustomSnooze {
	screen = presencemenu.CustomSnoozeView(a.width, a.height, screen, a.presenceCustomBuf)
}
```

Find the corresponding "any overlay visible" check (around line 2791) and add the menu and sub-mode there:

```go
a.presenceMenu.IsVisible() ||
	a.mode == ModePresenceCustomSnooze ||
```

- [ ] **Step 12.11: Handle StatusChangeMsg**

In the App's `Update` method, add a case (place it near `case PresenceChangeMsg:` around line 1094):

```go
case StatusChangeMsg:
	st := workspaceStatus{
		Presence:   msg.Presence,
		DNDEnabled: msg.DNDEnabled,
		DNDEndTS:   msg.DNDEndTS,
	}
	a.statusByTeam[msg.TeamID] = st
	if msg.TeamID == a.activeTeamID {
		a.statusbar.SetStatus(st.Presence, st.DNDEnabled, st.DNDEndTS)
		// Start the once-a-minute countdown tick if DND is active.
		if st.DNDEnabled && !st.DNDEndTS.IsZero() && time.Now().Before(st.DNDEndTS) {
			return tea.Tick(time.Minute, func(time.Time) tea.Msg {
				return statusbar.DNDTickMsg{}
			})
		}
	}
	return nil

case statusbar.DNDTickMsg:
	st, ok := a.statusByTeam[a.activeTeamID]
	if !ok {
		return nil
	}
	a.statusbar.SetStatus(st.Presence, st.DNDEnabled, st.DNDEndTS)
	if st.DNDEnabled && !st.DNDEndTS.IsZero() && time.Now().Before(st.DNDEndTS) {
		return tea.Tick(time.Minute, func(time.Time) tea.Msg {
			return statusbar.DNDTickMsg{}
		})
	}
	// DND expired locally — flip the flag so the segment falls back to presence.
	if st.DNDEnabled && !time.Now().Before(st.DNDEndTS) {
		st.DNDEnabled = false
		st.DNDEndTS = time.Time{}
		a.statusByTeam[a.activeTeamID] = st
		a.statusbar.SetStatus(st.Presence, false, time.Time{})
	}
	return nil
```

- [ ] **Step 12.12: Refresh status bar on workspace switch**

Find where the active workspace changes (search for `a.activeTeamID = msg.TeamID`, around line 947 and 1013). After each assignment, push the cached status to the bar:

```go
if st, ok := a.statusByTeam[a.activeTeamID]; ok {
	a.statusbar.SetStatus(st.Presence, st.DNDEnabled, st.DNDEndTS)
} else {
	a.statusbar.SetStatus("", false, time.Time{})
}
```

- [ ] **Step 12.13: Wire the setStatusFn from main.go**

In `cmd/slk/main.go`, find where other App setters are called (search `SetThemeSaveFn\|app.Set`):

```bash
grep -n "app.Set" cmd/slk/main.go | head
```

Add after similar setters (typically after the workspace bootstrap completes):

```go
app.SetSetStatusFn(func(action presencemenu.Action, snoozeMinutes int) {
	wctx := workspaces[activeTeamID]
	if wctx == nil || wctx.Client == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		switch action {
		case presencemenu.ActionSetActive:
			if err := wctx.Client.SetUserPresence(ctx, "auto"); err != nil {
				program.Send(statusbar.CopiedMsg{N: 0}) // no-op fallback
				program.Send(toastErrMsg(err.Error()))
			}
		case presencemenu.ActionSetAway:
			if err := wctx.Client.SetUserPresence(ctx, "away"); err != nil {
				program.Send(toastErrMsg(err.Error()))
			}
		case presencemenu.ActionSnooze:
			if _, err := wctx.Client.SetSnooze(ctx, snoozeMinutes); err != nil {
				program.Send(toastErrMsg(err.Error()))
			}
		case presencemenu.ActionEndDND:
			if _, err := wctx.Client.EndSnooze(ctx); err != nil {
				program.Send(toastErrMsg(err.Error()))
			}
		}
	}()
})
```

Add the helper near the bottom of `main.go`:

```go
// toastErrMsg constructs a status-bar toast message for a failed status
// change. The App handles ToastMsg by routing it through the existing
// CopiedMsg/ClearMsg machinery.
func toastErrMsg(s string) tea.Msg {
	return ui.ToastMsg{Text: s}
}
```

If `ui.ToastMsg` doesn't yet exist, add a simple type to `internal/ui/app.go`:

```go
// ToastMsg sets a transient string in the status bar's toast slot.
ToastMsg struct{ Text string }
```

And handle it in `Update`:

```go
case ToastMsg:
	a.statusbar.SetToast(msg.Text)
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return statusbar.CopiedClearMsg{}
	})
```

Add `"github.com/gammons/slk/internal/ui/presencemenu"` to main.go's imports.

- [ ] **Step 12.14: Build, run all tests**

```bash
go build ./...
go test ./...
```

Expected: clean build, all tests pass. The new behavior is now end-to-end.

- [ ] **Step 12.15: Commit**

```bash
git add internal/ui/keys.go internal/ui/mode.go internal/ui/app.go cmd/slk/main.go
git commit -m "feat(ui): wire Ctrl+S presence menu and live status segment"
```

---

## Task 13: Manual smoke test

This is hands-on verification, not automated. Required before marking the work done.

- [ ] **Step 13.1: Build and run**

```bash
make build
./bin/slk
```

- [ ] **Step 13.2: Verify status bar baseline**

After connect, the status bar should show `● Active` (or `○ Away` if Slack reports your initial presence as away). The segment appears between the unread badge and the connection indicator.

- [ ] **Step 13.3: Press Ctrl+S**

The picker overlay opens. Title reads `Status — <YourWorkspaceName>`. The current state row is decorated with `✓` and rendered in the accent color.

- [ ] **Step 13.4: Toggle Away**

Navigate to `○ Away` with `j`, press `Enter`. The picker closes; the status bar segment immediately shows `○ Away`. Confirm in your browser Slack client that you appear away.

- [ ] **Step 13.5: Snooze 1 hour**

Press `Ctrl+S` again, select `🌙 Snooze for 1 hour`. The status bar shows `🌙 DND 59m` (or 1h 0m). Wait a minute — confirm it ticks down.

- [ ] **Step 13.6: External change reflects live**

While snooze is active, run your external script that calls `users.setPresence` to flip back to active, or use the official Slack client to flip presence. The TUI status bar should reflect the change within seconds (without restart).

- [ ] **Step 13.7: DND suppresses notifications**

While DND is active, have someone (or your script) send a DM. Confirm no OS notification fires from slk. End the snooze and confirm notifications resume.

- [ ] **Step 13.8: Custom snooze**

Press `Ctrl+S`, navigate to `🌙 Snooze custom…`, press Enter. The input box appears. Type `5`, press Enter. Status bar shows `🌙 DND 4m` (or 5m).

- [ ] **Step 13.9: End DND**

Press `Ctrl+S`. The `End snooze / DND` row should be present (only when snoozed). Select it. Status bar reverts to plain presence.

- [ ] **Step 13.10: Workspace switch preserves per-workspace state**

If you have multiple workspaces, set Away on one and snooze on another. Switch with `Ctrl+w` or `1`–`9`. The status bar should reflect the active workspace's state, not whatever was set last.

- [ ] **Step 13.11: Commit nothing** — this task only verifies.

If anything fails, file the symptom and fix in a follow-up task before merging.

---

## Task 14: Documentation updates

**Files:**
- Modify: `README.md`
- Modify: `docs/STATUS.md`

- [ ] **Step 14.1: README — keybinding table**

In the Keybindings table (around line 200-225), add:

```markdown
| `Ctrl+s` | Any | Status menu (Active / Away / DND) |
```

- [ ] **Step 14.2: README — feature list**

In the "On the roadmap" list, remove the line:

```
- Presence change events (online/away/DND)
```

In the "Connectivity" or a new "Status" section, add a short blurb:

```markdown
### Status
- Set self presence (Active / Away) and DND/snooze from `Ctrl+S`
- Live status bar segment with snooze countdown
- Reflects external state changes (other clients, API scripts) in real time via `manual_presence_change` and `dnd_updated` WebSocket events
- DND suppresses slk's OS notifications
```

- [ ] **Step 14.3: STATUS.md**

Find the line:

```
[ ] User presence tracking (online/away/DND updates)
```

Change to:

```
[x] User presence tracking (online/away/DND updates)
[x] Self presence + DND set from Ctrl+S menu
```

- [ ] **Step 14.4: Final tests**

```bash
go build ./...
go test ./...
```

Expected: green.

- [ ] **Step 14.5: Commit**

```bash
git add README.md docs/STATUS.md
git commit -m "docs: presence and DND controls"
```

---

## Done

After Task 14, branch `feature/presence-and-dnd` is ready for the **finishing-a-development-branch** skill to merge or open a PR.
