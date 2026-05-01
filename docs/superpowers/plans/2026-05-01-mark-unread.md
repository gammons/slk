# Mark Unread — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `U` keybinding that marks the selected message (or thread reply) and everything newer as unread, syncing the change to Slack so it's visible across clients. Also handle inbound `channel_marked` / `thread_marked` WebSocket events for cross-client read-state sync.

**Architecture:** Mirrors the existing Edit/Delete action pattern. New `MarkUnreadMsg` / `MessageMarkedUnreadMsg` types and a `MarkUnreadFunc` callback registered via `SetMessageMarkUnreader`. `markUnreadOfSelected()` uses the existing `selectedMessageContext()` to resolve the focused pane, computes the new `last_read_ts` boundary by walking the loaded buffer, and dispatches. The Slack client gains `MarkChannelUnread` / `MarkThreadUnread` (HTTP wrappers), refactored alongside the existing `MarkChannel` / `MarkThread` to share an internal helper and gain DI on the HTTP client for testability. The `EventHandler` interface gains `OnChannelMarked` / `OnThreadMarked` methods; `dispatchWebSocketEvent` learns five new event types. App-side Update arms apply the changes through shared `applyChannelMark` / `applyThreadMark` helpers used by both the local-press and remote-event paths.

**Tech Stack:** Go 1.22+, charm.land/bubbletea/v2, charm.land/bubbles/v2, charm.land/lipgloss/v2, slack-go/slack (already wrapped in `internal/slack`), SQLite via `internal/cache`.

---

## File Structure

**Modified files:**
- `internal/slack/client.go` — DI'd `httpClient` field; refactor `MarkChannel`/`MarkThread` to share private helpers; add `MarkChannelUnread`/`MarkThreadUnread`.
- `internal/slack/client_test.go` — backfilled tests for `MarkChannel`/`MarkThread` plus new `MarkChannelUnread`/`MarkThreadUnread` tests using `httptest.NewServer`.
- `internal/slack/events.go` — extend `EventHandler` with `OnChannelMarked`/`OnThreadMarked`; new dispatch cases for `channel_marked`/`im_marked`/`group_marked`/`mpim_marked`/`thread_marked`.
- `internal/slack/events_test.go` — extend `mockEventHandler` to satisfy the new interface; new dispatch tests.
- `internal/cache/channels_test.go` — backfilled `TestUpdateLastReadTS_RoundTrip`.
- `internal/ui/keys.go` — `MarkUnread` binding (`"U"`).
- `internal/ui/sidebar/model.go` — new `SetUnreadCount(channelID string, n int)` setter.
- `internal/ui/sidebar/model_test.go` — test for the new setter.
- `internal/ui/threadsview/model.go` — new `MarkByThreadTSUnread(channelID, threadTS string) bool` (sets `Unread=true`).
- `internal/ui/threadsview/model_test.go` — test for the new method.
- `internal/ui/statusbar/model.go` — new `MarkedUnreadMsg` / `MarkUnreadFailedMsg` toast types.
- `internal/ui/app.go` — new msg types, func type, setter, dispatcher, `markUnreadOfSelected` helper, `applyChannelMark`/`applyThreadMark` helpers, Update arms (local + remote + toast).
- `internal/ui/app_test.go` — tests mirroring the Edit/Delete pattern.
- `cmd/slk/main.go` — wire `SetMessageMarkUnreader`; implement `OnChannelMarked`/`OnThreadMarked` on `rtmEventHandler` and add new app msg types `ChannelMarkedRemoteMsg` / `ThreadMarkedRemoteMsg`.
- `README.md` — keybinding table: add `U`.

---

## Sequencing Rationale

Bottom-up so each step is independently testable:

1. Cache: backfill missing test for `UpdateLastReadTS` (no behavior change; warmup).
2. Sidebar setter (no other deps).
3. Threadsview helper (no other deps).
4. Slack client: refactor `MarkChannel`/`MarkThread` for DI; backfill tests.
5. Slack client: add `MarkChannelUnread`/`MarkThreadUnread`.
6. Statusbar toast types.
7. WS event interface extension (compile-time gating: introduce stubs everywhere that implements `EventHandler`).
8. WS dispatch for `channel_marked` family.
9. WS dispatch for `thread_marked`.
10. Keymap binding.
11. App-level types + setter + Update plumbing for the local action.
12. App-level `markUnreadOfSelected` helper + key dispatch arm.
13. App-level shared `applyChannelMark`/`applyThreadMark` + `MessageMarkedUnreadMsg` Update arm.
14. App-level remote messages + their Update arms.
15. Statusbar toast Update arms.
16. Wiring: `SetMessageMarkUnreader` callback in main.go.
17. Wiring: real `OnChannelMarked`/`OnThreadMarked` bodies on `rtmEventHandler`.
18. README update.

---

## Task 1: Backfill `TestUpdateLastReadTS_RoundTrip`

**Files:**
- Test: `internal/cache/channels_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/cache/channels_test.go`:

```go
func TestUpdateLastReadTS_RoundTrip(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	if err := db.UpdateLastReadTS("C1", "1234567890.000100"); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetLastReadTS("C1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1234567890.000100" {
		t.Errorf("expected '1234567890.000100', got %q", got)
	}

	// Update again — overwrites prior value.
	if err := db.UpdateLastReadTS("C1", "1234567890.000200"); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetLastReadTS("C1")
	if got != "1234567890.000200" {
		t.Errorf("expected '1234567890.000200' after overwrite, got %q", got)
	}

	// Roll backward — also allowed (mark-unread will need this).
	if err := db.UpdateLastReadTS("C1", "1234567890.000050"); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetLastReadTS("C1")
	if got != "1234567890.000050" {
		t.Errorf("expected backward roll to '1234567890.000050', got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/cache/ -run TestUpdateLastReadTS_RoundTrip -v`
Expected: PASS (functionality already exists; this is just backfilled coverage).

- [ ] **Step 3: Commit**

```bash
git add internal/cache/channels_test.go
git commit -m "test(cache): backfill UpdateLastReadTS round-trip coverage"
```

---

## Task 2: Add `SetUnreadCount` to sidebar

**Files:**
- Modify: `internal/ui/sidebar/model.go` (next to `MarkUnread` / `ClearUnread`, ~line 559)
- Test: `internal/ui/sidebar/model_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/sidebar/model_test.go`:

```go
func TestSetUnreadCount_SetsExactValue(t *testing.T) {
	m := New()
	m.SetItems([]ChannelItem{
		{ID: "C1", Name: "general", Section: "Channels"},
	})

	m.SetUnreadCount("C1", 7)

	for _, it := range m.Items() {
		if it.ID == "C1" {
			if it.UnreadCount != 7 {
				t.Errorf("expected UnreadCount=7, got %d", it.UnreadCount)
			}
			return
		}
	}
	t.Fatal("C1 not found in items")
}

func TestSetUnreadCount_Zero_ClearsBadge(t *testing.T) {
	m := New()
	m.SetItems([]ChannelItem{{ID: "C1", Name: "general", Section: "Channels"}})
	m.MarkUnread("C1")
	m.MarkUnread("C1")
	// preconditions: count is 2.

	m.SetUnreadCount("C1", 0)

	for _, it := range m.Items() {
		if it.ID == "C1" {
			if it.UnreadCount != 0 {
				t.Errorf("expected UnreadCount=0, got %d", it.UnreadCount)
			}
			return
		}
	}
}

func TestSetUnreadCount_UnknownChannel_NoOp(t *testing.T) {
	m := New()
	m.SetItems([]ChannelItem{{ID: "C1", Name: "general", Section: "Channels"}})

	// Should not panic, should not affect existing items.
	m.SetUnreadCount("CDOESNOTEXIST", 5)

	for _, it := range m.Items() {
		if it.ID == "C1" && it.UnreadCount != 0 {
			t.Errorf("untouched item changed: %d", it.UnreadCount)
		}
	}
}
```

If `Items()` doesn't exist as a public accessor (check first with `grep`), add a quick read accessor as part of this task, or change the test to inspect via `m.items` from within the same package.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/sidebar/ -run TestSetUnreadCount -v`
Expected: FAIL with `m.SetUnreadCount undefined`.

- [ ] **Step 3: Implement**

In `internal/ui/sidebar/model.go`, after `ClearUnread` (around line 559):

```go
// SetUnreadCount sets the unread count for the given channel to an exact
// value (use n=0 to clear). Re-runs the staleness filter so a channel that
// becomes unread reappears in the sidebar (the staleness rule exempts items
// with UnreadCount > 0). No-op if the channel is not in the sidebar.
//
// Differs from MarkUnread (which only increments by 1) and ClearUnread
// (which only sets to 0): this setter is used by mark-unread and by the
// channel_marked WS event reconciliation, both of which know the desired
// final count.
func (m *Model) SetUnreadCount(channelID string, n int) {
	for i := range m.items {
		if m.items[i].ID == channelID {
			if m.items[i].UnreadCount == n {
				return
			}
			m.items[i].UnreadCount = n
			m.rebuildFilter()
			m.rebuildNavPreserveCursor()
			m.cacheValid = false
			m.dirty()
			return
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/sidebar/ -run TestSetUnreadCount -v`
Expected: PASS.

- [ ] **Step 5: Run full sidebar package tests**

Run: `go test ./internal/ui/sidebar/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/sidebar/model.go internal/ui/sidebar/model_test.go
git commit -m "feat(sidebar): add SetUnreadCount for exact unread badge value"
```

---

## Task 3: Add `MarkByThreadTSUnread` to threadsview

**Files:**
- Modify: `internal/ui/threadsview/model.go` (next to `MarkByThreadTSRead`, ~line 343)
- Test: `internal/ui/threadsview/model_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/threadsview/model_test.go`:

The `cache.ThreadSummary` type is the public summary shape (the threadsview reuses it directly, see `internal/cache/threads.go:11`). The test file already imports `github.com/gammons/slk/internal/cache`.

```go
func TestMarkByThreadTSUnread_FlipsFlagAndReturnsTrue(t *testing.T) {
	m := New()
	m.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", Unread: false},
		{ChannelID: "C1", ThreadTS: "P2", Unread: false},
	})

	if !m.MarkByThreadTSUnread("C1", "P2") {
		t.Fatal("expected return true when flag flipped")
	}

	for _, s := range m.Summaries() {
		if s.ThreadTS == "P2" && !s.Unread {
			t.Error("P2 should be Unread=true after MarkByThreadTSUnread")
		}
		if s.ThreadTS == "P1" && s.Unread {
			t.Error("P1 should remain Unread=false")
		}
	}
}

func TestMarkByThreadTSUnread_AlreadyUnread_ReturnsFalse(t *testing.T) {
	m := New()
	m.SetSummaries([]cache.ThreadSummary{{ChannelID: "C1", ThreadTS: "P1", Unread: true}})

	if m.MarkByThreadTSUnread("C1", "P1") {
		t.Error("expected false when flag was already true")
	}
}

func TestMarkByThreadTSUnread_NotFound_ReturnsFalse(t *testing.T) {
	m := New()
	m.SetSummaries([]cache.ThreadSummary{{ChannelID: "C1", ThreadTS: "P1", Unread: false}})

	if m.MarkByThreadTSUnread("C2", "P9") {
		t.Error("expected false when (channel, thread) not in summaries")
	}
}

func TestMarkByThreadTSUnread_EmptyArgs_ReturnsFalse(t *testing.T) {
	m := New()
	m.SetSummaries([]cache.ThreadSummary{{ChannelID: "C1", ThreadTS: "P1", Unread: false}})

	if m.MarkByThreadTSUnread("", "P1") {
		t.Error("expected false for empty channelID")
	}
	if m.MarkByThreadTSUnread("C1", "") {
		t.Error("expected false for empty threadTS")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/threadsview/ -run TestMarkByThreadTSUnread -v`
Expected: FAIL with `m.MarkByThreadTSUnread undefined`.

- [ ] **Step 3: Implement**

In `internal/ui/threadsview/model.go`, after `MarkByThreadTSRead` (around line 343):

```go
// MarkByThreadTSUnread sets the local Unread flag on the summary matching
// (channelID, threadTS) to true. Returns true when a flag was actually
// flipped (i.e., the row existed and was previously read). Like
// MarkByThreadTSRead this is presentation-only: it does not touch Slack
// server state. Used by the U-key mark-unread flow and by the inbound
// thread_marked WS handler.
//
// Note: the threads-view's underlying heuristic
// (LastReplyTS > channel.last_read_ts AND LastReplyBy != self) may
// re-clear this flag on the next refresh from cache.ListInvolvedThreads
// if the heuristic considers the thread read. This is the documented
// v1 limitation; a per-thread last_read_ts column is future work.
func (m *Model) MarkByThreadTSUnread(channelID, threadTS string) bool {
	if channelID == "" || threadTS == "" {
		return false
	}
	for i := range m.summaries {
		if m.summaries[i].ChannelID == channelID && m.summaries[i].ThreadTS == threadTS {
			if m.summaries[i].Unread {
				return false
			}
			m.summaries[i].Unread = true
			m.dirty()
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/threadsview/ -run TestMarkByThreadTSUnread -v`
Expected: PASS.

- [ ] **Step 5: Run full threadsview tests**

Run: `go test ./internal/ui/threadsview/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/threadsview/model.go internal/ui/threadsview/model_test.go
git commit -m "feat(threadsview): add MarkByThreadTSUnread sibling to MarkByThreadTSRead"
```

---

## Task 4: Refactor Slack client `MarkChannel`/`MarkThread` for DI

This task introduces a private `httpClient` field on `*Client` plus shared private helpers, without changing public behavior. It backfills missing test coverage for the existing functions while we're here. The new `MarkChannelUnread` / `MarkThreadUnread` will land in Task 5.

**Files:**
- Modify: `internal/slack/client.go`
- Test: `internal/slack/client_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/slack/client_test.go`:

```go
import (
	"net/http"
	"net/http/httptest"
	"net/url"
	// ...existing imports
)

// newTestClient returns a *Client wired to point at the given test server.
// Internal helpers like markChannel use c.httpClient (set here) and a
// pluggable c.markBaseURL (which defaults to https://slack.com/api/...).
func newTestClient(server *httptest.Server) *Client {
	c := &Client{
		token:        "xoxc-test",
		cookie:       "test-cookie",
		httpClient:   server.Client(),
		markBaseURL:  server.URL,
	}
	return c
}

func TestMarkChannel_PostsCorrectForm(t *testing.T) {
	var gotPath, gotAuth, gotContentType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.MarkChannel(context.Background(), "C123", "1700000000.000100"); err != nil {
		t.Fatalf("MarkChannel: %v", err)
	}

	if !strings.HasSuffix(gotPath, "/conversations.mark") {
		t.Errorf("path: got %q, want suffix /conversations.mark", gotPath)
	}
	if gotAuth != "Bearer xoxc-test" {
		t.Errorf("auth: got %q", gotAuth)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Errorf("content-type: got %q", gotContentType)
	}
	form, _ := url.ParseQuery(gotBody)
	if form.Get("channel") != "C123" {
		t.Errorf("channel: got %q", form.Get("channel"))
	}
	if form.Get("ts") != "1700000000.000100" {
		t.Errorf("ts: got %q", form.Get("ts"))
	}
}

func TestMarkThread_PostsReadOne(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.MarkThread(context.Background(), "C1", "P1", "R5"); err != nil {
		t.Fatalf("MarkThread: %v", err)
	}

	if !strings.HasSuffix(gotPath, "/subscriptions.thread.mark") {
		t.Errorf("path: got %q", gotPath)
	}
	form, _ := url.ParseQuery(gotBody)
	if form.Get("channel") != "C1" || form.Get("thread_ts") != "P1" || form.Get("ts") != "R5" {
		t.Errorf("form: %v", form)
	}
	if form.Get("read") != "1" {
		t.Errorf("expected read=1, got %q", form.Get("read"))
	}
}

func TestMarkThread_EmptyArgs_NoOp(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.MarkThread(context.Background(), "", "P1", "R5"); err != nil {
		t.Errorf("expected nil err on empty channelID, got %v", err)
	}
	if err := c.MarkThread(context.Background(), "C1", "", "R5"); err != nil {
		t.Errorf("expected nil err on empty threadTS, got %v", err)
	}
	if called {
		t.Error("expected no HTTP call when args are empty")
	}
}
```

Make sure these imports are present at the top of the test file (the file currently imports `context`, `errors`, `fmt`, `strings`, `testing`, `github.com/slack-go/slack`):

```go
"io"
"net/http"
"net/http/httptest"
"net/url"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/slack/ -run "TestMarkChannel_PostsCorrectForm|TestMarkThread_PostsReadOne|TestMarkThread_EmptyArgs_NoOp" -v`
Expected: FAIL — `Client.httpClient` and `Client.markBaseURL` undefined; `newTestClient` references undefined fields.

- [ ] **Step 3: Refactor `*Client` for DI**

In `internal/slack/client.go`, modify the `Client` struct (around line 49):

```go
type Client struct {
	api    SlackAPI
	wsConn *websocket.Conn
	wsMu   sync.Mutex
	wsDone chan struct{}
	teamID string
	userID string
	token  string
	cookie string

	// httpClient is used for the raw-HTTP wrapper methods that bypass
	// slack-go (MarkChannel, MarkThread, MarkChannelUnread,
	// MarkThreadUnread, GetChannelSections, etc.). Defaults to a
	// cookie-bearing client built from `cookie`. Tests override.
	httpClient *http.Client
	// markBaseURL is the base URL for conversations.mark and
	// subscriptions.thread.mark. Defaults to https://slack.com/api.
	// Tests point this at httptest.NewServer.
	markBaseURL string
}
```

Update `NewClient` (around line 63) to populate the two new fields:

```go
func NewClient(xoxcToken, dCookie string) *Client {
	httpClient := newCookieHTTPClient(dCookie)

	api := slack.New(
		xoxcToken,
		slack.OptionHTTPClient(httpClient),
	)

	return &Client{
		api:         api,
		token:       xoxcToken,
		cookie:      dCookie,
		httpClient:  httpClient,
		markBaseURL: "https://slack.com/api",
	}
}
```

- [ ] **Step 4: Extract shared helpers, rewrite `MarkChannel` / `MarkThread`**

Replace `MarkChannel` and `MarkThread` (lines 728-787) with:

```go
// markChannel posts to conversations.mark with the given form values.
// Used by both MarkChannel (read up to ts) and MarkChannelUnread
// (roll the watermark backward to ts).
func (c *Client) markChannel(ctx context.Context, channelID, ts string) error {
	data := url.Values{
		"channel": {channelID},
		"ts":      {ts},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.markBaseURL+"/conversations.mark",
		strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("creating mark request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("marking channel: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// markThread posts to subscriptions.thread.mark with the given args.
// Used by both MarkThread (read=1) and MarkThreadUnread (read=0).
func (c *Client) markThread(ctx context.Context, channelID, threadTS, ts string, read bool) error {
	if channelID == "" || threadTS == "" {
		return nil
	}
	if ts == "" {
		ts = threadTS
	}
	readVal := "0"
	if read {
		readVal = "1"
	}
	data := url.Values{
		"channel":   {channelID},
		"thread_ts": {threadTS},
		"ts":        {ts},
		"read":      {readVal},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.markBaseURL+"/subscriptions.thread.mark",
		strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("creating thread mark request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("marking thread: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// MarkChannel marks a channel as read up to the given timestamp.
func (c *Client) MarkChannel(ctx context.Context, channelID, ts string) error {
	return c.markChannel(ctx, channelID, ts)
}

// MarkThread marks a thread as read up to the given timestamp using
// Slack's undocumented subscriptions.thread.mark endpoint. channelID is
// the parent channel, threadTS is the parent message ts, and ts is the
// latest reply ts the user has now seen (use threadTS itself when there
// are no replies). Best-effort: the endpoint is undocumented and may
// break if Slack changes its API.
func (c *Client) MarkThread(ctx context.Context, channelID, threadTS, ts string) error {
	return c.markThread(ctx, channelID, threadTS, ts, true)
}
```

- [ ] **Step 5: Run the new tests to verify they pass**

Run: `go test ./internal/slack/ -run "TestMarkChannel_PostsCorrectForm|TestMarkThread_PostsReadOne|TestMarkThread_EmptyArgs_NoOp" -v`
Expected: PASS.

- [ ] **Step 6: Run full slack package tests**

Run: `go test ./internal/slack/`
Expected: PASS — no regressions.

- [ ] **Step 7: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "refactor(slack): DI'd httpClient + shared mark helpers, with backfilled tests"
```

---

## Task 5: Add `MarkChannelUnread` and `MarkThreadUnread`

**Files:**
- Modify: `internal/slack/client.go`
- Test: `internal/slack/client_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/slack/client_test.go`:

```go
func TestMarkChannelUnread_PostsCorrectForm(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.MarkChannelUnread(context.Background(), "C123", "1700000000.000050"); err != nil {
		t.Fatalf("MarkChannelUnread: %v", err)
	}

	if !strings.HasSuffix(gotPath, "/conversations.mark") {
		t.Errorf("path: got %q", gotPath)
	}
	form, _ := url.ParseQuery(gotBody)
	if form.Get("channel") != "C123" {
		t.Errorf("channel: got %q", form.Get("channel"))
	}
	if form.Get("ts") != "1700000000.000050" {
		t.Errorf("ts: got %q", form.Get("ts"))
	}
}

func TestMarkChannelUnread_EmptyTSSendsZero(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.MarkChannelUnread(context.Background(), "C1", ""); err != nil {
		t.Fatalf("MarkChannelUnread: %v", err)
	}

	form, _ := url.ParseQuery(gotBody)
	if form.Get("ts") != "0" {
		t.Errorf("expected ts=0 for empty input, got %q", form.Get("ts"))
	}
}

func TestMarkThreadUnread_PostsReadZero(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.MarkThreadUnread(context.Background(), "C1", "P1", "R5"); err != nil {
		t.Fatalf("MarkThreadUnread: %v", err)
	}

	if !strings.HasSuffix(gotPath, "/subscriptions.thread.mark") {
		t.Errorf("path: got %q", gotPath)
	}
	form, _ := url.ParseQuery(gotBody)
	if form.Get("channel") != "C1" || form.Get("thread_ts") != "P1" || form.Get("ts") != "R5" {
		t.Errorf("form: %v", form)
	}
	if form.Get("read") != "0" {
		t.Errorf("expected read=0, got %q", form.Get("read"))
	}
}

func TestMarkThreadUnread_EmptyArgs_NoOp(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if err := c.MarkThreadUnread(context.Background(), "", "P1", "R5"); err != nil {
		t.Errorf("expected nil err, got %v", err)
	}
	if err := c.MarkThreadUnread(context.Background(), "C1", "", "R5"); err != nil {
		t.Errorf("expected nil err, got %v", err)
	}
	if called {
		t.Error("expected no HTTP call when args empty")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/slack/ -run "TestMarkChannelUnread|TestMarkThreadUnread" -v`
Expected: FAIL — `MarkChannelUnread` / `MarkThreadUnread` undefined.

- [ ] **Step 3: Implement**

In `internal/slack/client.go`, after `MarkThread` (around the end of the existing mark block), add:

```go
// MarkChannelUnread rolls the channel's read watermark backward to ts,
// effectively making the message at ts and every newer message in the
// channel unread again. Pass ts == "" to mark the entire channel unread
// (Slack's "0" sentinel).
func (c *Client) MarkChannelUnread(ctx context.Context, channelID, ts string) error {
	if ts == "" {
		ts = "0"
	}
	return c.markChannel(ctx, channelID, ts)
}

// MarkThreadUnread marks a thread as unread starting at ts using Slack's
// subscriptions.thread.mark endpoint with read=0. Mirrors MarkThread but
// flips the read flag. channelID is the parent channel, threadTS is the
// parent message ts, and ts is the reply that should become the new
// "first unread" boundary (use threadTS to mark the entire thread
// unread when there are no replies). Best-effort.
func (c *Client) MarkThreadUnread(ctx context.Context, channelID, threadTS, ts string) error {
	return c.markThread(ctx, channelID, threadTS, ts, false)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/slack/ -run "TestMarkChannelUnread|TestMarkThreadUnread" -v`
Expected: PASS.

- [ ] **Step 5: Run full slack package tests**

Run: `go test ./internal/slack/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "feat(slack): add MarkChannelUnread and MarkThreadUnread"
```

---

## Task 6: Add `MarkedUnreadMsg` and `MarkUnreadFailedMsg` toast types

**Files:**
- Modify: `internal/ui/statusbar/model.go` (alongside the existing toast message types, ~line 320)

- [ ] **Step 1: Add types**

Append to `internal/ui/statusbar/model.go`:

```go
// MarkedUnreadMsg is delivered when the user successfully marks a
// message (or thread reply) as unread. App handles by setting the toast
// to "Marked unread" and scheduling a CopiedClearMsg.
type MarkedUnreadMsg struct{}

// MarkUnreadFailedMsg is delivered when conversations.mark or
// subscriptions.thread.mark fail during a mark-unread. App handles by
// setting the toast to "Mark unread failed" and scheduling a
// CopiedClearMsg.
type MarkUnreadFailedMsg struct{ Reason string }
```

- [ ] **Step 2: Build the package to verify**

Run: `go build ./internal/ui/statusbar/`
Expected: success.

Run: `go test ./internal/ui/statusbar/`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/statusbar/model.go
git commit -m "feat(statusbar): add MarkedUnreadMsg/MarkUnreadFailedMsg toast types"
```

---

## Task 7: Extend `EventHandler` interface with `OnChannelMarked` / `OnThreadMarked`

This task is interface-only: it adds the new methods to the interface, the test mock, and stub implementations on `rtmEventHandler` so the build still passes. Real bodies for `rtmEventHandler` come in Task 17.

**Files:**
- Modify: `internal/slack/events.go`
- Modify: `internal/slack/events_test.go`
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Extend the interface**

In `internal/slack/events.go`, modify the `EventHandler` interface (around line 11):

```go
type EventHandler interface {
	OnMessage(channelID, userID, ts, text, threadTS, subtype string, edited bool, files []slack.File, blocks slack.Blocks, attachments []slack.Attachment)
	OnMessageDeleted(channelID, ts string)
	OnReactionAdded(channelID, ts, userID, emoji string)
	OnReactionRemoved(channelID, ts, userID, emoji string)
	OnPresenceChange(userID, presence string)
	OnUserTyping(channelID, userID string)
	OnConnect()
	OnDisconnect()
	OnSelfPresenceChange(presence string)
	OnDNDChange(enabled bool, endUnix int64)

	// OnChannelMarked is delivered when Slack pushes a channel_marked /
	// im_marked / group_marked / mpim_marked event (read state changed
	// in another client, or via slk's own MarkChannel/MarkChannelUnread
	// echoing back). ts is the new last_read watermark; unreadCount is
	// the canonical workspace-side unread count for the channel (use to
	// drive the sidebar badge).
	OnChannelMarked(channelID, ts string, unreadCount int)
	// OnThreadMarked is delivered when Slack pushes a thread_marked
	// event. read indicates whether the thread is now read (true) or
	// unread (false). ts is the new boundary within the thread.
	OnThreadMarked(channelID, threadTS, ts string, read bool)
}
```

- [ ] **Step 2: Update `mockEventHandler` in the test file**

In `internal/slack/events_test.go`, add fields and method stubs to `mockEventHandler` (around line 16):

```go
type mockEventHandler struct {
	messages            []string
	subtypes            []string
	deletedMessages     []string
	reactions           []string
	presenceChanges     []string
	typingEvents        []string
	selfPresenceChanges []string
	dndChanges          []dndChangeRecord
	lastBlocks          slack.Blocks
	lastAttachments     []slack.Attachment

	channelMarks []channelMarkRecord
	threadMarks  []threadMarkRecord
}

type channelMarkRecord struct {
	channelID   string
	ts          string
	unreadCount int
}

type threadMarkRecord struct {
	channelID, threadTS, ts string
	read                    bool
}
```

Add the two methods (next to the other `mockEventHandler` methods, after `OnDNDChange`):

```go
func (m *mockEventHandler) OnChannelMarked(channelID, ts string, unreadCount int) {
	m.channelMarks = append(m.channelMarks, channelMarkRecord{channelID, ts, unreadCount})
}

func (m *mockEventHandler) OnThreadMarked(channelID, threadTS, ts string, read bool) {
	m.threadMarks = append(m.threadMarks, threadMarkRecord{channelID, threadTS, ts, read})
}
```

- [ ] **Step 3: Add stub methods on `rtmEventHandler` in main.go**

In `cmd/slk/main.go`, after `OnDNDChange` (around line 1831, end of `rtmEventHandler` methods), add stub implementations. Real bodies land in Task 17:

```go
func (h *rtmEventHandler) OnChannelMarked(channelID, ts string, unreadCount int) {
	// Real body wired in main.go's wireCallbacks closure (see Task 17);
	// this stub satisfies the interface so the package builds.
}

func (h *rtmEventHandler) OnThreadMarked(channelID, threadTS, ts string, read bool) {
	// Real body wired in main.go's wireCallbacks closure (see Task 17);
	// this stub satisfies the interface so the package builds.
}
```

- [ ] **Step 4: Build everything**

Run: `go build ./...`
Expected: success.

- [ ] **Step 5: Run all package tests to confirm no regressions**

Run: `go test ./internal/slack/ ./cmd/slk/`
Expected: PASS (or whatever was passing before).

- [ ] **Step 6: Commit**

```bash
git add internal/slack/events.go internal/slack/events_test.go cmd/slk/main.go
git commit -m "feat(slack): extend EventHandler with OnChannelMarked/OnThreadMarked stubs"
```

---

## Task 8: Dispatch `channel_marked` / `im_marked` / `group_marked` / `mpim_marked` events

**Files:**
- Modify: `internal/slack/events.go`
- Test: `internal/slack/events_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/slack/events_test.go`:

```go
func TestDispatch_ChannelMarked_CallsHandler(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"channel_marked","channel":"C123","ts":"1700000000.000100","unread_count_display":3}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.channelMarks) != 1 {
		t.Fatalf("expected 1 channelMark, got %d", len(handler.channelMarks))
	}
	got := handler.channelMarks[0]
	if got.channelID != "C123" || got.ts != "1700000000.000100" || got.unreadCount != 3 {
		t.Errorf("unexpected: %+v", got)
	}
}

func TestDispatch_IMMarked_CallsHandler(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"im_marked","channel":"D1","ts":"1.0","unread_count_display":1}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.channelMarks) != 1 {
		t.Fatalf("expected 1 channelMark, got %d", len(handler.channelMarks))
	}
	if handler.channelMarks[0].channelID != "D1" {
		t.Errorf("channel: %q", handler.channelMarks[0].channelID)
	}
}

func TestDispatch_GroupMarked_CallsHandler(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"group_marked","channel":"G1","ts":"1.0","unread_count_display":0}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.channelMarks) != 1 {
		t.Fatalf("expected 1 channelMark, got %d", len(handler.channelMarks))
	}
	if handler.channelMarks[0].unreadCount != 0 {
		t.Errorf("unreadCount: %d", handler.channelMarks[0].unreadCount)
	}
}

func TestDispatch_MPIMMarked_CallsHandler(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"mpim_marked","channel":"G2","ts":"1.0","unread_count_display":2}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.channelMarks) != 1 {
		t.Fatalf("expected 1 channelMark, got %d", len(handler.channelMarks))
	}
}

func TestDispatch_ChannelMarked_MalformedJSON_NoCall(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"channel_marked","channel":`) // truncated
	dispatchWebSocketEvent(data, handler)

	if len(handler.channelMarks) != 0 {
		t.Errorf("expected 0 calls on malformed JSON, got %d", len(handler.channelMarks))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/slack/ -run "TestDispatch_(Channel|IM|Group|MPIM)Marked" -v`
Expected: FAIL — the dispatch table doesn't recognize these event types yet.

- [ ] **Step 3: Add the event struct and dispatch case**

In `internal/slack/events.go`, add a struct above `dispatchWebSocketEvent` (around line 110, near `wsDNDUpdatedEvent`):

```go
// wsChannelMarkedEvent represents a channel_marked / im_marked /
// group_marked / mpim_marked event. Slack uses the same payload
// shape across all four — the type field disambiguates.
type wsChannelMarkedEvent struct {
	Type               string `json:"type"`
	Channel            string `json:"channel"`
	TS                 string `json:"ts"`
	UnreadCountDisplay int    `json:"unread_count_display"`
}
```

Add a case to `dispatchWebSocketEvent` (in the switch, after the existing `dnd_updated` arm, around line 177):

```go
	case "channel_marked", "im_marked", "group_marked", "mpim_marked":
		var evt wsChannelMarkedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		handler.OnChannelMarked(evt.Channel, evt.TS, evt.UnreadCountDisplay)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/slack/ -run "TestDispatch_(Channel|IM|Group|MPIM)Marked" -v`
Expected: PASS.

- [ ] **Step 5: Run full slack package tests**

Run: `go test ./internal/slack/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/slack/events.go internal/slack/events_test.go
git commit -m "feat(slack): dispatch channel_marked/im_marked/group_marked/mpim_marked events"
```

---

## Task 9: Dispatch `thread_marked` events

**Files:**
- Modify: `internal/slack/events.go`
- Test: `internal/slack/events_test.go`

The `thread_marked` payload nests `channel`, `thread_ts`, and `ts` under a `subscription` key, with a top-level read-state indicator. Slack's exact shape varies but is approximately:

```json
{
  "type": "thread_marked",
  "subscription": {
    "channel": "C1",
    "thread_ts": "1700000000.000100",
    "last_read": "1700000000.000200",
    "active": true
  }
}
```

We treat `active=true` as "thread is unread" and `active=false` as "thread is read". `last_read` is the boundary ts.

- [ ] **Step 1: Write failing tests**

Append to `internal/slack/events_test.go`:

```go
func TestDispatch_ThreadMarked_Unread_CallsHandler(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"thread_marked","subscription":{"channel":"C1","thread_ts":"1700000000.000100","last_read":"1700000000.000200","active":true}}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.threadMarks) != 1 {
		t.Fatalf("expected 1 threadMark, got %d", len(handler.threadMarks))
	}
	got := handler.threadMarks[0]
	if got.channelID != "C1" || got.threadTS != "1700000000.000100" || got.ts != "1700000000.000200" {
		t.Errorf("unexpected: %+v", got)
	}
	if got.read {
		t.Error("expected read=false (active=true means unread)")
	}
}

func TestDispatch_ThreadMarked_Read_CallsHandler(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"thread_marked","subscription":{"channel":"C1","thread_ts":"P1","last_read":"R5","active":false}}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.threadMarks) != 1 {
		t.Fatalf("expected 1 threadMark, got %d", len(handler.threadMarks))
	}
	if !handler.threadMarks[0].read {
		t.Error("expected read=true (active=false means read)")
	}
}

func TestDispatch_ThreadMarked_MalformedJSON_NoCall(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"thread_marked","subscription":{`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.threadMarks) != 0 {
		t.Errorf("expected 0 calls on malformed JSON, got %d", len(handler.threadMarks))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/slack/ -run TestDispatch_ThreadMarked -v`
Expected: FAIL.

- [ ] **Step 3: Add the event struct and dispatch case**

In `internal/slack/events.go`, add a struct above `dispatchWebSocketEvent`:

```go
// wsThreadMarkedEvent represents a thread_marked event from Slack's
// browser-protocol WebSocket. The subscription block carries the
// channel/thread/last-read-ts and an `active` flag (true means the
// thread is now unread / subscribed for unread updates; false means
// the thread is now read).
type wsThreadMarkedEvent struct {
	Type         string `json:"type"`
	Subscription struct {
		Channel  string `json:"channel"`
		ThreadTS string `json:"thread_ts"`
		LastRead string `json:"last_read"`
		Active   bool   `json:"active"`
	} `json:"subscription"`
}
```

Add a case to `dispatchWebSocketEvent` (after the channel_marked arm):

```go
	case "thread_marked":
		var evt wsThreadMarkedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		// active=true means subscribed-for-unread, i.e. the thread is
		// now unread. Invert for the read flag we hand to the handler.
		read := !evt.Subscription.Active
		handler.OnThreadMarked(evt.Subscription.Channel, evt.Subscription.ThreadTS, evt.Subscription.LastRead, read)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/slack/ -run TestDispatch_ThreadMarked -v`
Expected: PASS.

- [ ] **Step 5: Run full slack package tests**

Run: `go test ./internal/slack/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/slack/events.go internal/slack/events_test.go
git commit -m "feat(slack): dispatch thread_marked events"
```

---

## Task 10: Add `MarkUnread` keybinding

**Files:**
- Modify: `internal/ui/keys.go`

- [ ] **Step 1: Add the binding field**

In `internal/ui/keys.go`, add a field to the `KeyMap` struct (near `OpenPreview`, around line 36):

```go
	OpenPreview         key.Binding
	MarkUnread          key.Binding
```

- [ ] **Step 2: Add the default binding**

In `DefaultKeyMap()`, add (after `OpenPreview`, around line 75):

```go
		OpenPreview:         key.NewBinding(key.WithKeys("O", "v"), key.WithHelp("O/v", "open image preview")),
		MarkUnread:          key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "mark unread")),
```

- [ ] **Step 3: Build to verify**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Run UI package tests**

Run: `go test ./internal/ui/`
Expected: PASS (no behavior tests yet — they come in Task 12).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/keys.go
git commit -m "feat(ui): add MarkUnread keybinding (U)"
```

---

## Task 11: Add `MarkUnreadMsg` / `MessageMarkedUnreadMsg` / `MarkUnreadFunc` / setter

This task introduces the type plumbing only; the dispatcher and helper come in Task 12, the apply Update arm in Task 13.

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add message types**

In `internal/ui/app.go`, after `MessageDeletedMsg` (around line 391):

```go
// MarkUnreadMsg requests the App to mark the given message as unread.
// ThreadTS is "" for channel-level mark-unread; non-empty for thread-level
// (in which case ChannelID is the parent channel and BoundaryTS is the
// boundary within the thread). BoundaryTS is the ts that should become
// the new last_read watermark — i.e., the ts of the message immediately
// before the user's selection. UnreadCount is computed by the dispatcher
// from the loaded buffer at press time and is forwarded to the sidebar
// for an exact badge value (0 for thread-level, since the sidebar only
// tracks channel-level unreads).
type MarkUnreadMsg struct {
	ChannelID   string
	ThreadTS    string
	BoundaryTS  string
	UnreadCount int
}

// MessageMarkedUnreadMsg carries the result of a MarkUnreadFunc call.
// On success Err is nil and the App's Update arm applies the local
// state changes (move the unread boundary, update the sidebar badge,
// flip the threads-view row, emit a toast). On error Err is populated
// and the toast goes to the failure path; no local state mutates.
type MessageMarkedUnreadMsg struct {
	ChannelID   string
	ThreadTS    string
	BoundaryTS  string
	UnreadCount int
	Err         error
}
```

- [ ] **Step 2: Add the func type**

After `MessageDeleteFunc` (around line 425):

```go
// MarkUnreadFunc performs the conversations.mark or
// subscriptions.thread.mark HTTP call (with the rolled-back ts /
// read=0 form), updates SQLite + in-memory caches if the call
// succeeded, and returns a tea.Msg (typically MessageMarkedUnreadMsg)
// describing the result. ThreadTS == "" means channel-level.
type MarkUnreadFunc func(channelID, threadTS, boundaryTS string, unreadCount int) tea.Msg
```

- [ ] **Step 3: Add the App field and setter**

Find where `messageDeleter` is declared on the `App` struct (search for `messageDeleter` to locate). Add adjacent:

```go
	messageMarkUnreader MarkUnreadFunc
```

After `SetMessageDeleter` (around line 3389) add:

```go
// SetMessageMarkUnreader wires the conversations.mark / subscriptions.thread.mark
// callback used by the U key. Implementations should perform the HTTP call
// best-effort, persist the new last_read_ts to SQLite for channel-level
// marks (no-op for thread-level until per-thread state lands), update the
// in-memory LastReadMap, and return MessageMarkedUnreadMsg.
func (a *App) SetMessageMarkUnreader(fn MarkUnreadFunc) {
	a.messageMarkUnreader = fn
}
```

- [ ] **Step 4: Add the dispatcher arm in `Update`**

After the `DeleteMessageMsg` arm (around line 1413), add:

```go
	case MarkUnreadMsg:
		if a.messageMarkUnreader != nil {
			marker := a.messageMarkUnreader
			chID, threadTS, ts, n := msg.ChannelID, msg.ThreadTS, msg.BoundaryTS, msg.UnreadCount
			cmds = append(cmds, func() tea.Msg {
				return marker(chID, threadTS, ts, n)
			})
		}
```

- [ ] **Step 5: Build to verify**

Run: `go build ./...`
Expected: success.

- [ ] **Step 6: Run UI tests**

Run: `go test ./internal/ui/`
Expected: PASS (no new behavior tests yet — they come in Task 12).

- [ ] **Step 7: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat(ui): add MarkUnreadMsg/MessageMarkedUnreadMsg/MarkUnreadFunc plumbing"
```

---

## Task 12: Add `markUnreadOfSelected` helper and key dispatch

**Files:**
- Modify: `internal/ui/messages/model.go` (add `SelectByIndex` test accessor)
- Modify: `internal/ui/thread/model.go` (add `SelectByIndex` test accessor)
- Modify: `internal/ui/app.go`
- Test: `internal/ui/app_test.go`

- [ ] **Step 1: Add `SelectByIndex` accessors to messages.Model and thread.Model**

In `internal/ui/messages/model.go`, after `SelectedMessage` (around line 542):

```go
// SelectByIndex moves the selection cursor to i. No-op if i is out of
// range. Used by tests that need a deterministic selection state.
func (m *Model) SelectByIndex(i int) {
	if i < 0 || i >= len(m.messages) {
		return
	}
	if m.selected != i {
		m.selected = i
		m.dirty()
	}
}
```

In `internal/ui/thread/model.go`, after `SelectedReply` (around line 342):

```go
// SelectByIndex moves the selection cursor to i (an index into Replies()).
// No-op if i is out of range. Used by tests that need a deterministic
// selection state.
func (m *Model) SelectByIndex(i int) {
	if i < 0 || i >= len(m.replies) {
		return
	}
	if m.selected != i {
		m.selected = i
		m.InvalidateCache()
	}
}
```

- [ ] **Step 2: Write failing tests**

Append to `internal/ui/app_test.go`:

```go
func TestMarkUnreadOfSelected_NoSelection_NoOp(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	// no messages loaded → no selection

	cmd := app.markUnreadOfSelected()
	if cmd != nil {
		t.Errorf("expected nil cmd when nothing selected, got non-nil")
	}
}

func TestMarkUnreadOfSelected_ChannelPane_EmitsMarkUnreadMsg(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.SetCurrentUserID("U_ME")
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U_OTHER", Text: "first"},
		{TS: "2.0", UserID: "U_OTHER", Text: "second"},
		{TS: "3.0", UserID: "U_ME", Text: "third (selected)"},
		{TS: "4.0", UserID: "U_OTHER", Text: "fourth"},
		{TS: "5.0", UserID: "U_OTHER", Text: "fifth"},
	})
	app.focusedPanel = PanelMessages
	// SetMessages selects the last message; force selection to index 2.
	app.messagepane.SelectByIndex(2)

	cmd := app.markUnreadOfSelected()
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	res := cmd()
	mu, ok := res.(MarkUnreadMsg)
	if !ok {
		t.Fatalf("expected MarkUnreadMsg, got %T", res)
	}
	if mu.ChannelID != "C1" {
		t.Errorf("ChannelID: got %q", mu.ChannelID)
	}
	if mu.ThreadTS != "" {
		t.Errorf("ThreadTS: expected empty for channel-pane mark, got %q", mu.ThreadTS)
	}
	if mu.BoundaryTS != "2.0" {
		t.Errorf("BoundaryTS: expected '2.0' (msg before selected), got %q", mu.BoundaryTS)
	}
	if mu.UnreadCount != 3 {
		t.Errorf("UnreadCount: expected 3 (selected + 2 newer), got %d", mu.UnreadCount)
	}
}

func TestMarkUnreadOfSelected_OldestMessage_BoundaryIsZero(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U_OTHER", Text: "first (selected)"},
		{TS: "2.0", UserID: "U_OTHER", Text: "second"},
	})
	app.focusedPanel = PanelMessages
	app.messagepane.SelectByIndex(0)

	cmd := app.markUnreadOfSelected()
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	res := cmd()
	mu := res.(MarkUnreadMsg)
	if mu.BoundaryTS != "0" {
		t.Errorf("expected BoundaryTS='0' for oldest-message case, got %q", mu.BoundaryTS)
	}
	if mu.UnreadCount != 2 {
		t.Errorf("UnreadCount: expected 2, got %d", mu.UnreadCount)
	}
}

func TestMarkUnreadOfSelected_ThreadPane_EmitsThreadMarkUnread(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("U_ME")
	parent := messages.MessageItem{TS: "P1", UserID: "U_OTHER", Text: "parent"}
	app.threadPanel.SetThread(parent, []messages.MessageItem{
		{TS: "R1", UserID: "U_OTHER", Text: "first reply"},
		{TS: "R2", UserID: "U_OTHER", Text: "second reply (selected)"},
		{TS: "R3", UserID: "U_OTHER", Text: "third reply"},
	}, "C1", "P1")
	app.threadVisible = true
	app.focusedPanel = PanelThread
	app.threadPanel.SelectByIndex(1)

	cmd := app.markUnreadOfSelected()
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	res := cmd()
	mu, ok := res.(MarkUnreadMsg)
	if !ok {
		t.Fatalf("expected MarkUnreadMsg, got %T", res)
	}
	if mu.ChannelID != "C1" || mu.ThreadTS != "P1" {
		t.Errorf("got channel=%q thread=%q", mu.ChannelID, mu.ThreadTS)
	}
	if mu.BoundaryTS != "R1" {
		t.Errorf("BoundaryTS: expected 'R1', got %q", mu.BoundaryTS)
	}
	if mu.UnreadCount != 0 {
		t.Errorf("UnreadCount: expected 0 for thread-level, got %d", mu.UnreadCount)
	}
}

func TestMarkUnreadOfSelected_ThreadPane_OldestReply_BoundaryIsParentTS(t *testing.T) {
	// Selecting the oldest reply → boundary is the parent ts (so the
	// whole thread, but not the parent message itself, becomes unread).
	app := NewApp()
	parent := messages.MessageItem{TS: "P1", UserID: "U_OTHER", Text: "parent"}
	app.threadPanel.SetThread(parent, []messages.MessageItem{
		{TS: "R1", UserID: "U_OTHER", Text: "first (selected)"},
		{TS: "R2", UserID: "U_OTHER", Text: "second"},
	}, "C1", "P1")
	app.threadVisible = true
	app.focusedPanel = PanelThread
	app.threadPanel.SelectByIndex(0)

	cmd := app.markUnreadOfSelected()
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	res := cmd()
	mu := res.(MarkUnreadMsg)
	if mu.BoundaryTS != "P1" {
		t.Errorf("expected boundary=P1 (parent ts) for oldest reply, got %q", mu.BoundaryTS)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run TestMarkUnreadOfSelected -v`
Expected: FAIL — `markUnreadOfSelected` undefined.

- [ ] **Step 4: Implement the helper**

In `internal/ui/app.go`, after `beginDeleteOfSelected` (around line 4673), add:

```go
// markUnreadOfSelected rolls the read watermark backward to the message
// immediately before the currently-selected message in the focused
// pane. Channel pane → emits MarkUnreadMsg with ThreadTS="". Thread
// pane → emits MarkUnreadMsg with ThreadTS=parent ts. Returns nil
// when nothing is selected (silent no-op, matches Edit/Delete).
//
// Boundary semantics:
//   - Channel pane, selection is i-th of N loaded messages →
//       BoundaryTS = messages[i-1].TS (or "0" if i == 0)
//       UnreadCount = N - i
//   - Thread pane, selection is i-th of N replies →
//       BoundaryTS = replies[i-1].TS (or threadTS if i == 0)
//       UnreadCount = 0 (sidebar isn't updated for thread-level)
func (a *App) markUnreadOfSelected() tea.Cmd {
	channelID, ts, _, _, panel, ok := a.selectedMessageContext()
	if !ok || channelID == "" || ts == "" {
		return nil
	}

	switch panel {
	case PanelMessages:
		msgs := a.messagepane.Messages()
		idx := -1
		for i := range msgs {
			if msgs[i].TS == ts {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil
		}
		boundary := "0"
		if idx > 0 {
			boundary = msgs[idx-1].TS
		}
		unreadCount := len(msgs) - idx
		chID := channelID
		bTS := boundary
		n := unreadCount
		return func() tea.Msg {
			return MarkUnreadMsg{
				ChannelID:   chID,
				ThreadTS:    "",
				BoundaryTS:  bTS,
				UnreadCount: n,
			}
		}

	case PanelThread:
		threadTS := a.threadPanel.ThreadTS()
		replies := a.threadPanel.Replies()
		idx := -1
		for i := range replies {
			if replies[i].TS == ts {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil
		}
		boundary := threadTS
		if idx > 0 {
			boundary = replies[idx-1].TS
		}
		chID := channelID
		tTS := threadTS
		bTS := boundary
		return func() tea.Msg {
			return MarkUnreadMsg{
				ChannelID:   chID,
				ThreadTS:    tTS,
				BoundaryTS:  bTS,
				UnreadCount: 0,
			}
		}
	}
	return nil
}
```

`threadPanel.ThreadTS()` is already a public accessor on `*thread.Model` (`internal/ui/thread/model.go:216`).

- [ ] **Step 5: Wire the dispatch arm**

In `handleNormalMode` (around line 2074, after `OpenPreview`), add:

```go
	case key.Matches(msg, a.keys.OpenPreview):
		return a.openImagePreviewOfSelected()

	case key.Matches(msg, a.keys.MarkUnread):
		return a.markUnreadOfSelected()
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run TestMarkUnreadOfSelected -v`
Expected: PASS.

- [ ] **Step 7: Run full UI tests**

Run: `go test ./internal/ui/ ./internal/ui/messages/ ./internal/ui/thread/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go internal/ui/messages/model.go internal/ui/thread/model.go
git commit -m "feat(ui): markUnreadOfSelected + U key dispatch + SelectByIndex test accessors"
```

---

## Task 13: Apply local mark-unread state via shared `applyChannelMark` / `applyThreadMark`

**Files:**
- Modify: `internal/ui/messages/model.go` (add `LastReadTS` test accessor)
- Modify: `internal/ui/thread/model.go` (add `UnreadBoundaryTS` test accessor)
- Modify: `internal/ui/app.go`
- Test: `internal/ui/app_test.go`

- [ ] **Step 1: Add read-state accessors**

In `internal/ui/messages/model.go`, after `SetLastReadTS` (around line 970):

```go
// LastReadTS returns the current "last read" boundary timestamp. Used by
// tests; production code reads the field via SetLastReadTS-driven render.
func (m *Model) LastReadTS() string {
	return m.lastReadTS
}
```

In `internal/ui/thread/model.go`, after `SetUnreadBoundary` (around line 182):

```go
// UnreadBoundaryTS returns the current unread-boundary ts. Used by tests.
func (m *Model) UnreadBoundaryTS() string {
	return m.unreadBoundaryTS
}
```

- [ ] **Step 2: Write failing tests**

Add `"reflect"` to `internal/ui/app_test.go`'s import block (alongside the existing `"errors"`).

Append to `internal/ui/app_test.go`:

```go
func TestMessageMarkedUnreadMsg_ChannelLevel_UpdatesPaneSidebarAndToasts(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.sidebar.SetItems([]sidebar.ChannelItem{
		{ID: "C1", Name: "general", Section: "Channels"},
	})
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U1", Text: "first"},
		{TS: "2.0", UserID: "U1", Text: "second"},
		{TS: "3.0", UserID: "U1", Text: "third"},
	})

	_, cmd := app.Update(MessageMarkedUnreadMsg{
		ChannelID:   "C1",
		ThreadTS:    "",
		BoundaryTS:  "1.0",
		UnreadCount: 2,
		Err:         nil,
	})

	// Toast should be queued via tea.Cmd.
	if cmd == nil {
		t.Fatal("expected toast cmd")
	}
	// The cmd may be a batch; flatten and look for MarkedUnreadMsg.
	if !cmdContainsMsgType(cmd, statusbar.MarkedUnreadMsg{}) {
		t.Errorf("expected MarkedUnreadMsg in cmd output")
	}

	// Messages-pane boundary moved.
	if got := app.messagepane.LastReadTS(); got != "1.0" {
		t.Errorf("expected messagepane lastReadTS=1.0, got %q", got)
	}

	// Sidebar count was set.
	for _, it := range app.sidebar.Items() {
		if it.ID == "C1" && it.UnreadCount != 2 {
			t.Errorf("expected sidebar UnreadCount=2, got %d", it.UnreadCount)
		}
	}
}

func TestMessageMarkedUnreadMsg_ThreadLevel_UpdatesThreadPaneAndThreadsView(t *testing.T) {
	app := NewApp()
	parent := messages.MessageItem{TS: "P1", UserID: "U1", Text: "parent"}
	app.threadPanel.SetThread(parent, []messages.MessageItem{
		{TS: "R1", UserID: "U1", Text: "r1"},
		{TS: "R2", UserID: "U1", Text: "r2"},
	}, "C1", "P1")
	app.threadVisible = true
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", Unread: false},
	})

	_, cmd := app.Update(MessageMarkedUnreadMsg{
		ChannelID:  "C1",
		ThreadTS:   "P1",
		BoundaryTS: "R1",
		Err:        nil,
	})

	if cmd == nil || !cmdContainsMsgType(cmd, statusbar.MarkedUnreadMsg{}) {
		t.Errorf("expected MarkedUnreadMsg toast cmd")
	}

	// Thread pane unread boundary moved.
	if got := app.threadPanel.UnreadBoundaryTS(); got != "R1" {
		t.Errorf("expected thread unreadBoundary=R1, got %q", got)
	}

	// Threads-view row was flipped to unread.
	for _, s := range app.threadsView.Summaries() {
		if s.ThreadTS == "P1" && !s.Unread {
			t.Errorf("expected thread-view row P1 to be Unread=true")
		}
	}
}

func TestMessageMarkedUnreadMsg_Error_ToastsFailureNoStateChange(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U1", Text: "first"},
		{TS: "2.0", UserID: "U1", Text: "second"},
	})
	prevLastRead := app.messagepane.LastReadTS()

	_, cmd := app.Update(MessageMarkedUnreadMsg{
		ChannelID:  "C1",
		BoundaryTS: "0",
		Err:        errors.New("boom"),
	})

	if cmd == nil {
		t.Fatal("expected failure toast cmd")
	}
	// No state change.
	if app.messagepane.LastReadTS() != prevLastRead {
		t.Error("messagepane lastReadTS should be unchanged on error")
	}
}
```

Add a test helper at the bottom of `app_test.go` (or in a shared `app_testhelpers_test.go`) if not already present. The helper executes a tea.Cmd and reports whether any of its returned messages match a target type:

```go
// cmdContainsMsgType returns true if cmd (or any sub-cmd in a batch)
// returns a value of the same dynamic type as want when invoked.
func cmdContainsMsgType(cmd tea.Cmd, want any) bool {
	if cmd == nil {
		return false
	}
	res := cmd()
	if res == nil {
		return false
	}
	if reflect.TypeOf(res) == reflect.TypeOf(want) {
		return true
	}
	if batch, ok := res.(tea.BatchMsg); ok {
		for _, sub := range batch {
			if cmdContainsMsgType(sub, want) {
				return true
			}
		}
	}
	return false
}
```

Add `"errors"` and `"reflect"` to the test file's import block if not already present.

`app.sidebar.Items()` already exists (`internal/ui/sidebar/model.go:506`); `app.threadsView.Summaries()` already exists (`internal/ui/threadsview/model.go:208`). The new `LastReadTS()` and `UnreadBoundaryTS()` were added in Step 1.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run "TestMessageMarkedUnreadMsg" -v`
Expected: FAIL — the Update arm doesn't exist yet.

- [ ] **Step 4: Add shared helpers**

In `internal/ui/app.go`, add near the other action helpers (before `beginDeleteOfSelected`, around line 4640):

```go
// applyChannelMark updates local state for a channel-level read-state
// change (used by both the local mark-unread press and the inbound
// channel_marked WS event). channelID is the channel; ts is the new
// last_read watermark; unreadCount is the canonical unread count to
// show in the sidebar badge.
//
// Idempotent: calling twice with the same values is a no-op past the
// first one (the underlying setters short-circuit on equality).
func (a *App) applyChannelMark(channelID, ts string, unreadCount int) {
	if channelID == a.activeChannelID {
		a.messagepane.SetLastReadTS(ts)
	}
	a.sidebar.SetUnreadCount(channelID, unreadCount)
}

// applyThreadMark updates local state for a thread-level read-state
// change. read=false means the thread is now unread (move boundary +
// flip threads-view row); read=true means the thread is now read
// (clear boundary + clear threads-view row).
func (a *App) applyThreadMark(channelID, threadTS, ts string, read bool) {
	if a.threadVisible &&
		a.threadPanel.ChannelID() == channelID &&
		a.threadPanel.ThreadTS() == threadTS {
		if read {
			a.threadPanel.SetUnreadBoundary("")
		} else {
			a.threadPanel.SetUnreadBoundary(ts)
		}
	}
	if read {
		if a.threadsView.MarkByThreadTSRead(channelID, threadTS) {
			a.sidebar.SetThreadsUnreadCount(a.threadsView.UnreadCount())
		}
	} else {
		if a.threadsView.MarkByThreadTSUnread(channelID, threadTS) {
			a.sidebar.SetThreadsUnreadCount(a.threadsView.UnreadCount())
		}
	}
}
```

- [ ] **Step 5: Add the `MessageMarkedUnreadMsg` Update arm**

In `Update`, after the `MessageDeletedMsg` arm (around line 1420), add:

```go
	case MessageMarkedUnreadMsg:
		if msg.Err != nil {
			cmds = append(cmds, func() tea.Msg {
				return statusbar.MarkUnreadFailedMsg{Reason: msg.Err.Error()}
			})
			break
		}
		if msg.ThreadTS == "" {
			a.applyChannelMark(msg.ChannelID, msg.BoundaryTS, msg.UnreadCount)
		} else {
			a.applyThreadMark(msg.ChannelID, msg.ThreadTS, msg.BoundaryTS, false)
		}
		cmds = append(cmds, func() tea.Msg {
			return statusbar.MarkedUnreadMsg{}
		})
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run "TestMessageMarkedUnreadMsg" -v`
Expected: PASS.

- [ ] **Step 7: Run full UI tests**

Run: `go test ./internal/ui/ ./internal/ui/messages/ ./internal/ui/thread/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go internal/ui/messages/model.go internal/ui/thread/model.go
git commit -m "feat(ui): apply local state via applyChannelMark/applyThreadMark on MessageMarkedUnreadMsg"
```

---

## Task 14: Add `ChannelMarkedRemoteMsg` / `ThreadMarkedRemoteMsg` and Update arms

These are App-level messages that the `rtmEventHandler` (Task 17) will emit when a channel_marked / thread_marked event arrives. The Update arms run the same `applyChannelMark` / `applyThreadMark` helpers as the local press, plus do the SQLite + LastReadMap writes that the local-press path does in main.go's wireCallbacks (Task 16). For the remote path, those persistence writes happen in the rtmEventHandler before the message is sent — so the App-side arms are pure UI updates and emit no toast.

**Files:**
- Modify: `internal/ui/app.go`
- Test: `internal/ui/app_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/ui/app_test.go`:

```go
func TestChannelMarkedRemoteMsg_UpdatesPaneAndSidebarSilently(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.sidebar.SetItems([]sidebar.ChannelItem{
		{ID: "C1", Name: "general", Section: "Channels"},
	})
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U1", Text: "first"},
		{TS: "2.0", UserID: "U1", Text: "second"},
	})

	_, cmd := app.Update(ChannelMarkedRemoteMsg{
		ChannelID:   "C1",
		TS:          "1.0",
		UnreadCount: 1,
	})

	// No toast on remote events.
	if cmd != nil && cmdContainsMsgType(cmd, statusbar.MarkedUnreadMsg{}) {
		t.Error("expected no MarkedUnreadMsg toast on remote event")
	}

	if got := app.messagepane.LastReadTS(); got != "1.0" {
		t.Errorf("messagepane lastReadTS: got %q", got)
	}
	for _, it := range app.sidebar.Items() {
		if it.ID == "C1" && it.UnreadCount != 1 {
			t.Errorf("sidebar UnreadCount: got %d", it.UnreadCount)
		}
	}
}

func TestChannelMarkedRemoteMsg_InactiveChannel_OnlyUpdatesSidebar(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C_OTHER"
	app.sidebar.SetItems([]sidebar.ChannelItem{
		{ID: "C1", Name: "general", Section: "Channels"},
		{ID: "C_OTHER", Name: "elsewhere", Section: "Channels"},
	})
	prevLastRead := app.messagepane.LastReadTS()

	_, _ = app.Update(ChannelMarkedRemoteMsg{
		ChannelID: "C1", TS: "1.0", UnreadCount: 3,
	})

	// messages pane (showing C_OTHER) is untouched.
	if app.messagepane.LastReadTS() != prevLastRead {
		t.Error("messagepane should be untouched when remote event is for non-active channel")
	}
	// Sidebar still updated.
	for _, it := range app.sidebar.Items() {
		if it.ID == "C1" && it.UnreadCount != 3 {
			t.Errorf("expected C1 sidebar UnreadCount=3, got %d", it.UnreadCount)
		}
	}
}

func TestThreadMarkedRemoteMsg_UnreadFlipsRow(t *testing.T) {
	app := NewApp()
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", Unread: false},
	})

	_, cmd := app.Update(ThreadMarkedRemoteMsg{
		ChannelID: "C1",
		ThreadTS:  "P1",
		TS:        "R5",
		Read:      false,
	})

	if cmd != nil && cmdContainsMsgType(cmd, statusbar.MarkedUnreadMsg{}) {
		t.Error("expected no toast on remote thread event")
	}

	for _, s := range app.threadsView.Summaries() {
		if s.ThreadTS == "P1" && !s.Unread {
			t.Errorf("expected P1 to be Unread=true after remote thread_marked")
		}
	}
}

func TestThreadMarkedRemoteMsg_ReadClearsRow(t *testing.T) {
	app := NewApp()
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", Unread: true},
	})

	_, _ = app.Update(ThreadMarkedRemoteMsg{
		ChannelID: "C1", ThreadTS: "P1", TS: "R5", Read: true,
	})

	for _, s := range app.threadsView.Summaries() {
		if s.ThreadTS == "P1" && s.Unread {
			t.Errorf("expected P1 Unread=false after remote thread_marked read=true")
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run "Test(Channel|Thread)MarkedRemoteMsg" -v`
Expected: FAIL — message types undefined.

- [ ] **Step 3: Add the message types**

In `internal/ui/app.go`, after `MessageMarkedUnreadMsg` (added in Task 11):

```go
// ChannelMarkedRemoteMsg is dispatched by the WS event handler when
// Slack pushes a channel_marked / im_marked / group_marked / mpim_marked
// event (read state changed in another client, or via this client's own
// mark echoing back). The handler has already persisted the new
// last_read_ts to SQLite + the in-memory LastReadMap; the App's
// Update arm only updates the UI. No toast.
type ChannelMarkedRemoteMsg struct {
	ChannelID   string
	TS          string
	UnreadCount int
}

// ThreadMarkedRemoteMsg is dispatched by the WS event handler when
// Slack pushes a thread_marked event. Read=true means the thread is
// now read (clear local boundary + threads-view row); Read=false means
// it's unread.
type ThreadMarkedRemoteMsg struct {
	ChannelID string
	ThreadTS  string
	TS        string
	Read      bool
}
```

- [ ] **Step 4: Add Update arms**

In `Update`, after the `MessageMarkedUnreadMsg` arm (added in Task 13):

```go
	case ChannelMarkedRemoteMsg:
		a.applyChannelMark(msg.ChannelID, msg.TS, msg.UnreadCount)

	case ThreadMarkedRemoteMsg:
		a.applyThreadMark(msg.ChannelID, msg.ThreadTS, msg.TS, msg.Read)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run "Test(Channel|Thread)MarkedRemoteMsg" -v`
Expected: PASS.

- [ ] **Step 6: Run full UI tests**

Run: `go test ./internal/ui/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "feat(ui): handle ChannelMarkedRemoteMsg/ThreadMarkedRemoteMsg from WS events"
```

---

## Task 15: Add toast Update arms for `MarkedUnreadMsg` / `MarkUnreadFailedMsg`

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add the toast arms**

In `internal/ui/app.go`, after the `statusbar.PermalinkCopyFailedMsg` arm (around line 1114):

```go
	case statusbar.MarkedUnreadMsg:
		a.statusbar.SetToast("Marked unread")
		cmds = append(cmds, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))

	case statusbar.MarkUnreadFailedMsg:
		a.statusbar.SetToast("Mark unread failed: " + truncateReason(msg.Reason, 40))
		cmds = append(cmds, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))
```

- [ ] **Step 2: Build to verify**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Run UI tests**

Run: `go test ./internal/ui/`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat(ui): toast handlers for MarkedUnreadMsg/MarkUnreadFailedMsg"
```

---

## Task 16: Wire `SetMessageMarkUnreader` callback in main.go

**Files:**
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Wire the callback in `wireCallbacks`**

In `cmd/slk/main.go`, after the `app.SetMessageDeleter(...)` block (around line 617), add:

```go
		app.SetMessageMarkUnreader(func(channelID, threadTS, boundaryTS string, unreadCount int) tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			var err error
			if threadTS == "" {
				err = client.MarkChannelUnread(ctx, channelID, boundaryTS)
				if err == nil {
					if dbErr := db.UpdateLastReadTS(channelID, boundaryTS); dbErr != nil {
						log.Printf("Warning: UpdateLastReadTS(%s, %s): %v", channelID, boundaryTS, dbErr)
					}
					lastReadMap[channelID] = boundaryTS
				} else {
					log.Printf("Warning: MarkChannelUnread(%s, %s): %v", channelID, boundaryTS, err)
				}
			} else {
				err = client.MarkThreadUnread(ctx, channelID, threadTS, boundaryTS)
				if err != nil {
					log.Printf("Warning: MarkThreadUnread(%s, %s, %s): %v", channelID, threadTS, boundaryTS, err)
				}
				// No SQLite write for thread-level — the schema has no
				// per-thread last_read_ts column in v1. The UI updates
				// via applyThreadMark; on next refresh
				// cache.ListInvolvedThreads will reconcile from the
				// channel's last_read_ts heuristic.
			}
			return ui.MessageMarkedUnreadMsg{
				ChannelID:   channelID,
				ThreadTS:    threadTS,
				BoundaryTS:  boundaryTS,
				UnreadCount: unreadCount,
				Err:         err,
			}
		})
```

- [ ] **Step 2: Build to verify**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Run all tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat(main): wire SetMessageMarkUnreader callback"
```

---

## Task 17: Implement real `OnChannelMarked` / `OnThreadMarked` on `rtmEventHandler`

The stubs from Task 7 satisfy the interface; this task makes them functional.

**Files:**
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Add lastReadMap access to `rtmEventHandler`**

The handler needs to be able to write to the workspace's in-memory `LastReadMap`. It already has `wsCtx *WorkspaceContext` as a back-reference (around line 1624). Confirm by reading that struct definition; if it's missing, add the field. (No changes if already present.)

- [ ] **Step 2: Replace the stub bodies**

In `cmd/slk/main.go`, replace the `OnChannelMarked` and `OnThreadMarked` stubs (added in Task 7) with real implementations:

```go
func (h *rtmEventHandler) OnChannelMarked(channelID, ts string, unreadCount int) {
	// Persist regardless of active workspace so the cache stays
	// authoritative across workspace switches.
	if err := h.db.UpdateLastReadTS(channelID, ts); err != nil {
		log.Printf("Warning: UpdateLastReadTS on channel_marked: %v", err)
	}
	if h.wsCtx != nil && h.wsCtx.LastReadMap != nil {
		h.wsCtx.LastReadMap[channelID] = ts
	}
	if h.isActive != nil && !h.isActive() {
		// Inactive workspace: nothing to draw, but the persistence
		// above already updated state for when the user switches in.
		return
	}
	if h.program == nil {
		return
	}
	h.program.Send(ui.ChannelMarkedRemoteMsg{
		ChannelID:   channelID,
		TS:          ts,
		UnreadCount: unreadCount,
	})
}

func (h *rtmEventHandler) OnThreadMarked(channelID, threadTS, ts string, read bool) {
	if h.isActive != nil && !h.isActive() {
		// Inactive workspace: skip (no per-thread persistence in v1).
		return
	}
	if h.program == nil {
		return
	}
	h.program.Send(ui.ThreadMarkedRemoteMsg{
		ChannelID: channelID,
		ThreadTS:  threadTS,
		TS:        ts,
		Read:      read,
	})
}
```

- [ ] **Step 3: Build to verify**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Run all tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat(main): real OnChannelMarked/OnThreadMarked handlers"
```

---

## Task 18: Update README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add the keybinding row**

In the `## Keybindings` table, after the `D` (delete message) row (around line 298):

```markdown
| `D` | Normal (message) | Delete your own message (with confirmation) |
| `U` | Normal (message) | Mark selected message and everything newer as unread |
```

- [ ] **Step 2: Add a Messaging feature bullet**

In the `### Messaging` section (around lines 23-35), add a new bullet between the existing mark-as-read bullet and the next item:

Find:
```markdown
- Mark-as-read synced to Slack on channel entry
```

Replace with:
```markdown
- Mark-as-read synced to Slack on channel entry
- Mark-as-unread (`U`) — rolls the read watermark backward to the selected message; thread replies supported. Inbound `channel_marked` / `thread_marked` events from other Slack clients are reflected live.
```

- [ ] **Step 3: Verify it renders**

Run: `git diff README.md`
Eyeball the output for correctness — specifically that the table row aligns and the bullets nest properly.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs(readme): document mark-unread (U) and channel_marked/thread_marked sync"
```

---

## Final Verification

- [ ] **Run the full test suite**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Build the binary**

Run: `make build` (or `go build -o bin/slk ./cmd/slk` if `make` is unavailable)
Expected: clean build, no warnings.

- [ ] **Manual smoke test (run `bin/slk` against a real workspace)**

1. Open a channel with several recent messages. Press `j`/`k` to select a message that's neither the first nor the last in the loaded buffer. Press `U`.
   - **Expected:** the `── new ──` line appears immediately above the selected message; the sidebar's badge for this channel shows the correct unread count and the channel name bolds; a `Marked unread` toast appears in the status bar for ~2 seconds; in the official Slack web client, the same channel becomes unread within ~1 second.
2. Open a thread side panel. Select a reply (not the parent). Press `U`.
   - **Expected:** the thread pane's `── new ──` line moves above the selected reply; the threads view (`⚑ Threads`) shows this thread in bold/unread state; the same `Marked unread` toast.
3. With slk still running on the same channel, mark that channel as read in the official Slack web client.
   - **Expected:** within ~1 second slk reflects the change — the `── new ──` line clears in the message pane, the sidebar badge clears, no toast.
4. Press `U` on the very oldest message in the loaded buffer of a channel.
   - **Expected:** the entire channel becomes unread (every message is below the `── new ──` line). The web client agrees.
5. (Failure path) Disconnect from the network briefly, then press `U` while disconnected.
   - **Expected:** a `Mark unread failed: …` toast for ~3 seconds; no local state mutation; reconnection restores normal operation.

If any of those checks fail, debug before considering the work done.

---

## Spec Coverage Check

- Channel-pane mark-unread (Section 1, "Channel pane") → Tasks 11, 12, 13, 16
- Thread-pane mark-unread (Section 1, "Thread side panel") → Tasks 3, 11, 12, 13, 16
- Sidebar badge with accurate count → Tasks 2, 13
- 2-second toast feedback → Tasks 6, 15
- No confirmation prompt → Task 12 (no confirm code path)
- Edge case: oldest loaded message → Task 12 test `TestMarkUnreadOfSelected_OldestMessage_BoundaryIsZero`
- Edge case: oldest reply in thread → Task 12 test `TestMarkUnreadOfSelected_ThreadPane_OldestReply_BoundaryIsParentTS`
- Edge case: no selection → Task 12 test `TestMarkUnreadOfSelected_NoSelection_NoOp`
- Failure feedback "Mark unread failed" toast → Tasks 6, 13, 15
- Slack client `MarkChannelUnread` / `MarkThreadUnread` (Section 2.A) → Tasks 4, 5
- DI'd HTTP client for testability (Section 2.A) → Task 4
- `MarkUnreadMsg` / `MessageMarkedUnreadMsg` / `MarkUnreadFunc` / setter (Section 2.B-C) → Task 11
- `markUnreadOfSelected` (Section 2.C) → Task 12
- Keymap `MarkUnread` binding (Section 2.D) → Task 10
- Sidebar `SetUnreadCount` (Section 2.E) → Task 2
- Cache `UpdateLastReadTS` reuse (Section 2.F) → Tasks 1 (test backfill), 16 (call site)
- Status bar toast types (Section 2.G) → Task 6
- Wiring in main.go (Section 2.H) → Task 16
- WS event handlers (Section 2.I) → Tasks 7, 8, 9, 17
- Shared `applyChannelMark` / `applyThreadMark` → Task 13
- Inbound remote messages `ChannelMarkedRemoteMsg` / `ThreadMarkedRemoteMsg` → Task 14
- README docs → Task 18

