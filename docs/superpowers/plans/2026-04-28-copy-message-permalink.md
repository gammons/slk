# Copy Message Permalink Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `yy` and `C` keybindings that copy a Slack permalink for the selected message (or thread reply) to the system clipboard, with a "Copied permalink" toast next to the connection indicator in the status bar.

**Architecture:** Mirrors the existing reaction wiring pattern. (1) Add `GetPermalink` to the Slack client; (2) generalize the status bar's `Copied N chars` toast into a string-typed slot that handles arbitrary toast text; (3) add a `PermalinkFetchFunc` callback on `App` that's wired in `cmd/slk/main.go`; (4) bind `C` and `yy` in `handleNormalMode` to a helper that resolves the selected `(channelID, ts)`, calls the fetcher, and emits `tea.SetClipboard(url)` plus a toast message that reuses the existing 2-second tick-and-clear lifecycle.

**Tech Stack:** Go, bubbletea v2 (`charm.land/bubbletea/v2`), lipgloss v2, slack-go (`github.com/slack-go/slack`).

**Spec:** `docs/superpowers/specs/2026-04-28-copy-message-permalink-design.md`

---

## File Map

| File | Change |
|---|---|
| `internal/slack/client.go` | Add `GetPermalinkContext` to `SlackAPI` interface; add `GetPermalink` wrapper on `Client`. |
| `internal/slack/client_test.go` | Extend `mockSlackAPI` with `GetPermalinkContext`; add `TestGetPermalink`. |
| `internal/ui/statusbar/model.go` | Replace `copiedChars int` with `toast string`; keep `ShowCopied`/`ClearCopied` as shims; add `SetToast`; add `PermalinkCopiedMsg` and `PermalinkCopyFailedMsg`. |
| `internal/ui/statusbar/model_test.go` | Add tests for `SetToast` and the new message types. |
| `internal/ui/keys.go` | Add `CopyPermalink` binding on `C`. |
| `internal/ui/app.go` | Add `PermalinkFetchFunc` type, field, `SetPermalinkFetcher`; add `pendingYank` flag and `YankTimeoutMsg`; add `copyPermalinkOfSelected`; handle `C` and `y`/`yy` in `handleNormalMode`; handle `PermalinkCopiedMsg`/`PermalinkCopyFailedMsg`/`YankTimeoutMsg` in `Update`. |
| `internal/ui/app_test.go` | Add tests for the C handler, yy prefix, error path, no-selection no-op. |
| `cmd/slk/main.go` | Call `app.SetPermalinkFetcher` next to `SetReactionSender`. |
| `README.md` | Add `yy` / `C` row to the keybindings table. |

---

## Task 1: Slack client — `GetPermalink`

**Files:**
- Modify: `internal/slack/client.go` (interface around line 21, wrapper added near line 556)
- Modify: `internal/slack/client_test.go` (mock around line 78, new test at end of file)

- [ ] **Step 1: Write the failing test**

Append to `internal/slack/client_test.go`:

```go
func TestGetPermalink(t *testing.T) {
	wantURL := "https://example.slack.com/archives/C123/p1700000001000200"
	var gotChannel, gotTS string
	mock := &mockSlackAPI{
		getPermalinkContextFn: func(ctx context.Context, params *slack.PermalinkParameters) (string, error) {
			gotChannel = params.Channel
			gotTS = params.Ts
			return wantURL, nil
		},
	}
	client := &Client{api: mock}

	url, err := client.GetPermalink(context.Background(), "C123", "1700000001.000200")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != wantURL {
		t.Errorf("url = %q, want %q", url, wantURL)
	}
	if gotChannel != "C123" {
		t.Errorf("channel = %q, want %q", gotChannel, "C123")
	}
	if gotTS != "1700000001.000200" {
		t.Errorf("ts = %q, want %q", gotTS, "1700000001.000200")
	}
}

func TestGetPermalinkPropagatesError(t *testing.T) {
	wantErr := errors.New("boom")
	mock := &mockSlackAPI{
		getPermalinkContextFn: func(ctx context.Context, params *slack.PermalinkParameters) (string, error) {
			return "", wantErr
		},
	}
	client := &Client{api: mock}

	_, err := client.GetPermalink(context.Background(), "C123", "1.0")
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want wraps %v", err, wantErr)
	}
}
```

Also add the `getPermalinkContextFn` field to the `mockSlackAPI` struct (around line 24) and a method on it. Replace the struct definition and add the new method:

```go
type mockSlackAPI struct {
	getConversationRepliesFn func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	getEmojiFn               func() (map[string]string, error)
	getPermalinkContextFn    func(ctx context.Context, params *slack.PermalinkParameters) (string, error)
}

// ...existing methods...

func (m *mockSlackAPI) GetPermalinkContext(ctx context.Context, params *slack.PermalinkParameters) (string, error) {
	if m.getPermalinkContextFn != nil {
		return m.getPermalinkContextFn(ctx, params)
	}
	return "", nil
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/slack/ -run TestGetPermalink -v
```

Expected: compile error — `Client` has no method `GetPermalink`, and `mockSlackAPI` does not satisfy `SlackAPI` (interface has no `GetPermalinkContext` yet, so adding it to the mock is fine, but `client.GetPermalink` doesn't exist).

- [ ] **Step 3: Add the interface method and wrapper**

In `internal/slack/client.go`, add to the `SlackAPI` interface (insert after the `RemoveReaction` line ~33):

```go
	GetPermalinkContext(ctx context.Context, params *slack.PermalinkParameters) (string, error)
```

Then add the wrapper method. Insert after the `RemoveReaction` wrapper around line 556 (just after the closing `}` of `RemoveReaction`):

```go
// GetPermalink returns the Slack permalink for a message. For a thread reply,
// pass the reply's ts; Slack returns a thread-aware URL with thread_ts and cid
// query parameters.
func (c *Client) GetPermalink(ctx context.Context, channelID, ts string) (string, error) {
	url, err := c.api.GetPermalinkContext(ctx, &slack.PermalinkParameters{
		Channel: channelID,
		Ts:      ts,
	})
	if err != nil {
		return "", fmt.Errorf("getting permalink: %w", err)
	}
	return url, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/slack/ -v
```

Expected: PASS for `TestGetPermalink`, `TestGetPermalinkPropagatesError`, and all preexisting tests (the new mock method satisfies the extended interface).

- [ ] **Step 5: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "feat(slack): add GetPermalink wrapper for chat.getPermalink"
```

---

## Task 2: Generalize the status bar toast

**Files:**
- Modify: `internal/ui/statusbar/model.go` (struct line 20, methods 88–107, View 140–147, message types 182–190)
- Modify: `internal/ui/statusbar/model_test.go` (append new tests)

The existing `ShowCopied(n int)` and `ClearCopied()` API and `CopiedMsg{N int}` message stay backwards-compatible. Internally we generalize the toast to a string field so the same slot can show "Copied permalink" or "Failed to copy link".

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/statusbar/model_test.go`:

```go
func TestModel_SetToastShowsArbitraryString(t *testing.T) {
	m := New()
	m.SetToast("Copied permalink")
	out := m.View(80)
	if !strings.Contains(out, "Copied permalink") {
		t.Fatalf("expected toast string in view; got %q", out)
	}
}

func TestModel_SetToastEmptyClears(t *testing.T) {
	m := New()
	m.SetToast("hello")
	m.SetToast("")
	out := m.View(80)
	if strings.Contains(out, "hello") {
		t.Fatalf("expected toast cleared after SetToast(\"\"); got %q", out)
	}
}

func TestModel_SetToastBumpsVersionOnChange(t *testing.T) {
	m := New()
	v0 := m.Version()
	m.SetToast("a")
	if m.Version() == v0 {
		t.Fatal("SetToast must bump Version() on change")
	}
	v1 := m.Version()
	m.SetToast("a")
	if m.Version() != v1 {
		t.Fatal("SetToast with same value must be a no-op")
	}
}

func TestModel_ShowCopiedStillRendersCopiedNChars(t *testing.T) {
	// Backwards-compat: existing CopiedMsg path.
	m := New()
	m.ShowCopied(13)
	if !strings.Contains(m.View(80), "Copied 13 chars") {
		t.Fatalf("expected legacy 'Copied N chars' toast; got %q", m.View(80))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/ui/statusbar/ -v
```

Expected: FAIL — `SetToast` is not defined.

- [ ] **Step 3: Generalize the model**

Replace the struct field and the toast methods. Open `internal/ui/statusbar/model.go` and:

(a) Replace line 27 (`copiedChars int // 0 == no toast; >0 == "Copied N chars"`) with:

```go
	toast       string // "" == no toast; otherwise rendered verbatim in the right slot
```

(b) Replace the `ShowCopied` and `ClearCopied` methods (lines 88–107) with:

```go
// SetToast displays an arbitrary string in the right-side toast slot. Pass ""
// to clear. Callers are responsible for clearing the toast (typically via a
// tea.Tick that delivers CopiedClearMsg).
func (m *Model) SetToast(s string) {
	if m.toast != s {
		m.toast = s
		m.dirty()
	}
}

// ShowCopied is a backwards-compatible shim that sets the toast to
// "Copied N chars". Pass 0 for a no-op.
func (m *Model) ShowCopied(n int) {
	if n <= 0 {
		return
	}
	m.SetToast(fmt.Sprintf("Copied %d chars", n))
}

// ClearCopied removes any toast.
func (m *Model) ClearCopied() {
	m.SetToast("")
}
```

(c) Replace the toast-rendering block in `View()` (lines 140–147):

```go
	if m.toast != "" {
		rightParts = append(rightParts,
			lipgloss.NewStyle().
				Foreground(styles.Accent).
				Background(styles.SurfaceDark).
				Bold(true).
				Render(m.toast))
	}
```

(d) Append two new message types after `CopiedClearMsg` (after line 190):

```go
// PermalinkCopiedMsg is delivered when a message permalink has been copied to
// the clipboard. App handles it by setting the toast to "Copied permalink"
// and scheduling a CopiedClearMsg.
type PermalinkCopiedMsg struct{}

// PermalinkCopyFailedMsg is delivered when fetching the permalink fails.
// App handles it by setting the toast to "Failed to copy link" and
// scheduling a CopiedClearMsg.
type PermalinkCopyFailedMsg struct{}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/ui/statusbar/ -v
```

Expected: PASS for all new tests AND the preexisting `TestModel_CopiedToastShowsAndClears`, `TestModel_ShowCopiedBumpsVersion`, `TestModel_ShowCopiedZeroIsNoop`, `TestModel_ClearCopiedIsIdempotent`.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/statusbar/model.go internal/ui/statusbar/model_test.go
git commit -m "feat(statusbar): generalize copy toast to arbitrary strings"
```

---

## Task 3: Add `CopyPermalink` keybinding

**Files:**
- Modify: `internal/ui/keys.go` (struct lines 6–36, defaults lines 38–70)

- [ ] **Step 1: Add the binding to the `KeyMap` struct**

In `internal/ui/keys.go`, add a field after `Yank` (line 32):

```go
	CopyPermalink       key.Binding
```

- [ ] **Step 2: Add the default**

After the `Yank` entry in `DefaultKeyMap()` (line 65), add:

```go
		CopyPermalink:       key.NewBinding(key.WithKeys("C"), key.WithHelp("yy/C", "copy permalink")),
```

Also update the existing `Yank` help text to advertise the new behavior. Replace:

```go
		Yank:                key.NewBinding(key.WithKeys("y"), key.WithHelp("yy", "yank")),
```

with:

```go
		Yank:                key.NewBinding(key.WithKeys("y"), key.WithHelp("yy", "copy permalink")),
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./...
```

Expected: builds cleanly. (No tests yet — the binding is unused so far.)

- [ ] **Step 4: Commit**

```bash
git add internal/ui/keys.go
git commit -m "feat(keys): add CopyPermalink binding on C"
```

---

## Task 4: Add `PermalinkFetchFunc` callback wiring on `App`

**Files:**
- Modify: `internal/ui/app.go` (callback type near line 260, field near line 351, setter near line 1969)

- [ ] **Step 1: Add the callback type**

In `internal/ui/app.go`, after line 261 (`type ReactionRemoveFunc func(...)`), add:

```go
// PermalinkFetchFunc is called to fetch the Slack permalink for a message.
// For thread replies, pass the reply's ts; Slack returns a thread-aware URL.
type PermalinkFetchFunc func(ctx context.Context, channelID, ts string) (string, error)
```

You will need to import `"context"` if it isn't already imported in this file. (It is not — see line 4–26. Add `"context"` to the import block.)

- [ ] **Step 2: Add the field on `App`**

In the `App` struct, after the `currentUserID` field (around line 351), add a new section:

```go
	// Permalink copying
	permalinkFetchFn PermalinkFetchFunc
```

- [ ] **Step 3: Add the setter**

After the `SetReactionSender` method (line 1969–1972), add:

```go
// SetPermalinkFetcher sets the callback used to look up message permalinks
// for the copy-permalink action.
func (a *App) SetPermalinkFetcher(fn PermalinkFetchFunc) {
	a.permalinkFetchFn = fn
}
```

- [ ] **Step 4: Verify it compiles**

```bash
go build ./...
```

Expected: builds cleanly.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat(app): add PermalinkFetchFunc callback plumbing"
```

---

## Task 5: Implement `copyPermalinkOfSelected` and wire `C` key + toast handlers

**Files:**
- Modify: `internal/ui/app.go` (key handler around line 1051, helper added near line 1464, message handlers added near line 661)

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/app_test.go`:

```go
func TestCopyPermalink_FromMessagesPane(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1700000001.000200", UserName: "alice", Text: "hi"},
	})

	var gotCh, gotTS string
	app.SetPermalinkFetcher(func(ctx context.Context, channelID, ts string) (string, error) {
		gotCh = channelID
		gotTS = ts
		return "https://example.slack.com/archives/C123/p1700000001000200", nil
	})

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'C', Text: "C"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from C key")
	}
	msg := cmd()
	// cmd returns a tea.BatchMsg containing tea.SetClipboard cmd + permalink-copied msg.
	// Easiest assertion: drain the batch and look for our marker types.
	found := drainForPermalinkCopied(t, msg)
	if !found {
		t.Fatalf("expected statusbar.PermalinkCopiedMsg in batch, got %#v", msg)
	}
	if gotCh != "C123" {
		t.Errorf("channel = %q, want C123", gotCh)
	}
	if gotTS != "1700000001.000200" {
		t.Errorf("ts = %q, want 1700000001.000200", gotTS)
	}
}

func TestCopyPermalink_FromThreadPane(t *testing.T) {
	app := NewApp()
	app.threadPanel.OpenThread("C999", "1700000000.000100", messages.MessageItem{TS: "1700000000.000100"})
	app.threadPanel.SetReplies([]messages.MessageItem{
		{TS: "1700000000.000100", UserName: "alice", Text: "parent"},
		{TS: "1700000050.000400", UserName: "bob", Text: "reply"},
	})
	app.threadVisible = true
	app.focusedPanel = PanelThread
	// Select the second reply (index 1 — selection is initialized to last by SetReplies, but be explicit):
	for app.threadPanel.SelectedReply() == nil || app.threadPanel.SelectedReply().TS != "1700000050.000400" {
		app.threadPanel.MoveDown()
		// Guard against infinite loop in case API differs.
		if app.threadPanel.SelectedReply() != nil && app.threadPanel.SelectedReply().TS == "1700000050.000400" {
			break
		}
		break
	}

	var gotCh, gotTS string
	app.SetPermalinkFetcher(func(ctx context.Context, channelID, ts string) (string, error) {
		gotCh = channelID
		gotTS = ts
		return "https://example.slack.com/archives/C999/p1700000050000400?thread_ts=1700000000.000100&cid=C999", nil
	})

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'C', Text: "C"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from C key")
	}
	if !drainForPermalinkCopied(t, cmd()) {
		t.Fatal("expected PermalinkCopiedMsg")
	}
	if gotCh != "C999" {
		t.Errorf("channel = %q, want C999", gotCh)
	}
	if gotTS != "1700000050.000400" {
		t.Errorf("ts = %q, want reply ts 1700000050.000400", gotTS)
	}
}

func TestCopyPermalink_NothingSelectedNoop(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	// No messages set.
	app.SetPermalinkFetcher(func(ctx context.Context, channelID, ts string) (string, error) {
		t.Fatal("fetcher must not be called when nothing is selected")
		return "", nil
	})
	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'C', Text: "C"})
	if cmd != nil {
		// cmd may be non-nil but must not invoke the fetcher; drain it.
		_ = cmd()
	}
}

func TestCopyPermalink_FetcherErrorEmitsFailedMsg(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", Text: "hi"},
	})
	app.SetPermalinkFetcher(func(ctx context.Context, channelID, ts string) (string, error) {
		return "", errors.New("boom")
	})

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'C', Text: "C"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(statusbar.PermalinkCopyFailedMsg); !ok {
		t.Fatalf("expected PermalinkCopyFailedMsg, got %T", msg)
	}
}

func TestApp_PermalinkCopiedMsgShowsToast(t *testing.T) {
	a := NewApp()
	_, cmd := a.Update(statusbar.PermalinkCopiedMsg{})
	if !strings.Contains(a.statusbar.View(80), "Copied permalink") {
		t.Fatalf("expected 'Copied permalink' toast; got %q", a.statusbar.View(80))
	}
	if cmd == nil {
		t.Fatal("expected a clear-tick cmd")
	}
}

func TestApp_PermalinkCopyFailedMsgShowsToast(t *testing.T) {
	a := NewApp()
	a.Update(statusbar.PermalinkCopyFailedMsg{})
	if !strings.Contains(a.statusbar.View(80), "Failed to copy link") {
		t.Fatalf("expected 'Failed to copy link' toast; got %q", a.statusbar.View(80))
	}
}

// drainForPermalinkCopied walks tea.BatchMsg / tea.Cmd structures looking for
// a statusbar.PermalinkCopiedMsg.
func drainForPermalinkCopied(t *testing.T, msg tea.Msg) bool {
	t.Helper()
	switch v := msg.(type) {
	case statusbar.PermalinkCopiedMsg:
		return true
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if drainForPermalinkCopied(t, c()) {
				return true
			}
		}
	}
	return false
}
```

You will need to add imports to `app_test.go`: `"context"`, `"errors"`, and ensure `"github.com/gammons/slk/internal/ui/messages"` and `"github.com/gammons/slk/internal/ui/statusbar"` are present. Check the existing import block and add what's missing.

Note on the thread test: the `app.threadPanel.OpenThread` and `SetReplies` method names are guesses based on the explore findings. Before running tests, open `internal/ui/thread/model.go` and use whatever the actual setup methods are (look near `ChannelID()` line 152 and `ThreadTS()` line 147, plus `SelectedReply()` line 196). If the existing test files in `internal/ui/` already populate a thread panel for testing, copy that exact pattern. If you cannot easily populate the thread panel, replace the thread test with a unit test that calls `copyPermalinkOfSelected` directly after manually setting up the thread panel via whatever public API exists.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/ui/ -run TestCopyPermalink -v
go test ./internal/ui/ -run TestApp_PermalinkCopied -v
go test ./internal/ui/ -run TestApp_PermalinkCopyFailed -v
```

Expected: FAIL — `SetPermalinkFetcher` exists from Task 4 but neither the `C` handler nor the new `Update` cases exist.

- [ ] **Step 3: Implement the action helper**

In `internal/ui/app.go`, after `toggleReactionOnSelectedThread` (ending around line 1530), add:

```go
// copyPermalinkOfSelected resolves the currently-selected message or thread
// reply, calls the permalink fetcher, and returns a tea.Cmd that writes the
// URL to the clipboard and emits a status-bar toast.
func (a *App) copyPermalinkOfSelected() tea.Cmd {
	if a.permalinkFetchFn == nil {
		return nil
	}
	var channelID, ts string
	switch a.focusedPanel {
	case PanelMessages:
		msg, ok := a.messagepane.SelectedMessage()
		if !ok {
			return nil
		}
		channelID = a.activeChannelID
		ts = msg.TS
	case PanelThread:
		reply := a.threadPanel.SelectedReply()
		if reply == nil {
			return nil
		}
		channelID = a.threadPanel.ChannelID()
		ts = reply.TS
	default:
		return nil
	}
	if channelID == "" || ts == "" {
		return nil
	}
	fetch := a.permalinkFetchFn
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		url, err := fetch(ctx, channelID, ts)
		if err != nil {
			log.Printf("copy permalink: %v", err)
			return statusbar.PermalinkCopyFailedMsg{}
		}
		return tea.BatchMsg{
			func() tea.Msg { return tea.SetClipboard(url)() },
			func() tea.Msg { return statusbar.PermalinkCopiedMsg{} },
		}
	}
}
```

Note: `tea.SetClipboard` returns a `tea.Cmd` (not a `tea.Msg`), so we wrap it. If the bubbletea v2 API in this codebase is different, mirror the existing call site at `app.go:657` (`cmds = append(cmds, tea.SetClipboard(text))`). If `tea.BatchMsg` is not a recognized public type, replace the return with a small struct that the App's `Update` recognizes — but check: the test helper above uses `tea.BatchMsg`, so confirm it's exported. If not, use this alternative shape:

```go
	return func() tea.Msg {
		// ...same as above...
		// On success, the caller (handleNormalMode) wraps us in tea.Batch.
		// To keep that simple, return a custom type:
		return permalinkResultMsg{url: url}
	}
```

Then add `type permalinkResultMsg struct{ url string }` and handle it in `Update` to emit `tea.Batch(tea.SetClipboard(url), func() tea.Msg { return statusbar.PermalinkCopiedMsg{} })`. **Choose whichever shape keeps the tests in step 1 honest;** if you switch to `permalinkResultMsg`, update `drainForPermalinkCopied` and the test that asserts `PermalinkCopyFailedMsg` accordingly.

- [ ] **Step 4: Wire the `C` key in `handleNormalMode`**

In `internal/ui/app.go`, after the `ReactionNav` case (ending around line 1063), add:

```go
	case key.Matches(msg, a.keys.CopyPermalink):
		return a.copyPermalinkOfSelected()
```

- [ ] **Step 5: Handle the new toast messages in `Update`**

In `internal/ui/app.go`, after the `case statusbar.CopiedClearMsg:` block (line 667–668), add:

```go
	case statusbar.PermalinkCopiedMsg:
		a.statusbar.SetToast("Copied permalink")
		cmds = append(cmds, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))

	case statusbar.PermalinkCopyFailedMsg:
		a.statusbar.SetToast("Failed to copy link")
		cmds = append(cmds, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
go test ./internal/ui/ -run TestCopyPermalink -v
go test ./internal/ui/ -run TestApp_Permalink -v
```

Expected: PASS for all four `TestCopyPermalink_*` tests and both `TestApp_Permalink*` tests.

- [ ] **Step 7: Run the full UI test suite to catch regressions**

```bash
go test ./internal/ui/...
```

Expected: PASS for everything.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "feat(app): bind C to copy message permalink"
```

---

## Task 6: Add `yy` prefix handling

**Files:**
- Modify: `internal/ui/app.go` (struct field, Update message case, key handler)
- Modify: `internal/ui/app_test.go` (append yy tests)

The `Yank` binding (`y`) already exists and is unwired. We add a one-second prefix window: pressing `y` once arms `pendingYank`; a second `y` within 1s triggers the same action as `C`; any other key clears the flag.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/app_test.go`:

```go
func TestYY_TriggersCopyPermalink(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", Text: "hi"},
	})
	called := 0
	app.SetPermalinkFetcher(func(ctx context.Context, channelID, ts string) (string, error) {
		called++
		return "https://example.slack.com/x", nil
	})

	// First y: arms the prefix, returns no command (or a timeout-tick).
	cmd1 := app.handleNormalMode(tea.KeyPressMsg{Code: 'y', Text: "y"})
	_ = cmd1
	if called != 0 {
		t.Fatalf("first y must not call fetcher; called=%d", called)
	}
	if !app.pendingYank {
		t.Fatal("expected pendingYank=true after first y")
	}

	// Second y: triggers copy.
	cmd2 := app.handleNormalMode(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd2 == nil {
		t.Fatal("expected non-nil cmd from second y")
	}
	_ = cmd2()
	if called != 1 {
		t.Fatalf("expected fetcher called once, got %d", called)
	}
	if app.pendingYank {
		t.Fatal("pendingYank must be cleared after second y")
	}
}

func TestYY_AnyOtherKeyClearsPrefix(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", Text: "hi"},
	})
	app.SetPermalinkFetcher(func(ctx context.Context, channelID, ts string) (string, error) {
		t.Fatal("fetcher must not be called when prefix was broken")
		return "", nil
	})

	// y, then j (down) — prefix should clear.
	app.handleNormalMode(tea.KeyPressMsg{Code: 'y', Text: "y"})
	app.handleNormalMode(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if app.pendingYank {
		t.Fatal("pendingYank must clear after non-y key")
	}
	// Now a single y should re-arm but not trigger.
	app.handleNormalMode(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if !app.pendingYank {
		t.Fatal("expected pendingYank=true after fresh y")
	}
}

func TestYY_TimeoutMsgClearsPrefix(t *testing.T) {
	app := NewApp()
	app.pendingYank = true
	app.Update(yankTimeoutMsg{})
	if app.pendingYank {
		t.Fatal("yankTimeoutMsg must clear pendingYank")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/ui/ -run TestYY -v
```

Expected: FAIL — `pendingYank` and `yankTimeoutMsg` don't exist; `Yank` is unwired.

- [ ] **Step 3: Add the prefix state and timeout message**

In `internal/ui/app.go`, add a field to the `App` struct (next to `permalinkFetchFn` from Task 4):

```go
	pendingYank bool // true after a single 'y' in normal mode, until the next key or timeout
```

Add a private message type. A good place is near the other internal message types — at the end of the `type (...)` block around line 100 is fine, but since these are all exported there, add it as a separate declaration just below that block:

```go
// yankTimeoutMsg clears a pending y prefix after a short window.
type yankTimeoutMsg struct{}
```

- [ ] **Step 4: Wire the `Yank` key**

In `handleNormalMode`, add a case for `Yank` BEFORE the `default:` block. Place it right after the `CopyPermalink` case from Task 5:

```go
	case key.Matches(msg, a.keys.Yank):
		if a.pendingYank {
			a.pendingYank = false
			return a.copyPermalinkOfSelected()
		}
		a.pendingYank = true
		return tea.Tick(1*time.Second, func(time.Time) tea.Msg {
			return yankTimeoutMsg{}
		})
```

Also, at the **very top** of `handleNormalMode` (right after the reaction-nav early returns at lines 965–970), add a single block that clears `pendingYank` when the incoming key is anything other than `y`:

```go
	// Any non-y key clears a pending yank prefix.
	if a.pendingYank && !key.Matches(msg, a.keys.Yank) {
		a.pendingYank = false
	}
```

- [ ] **Step 5: Handle the timeout in `Update`**

In `internal/ui/app.go`, alongside the other status-bar toast cases (after the `PermalinkCopyFailedMsg` case from Task 5), add:

```go
	case yankTimeoutMsg:
		a.pendingYank = false
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
go test ./internal/ui/ -run TestYY -v
go test ./internal/ui/...
```

Expected: PASS for all `TestYY_*` tests and no regressions in the rest of the UI suite.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "feat(app): bind yy as vim-style copy-permalink shortcut"
```

---

## Task 7: Wire the fetcher in `cmd/slk/main.go`

**Files:**
- Modify: `cmd/slk/main.go` (around line 406, after `SetReactionSender`)

- [ ] **Step 1: Add the fetcher wiring**

In `cmd/slk/main.go`, immediately after the `app.SetReactionSender(...)` call (line 406–413), add:

```go
		app.SetPermalinkFetcher(func(ctx context.Context, channelID, ts string) (string, error) {
			return client.GetPermalink(ctx, channelID, ts)
		})
```

Verify `"context"` is already imported in `cmd/slk/main.go`; if not, add it.

- [ ] **Step 2: Verify the project builds**

```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 3: Manual smoke check (optional, requires real Slack creds)**

Skip if you don't have a workspace configured. Otherwise:

```bash
make build && ./bin/slk
```

Select a message, press `C`. Confirm the status bar shows "Copied permalink" for ~2s next to the connection indicator and that pasting elsewhere yields a `https://...slack.com/archives/...` URL. Repeat in a thread panel with a reply selected; confirm the URL contains `thread_ts=...&cid=...`.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat(slk): wire permalink fetcher to slack client"
```

---

## Task 8: Update README

**Files:**
- Modify: `README.md` (keybindings table around lines 195–215)

- [ ] **Step 1: Add the row**

In the keybindings table, after the `R` reaction-nav row (line 213), add:

```
| `yy` / `C` | Normal (message) | Copy message permalink |
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs(readme): document yy/C copy-permalink keybinding"
```

---

## Task 9: Final verification

- [ ] **Step 1: Run the full test suite**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 2: Build the binary**

```bash
go build ./...
make build
```

Expected: clean build, binary at `bin/slk`.

- [ ] **Step 3: Confirm `git status` is clean**

```bash
git status
```

Expected: working tree clean, branch ahead of origin by 7 commits (one per task that produced changes).

---

## Self-Review Notes

**Spec coverage:**
- Slack client `GetPermalink` — Task 1 ✓
- Status bar generalization + new message types — Task 2 ✓
- `C` keybinding — Task 3, wired in Task 5 ✓
- `yy` keybinding — Tasks 3 (Yank already exists), 6 ✓
- `PermalinkFetchFunc` type, field, setter — Task 4 ✓
- Action helper `copyPermalinkOfSelected` — Task 5 ✓
- Toast lifecycle — Task 5 (reuses existing `CopiedClearMsg` 2s tick) ✓
- Wiring in `cmd/slk/main.go` — Task 7 ✓
- README update — Task 8 ✓
- Tests for both panes, error path, no-selection no-op, yy prefix, timeout — Tasks 1, 2, 5, 6 ✓

**Risk notes:**
- Task 5 step 3 has two alternative shapes for the success path (`tea.BatchMsg` literal vs. `permalinkResultMsg` indirection). Pick whichever compiles cleanly with the actual bubbletea v2 surface in this codebase and update the test helper to match.
- Task 5's thread-pane test depends on real method names of `thread.Model` for setup. The exploration noted `OpenThread` / `SetReplies` exist but the exact signatures weren't read; verify against `internal/ui/thread/model.go` before running the test, and if needed adapt or replace the test with a direct call to `copyPermalinkOfSelected` after manually setting up the thread panel.
