# Focus-Gated Read Marking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Advance Slack's server-side read cursor for messages arriving in the channel/thread slk is displaying, but only while the terminal is focused — and fix the thread read-state defects that block doing so correctly.

**Architecture:** Terminal focus is tracked on the Bubble Tea UI goroutine via `tea.View.ReportFocus` plus `tea.FocusMsg`/`tea.BlurMsg`. Arriving messages stage a pending read-cursor advance in a single-slot, newest-wins buffer; a debounced flush issues `conversations.mark` / `subscriptions.thread.mark` through the existing service-closure seams. Local read state is persisted only after Slack accepts the mark.

**Tech Stack:** Go 1.26, `charm.land/bubbletea/v2` v2.0.6, SQLite via `internal/cache`, hand-rolled Slack Web API calls in `internal/slack`.

**Spec:** `docs/superpowers/specs/2026-09-04-focus-gated-read-marking-design.md`

## Global Constraints

- Go module path is `github.com/gammons/slk`. Import internal packages with that prefix.
- Bubble Tea v2: `App.View()` returns `tea.View`, not `string`. Import as `tea "charm.land/bubbletea/v2"`.
- Run tests with `make test` (`go test ./... -race -v`). Lint with `make lint` (`golangci-lint run ./...`).
- Never call `p.Send` from the Bubble Tea `Update` goroutine — the v2 program channel is unbuffered. `p.Send` is only for WS/background goroutines in `cmd/slk`.
- `cache.UpdateChannelReadState(channelID, lastReadTS, hasUnread)` treats `lastReadTS == ""` as "preserve existing". It is the only function permitted to modify channel read state after bootstrap.
- Slack timestamps are decimal strings (`"1700000000.000100"`) that compare correctly with Go's `>` / `>=` string operators. Compare them as strings; never parse to float.
- The repo has a `.worktrees/` directory containing copies of the tree. Never edit anything under `.worktrees/`.
- Commit after each task. Use Conventional Commits (`fix:`, `feat:`, `refactor:`, `docs:`).

## File Structure

| Path | Responsibility | Tasks |
|---|---|---|
| `internal/slack/client.go` | `markChannel`/`markThread` route through `postForm`, parse `ok` | 1 |
| `internal/slack/client_test.go` | failure-response coverage for mark endpoints | 1 |
| `internal/cache/thread_subscriptions.go` | `UpdateThreadLastRead`, `GetThreadLastRead` | 2 |
| `internal/cache/thread_subscriptions_test.go` | cursor accessors preserve `active` | 2 |
| `internal/ui/threadsview/model.go` | `MarkByThreadTSReadAt` cursor-comparison flag update | 3 |
| `internal/slack/events.go` | `thread_marked` stops deriving read from `active` | 4 |
| `cmd/slk/main.go` | handler + service wiring for every task | 4, 5, 6, 7, 9 |
| `internal/ui/msgs.go` | `ThreadMarkedRemoteMsg` reshape, `ThreadMarkedLocalMsg` | 4, 6 |
| `internal/ui/app.go` | `applyThreadMark` split, focus flag, pending-mark slots | 4, 7, 8, 9 |
| `internal/ui/reducer_threads.go` | thread mark arms | 4, 6 |
| `internal/ui/services.go`, `internal/ui/callbacks.go` | `ThreadService` signature changes | 6, 7 |
| `internal/ui/reducer_focus.go` | **new** — owns `tea.FocusMsg`/`tea.BlurMsg`/`markFlushMsg` | 8, 9 |
| `internal/ui/reducer_send.go` | `reduceNewMessage` staging logic | 4, 9 |
| `wiki/Terminal-Compatibility.md` | focus-reporting docs | 10 |

---

### Task 1: Mark endpoints report real failures

**Files:**
- Modify: `internal/slack/client.go:1215-1278` (`markChannel`, `markThread`)
- Test: `internal/slack/client_test.go`

**Interfaces:**
- Consumes: existing `postForm(ctx, method string, form url.Values) ([]byte, error)` at `client.go:1494`, and `truncateForLog([]byte) string` at `client.go:1547`.
- Produces: `MarkChannel`, `MarkThread`, `MarkChannelUnread`, `MarkThreadUnread` now return a non-nil error on HTTP failure, HTTP 429, or a Slack `{"ok":false}` body. Signatures are unchanged.

Background: `postForm` injects `token` into the form body itself, checks HTTP status, and maps 429 to `rateLimitError`. It uses `c.apiHTTPClient()`, which returns `c.httpClient` when set (`client.go:211`) — the field `newTestClient` populates — so existing test wiring and Enterprise Grid `apiBaseURL` discovery both keep working.

- [ ] **Step 1: Write the failing tests**

Append to `internal/slack/client_test.go`:

```go
func TestMarkChannel_NotOK_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	}))
	defer srv.Close()

	err := newTestClient(srv).MarkChannel(context.Background(), "C123", "1700000000.000100")
	if err == nil {
		t.Fatal("MarkChannel: want error for ok:false, got nil")
	}
	if !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("error should name the Slack error code, got %q", err)
	}
}

func TestMarkChannel_HTTP500_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<html>nope</html>`))
	}))
	defer srv.Close()

	if err := newTestClient(srv).MarkChannel(context.Background(), "C123", "1700000000.000100"); err == nil {
		t.Fatal("MarkChannel: want error for HTTP 500, got nil")
	}
}

func TestMarkThread_NotOK_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":false,"error":"thread_not_found"}`))
	}))
	defer srv.Close()

	err := newTestClient(srv).MarkThread(context.Background(), "C1", "P1", "R5")
	if err == nil {
		t.Fatal("MarkThread: want error for ok:false, got nil")
	}
	if !strings.Contains(err.Error(), "thread_not_found") {
		t.Errorf("error should name the Slack error code, got %q", err)
	}
}

func TestMarkThreadUnread_NotOK_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":false,"error":"thread_not_found"}`))
	}))
	defer srv.Close()

	if err := newTestClient(srv).MarkThreadUnread(context.Background(), "C1", "P1", "R5"); err == nil {
		t.Fatal("MarkThreadUnread: want error for ok:false, got nil")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/slack/ -run 'TestMark.*(NotOK|HTTP500)' -v`
Expected: FAIL — all four report `want error ..., got nil`, because the current helpers discard the response body.

- [ ] **Step 3: Rewrite the two helpers**

Replace `internal/slack/client.go:1215-1278` in full with:

```go
// markChannel posts to conversations.mark. Used by both MarkChannel
// (read up to ts) and MarkChannelUnread (roll the watermark backward to
// ts). Routed through postForm so HTTP status, 429, and the Slack
// {"ok":false} envelope are all surfaced as errors — read state must
// never be persisted locally for a mark Slack rejected.
func (c *Client) markChannel(ctx context.Context, channelID, ts string) error {
	raw, err := c.postForm(ctx, "conversations.mark", url.Values{
		"channel": {channelID},
		"ts":      {ts},
	})
	if err != nil {
		return err
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parsing conversations.mark: %w (body=%s)", err, truncateForLog(raw))
	}
	if !resp.OK {
		return fmt.Errorf("conversations.mark: %s (body=%s)", resp.Error, truncateForLog(raw))
	}
	return nil
}

// markThread posts to subscriptions.thread.mark. Used by both MarkThread
// (read=true => "1") and MarkThreadUnread (read=false => "0").
// channelID/threadTS empty is a no-op. ts defaults to threadTS when empty
// (parent has no replies yet). Error handling mirrors markChannel.
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
	raw, err := c.postForm(ctx, "subscriptions.thread.mark", url.Values{
		"channel":   {channelID},
		"thread_ts": {threadTS},
		"ts":        {ts},
		"read":      {readVal},
	})
	if err != nil {
		return err
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parsing subscriptions.thread.mark: %w (body=%s)", err, truncateForLog(raw))
	}
	if !resp.OK {
		return fmt.Errorf("subscriptions.thread.mark: %s (body=%s)", resp.Error, truncateForLog(raw))
	}
	return nil
}
```

Leave `MarkChannel`, `MarkThread`, `MarkChannelUnread`, and `MarkThreadUnread` (`client.go:1280-1314`) untouched.

- [ ] **Step 4: Run the full slack package suite**

Run: `go test ./internal/slack/ -race`
Expected: PASS. The pre-existing `TestMarkChannel_PostsCorrectForm`, `TestMarkChannel_UsesAPIBaseURL`, `TestMarkThread_PostsReadOne`, `TestMarkThread_EmptyArgs_NoOp`, `TestMarkChannelUnread_PostsCorrectForm`, and `TestMarkChannelUnread_EmptyTSSendsZero` must all still pass — `postForm` supplies `token` in the body and sets no `Authorization` header, which is what they assert.

If `go vet` flags unused imports (`net/http`, `strings` may no longer be referenced by these two functions), leave them — they are used elsewhere in the file. Only remove an import if the compiler reports it unused.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "fix(slack): surface HTTP and ok:false failures from mark endpoints"
```

---

### Task 2: Thread read-cursor accessors that never touch `active`

**Files:**
- Modify: `internal/cache/thread_subscriptions.go` (append after `DeleteThreadSubscription`, which ends at line 68)
- Test: `internal/cache/thread_subscriptions_test.go`

**Interfaces:**
- Consumes: the `thread_subscriptions` table (`internal/cache/db.go:164-180`), columns `workspace_id, channel_id, thread_ts, last_read, latest_reply, active, updated_at`.
- Produces:
  - `func (db *DB) UpdateThreadLastRead(workspaceID, channelID, threadTS, lastRead string) error`
  - `func (db *DB) GetThreadLastRead(workspaceID, channelID, threadTS string) (string, error)`

Why this exists: `UpsertThreadSubscription` writes `active` on every call. `thread_marked` carries a read cursor, not a subscription change, so it needs a writer that leaves `active` alone. A tombstoned row (`active=0`) must stay tombstoned.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cache/thread_subscriptions_test.go`:

```go
func TestUpdateThreadLastRead_PreservesTombstone(t *testing.T) {
	db := newTestDB(t)
	// Tombstone the row: active=false.
	if err := db.UpsertThreadSubscription("T1", "C1", "P1", "R1", false); err != nil {
		t.Fatalf("UpsertThreadSubscription: %v", err)
	}

	if err := db.UpdateThreadLastRead("T1", "C1", "P1", "R9"); err != nil {
		t.Fatalf("UpdateThreadLastRead: %v", err)
	}

	active, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("UpdateThreadLastRead must not resurrect a tombstoned row, got %d active", len(active))
	}
	got, err := db.GetThreadLastRead("T1", "C1", "P1")
	if err != nil {
		t.Fatalf("GetThreadLastRead: %v", err)
	}
	if got != "R9" {
		t.Errorf("GetThreadLastRead = %q, want %q", got, "R9")
	}
}

func TestUpdateThreadLastRead_PreservesActive(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertThreadSubscription("T1", "C1", "P1", "R1", true); err != nil {
		t.Fatalf("UpsertThreadSubscription: %v", err)
	}

	if err := db.UpdateThreadLastRead("T1", "C1", "P1", "R9"); err != nil {
		t.Fatalf("UpdateThreadLastRead: %v", err)
	}

	active, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("want 1 active row, got %d", len(active))
	}
	if active[0].LastRead != "R9" {
		t.Errorf("LastRead = %q, want %q", active[0].LastRead, "R9")
	}
}

func TestUpdateThreadLastRead_InsertsActiveRow(t *testing.T) {
	db := newTestDB(t)
	// No prior row: Slack only sends thread_marked for threads the user
	// is subscribed to, so an inserted row starts active.
	if err := db.UpdateThreadLastRead("T1", "C1", "P1", "R5"); err != nil {
		t.Fatalf("UpdateThreadLastRead: %v", err)
	}

	active, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(active) != 1 || active[0].LastRead != "R5" {
		t.Fatalf("want one active row with LastRead=R5, got %+v", active)
	}
}

func TestGetThreadLastRead_MissingRowReturnsEmpty(t *testing.T) {
	db := newTestDB(t)
	got, err := db.GetThreadLastRead("T1", "C1", "P1")
	if err != nil {
		t.Fatalf("GetThreadLastRead on missing row: %v", err)
	}
	if got != "" {
		t.Errorf("GetThreadLastRead = %q, want empty", got)
	}
}

func TestUpdateThreadLastRead_RejectsEmptyKey(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpdateThreadLastRead("", "C1", "P1", "R5"); err == nil {
		t.Error("want error for empty workspaceID, got nil")
	}
	if err := db.UpdateThreadLastRead("T1", "", "P1", "R5"); err == nil {
		t.Error("want error for empty channelID, got nil")
	}
	if err := db.UpdateThreadLastRead("T1", "C1", "", "R5"); err == nil {
		t.Error("want error for empty threadTS, got nil")
	}
}
```

If `newTestDB` is not already available in `internal/cache`'s test package, find the existing helper the other tests in `internal/cache/*_test.go` use to open a temp DB and use that name instead. Do not add a second helper.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cache/ -run 'ThreadLastRead' -v`
Expected: FAIL to compile — `db.UpdateThreadLastRead undefined` and `db.GetThreadLastRead undefined`.

- [ ] **Step 3: Implement both accessors**

Insert into `internal/cache/thread_subscriptions.go` immediately after `DeleteThreadSubscription` (line 68):

```go
// UpdateThreadLastRead advances (or rewinds) a thread's read cursor
// WITHOUT touching `active`. thread_marked carries a read cursor, not a
// subscription change, so it must not be able to tombstone a row or
// resurrect one: `active` is owned solely by thread_subscribed,
// thread_unsubscribed, and the getView reconcile.
//
// A missing row is inserted with active=1 because Slack only pushes
// thread_marked for threads the user is subscribed to.
func (db *DB) UpdateThreadLastRead(workspaceID, channelID, threadTS, lastRead string) error {
	if workspaceID == "" || channelID == "" || threadTS == "" {
		return fmt.Errorf("UpdateThreadLastRead: workspace/channel/thread_ts required")
	}
	const q = `
INSERT INTO thread_subscriptions
    (workspace_id, channel_id, thread_ts, last_read, active, updated_at)
VALUES (?, ?, ?, ?, 1, ?)
ON CONFLICT(workspace_id, channel_id, thread_ts) DO UPDATE SET
    last_read  = excluded.last_read,
    updated_at = excluded.updated_at
`
	if _, err := db.conn.Exec(q, workspaceID, channelID, threadTS, lastRead, time.Now().Unix()); err != nil {
		return fmt.Errorf("updating thread last_read: %w", err)
	}
	return nil
}

// GetThreadLastRead returns a thread's read cursor, or "" when no row
// exists (which the thread panel treats as "no unread boundary").
// A missing row is not an error.
func (db *DB) GetThreadLastRead(workspaceID, channelID, threadTS string) (string, error) {
	const q = `SELECT last_read FROM thread_subscriptions
WHERE workspace_id=? AND channel_id=? AND thread_ts=?`
	var lastRead string
	err := db.conn.QueryRow(q, workspaceID, channelID, threadTS).Scan(&lastRead)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading thread last_read: %w", err)
	}
	return lastRead, nil
}
```

Add `"database/sql"` and `"errors"` to the import block at `internal/cache/thread_subscriptions.go:3-6`, which currently holds only `"fmt"` and `"time"`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cache/ -race`
Expected: PASS, including the pre-existing `db_test.go` column-count assertion (no schema change was made).

- [ ] **Step 5: Commit**

```bash
git add internal/cache/thread_subscriptions.go internal/cache/thread_subscriptions_test.go
git commit -m "feat(cache): add thread read-cursor accessors that preserve active"
```

---

### Task 3: Threads-view unread flag derived from the cursor

**Files:**
- Modify: `internal/ui/threadsview/model.go` (add after `MarkByThreadTSUnread`, which ends at line 470)
- Test: `internal/ui/threadsview/model_test.go`

**Interfaces:**
- Consumes: `cache.ThreadSummary` fields `ChannelID`, `ThreadTS`, `LastReplyTS`, `Unread` (`internal/cache/threads.go:12-24`); the private `m.summaries []cache.ThreadSummary` and `m.dirty()`.
- Produces: `func (m *Model) MarkByThreadTSReadAt(channelID, threadTS, lastRead string) bool` — sets `Unread = LastReplyTS > lastRead` on the matching row. Returns true only when the flag actually changed.

`MarkByThreadTSRead` and `MarkByThreadTSUnread` stay: the explicit user-initiated mark-unread flow still needs to force a row unread (Task 4 keeps it on a separate path).

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/threadsview/model_test.go`:

```go
func TestMarkByThreadTSReadAt_CursorBehindLatestSetsUnread(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: false},
	})

	if !m.MarkByThreadTSReadAt("C1", "P1", "4.000000") {
		t.Fatal("want true when the flag flips to unread")
	}
	if !m.Summaries()[0].Unread {
		t.Error("cursor behind latest reply must render unread")
	}
}

func TestMarkByThreadTSReadAt_CursorAtLatestClearsUnread(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: true},
	})

	if !m.MarkByThreadTSReadAt("C1", "P1", "5.000000") {
		t.Fatal("want true when the flag flips to read")
	}
	if m.Summaries()[0].Unread {
		t.Error("cursor at latest reply must render read")
	}
}

func TestMarkByThreadTSReadAt_NoChangeReturnsFalse(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: true},
	})

	if m.MarkByThreadTSReadAt("C1", "P1", "4.000000") {
		t.Error("want false when the flag is already correct")
	}
}

func TestMarkByThreadTSReadAt_UnknownThreadReturnsFalse(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: true},
	})

	if m.MarkByThreadTSReadAt("C1", "P_MISSING", "9.000000") {
		t.Error("want false for a thread not in the list")
	}
	if m.MarkByThreadTSReadAt("", "P1", "9.000000") {
		t.Error("want false for an empty channelID")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/threadsview/ -run MarkByThreadTSReadAt -v`
Expected: FAIL to compile — `m.MarkByThreadTSReadAt undefined`.

- [ ] **Step 3: Implement the method**

Insert into `internal/ui/threadsview/model.go` after `MarkByThreadTSUnread` (line 470):

```go
// MarkByThreadTSReadAt sets the Unread flag on the summary matching
// (channelID, threadTS) by comparing the thread's read cursor against
// its newest known reply: unread iff LastReplyTS > lastRead. Returns
// true only when the flag actually changed, so callers can skip the
// sidebar badge refresh.
//
// This is the correct handler for an inbound thread_marked echo, which
// carries a cursor. Slack's subscription `active` flag means
// "subscribed", not "unread", and must never be used for this decision.
// Presentation-only: the next cache.ListSubscribedThreads refresh
// recomputes Unread from the same rule against durable state.
func (m *Model) MarkByThreadTSReadAt(channelID, threadTS, lastRead string) bool {
	if channelID == "" || threadTS == "" {
		return false
	}
	for i := range m.summaries {
		if m.summaries[i].ChannelID != channelID || m.summaries[i].ThreadTS != threadTS {
			continue
		}
		want := m.summaries[i].LastReplyTS > lastRead
		if m.summaries[i].Unread == want {
			return false
		}
		m.summaries[i].Unread = want
		m.dirty()
		return true
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/threadsview/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/threadsview/model.go internal/ui/threadsview/model_test.go
git commit -m "feat(threadsview): derive unread from the thread read cursor"
```

---

### Task 4: Stop conflating subscription `active` with unread

This is the fix for the reported bug: posting a thread reply auto-subscribes you, Slack echoes `thread_marked` with `subscription.active=true`, and slk currently reads that as "unread" and re-flags the thread you are looking at.

**Files:**
- Modify: `internal/slack/events.go:37-40` (`EventHandler.OnThreadMarked`), `:246-259` (comment), `:377-387` (dispatch)
- Modify: `cmd/slk/main.go:4409-4440` (`OnThreadMarked`)
- Modify: `internal/ui/msgs.go:525-534` (`ThreadMarkedRemoteMsg`)
- Modify: `internal/ui/app.go:3149-3174` (`applyThreadMark`)
- Modify: `internal/ui/reducer_threads.go:55-57`
- Modify: `internal/ui/reducer_send.go:172`
- Test: `internal/slack/events_test.go:505-537`, `cmd/slk/event_handler_test.go:235-265` and `:320-344`, `internal/ui/app_test.go:2942-2981`

**Interfaces:**
- Consumes: `db.UpdateThreadLastRead` (Task 2), `threadsView.MarkByThreadTSReadAt` (Task 3).
- Produces:
  - `EventHandler.OnThreadMarked(channelID, threadTS, lastRead string)` — the `read bool` parameter is gone.
  - `ui.ThreadMarkedRemoteMsg{ChannelID, ThreadTS, LastRead string}` — `TS` and `Read` are gone.
  - `(*App).applyThreadMark(channelID, threadTS, lastRead string)` — cursor-based, used by the remote echo path.
  - `(*App).applyThreadMarkUnread(channelID, threadTS, boundaryTS string)` — forces unread, used by the user-initiated `U` mark-unread flow.

Why the split: Slack's mark-unread sets the cursor to just *before* the boundary ts. Passing `boundaryTS` into a `LastReplyTS > lastRead` comparison would report the thread read when the boundary is the newest reply. The user-initiated path already knows the intent, so it forces the flag directly.

- [ ] **Step 1: Update the event-dispatch tests**

In `internal/slack/events_test.go`, replace `TestDispatch_ThreadMarked_Unread_CallsHandler` (line 505) and `TestDispatch_ThreadMarked_Read_CallsHandler` (line 522) with:

```go
func TestDispatch_ThreadMarked_PassesCursorThrough(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"thread_marked","subscription":{"channel":"C1","thread_ts":"1700000000.000100","last_read":"1700000000.000200","active":true}}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.threadMarks) != 1 {
		t.Fatalf("expected 1 threadMark, got %d", len(handler.threadMarks))
	}
	got := handler.threadMarks[0]
	if got.channelID != "C1" || got.threadTS != "1700000000.000100" || got.lastRead != "1700000000.000200" {
		t.Errorf("unexpected: %+v", got)
	}
}

// active=false previously meant "read". It means "unsubscribed", which
// thread_marked does not report, so it must not change what we pass on.
func TestDispatch_ThreadMarked_IgnoresActiveFlag(t *testing.T) {
	handler := &mockEventHandler{}
	data := []byte(`{"type":"thread_marked","subscription":{"channel":"C1","thread_ts":"P1","last_read":"R5","active":false}}`)
	dispatchWebSocketEvent(data, handler)

	if len(handler.threadMarks) != 1 {
		t.Fatalf("expected 1 threadMark, got %d", len(handler.threadMarks))
	}
	if handler.threadMarks[0].lastRead != "R5" {
		t.Errorf("lastRead = %q, want R5", handler.threadMarks[0].lastRead)
	}
}
```

Then update `mockEventHandler` in the same file: its `threadMarks` element struct currently has fields `channelID, threadTS, ts string; read bool`. Change it to `channelID, threadTS, lastRead string` and update its `OnThreadMarked` method to the three-argument signature. Leave `TestDispatch_ThreadMarked_MalformedJSON_NoCall` (line 535) unchanged.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/slack/ -run ThreadMarked -v`
Expected: FAIL to compile — the mock no longer matches `EventHandler`, and `got.lastRead` does not exist yet.

- [ ] **Step 3: Change the interface and dispatch**

In `internal/slack/events.go`, change the interface method at line 40 to:

```go
	// OnThreadMarked is delivered when Slack pushes a thread_marked
	// event: the user's read cursor inside a thread moved (in either
	// direction). lastRead is the new cursor. The subscription block's
	// `active` flag is deliberately NOT passed on — it means
	// "subscribed", not "unread"; see OnThreadSubscriptionChanged,
	// which owns that bit.
	OnThreadMarked(channelID, threadTS, lastRead string)
```

Replace the `case "thread_marked":` body (lines 377-387) with:

```go
	case "thread_marked":
		var evt wsThreadMarkedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		debuglog.WS("thread_marked: channel=%s thread_ts=%s last_read=%s",
			evt.Subscription.Channel, evt.Subscription.ThreadTS, evt.Subscription.LastRead)
		handler.OnThreadMarked(evt.Subscription.Channel, evt.Subscription.ThreadTS, evt.Subscription.LastRead)
```

Fix the now-wrong doc comment on `wsThreadMarkedEvent` (lines 246-250):

```go
// wsThreadMarkedEvent represents a thread_marked event from Slack's
// browser-protocol WebSocket. The subscription block carries the
// channel/thread and the new last-read ts. It also carries an `active`
// flag, which means "subscribed for unread updates" — the same meaning
// it has in wsThreadSubscribedEvent — and is NOT a read/unread signal.
```

- [ ] **Step 4: Update the WS handler**

Replace `cmd/slk/main.go:4409-4440` (the whole `OnThreadMarked` function, from its doc comment through its closing brace) with:

```go
// OnThreadMarked persists a thread read-cursor move from Slack's
// thread_marked event. It writes last_read ONLY: `active` is owned by
// thread_subscribed / thread_unsubscribed / the getView reconcile.
// Writing `active` here used to tombstone the row on every read, which
// made the thread vanish from the Threads list until the next sweep.
func (h *rtmEventHandler) OnThreadMarked(channelID, threadTS, lastRead string) {
	// Persist regardless of active-workspace state, matching OnMessage
	// and OnChannelMarked: dropping the write on inactive workspaces
	// leaves stale read state behind on the next switch.
	if h.db != nil {
		if err := h.db.UpdateThreadLastRead(h.workspaceID, channelID, threadTS, lastRead); err != nil {
			debuglog.Cache("OnThreadMarked: UpdateThreadLastRead %s/%s: %v",
				channelID, threadTS, err)
		}
	}

	// UI dispatch is active-only: the threads-view list and sidebar
	// badge live on the active workspace; inactive workspaces pick up
	// fresh state on the next switch via threadsListFetcher.
	if h.isActive != nil && !h.isActive() {
		return
	}
	if h.program == nil {
		return
	}
	h.program.Send(ui.ThreadMarkedRemoteMsg{
		ChannelID: channelID,
		ThreadTS:  threadTS,
		LastRead:  lastRead,
	})
	// Optimistic flag updates above are in-memory only; this schedules
	// the authoritative recompute from cache.ListSubscribedThreads.
	h.program.Send(ui.ThreadsListDirtyMsg{TeamID: h.workspaceID})
}
```

- [ ] **Step 5: Reshape the message and split applyThreadMark**

In `internal/ui/msgs.go`, replace lines 525-534 with:

```go
// ThreadMarkedRemoteMsg is dispatched by the WS event handler when
// Slack pushes a thread_marked event. LastRead is the thread's new read
// cursor; whether that means read or unread is decided by comparing it
// against the thread's newest known reply.
type ThreadMarkedRemoteMsg struct {
	ChannelID string
	ThreadTS  string
	LastRead  string
}
```

In `internal/ui/app.go`, replace `applyThreadMark` (lines 3149-3174) with these two functions:

```go
// applyThreadMark updates local state for an inbound thread read-cursor
// move. Read/unread is derived by comparing the cursor against the
// thread's newest known reply — never from the subscription's `active`
// flag, which means "subscribed".
func (a *App) applyThreadMark(channelID, threadTS, lastRead string) {
	debuglog.Cache("applyThreadMark: channel=%s thread_ts=%s last_read=%s active=%s",
		channelID, threadTS, lastRead, a.activeChannelID)
	if a.threadVisible &&
		a.threadPanel.ChannelID() == channelID &&
		a.threadPanel.ThreadTS() == threadTS {
		// The panel renders its "── new ──" landmark after the boundary
		// ts, so a fully-caught-up cursor renders no landmark.
		a.threadPanel.SetUnreadBoundary(lastRead)
	}
	if a.threadsView.MarkByThreadTSReadAt(channelID, threadTS, lastRead) {
		a.sidebar.SetThreadsUnreadCount(a.threadsView.UnreadCount())
	}
}

// applyThreadMarkUnread forces a thread unread from boundaryTS. Used by
// the user-initiated mark-unread flow, which knows its own intent.
// It does not go through applyThreadMark's cursor comparison: Slack sets
// the cursor to just BEFORE boundaryTS, so comparing boundaryTS against
// the newest reply would wrongly report the thread read whenever the
// boundary is that newest reply.
func (a *App) applyThreadMarkUnread(channelID, threadTS, boundaryTS string) {
	debuglog.Cache("applyThreadMarkUnread: channel=%s thread_ts=%s boundary=%s active=%s",
		channelID, threadTS, boundaryTS, a.activeChannelID)
	if a.threadVisible &&
		a.threadPanel.ChannelID() == channelID &&
		a.threadPanel.ThreadTS() == threadTS {
		a.threadPanel.SetUnreadBoundary(boundaryTS)
	}
	if a.threadsView.MarkByThreadTSUnread(channelID, threadTS) {
		a.sidebar.SetThreadsUnreadCount(a.threadsView.UnreadCount())
	}
}
```

Update the two call sites:

`internal/ui/reducer_threads.go:55-57` becomes:

```go
	case ThreadMarkedRemoteMsg:
		a.applyThreadMark(m.ChannelID, m.ThreadTS, m.LastRead)
		return nil, true
```

`internal/ui/reducer_send.go:172` becomes:

```go
			a.applyThreadMarkUnread(m.ChannelID, m.ThreadTS, m.BoundaryTS)
```

- [ ] **Step 6: Update the handler and UI tests**

In `cmd/slk/event_handler_test.go`, replace `TestOnThreadMarked_UpsertsSubscription` (line 235 through its closing brace at 265) with:

```go
func TestOnThreadMarked_AdvancesCursorWithoutTombstoning(t *testing.T) {
	db := newTestDB(t)
	h := &rtmEventHandler{
		db:          db,
		workspaceID: "T1",
		isActive:    func() bool { return true },
	}

	h.OnThreadMarked("C1", "1700000100.000000", "1700000150.000000")

	got, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 active sub after thread_marked, got %d", len(got))
	}
	if got[0].ChannelID != "C1" || got[0].ThreadTS != "1700000100.000000" ||
		got[0].LastRead != "1700000150.000000" || !got[0].Active {
		t.Fatalf("subscription row mismatch: %+v", got[0])
	}

	// A later cursor move must advance last_read and leave the row
	// active. Writing `active` here used to tombstone the row, making
	// the thread disappear from the Threads list.
	h.OnThreadMarked("C1", "1700000100.000000", "1700000200.000000")
	got, err = db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("thread must stay in the list after being read, got %d active", len(got))
	}
	if got[0].LastRead != "1700000200.000000" {
		t.Errorf("LastRead = %q, want 1700000200.000000", got[0].LastRead)
	}
}
```

In the same file, update `TestOnThreadMarked_PersistsOnInactiveWorkspace` (line 320): change the call at line 331 to `h.OnThreadMarked("C1", "1700000100.000000", "1700000150.000000")` and delete the `// read=false → ...` comment above it. Its assertions already expect one active row with `LastRead == "1700000150.000000"` and stay valid.

In `internal/ui/app_test.go`, replace `TestThreadMarkedRemoteMsg_UnreadFlipsRow` (line 2942) and `TestThreadMarkedRemoteMsg_ReadClearsRow` (line 2966) with:

```go
func TestThreadMarkedRemoteMsg_CursorBehindLatestFlipsRowUnread(t *testing.T) {
	app := NewApp()
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: false},
	})

	_, cmd := app.Update(ThreadMarkedRemoteMsg{
		ChannelID: "C1",
		ThreadTS:  "P1",
		LastRead:  "4.000000",
	})

	if cmd != nil && cmdContainsMsgType(cmd, statusbar.MarkedUnreadMsg{}) {
		t.Error("expected no toast on remote thread event")
	}
	for _, s := range app.threadsView.Summaries() {
		if s.ThreadTS == "P1" && !s.Unread {
			t.Error("expected P1 Unread=true when the cursor is behind the latest reply")
		}
	}
}

func TestThreadMarkedRemoteMsg_CursorAtLatestClearsRow(t *testing.T) {
	app := NewApp()
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: true},
	})

	_, _ = app.Update(ThreadMarkedRemoteMsg{
		ChannelID: "C1", ThreadTS: "P1", LastRead: "5.000000",
	})

	for _, s := range app.threadsView.Summaries() {
		if s.ThreadTS == "P1" && s.Unread {
			t.Error("expected P1 Unread=false when the cursor reaches the latest reply")
		}
	}
}

// Regression for the reported bug: posting a reply auto-subscribes you,
// so Slack echoes thread_marked with active=true. slk used to read that
// as "unread" and re-flag the thread the user was looking at.
func TestThreadMarkedRemoteMsg_SelfReplyDoesNotReFlagUnread(t *testing.T) {
	app := NewApp()
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "7.000000", Unread: false},
	})

	// Slack's echo after our own reply: cursor caught up to our reply.
	_, _ = app.Update(ThreadMarkedRemoteMsg{
		ChannelID: "C1", ThreadTS: "P1", LastRead: "7.000000",
	})

	for _, s := range app.threadsView.Summaries() {
		if s.ThreadTS == "P1" && s.Unread {
			t.Error("replying to a thread must not mark it unread")
		}
	}
}
```

- [ ] **Step 7: Run the full suite**

Run: `make test`
Expected: PASS. If any other file fails to compile because it calls `OnThreadMarked` or `applyThreadMark` with the old arity, update it to the new signature; do not reintroduce the `read` bool.

- [ ] **Step 8: Commit**

```bash
git add internal/slack/events.go internal/slack/events_test.go cmd/slk/main.go cmd/slk/event_handler_test.go internal/ui/msgs.go internal/ui/app.go internal/ui/app_test.go internal/ui/reducer_threads.go internal/ui/reducer_send.go
git commit -m "fix(threads): stop reading subscription active as unread"
```

---

### Task 5: Channel marks honor Slack's answer

**Files:**
- Modify: `cmd/slk/main.go:3098-3121` (`markChannelReadAsync`), `:1453-1480` (the two call sites)
- Test: `cmd/slk/event_handler_marked_test.go` (replace the skipped `TestMarkChannelReadAsync_UpdatesReadState` at line ~129)

**Interfaces:**
- Consumes: `cache.UpdateChannelReadState`, `ui.ChannelMarkedReadMsg`, and `MarkChannel`'s new error contract from Task 1.
- Produces:
  - `type channelMarker interface { MarkChannel(ctx context.Context, channelID, ts string) error }`
  - `func markChannelRead(ctx context.Context, client channelMarker, db *cache.DB, channelID, ts string) error` — synchronous, testable.
  - `func markChannelReadAsync(ctx context.Context, client channelMarker, db *cache.DB, p *tea.Program, channelID, ts string)` — the parameter changed from `wctx *WorkspaceContext` to `client channelMarker`.

The synchronous split exists so the failure path is deterministically testable. Asserting "the DB was *not* written" against a fire-and-forget goroutine can only be done with a sleep, which is a flaky test.

- [ ] **Step 1: Write the failing tests**

Replace the skipped test in `cmd/slk/event_handler_marked_test.go` with:

```go
type fakeChannelMarker struct {
	err   error
	calls []string // "channelID/ts" per call
}

func (f *fakeChannelMarker) MarkChannel(_ context.Context, channelID, ts string) error {
	f.calls = append(f.calls, channelID+"/"+ts)
	return f.err
}

func TestMarkChannelRead_SuccessPersistsReadState(t *testing.T) {
	db := newTestDB(t)
	_ = db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	if err := db.UpdateChannelReadState("C1", "", true); err != nil {
		t.Fatalf("seed has_unread: %v", err)
	}
	marker := &fakeChannelMarker{}

	if err := markChannelRead(context.Background(), marker, db, "C1", "1700000000.000100"); err != nil {
		t.Fatalf("markChannelRead: %v", err)
	}

	if len(marker.calls) != 1 || marker.calls[0] != "C1/1700000000.000100" {
		t.Fatalf("unexpected MarkChannel calls: %v", marker.calls)
	}
	s, _ := db.GetChannelReadState("C1")
	if s.LastReadTS != "1700000000.000100" || s.HasUnread {
		t.Errorf("read state = %+v, want LastReadTS advanced and HasUnread=false", s)
	}
}

func TestMarkChannelRead_FailureLeavesChannelUnread(t *testing.T) {
	db := newTestDB(t)
	_ = db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	if err := db.UpdateChannelReadState("C1", "", true); err != nil {
		t.Fatalf("seed has_unread: %v", err)
	}
	marker := &fakeChannelMarker{err: errors.New("conversations.mark: invalid_auth")}

	if err := markChannelRead(context.Background(), marker, db, "C1", "1700000000.000100"); err == nil {
		t.Fatal("markChannelRead: want error, got nil")
	}

	s, _ := db.GetChannelReadState("C1")
	if s.LastReadTS != "" {
		t.Errorf("LastReadTS = %q, want unchanged after a rejected mark", s.LastReadTS)
	}
	if !s.HasUnread {
		t.Error("HasUnread must stay true so the state can be reconciled later")
	}
}
```

Add `"errors"` to that file's imports if absent. Keep the existing `cache` and `context` imports.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/slk/ -run MarkChannelRead -v`
Expected: FAIL to compile — `markChannelRead undefined`.

- [ ] **Step 3: Implement the seam**

Replace `cmd/slk/main.go:3098-3121` (the `markChannelReadAsync` doc comment through its closing brace) with:

```go
// channelMarker is the single Slack operation the mark-read path needs.
// Narrowing it to an interface (rather than taking *WorkspaceContext and
// reaching through to a concrete *slackclient.Client) is what makes the
// failure path testable without real HTTP wiring.
type channelMarker interface {
	MarkChannel(ctx context.Context, channelID, ts string) error
}

// markChannelRead calls conversations.mark and, ONLY if Slack accepts
// it, persists the local read state. On failure the channel stays
// unread locally, which is the honest state: it reconciles on the next
// channel entry or reconnect sync. Synchronous so the failure path is
// deterministically testable; markChannelReadAsync is the goroutine
// wrapper.
func markChannelRead(ctx context.Context, client channelMarker, db *cache.DB, channelID, ts string) error {
	if err := client.MarkChannel(ctx, channelID, ts); err != nil {
		return err
	}
	if db != nil {
		if err := db.UpdateChannelReadState(channelID, ts, false); err != nil {
			log.Printf("Warning: failed to update read state in markChannelRead %s/%s: %v", channelID, ts, err)
		}
	}
	return nil
}

// markChannelReadAsync fires markChannelRead in a background goroutine
// and returns immediately. client may be nil (returns silently).
func markChannelReadAsync(
	ctx context.Context,
	client channelMarker,
	db *cache.DB,
	p *tea.Program,
	channelID, ts string,
) {
	if client == nil || ts == "" {
		return
	}
	go func() {
		if err := markChannelRead(ctx, client, db, channelID, ts); err != nil {
			log.Printf("Warning: conversations.mark %s/%s failed, leaving channel unread: %v", channelID, ts, err)
			return
		}
		if p != nil {
			p.Send(ui.ChannelMarkedReadMsg{ChannelID: channelID})
		}
	}()
}
```

- [ ] **Step 4: Update both call sites**

`ChannelService.Fetch` (`cmd/slk/main.go`, the `markChannelReadAsync(ctx, wctx, db, p, chIDStr, latestTS)` line around 1467) becomes:

```go
			markChannelReadAsync(ctx, wctx.Client, db, p, chIDStr, latestTS)
```

That closure already returns early when `wctx == nil`, so `wctx.Client` is safe there.

`ChannelService.MarkRead` (around line 1476) currently passes a possibly-nil `wctx` and relies on the old internal nil check. Replace the whole closure with:

```go
			MarkRead: func(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg {
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				markChannelReadAsync(ctx, wctx.Client, db, p, string(channelID), string(ts))
				return nil // ChannelMarkedReadMsg is emitted from inside the goroutine
			},
```

- [ ] **Step 5: Run tests**

Run: `go test ./cmd/slk/ -race`
Expected: PASS, with the previously skipped mark-read test now replaced by two real ones.

- [ ] **Step 6: Commit**

```bash
git add cmd/slk/main.go cmd/slk/event_handler_marked_test.go
git commit -m "fix: only persist channel read state after Slack accepts the mark"
```

---

### Task 6: Thread marks persist the cursor and report their outcome

**Files:**
- Modify: `internal/ui/callbacks.go:78-82` (`ThreadMarkFunc`)
- Modify: `internal/ui/services.go:146-150` (interface), `:223-228` (adapter)
- Modify: `internal/ui/msgs.go` (add `ThreadMarkedLocalMsg`)
- Modify: `cmd/slk/main.go:1741-1755` (`ThreadService.Mark` closure)
- Modify: `internal/ui/reducer_threads.go:109-137` (call site) and add a new arm
- Test: `internal/ui/reducer_threads_test.go`

**Interfaces:**
- Consumes: `db.UpdateThreadLastRead` (Task 2), `applyThreadMark` (Task 4), `MarkThread`'s error contract (Task 1), `WorkspaceContext.TeamID`.
- Produces:
  - `type ThreadMarkFunc func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) tea.Cmd`
  - `ThreadService.Mark(...) tea.Cmd`
  - `ui.ThreadMarkedLocalMsg{ChannelID, ThreadTS, TS string; Err error}`

Today `Mark` is `void` and remote-only, so `thread_subscriptions.last_read` only ever advances via a WS echo. That lag is what lets a stale cursor flip a thread back to unread on the next list refresh.

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/reducer_threads_test.go`:

```go
func TestThreadMarkedLocalMsg_SuccessAppliesCursor(t *testing.T) {
	app := NewApp()
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: true},
	})

	_, _ = app.Update(ThreadMarkedLocalMsg{ChannelID: "C1", ThreadTS: "P1", TS: "5.000000"})

	for _, s := range app.threadsView.Summaries() {
		if s.ThreadTS == "P1" && s.Unread {
			t.Error("a successful thread mark must clear the unread flag")
		}
	}
}

func TestThreadMarkedLocalMsg_FailureLeavesFlagAlone(t *testing.T) {
	app := NewApp()
	app.threadsView.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: true},
	})

	_, _ = app.Update(ThreadMarkedLocalMsg{
		ChannelID: "C1", ThreadTS: "P1", TS: "5.000000",
		Err: errors.New("subscriptions.thread.mark: thread_not_found"),
	})

	for _, s := range app.threadsView.Summaries() {
		if s.ThreadTS == "P1" && !s.Unread {
			t.Error("a rejected thread mark must leave the thread unread")
		}
	}
}

func TestThreadRepliesLoaded_ReturnsMarkCmd(t *testing.T) {
	app := NewApp()
	var got []string
	app.SetThreadService(NewThreadService(ThreadServiceFuncs{
		Mark: func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) tea.Cmd {
			got = append(got, string(channelID)+"/"+string(threadTS)+"/"+string(ts))
			return nil
		},
	}))
	app.threadVisible = true
	app.threadPanel.SetThread(messages.MessageItem{TS: "P1"}, nil, "C1", "P1")

	_, _ = app.Update(ThreadRepliesLoadedMsg{
		ThreadTS: "P1",
		Replies:  []messages.MessageItem{{TS: "R1"}, {TS: "R5"}},
	})

	if len(got) != 1 || got[0] != "C1/P1/R5" {
		t.Fatalf("Mark calls = %v, want one call marking up to the newest reply", got)
	}
}
```

Add `"errors"` and any missing imports (`cache`, `ids`, `messages`, `tea`) to that file.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run 'ThreadMarkedLocalMsg|ThreadRepliesLoaded_ReturnsMarkCmd' -v`
Expected: FAIL to compile — `ThreadMarkedLocalMsg` undefined, and the `Mark` closure returns `tea.Cmd` which does not match the current `ThreadMarkFunc`.

- [ ] **Step 3: Change the signature through the layers**

`internal/ui/callbacks.go`, replace the `ThreadMarkFunc` declaration (line 82):

```go
// ThreadMarkFunc is called to mark a thread as read on Slack's servers
// (subscriptions.thread.mark) and, on success, to advance the local
// thread_subscriptions cursor. Returns a tea.Cmd yielding
// ThreadMarkedLocalMsg, or nil when no workspace is active.
type ThreadMarkFunc func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) tea.Cmd
```

`internal/ui/services.go`, replace the interface method (lines 146-150):

```go
	// Mark marks the thread as read on Slack's servers
	// (subscriptions.thread.mark) and, on success, advances the local
	// thread_subscriptions cursor. channelID is the parent channel,
	// threadTS is the parent message ts, ts is the latest reply ts the
	// user has now seen. Returns a tea.Cmd yielding ThreadMarkedLocalMsg.
	Mark(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) tea.Cmd
```

And the adapter (lines 223-228):

```go
func (t threadAdapter) Mark(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) tea.Cmd {
	if t.fns.Mark == nil {
		return nil
	}
	return t.fns.Mark(channelID, threadTS, ts)
}
```

`internal/ui/msgs.go`, add near `ThreadMarkedRemoteMsg`:

```go
// ThreadMarkedLocalMsg reports the outcome of an slk-initiated
// subscriptions.thread.mark. Err is nil on success, in which case
// thread_subscriptions.last_read has already been advanced to TS.
type ThreadMarkedLocalMsg struct {
	ChannelID string
	ThreadTS  string
	TS        string
	Err       error
}
```

- [ ] **Step 4: Rewire the production closure**

Replace the `Mark:` closure in `cmd/slk/main.go` (lines 1741-1755) with:

```go
			Mark: func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) tea.Cmd {
				chIDStr, threadTSStr, tsStr := string(channelID), string(threadTS), string(ts)
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				client := wctx.Client
				teamID := wctx.TeamID
				return func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					if err := client.MarkThread(ctx, chIDStr, threadTSStr, tsStr); err != nil {
						log.Printf("Warning: MarkThread(%s, %s): %v", chIDStr, threadTSStr, err)
						return ui.ThreadMarkedLocalMsg{
							ChannelID: chIDStr, ThreadTS: threadTSStr, TS: tsStr, Err: err,
						}
					}
					// Persist only after Slack accepts. Without this the
					// cursor advanced solely via the WS echo, and a lost
					// echo left last_read stale enough to flip the thread
					// back to unread on the next list refresh.
					if err := db.UpdateThreadLastRead(teamID, chIDStr, threadTSStr, tsStr); err != nil {
						debuglog.Cache("ThreadService.Mark: UpdateThreadLastRead %s/%s: %v",
							chIDStr, threadTSStr, err)
					}
					return ui.ThreadMarkedLocalMsg{
						ChannelID: chIDStr, ThreadTS: threadTSStr, TS: tsStr,
					}
				}
			},
```

- [ ] **Step 5: Update the reducer**

In `internal/ui/reducer_threads.go`, replace lines 123-136 (from `var cmd tea.Cmd` through the `MarkByThreadTSRead` block) with:

```go
		var cmd tea.Cmd
		if channelID != "" && m.ThreadTS != "" {
			cmd = a.threads.Mark(
				ids.ChannelID(channelID),
				ids.ThreadTS(m.ThreadTS),
				ids.MessageTS(latestTS),
			)
		}
		// Optimistic local clear so the badge updates immediately; the
		// ThreadMarkedLocalMsg above reconciles it against the cursor
		// Slack actually accepted.
		if a.threadsView.MarkByThreadTSReadAt(channelID, m.ThreadTS, latestTS) {
			a.sidebar.SetThreadsUnreadCount(a.threadsView.UnreadCount())
		}
		return cmd, true
```

Add a new arm to the same switch, immediately after the `ThreadMarkedRemoteMsg` arm:

```go
	case ThreadMarkedLocalMsg:
		if m.Err != nil {
			debuglog.Cache("ThreadMarkedLocalMsg: channel=%s thread_ts=%s failed: %v",
				m.ChannelID, m.ThreadTS, m.Err)
			return nil, true
		}
		a.applyThreadMark(m.ChannelID, m.ThreadTS, m.TS)
		return nil, true
```

Add `"github.com/gammons/slk/internal/debuglog"` to that file's imports if it is not already present.

- [ ] **Step 6: Run the full suite**

Run: `make test`
Expected: PASS. Any test that supplied a `Mark` closure with the old void signature must be updated to return `tea.Cmd` (return `nil`).

- [ ] **Step 7: Commit**

```bash
git add internal/ui/callbacks.go internal/ui/services.go internal/ui/msgs.go internal/ui/reducer_threads.go internal/ui/reducer_threads_test.go cmd/slk/main.go
git commit -m "feat(threads): persist the thread read cursor after a successful mark"
```

---

### Task 7: Thread divider uses the thread's cursor, not the channel's

**Files:**
- Modify: `internal/ui/services.go:174-177` (interface), `:183-191` (funcs), `:251-256` (adapter)
- Modify: `cmd/slk/main.go:1831-1843` (`ChannelLastRead` closure)
- Modify: `internal/ui/app.go:1814-1823` (`applyThreadUnreadBoundary`), `:1592`, `:1784`
- Test: `internal/ui/app_test.go`

**Interfaces:**
- Consumes: `db.GetThreadLastRead` (Task 2), `WorkspaceContext.TeamID`.
- Produces: `ThreadService.ThreadLastRead(channelID ids.ChannelID, threadTS ids.ThreadTS) string`, replacing `ChannelLastRead(channelID ids.ChannelID) string`, which is removed. `(*App).applyThreadUnreadBoundary(channelID, threadTS string)` gains the second parameter.

Plain thread replies never advance the channel watermark, so the channel cursor is systematically older than the thread's own. Feeding it to the thread panel puts the `── new ──` divider too early, showing already-read replies as new.

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/app_test.go`:

```go
func TestOpenThreadPanel_BoundaryUsesThreadCursor(t *testing.T) {
	app := NewApp()
	var gotChannel, gotThread string
	app.SetThreadService(NewThreadService(ThreadServiceFuncs{
		ThreadLastRead: func(channelID ids.ChannelID, threadTS ids.ThreadTS) string {
			gotChannel, gotThread = string(channelID), string(threadTS)
			return "R7"
		},
	}))

	_ = app.openThreadPanel(messages.MessageItem{TS: "P1"}, "C1", "P1")

	if gotChannel != "C1" || gotThread != "P1" {
		t.Fatalf("ThreadLastRead called with (%q, %q), want (C1, P1)", gotChannel, gotThread)
	}
	if got := app.threadPanel.UnreadBoundary(); got != "R7" {
		t.Errorf("thread panel boundary = %q, want the thread cursor R7", got)
	}
}
```

If `thread.Model` has no `UnreadBoundary()` accessor, add one next to `SetUnreadBoundary` in `internal/ui/thread/model.go`:

```go
// UnreadBoundary returns the ts after which replies render as new.
func (m *Model) UnreadBoundary() string { return m.unreadBoundaryTS }
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run OpenThreadPanel_BoundaryUsesThreadCursor -v`
Expected: FAIL to compile — `ThreadServiceFuncs` has no field `ThreadLastRead`.

- [ ] **Step 3: Swap the accessor**

`internal/ui/services.go`, replace the `ChannelLastRead` interface method (lines 174-177):

```go
	// ThreadLastRead returns the thread's own last-read cursor from
	// thread_subscriptions so the thread panel can render a "── new ──"
	// boundary. The parent channel's cursor is NOT a substitute: plain
	// thread replies never advance it, so it is systematically stale and
	// puts the divider too early. Optional; "" disables the boundary.
	ThreadLastRead(channelID ids.ChannelID, threadTS ids.ThreadTS) string
```

In `ThreadServiceFuncs` (lines 183-191), replace the `ChannelLastRead` field with:

```go
	ThreadLastRead      func(channelID ids.ChannelID, threadTS ids.ThreadTS) string
```

Replace the adapter method (lines 251-256):

```go
func (t threadAdapter) ThreadLastRead(channelID ids.ChannelID, threadTS ids.ThreadTS) string {
	if t.fns.ThreadLastRead == nil {
		return ""
	}
	return t.fns.ThreadLastRead(channelID, threadTS)
}
```

`cmd/slk/main.go`, replace the `ChannelLastRead:` closure (lines 1831-1843):

```go
			ThreadLastRead: func(channelID ids.ChannelID, threadTS ids.ThreadTS) string {
				wctx := router.Active()
				if wctx == nil {
					return ""
				}
				lastRead, err := db.GetThreadLastRead(wctx.TeamID, string(channelID), string(threadTS))
				if err != nil {
					debuglog.Cache("ThreadLastRead: %s/%s: %v", channelID, threadTS, err)
					return ""
				}
				return lastRead
			},
```

`internal/ui/app.go`, replace `applyThreadUnreadBoundary` (lines 1814-1823):

```go
// applyThreadUnreadBoundary tells the thread panel where the unread
// boundary is for (channelID, threadTS) so it can render a "── new ──"
// landmark. Sourced from the thread's own cursor in
// thread_subscriptions — the parent channel's cursor is stale for
// threads, since plain replies never advance it.
func (a *App) applyThreadUnreadBoundary(channelID, threadTS string) {
	if channelID == "" || threadTS == "" {
		return
	}
	a.threadPanel.SetUnreadBoundary(a.threads.ThreadLastRead(
		ids.ChannelID(channelID), ids.ThreadTS(threadTS)))
}
```

Update the two call sites:

- `internal/ui/app.go:1592` → `a.applyThreadUnreadBoundary(channelID, threadTS)`
- `internal/ui/app.go:1784` → `a.applyThreadUnreadBoundary(sum.ChannelID, sum.ThreadTS)`

At line 1780, replace the now-inaccurate comment ("Snapshot the parent channel's last_read_ts...") with:

```go
	// Snapshot the thread's own last-read cursor BEFORE the local mark-
	// read flips below, so the "── new ──" landmark reflects what the
	// user had actually seen prior to opening this thread.
```

- [ ] **Step 4: Run the full suite**

Run: `make test`
Expected: PASS. The compiler will point at any remaining `ChannelLastRead` reference; remove each one rather than reintroducing the method.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/services.go internal/ui/app.go internal/ui/app_test.go internal/ui/thread/model.go cmd/slk/main.go
git commit -m "fix(threads): draw the unread divider from the thread cursor"
```

---

### Task 8: Track terminal focus

**Files:**
- Create: `internal/ui/reducer_focus.go`
- Modify: `internal/ui/app.go` (add `terminalFocused` field near `threadsDirtyDebounce` at line 206; initialize in `NewApp` near line 517; set `ReportFocus` in `View` at line 2767; add `reduceFocus` to the chain at line 609)
- Test: `internal/ui/reducer_focus_test.go`

**Interfaces:**
- Consumes: `tea.FocusMsg`, `tea.BlurMsg`, `tea.View.ReportFocus` (all from `charm.land/bubbletea/v2` v2.0.6).
- Produces: `App.terminalFocused bool`, true by default. Task 9 reads it.

Terminals report focus *transitions* only — enabling DECSET 1004 elicits no current-state report. Initializing to `true` means a terminal without focus-event support never sends `BlurMsg`, the flag stays `true`, and behavior is identical to today's. Initializing to `false` would make the feature permanently dead there.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/reducer_focus_test.go`:

```go
package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestNewApp_StartsFocused(t *testing.T) {
	// Terminals report focus transitions only, never current state. A
	// terminal without focus-event support sends nothing at all, so
	// defaulting to focused is what preserves today's behavior there.
	if !NewApp().terminalFocused {
		t.Error("terminalFocused must default to true")
	}
}

func TestBlurMsg_ClearsFocus(t *testing.T) {
	app := NewApp()
	_, _ = app.Update(tea.BlurMsg{})
	if app.terminalFocused {
		t.Error("terminalFocused must be false after BlurMsg")
	}
}

func TestFocusMsg_RestoresFocus(t *testing.T) {
	app := NewApp()
	_, _ = app.Update(tea.BlurMsg{})
	_, _ = app.Update(tea.FocusMsg{})
	if !app.terminalFocused {
		t.Error("terminalFocused must be true after FocusMsg")
	}
}

func TestView_EnablesFocusReporting(t *testing.T) {
	app := NewApp()
	app.width, app.height = 80, 24
	if !app.View().ReportFocus {
		t.Error("View must set ReportFocus so the terminal sends focus events")
	}
}
```

`App.View()` is a large function with memoization and sixel handling. If a bare `NewApp()` with only width/height set panics or short-circuits, build the app with whatever helper the existing view tests in `internal/ui/` use (look for `newTestApp`-style helpers near the other `App.View()` assertions) rather than weakening the assertion. `ReportFocus` must be verified on a real `tea.View`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run 'StartsFocused|BlurMsg_ClearsFocus|FocusMsg_RestoresFocus|EnablesFocusReporting' -v`
Expected: FAIL to compile — `app.terminalFocused` undefined.

- [ ] **Step 3: Add the field and the reducer**

In `internal/ui/app.go`, add after the `threadsDirtyDebounce` field (line 206):

```go
	// terminalFocused tracks whether the terminal running slk currently
	// has focus, via tea.FocusMsg / tea.BlurMsg (enabled by
	// View().ReportFocus). Terminals report transitions only — there is
	// no way to query current state — so this starts true: the user just
	// launched slk, and a terminal without focus-event support will never
	// send BlurMsg, leaving the flag true and behavior unchanged.
	//
	// tmux users need `set -g focus-events on` for this to be accurate;
	// see wiki/Terminal-Compatibility.md.
	terminalFocused bool
```

In `NewApp`, add to the struct literal next to `threadsDirtyDebounce` (line 517):

```go
		terminalFocused:       true,
```

In `View`, add after `v.MouseMode = tea.MouseModeCellMotion` (line 2769):

```go
	v.ReportFocus = true
```

Create `internal/ui/reducer_focus.go`:

```go
// internal/ui/reducer_focus.go
//
// Terminal-focus reducer for App.Update.
//
// Owns:
//
//	tea.FocusMsg - the terminal running slk gained focus.
//	tea.BlurMsg  - the terminal running slk lost focus.
//
// Focus gates automatic read-marking: a channel being *selected* does
// not mean the user can see it. slk may be sitting in a background
// terminal, an inactive tmux window, or an unfocused tmux pane with the
// same channel still selected. Marking those messages read would
// diverge from Slack and suppress mobile notifications for messages the
// user never saw.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

var reduceFocus reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch msg.(type) {
	case tea.FocusMsg:
		a.terminalFocused = true
		return nil, true

	case tea.BlurMsg:
		a.terminalFocused = false
		return nil, true
	}
	return nil, false
}
```

Add `reduceFocus,` to the `dispatchReducers` chain in `App.Update` (`internal/ui/app.go:609-626`), immediately after `reduceThreads,`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -race`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/reducer_focus.go internal/ui/reducer_focus_test.go internal/ui/app.go
git commit -m "feat(ui): track terminal focus via ReportFocus"
```

---

### Task 9: Focus-gated auto-marking

The headline feature. Arriving messages stage a pending cursor advance; a debounced flush issues the marks, but only while focused. A blurred arrival stays pending and flushes on the next `FocusMsg`.

**Files:**
- Modify: `internal/ui/app.go` (four fields + three methods)
- Modify: `internal/ui/reducer_focus.go` (flush on focus, `markFlushMsg` arm)
- Modify: `internal/ui/reducer_send.go:289-333` (`reduceNewMessage` tail)
- Modify: `cmd/slk/main.go:4025-4048` (drop the active-channel exemption)
- Test: `internal/ui/reducer_focus_test.go`, `cmd/slk/event_handler_test.go:165-181`

**Interfaces:**
- Consumes: `App.terminalFocused` (Task 8), `ChannelService.MarkRead(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg`, `ThreadService.Mark(...) tea.Cmd` (Task 6), `App.scheduleThreadsDirty`, `App.notifyReadStateChanged`.
- Produces:
  - `App.recordChannelMark(channelID, ts string)`, `App.recordThreadMark(channelID, threadTS, ts string)`
  - `App.scheduleMarkFlush() tea.Cmd`, `App.flushPendingMarks() tea.Cmd`
  - `markFlushMsg struct{}`

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/reducer_focus_test.go`:

```go
// markCapture wires fake mark services and records every call.
func markCapture(t *testing.T) (*App, *[]string) {
	t.Helper()
	app := NewApp()
	app.markFlushDebounce = time.Millisecond
	calls := &[]string{}
	app.SetChannelService(NewChannelService(ChannelServiceFuncs{
		MarkRead: func(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg {
			*calls = append(*calls, "ch:"+string(channelID)+"/"+string(ts))
			return nil
		},
	}))
	app.SetThreadService(NewThreadService(ThreadServiceFuncs{
		Mark: func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) tea.Cmd {
			*calls = append(*calls, "th:"+string(channelID)+"/"+string(threadTS)+"/"+string(ts))
			return nil
		},
	}))
	return app, calls
}

// feed runs cmd to completion and pushes every resulting message back
// through app.Update, so a scheduled flush tick actually fires within
// the test. Reuses the existing drainBatch helper in
// internal/ui/app_selection_test.go:29. Depth-bounded so a
// self-rescheduling cmd cannot loop forever.
func feed(t *testing.T, app *App, cmd tea.Cmd, depth int) {
	t.Helper()
	if cmd == nil || depth > 5 {
		return
	}
	for _, msg := range drainBatch(cmd) {
		if msg == nil {
			continue
		}
		_, next := app.Update(msg)
		feed(t, app, next, depth+1)
	}
}

func TestFocusedArrival_MarksActiveChannelRead(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", UserID: "U2"},
	})
	feed(t, app, cmd, 0)

	if len(*calls) != 1 || (*calls)[0] != "ch:C1/5.000000" {
		t.Fatalf("calls = %v, want one channel mark at 5.000000", *calls)
	}
}

func TestBlurredArrival_DoesNotMarkUntilFocusReturns(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"
	_, _ = app.Update(tea.BlurMsg{})

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", UserID: "U2"},
	})
	feed(t, app, cmd, 0)

	if len(*calls) != 0 {
		t.Fatalf("blurred arrival must not mark read, got %v", *calls)
	}

	_, cmd = app.Update(tea.FocusMsg{})
	feed(t, app, cmd, 0)

	if len(*calls) != 1 || (*calls)[0] != "ch:C1/5.000000" {
		t.Fatalf("calls after refocus = %v, want the staged mark to flush", *calls)
	}
}

func TestBurstCoalescesToNewestTS(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"

	var last tea.Cmd
	for _, ts := range []string{"1.000000", "2.000000", "3.000000"} {
		_, last = app.Update(NewMessageMsg{
			ChannelID: "C1",
			Message:   messages.MessageItem{TS: ts, UserID: "U2"},
		})
	}
	feed(t, app, last, 0)

	if len(*calls) != 1 || (*calls)[0] != "ch:C1/3.000000" {
		t.Fatalf("calls = %v, want a single mark at the newest ts", *calls)
	}
}

func TestPlainThreadReplyDoesNotMarkChannel(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", ThreadTS: "1.000000", UserID: "U2"},
	})
	feed(t, app, cmd, 0)

	for _, c := range *calls {
		if strings.HasPrefix(c, "ch:") {
			t.Fatalf("a plain thread reply must not advance the channel cursor, got %v", *calls)
		}
	}
}

func TestBroadcastReplyMarksChannel(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS: "5.000000", ThreadTS: "1.000000", Subtype: "thread_broadcast", UserID: "U2",
		},
	})
	feed(t, app, cmd, 0)

	var found bool
	for _, c := range *calls {
		if c == "ch:C1/5.000000" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a broadcast reply must advance the channel cursor, got %v", *calls)
	}
}

func TestFocusedReplyInOpenThreadMarksThread(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"
	app.threadVisible = true
	app.threadPanel.SetThread(messages.MessageItem{TS: "1.000000"}, nil, "C1", "1.000000")

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", ThreadTS: "1.000000", UserID: "U2"},
	})
	feed(t, app, cmd, 0)

	if len(*calls) != 1 || (*calls)[0] != "th:C1/1.000000/5.000000" {
		t.Fatalf("calls = %v, want one thread mark", *calls)
	}
}

func TestReplyInClosedThreadDoesNotMark(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"
	app.threadVisible = false

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", ThreadTS: "1.000000", UserID: "U2"},
	})
	feed(t, app, cmd, 0)

	if len(*calls) != 0 {
		t.Fatalf("a reply in a thread the user is not viewing must not mark, got %v", *calls)
	}
}

func TestInactiveChannelArrivalDoesNotMark(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C_OTHER"

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", UserID: "U2"},
	})
	feed(t, app, cmd, 0)

	if len(*calls) != 0 {
		t.Fatalf("a message in a non-active channel must not mark, got %v", *calls)
	}
}

func TestFocusedArrivalLeavesDividerInPlace(t *testing.T) {
	app, _ := markCapture(t)
	app.activeChannelID = "C1"
	app.messagepane.SetLastReadTS("1.000000")

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", UserID: "U2"},
	})
	feed(t, app, cmd, 0)

	if got := app.messagepane.LastReadTS(); got != "1.000000" {
		t.Errorf("messagepane LastReadTS = %q, want the divider left at 1.000000", got)
	}
}
```

Add `"strings"`, `"time"`, and the `ids` / `messages` imports to the file.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run 'Focused|Blurred|Burst|ThreadReply|Broadcast|InactiveChannelArrival' -v`
Expected: FAIL to compile — `app.markFlushDebounce` undefined.

- [ ] **Step 3: Add the pending-mark machinery**

This step alone will not compile: `scheduleMarkFlush` references `markFlushMsg`, which Step 4 declares. Do Steps 3 and 4 back to back before building.

In `internal/ui/app.go`, add next to `terminalFocused` (Task 8):

```go
	// pendingChannelMark / pendingThreadMark stage a read-cursor advance
	// for a message that arrived in what the user is currently looking
	// at. Single-slot and newest-wins: a burst coalesces into one
	// request, and a slot can never issue a ts older than one it already
	// holds. Staged while blurred, flushed on the next FocusMsg.
	pendingChannelMark pendingChannelMarkState
	pendingThreadMark  pendingThreadMarkState
	// markFlushScheduled guards against stacking flush ticks; see
	// scheduleMarkFlush.
	markFlushScheduled bool
	// markFlushDebounce bounds a burst to one network write per
	// interval per target. Longer than threadsDirtyDebounce (150ms)
	// because that one only re-queries local SQLite.
	markFlushDebounce time.Duration
```

Add the two state types near the field block:

```go
type pendingChannelMarkState struct {
	channelID string
	ts        string
}

type pendingThreadMarkState struct {
	channelID string
	threadTS  string
	ts        string
}
```

In `NewApp`, next to `terminalFocused: true`:

```go
		markFlushDebounce:     time.Second,
```

Add these three methods next to `scheduleThreadsDirty` in `internal/ui/app.go`:

```go
// recordChannelMark stages a channel read-cursor advance. Newest-wins:
// an older ts for the same channel is ignored, so out-of-order arrivals
// can never roll the cursor backward.
func (a *App) recordChannelMark(channelID, ts string) {
	if channelID == "" || ts == "" {
		return
	}
	if a.pendingChannelMark.channelID == channelID && a.pendingChannelMark.ts >= ts {
		return
	}
	a.pendingChannelMark = pendingChannelMarkState{channelID: channelID, ts: ts}
}

// recordThreadMark stages a thread read-cursor advance. Newest-wins for
// the same (channel, thread); a different thread replaces the slot.
func (a *App) recordThreadMark(channelID, threadTS, ts string) {
	if channelID == "" || threadTS == "" || ts == "" {
		return
	}
	if a.pendingThreadMark.channelID == channelID &&
		a.pendingThreadMark.threadTS == threadTS &&
		a.pendingThreadMark.ts >= ts {
		return
	}
	a.pendingThreadMark = pendingThreadMarkState{channelID: channelID, threadTS: threadTS, ts: ts}
}

// scheduleMarkFlush returns a tick that flushes the pending marks after
// the debounce interval, or nil when nothing is pending, a tick is
// already in flight, or the terminal is blurred. Blurred slots stay
// staged and flush on the next FocusMsg instead.
func (a *App) scheduleMarkFlush() tea.Cmd {
	if a.markFlushScheduled || !a.terminalFocused {
		return nil
	}
	if a.pendingChannelMark.ts == "" && a.pendingThreadMark.ts == "" {
		return nil
	}
	a.markFlushScheduled = true
	d := a.markFlushDebounce
	if d == 0 {
		d = time.Second
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return markFlushMsg{} })
}

// flushPendingMarks clears both slots and returns the commands that
// issue their marks. Clearing before issuing is deliberate: a mark
// Slack rejects is not retried here, it is reconciled by the next
// arrival, the next channel entry, or reconnect sync. Retrying a
// rejected mark risks hammering a rate-limited endpoint.
//
// Note this does NOT move any on-screen "── new ──" divider. MarkRead
// yields ChannelMarkedReadMsg, whose reducer arm only invalidates the
// sidebar; the divider recomputes on the next channel entry.
func (a *App) flushPendingMarks() tea.Cmd {
	var cmds []tea.Cmd
	if pc := a.pendingChannelMark; pc.ts != "" {
		a.pendingChannelMark = pendingChannelMarkState{}
		channels := a.channels
		chID := ids.ChannelID(pc.channelID)
		ts := ids.MessageTS(pc.ts)
		cmds = append(cmds, func() tea.Msg { return channels.MarkRead(chID, ts) })
	}
	if pt := a.pendingThreadMark; pt.ts != "" {
		a.pendingThreadMark = pendingThreadMarkState{}
		if c := a.threads.Mark(
			ids.ChannelID(pt.channelID),
			ids.ThreadTS(pt.threadTS),
			ids.MessageTS(pt.ts),
		); c != nil {
			cmds = append(cmds, c)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}
```

- [ ] **Step 4: Wire the flush into the focus reducer**

In `internal/ui/reducer_focus.go`, add the message type below the imports:

```go
// markFlushMsg fires after the mark debounce interval; see
// App.scheduleMarkFlush.
type markFlushMsg struct{}
```

Replace the reducer body with:

```go
var reduceFocus reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch msg.(type) {
	case tea.FocusMsg:
		a.terminalFocused = true
		// Catch-up: marks staged while blurred go out now. Without
		// this, a user who alt-tabs back, reads everything, and never
		// switches channels leaves the channel unread on Slack.
		return a.flushPendingMarks(), true

	case tea.BlurMsg:
		a.terminalFocused = false
		return nil, true

	case markFlushMsg:
		a.markFlushScheduled = false
		if !a.terminalFocused {
			// Blurred between scheduling and firing: keep the slots
			// staged rather than dropping them.
			return nil, true
		}
		return a.flushPendingMarks(), true
	}
	return nil, false
}
```

- [ ] **Step 5: Rewrite the reduceNewMessage tail**

In `internal/ui/reducer_send.go`, replace everything from line 289 (`if m.ChannelID == a.activeChannelID {`) through line 333 (`return nil`) with:

```go
	isThreadReply := m.Message.ThreadTS != "" && m.Message.ThreadTS != m.Message.TS
	isBroadcast := m.Message.Subtype == "thread_broadcast"

	// Thread-eligible: a reply landing in the thread panel the user has
	// open. The gate mirrors the channel rule — panel open on this
	// thread, not "the thread pane holds slk-internal focus" — and the
	// reply is already rendered there by the AddReply above.
	if isThreadReply && a.terminalFocused &&
		a.threadVisible &&
		m.ChannelID == a.threadPanel.ChannelID() &&
		m.Message.ThreadTS == a.threadPanel.ThreadTS() {
		a.recordThreadMark(m.ChannelID, m.Message.ThreadTS, m.Message.TS)
	}

	// Channel-eligible: top-level messages and thread_broadcasts. Plain
	// thread replies never advance the parent channel cursor on Slack.
	if !isThreadReply || isBroadcast {
		switch {
		case m.ChannelID != a.activeChannelID:
			// Not the channel on screen. The has_unread=true DB write
			// already happened in the WS handler; force the sidebar and
			// workspace rail to re-read it.
			debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=mark_unread",
				m.ChannelID, m.Message.TS)
			a.notifyReadStateChanged()
		case a.terminalFocused:
			// On screen AND the user can actually see it: stage a read-
			// cursor advance. No notifyReadStateChanged, so this event
			// triggers no sidebar repaint and the dot never appears.
			debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=active_focused_mark_read",
				m.ChannelID, m.Message.TS)
			a.recordChannelMark(m.ChannelID, m.Message.TS)
		default:
			// Selected, but slk is in a background terminal / inactive
			// tmux window or pane. The user has NOT seen this. Leave it
			// unread locally and on Slack; the staged nothing here means
			// it reconciles on channel entry.
			debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=active_blurred_stay_unread",
				m.ChannelID, m.Message.TS)
			a.notifyReadStateChanged()
		}
	} else {
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=skipped_thread_reply",
			m.ChannelID, m.Message.TS)
	}

	var cmds []tea.Cmd
	if c := a.scheduleMarkFlush(); c != nil {
		cmds = append(cmds, c)
	}
	// A thread reply (regardless of channel) may have changed the
	// involved-threads list -- schedule a debounced re-query so a
	// burst of replies coalesces into a single fetch.
	if m.Message.ThreadTS != "" {
		if c := a.scheduleThreadsDirty(); c != nil {
			cmds = append(cmds, c)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}
```

Note this changes the tail from returning a single cmd to returning a batch — the previous code returned early on `scheduleThreadsDirty`, which would have dropped the flush tick.

Blurred arrivals stage nothing for the channel path: the message is genuinely unread, so there is no cursor to advance. The `FocusMsg` catch-up covers threads staged before a blur; a channel the user returns to is reconciled by its entry mark.

- [ ] **Step 6: Drop the active-channel exemption in the WS handler**

In `cmd/slk/main.go`, replace lines 4025-4048 (the read-state comment block through the closing brace of the `if h.db != nil && ...` statement) with:

```go
	// Read-state: mark the channel has_unread=true for every eligible
	// message. Mirrors Slack's channel-unread semantics — non-broadcast
	// thread replies do not mark the parent channel unread (only
	// top-level messages and thread_broadcast subtypes do).
	//
	// There is deliberately NO active-channel exemption here. Whether
	// the user can actually see the active channel depends on terminal
	// focus, which is only known on the UI goroutine. reduceNewMessage
	// owns that decision and clears this flag by marking read when the
	// terminal is focused; if the mark fails, or the terminal is
	// blurred, the flag correctly stands.
	//
	// This write runs for BOTH active and inactive workspaces; the
	// active/inactive split below only governs the UI dispatch path,
	// not durable read state.
	isThreadReply := threadTS != "" && threadTS != ts
	isBroadcast := subtype == "thread_broadcast"
	shouldMarkChannel := !isThreadReply || isBroadcast
	if h.db != nil && shouldMarkChannel {
		if err := h.db.UpdateChannelReadState(channelID, "", true); err != nil {
			log.Printf("Warning: failed to set has_unread for %s: %v", channelID, err)
		}
	}
```

`h.activeChannelID` is still used by the notification block above, so leave the struct field in place — only the local `activeChIDForRead` variable goes.

Replace `TestOnMessage_ActiveChannel_DoesNotSetHasUnread` in `cmd/slk/event_handler_test.go` (lines 165-181) with:

```go
// The active channel is no longer exempt: whether the user can see it
// depends on terminal focus, which only the UI goroutine knows. The WS
// handler always flags, and reduceNewMessage clears it by marking read
// when focused.
func TestOnMessage_ActiveChannel_StillSetsHasUnread(t *testing.T) {
	db := newTestDB(t)
	_ = db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	h := &rtmEventHandler{
		db:              db,
		wsCtx:           &WorkspaceContext{},
		isActive:        func() bool { return true },
		activeChannelID: func() string { return "C1" },
	}
	h.OnMessage("C1", "U1", "1.001", "hi", "", "", false, nil, slack.Blocks{}, nil, "", "")

	s, _ := db.GetChannelReadState("C1")
	if !s.HasUnread {
		t.Error("HasUnread = false, want true: the UI layer owns the focus-gated clear")
	}
}
```

- [ ] **Step 7: Run the full suite**

Run: `make test`
Expected: PASS. If a pre-existing test asserted that an active-channel arrival leaves the sidebar untouched, update it: a *blurred* arrival now calls `notifyReadStateChanged`, a focused one still does not.

- [ ] **Step 8: Lint and commit**

```bash
make lint
git add internal/ui/app.go internal/ui/reducer_focus.go internal/ui/reducer_focus_test.go internal/ui/reducer_send.go cmd/slk/main.go cmd/slk/event_handler_test.go
git commit -m "feat: mark messages read on Slack only while the terminal is focused (#159)"
```

---

### Task 10: Document focus reporting

**Files:**
- Modify: `wiki/Terminal-Compatibility.md` (insert after the "Unread indicator in the window title" section, which ends at line 89)

**Interfaces:**
- Consumes: nothing. Documentation only.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Add the section**

Insert between the "Unread indicator in the window title" section and "Overriding the image protocol":

```markdown
## Focus reporting and read state

slk marks a message read on Slack's servers when it arrives in the
channel you're viewing — but only while the terminal running slk is
focused. Having a channel selected doesn't mean you can see it: slk may
be sitting in a background terminal, an inactive tmux window, or an
unfocused tmux pane. Marking those messages read would diverge from
Slack's own read state and suppress the mobile notification for a
message you never saw.

Focus is detected with DECSET 1004 focus reporting, which every modern
terminal supports (kitty, Ghostty, WezTerm, foot, iTerm2, Alacritty,
gnome-terminal, Windows Terminal).

Inside tmux there's an extra step. tmux swallows focus events unless you
turn them on explicitly. Add this to `~/.tmux.conf`:

    set -g focus-events on

Without it, tmux never tells slk that its pane lost focus, so slk
behaves as though the terminal is always focused and marks messages read
even when the pane is in the background.

Terminals report focus *transitions*, never their current state, so slk
assumes it starts focused — you did just launch it. On a terminal with
no focus reporting at all, slk stays in that assumed-focused state and
marks read exactly as it did before this feature existed.
```

- [ ] **Step 2: Verify the surrounding structure still reads correctly**

Run: `grep -n '^## ' wiki/Terminal-Compatibility.md`
Expected: the new `## Focus reporting and read state` heading appears between `## Unread indicator in the window title` and `## Overriding the image protocol`.

- [ ] **Step 3: Commit**

```bash
git add wiki/Terminal-Compatibility.md
git commit -m "docs: document focus reporting and tmux focus-events"
```

---

## Verification

After Task 10, confirm the whole feature end to end:

- [ ] `make test` passes with `-race`.
- [ ] `make lint` is clean.
- [ ] `make build` succeeds.
- [ ] Manual: run slk in tmux with `set -g focus-events on`. Post to the open channel from another client while the pane is focused — the channel should show read in the official Slack client within ~1s. Repeat with the tmux pane unfocused — the message must stay unread in both slk and Slack until you focus the pane again.
- [ ] Manual: reply to a thread from inside slk. The thread must not flip to unread in the Threads list, and it must not disappear from that list.

