# Threads View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Threads" view (top-of-sidebar entry) that lists threads the user is involved in for the active workspace, computed from the local SQLite cache, with live updates as new replies arrive.

**Architecture:** New `internal/cache/threads.go` exposes one query, `ListInvolvedThreads`. New `internal/ui/threadsview/` package implements the list UI, mirroring the existing `sidebar` package. The `sidebar` model gains a synthetic top item ("Threads") with an unread badge. The App grows a `view` field (`ViewChannels` | `ViewThreads`) that swaps the message pane for the threads-view model. WebSocket messages with `thread_ts != ""` enqueue a debounced re-query.

**Tech Stack:** Go, bubbletea, lipgloss, SQLite (modernc.org/sqlite). Reuses existing `internal/ui/messages` rendering helpers and the existing `thread.Model` side panel.

**Spec:** `docs/superpowers/specs/2026-04-28-threads-view-design.md`

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `internal/cache/threads.go` | `ListInvolvedThreads` query + `ThreadSummary` row type |
| `internal/cache/threads_test.go` | Tests for the query and unread heuristic |
| `internal/ui/threadsview/model.go` | UI model: list of `ThreadSummary` rows, j/k nav, render |
| `internal/ui/threadsview/model_test.go` | Tests for selection, version, render |

### Modified Files

| File | Changes |
|------|---------|
| `internal/ui/sidebar/model.go` | Synthetic "Threads" top item; `SetThreadsUnreadCount`, `IsThreadsSelected`, render; selection includes the synthetic item |
| `internal/ui/sidebar/model_test.go` | Tests for the synthetic item |
| `internal/ui/app.go` | `View` enum, new tea.Msg types, threadsview wiring, debouncer, View() switch |
| `internal/ui/app_test.go` | Integration tests for view-switching and live updates |
| `cmd/slk/main.go` | Wire `ThreadsListFetchFunc`, trigger initial + on-workspace-switch loads |

---

## Task 1: Cache query — `ListInvolvedThreads`

**Files:**
- Create: `internal/cache/threads.go`
- Create: `internal/cache/threads_test.go`

- [ ] **Step 1: Write the failing tests for `ListInvolvedThreads`**

Create `internal/cache/threads_test.go`:

```go
package cache

import (
	"sort"
	"testing"
)

// seedThreadFixtures inserts a workspace, a few channels, and several
// thread parents + replies for testing ListInvolvedThreads.
func seedThreadFixtures(t *testing.T, db *DB, selfID string) {
	t.Helper()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true, LastReadTS: "1700000000.000000"})
	db.UpsertChannel(Channel{ID: "C2", WorkspaceID: "T1", Name: "design", Type: "channel", IsMember: true, LastReadTS: "1700000500.000000"})

	// Thread A in C1: self authored parent, others replied. Unread (last reply > last_read, by other).
	db.UpsertMessage(Message{TS: "1700000100.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: selfID, Text: "started by me", ThreadTS: "1700000100.000000"})
	db.UpsertMessage(Message{TS: "1700000200.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U2", Text: "reply by other", ThreadTS: "1700000100.000000"})

	// Thread B in C2: someone else's parent, self replied. Read (last reply by self).
	db.UpsertMessage(Message{TS: "1700000300.000000", ChannelID: "C2", WorkspaceID: "T1", UserID: "U2", Text: "alice parent", ThreadTS: "1700000300.000000"})
	db.UpsertMessage(Message{TS: "1700000400.000000", ChannelID: "C2", WorkspaceID: "T1", UserID: selfID, Text: "my reply", ThreadTS: "1700000300.000000"})

	// Thread C in C1: self mentioned in parent, no reply by self. Unread.
	db.UpsertMessage(Message{TS: "1700000600.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U3", Text: "hey <@" + selfID + "> ping", ThreadTS: "1700000600.000000"})
	db.UpsertMessage(Message{TS: "1700000700.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U3", Text: "follow up", ThreadTS: "1700000600.000000"})

	// Thread D in C2: not involved (no self, no mention). Should be excluded.
	db.UpsertMessage(Message{TS: "1700000800.000000", ChannelID: "C2", WorkspaceID: "T1", UserID: "U4", Text: "unrelated", ThreadTS: "1700000800.000000"})
	db.UpsertMessage(Message{TS: "1700000900.000000", ChannelID: "C2", WorkspaceID: "T1", UserID: "U5", Text: "also unrelated", ThreadTS: "1700000800.000000"})
}

func TestListInvolvedThreads_IncludesAuthoredRepliedMentioned(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	seedThreadFixtures(t, db, "USELF")

	got, err := db.ListInvolvedThreads("T1", "USELF")
	if err != nil {
		t.Fatalf("ListInvolvedThreads: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 involved threads, got %d: %+v", len(got), got)
	}
	threadTSs := []string{}
	for _, s := range got {
		threadTSs = append(threadTSs, s.ThreadTS)
	}
	sort.Strings(threadTSs)
	want := []string{"1700000100.000000", "1700000300.000000", "1700000600.000000"}
	for i := range want {
		if threadTSs[i] != want[i] {
			t.Errorf("threadTSs[%d] = %s, want %s", i, threadTSs[i], want[i])
		}
	}
}

func TestListInvolvedThreads_OrderingUnreadFirst(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	seedThreadFixtures(t, db, "USELF")

	got, err := db.ListInvolvedThreads("T1", "USELF")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 threads, got %d", len(got))
	}
	// Unread first: A (last reply ts 200) and C (last reply ts 700) are unread; B is read.
	// Within unread, newest first: C then A.
	if got[0].ThreadTS != "1700000600.000000" {
		t.Errorf("got[0] = %s, want C (1700000600.000000)", got[0].ThreadTS)
	}
	if !got[0].Unread {
		t.Errorf("got[0] should be unread")
	}
	if got[1].ThreadTS != "1700000100.000000" {
		t.Errorf("got[1] = %s, want A (1700000100.000000)", got[1].ThreadTS)
	}
	if !got[1].Unread {
		t.Errorf("got[1] should be unread")
	}
	if got[2].ThreadTS != "1700000300.000000" {
		t.Errorf("got[2] = %s, want B (1700000300.000000)", got[2].ThreadTS)
	}
	if got[2].Unread {
		t.Errorf("got[2] should be read")
	}
}

func TestListInvolvedThreads_PopulatesParentAndReplyCount(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	seedThreadFixtures(t, db, "USELF")

	got, err := db.ListInvolvedThreads("T1", "USELF")
	if err != nil {
		t.Fatal(err)
	}
	byTS := map[string]ThreadSummary{}
	for _, s := range got {
		byTS[s.ThreadTS] = s
	}

	a := byTS["1700000100.000000"]
	if a.ParentUserID != "USELF" || a.ParentText != "started by me" {
		t.Errorf("thread A parent wrong: %+v", a)
	}
	if a.ReplyCount != 1 {
		t.Errorf("thread A reply count = %d, want 1", a.ReplyCount)
	}
	if a.LastReplyBy != "U2" {
		t.Errorf("thread A last reply by = %s, want U2", a.LastReplyBy)
	}
	if a.ChannelName != "general" || a.ChannelType != "channel" {
		t.Errorf("thread A channel wrong: %+v", a)
	}
}

func TestListInvolvedThreads_MentionRequiresAngleBrackets(t *testing.T) {
	// Plain "USELF" in text without <@…> wrapping must NOT count as a mention.
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})
	db.UpsertMessage(Message{TS: "1.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U2", Text: "the user USELF mentioned in plain text", ThreadTS: "1.000000"})
	db.UpsertMessage(Message{TS: "2.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U2", Text: "more", ThreadTS: "1.000000"})

	got, err := db.ListInvolvedThreads("T1", "USELF")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 threads, got %d", len(got))
	}
}

func TestListInvolvedThreads_ParentMissingFromCache(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})
	// Reply by self exists; parent does not.
	db.UpsertMessage(Message{TS: "2.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "USELF", Text: "my reply", ThreadTS: "1.000000"})

	got, err := db.ListInvolvedThreads("T1", "USELF")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(got))
	}
	if got[0].ParentUserID != "" || got[0].ParentText != "" {
		t.Errorf("missing parent should leave ParentUserID/ParentText empty, got %+v", got[0])
	}
	if got[0].ThreadTS != "1.000000" {
		t.Errorf("ThreadTS = %s, want 1.000000", got[0].ThreadTS)
	}
}

func TestListInvolvedThreads_PerWorkspaceIsolation(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertWorkspace(Workspace{ID: "T2", Name: "Other"})
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})
	db.UpsertChannel(Channel{ID: "C2", WorkspaceID: "T2", Name: "general", Type: "channel", IsMember: true})
	db.UpsertMessage(Message{TS: "1.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "USELF", Text: "T1 thread", ThreadTS: "1.000000"})
	db.UpsertMessage(Message{TS: "2.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U2", Text: "reply", ThreadTS: "1.000000"})
	db.UpsertMessage(Message{TS: "3.000000", ChannelID: "C2", WorkspaceID: "T2", UserID: "USELF", Text: "T2 thread", ThreadTS: "3.000000"})
	db.UpsertMessage(Message{TS: "4.000000", ChannelID: "C2", WorkspaceID: "T2", UserID: "U2", Text: "reply", ThreadTS: "3.000000"})

	got1, _ := db.ListInvolvedThreads("T1", "USELF")
	got2, _ := db.ListInvolvedThreads("T2", "USELF")
	if len(got1) != 1 || got1[0].ThreadTS != "1.000000" {
		t.Errorf("T1 query should return only T1 thread, got %+v", got1)
	}
	if len(got2) != 1 || got2[0].ThreadTS != "3.000000" {
		t.Errorf("T2 query should return only T2 thread, got %+v", got2)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cache/ -run ListInvolvedThreads -v`
Expected: FAIL with "ListInvolvedThreads undefined" / "ThreadSummary undefined".

- [ ] **Step 3: Implement `ListInvolvedThreads` and `ThreadSummary`**

Create `internal/cache/threads.go`:

```go
package cache

import (
	"fmt"
	"sort"
)

// ThreadSummary is one row in the Threads view: a thread the user is
// involved in (authored, replied to, or @-mentioned in). Computed from
// the local cache; v1 has no Slack-side authoritative data.
type ThreadSummary struct {
	ChannelID    string
	ChannelName  string
	ChannelType  string // "channel" | "private" | "dm" | "group_dm"
	ThreadTS     string
	ParentUserID string
	ParentText   string
	ParentTS     string
	ReplyCount   int  // number of replies (does not count the parent)
	LastReplyTS  string
	LastReplyBy  string
	Unread       bool
}

// ListInvolvedThreads returns threads in the given workspace where the user
// (selfUserID) authored the parent, posted a reply, or was @-mentioned
// (`<@UID>`) anywhere in the thread.
//
// Ordering: unread first, then newest LastReplyTS first.
//
// Unread heuristic: LastReplyTS > channel.last_read_ts AND LastReplyBy != self.
// This is approximate; v2 will replace it with subscriptions.thread state.
func (db *DB) ListInvolvedThreads(workspaceID, selfUserID string) ([]ThreadSummary, error) {
	mention := "%<@" + selfUserID + ">%"

	// One pass: pull every message in any thread in this workspace where
	// either: this message is by self, or this message mentions self, or
	// the thread already has another row that matches.  We keep it simple:
	// a CTE selecting distinct (channel_id, thread_ts) pairs that have ANY
	// matching row, then aggregate over all messages in those threads.
	const q = `
WITH involved AS (
  SELECT DISTINCT thread_ts, channel_id
  FROM messages
  WHERE workspace_id = ?
    AND thread_ts != ''
    AND is_deleted = 0
    AND (user_id = ? OR text LIKE ?)
)
SELECT
  m.channel_id,
  m.thread_ts,
  COALESCE(c.name, ''),
  COALESCE(c.type, ''),
  COALESCE(c.last_read_ts, ''),
  COALESCE((SELECT user_id FROM messages
              WHERE channel_id = m.channel_id AND ts = m.thread_ts AND is_deleted = 0), '')
    AS parent_user,
  COALESCE((SELECT text FROM messages
              WHERE channel_id = m.channel_id AND ts = m.thread_ts AND is_deleted = 0), '')
    AS parent_text,
  -- reply count excludes the parent (rows where ts == thread_ts)
  SUM(CASE WHEN m.ts != m.thread_ts THEN 1 ELSE 0 END) AS reply_count,
  MAX(m.ts) AS last_ts,
  (SELECT user_id FROM messages
     WHERE channel_id = m.channel_id AND thread_ts = m.thread_ts AND is_deleted = 0
     ORDER BY ts DESC LIMIT 1) AS last_by
FROM messages m
JOIN involved i ON i.thread_ts = m.thread_ts AND i.channel_id = m.channel_id
LEFT JOIN channels c ON c.id = m.channel_id
WHERE m.is_deleted = 0
GROUP BY m.channel_id, m.thread_ts
`

	rows, err := db.conn.Query(q, workspaceID, selfUserID, mention)
	if err != nil {
		return nil, fmt.Errorf("listing involved threads: %w", err)
	}
	defer rows.Close()

	var out []ThreadSummary
	for rows.Next() {
		var s ThreadSummary
		var lastRead string
		if err := rows.Scan(
			&s.ChannelID,
			&s.ThreadTS,
			&s.ChannelName,
			&s.ChannelType,
			&lastRead,
			&s.ParentUserID,
			&s.ParentText,
			&s.ReplyCount,
			&s.LastReplyTS,
			&s.LastReplyBy,
		); err != nil {
			return nil, fmt.Errorf("scanning thread summary: %w", err)
		}
		s.ParentTS = s.ThreadTS
		s.Unread = s.LastReplyTS > lastRead && s.LastReplyBy != selfUserID
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Order: unread DESC, last_reply_ts DESC.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Unread != out[j].Unread {
			return out[i].Unread // true sorts first
		}
		return out[i].LastReplyTS > out[j].LastReplyTS
	})
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cache/ -run ListInvolvedThreads -v`
Expected: PASS for all 6 tests.

- [ ] **Step 5: Run the full cache package tests to check no regressions**

Run: `go test ./internal/cache/ -v`
Expected: PASS (all existing tests still pass).

- [ ] **Step 6: Commit**

```bash
git add internal/cache/threads.go internal/cache/threads_test.go
git commit -m "feat(cache): add ListInvolvedThreads query for threads view"
```

---

## Task 2: Threadsview UI model

**Files:**
- Create: `internal/ui/threadsview/model.go`
- Create: `internal/ui/threadsview/model_test.go`

The threadsview model holds a `[]cache.ThreadSummary`, a selected index, scroll offset, and renders a vertically-stacked list of cards.

- [ ] **Step 1: Write the failing tests for the model**

Create `internal/ui/threadsview/model_test.go`:

```go
package threadsview

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/cache"
)

func sampleSummaries() []cache.ThreadSummary {
	return []cache.ThreadSummary{
		{
			ChannelID: "C1", ChannelName: "general", ChannelType: "channel",
			ThreadTS: "1.000000", ParentUserID: "U1", ParentText: "hello world",
			ParentTS: "1.000000", ReplyCount: 3, LastReplyTS: "5.000000", LastReplyBy: "U2",
			Unread: true,
		},
		{
			ChannelID: "C2", ChannelName: "design", ChannelType: "channel",
			ThreadTS: "2.000000", ParentUserID: "U2", ParentText: "spec review",
			ParentTS: "2.000000", ReplyCount: 1, LastReplyTS: "4.000000", LastReplyBy: "USELF",
			Unread: false,
		},
	}
}

func TestNew_StartsAtTop(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(sampleSummaries())
	if got := m.SelectedIndex(); got != 0 {
		t.Errorf("SelectedIndex = %d, want 0", got)
	}
}

func TestMoveDown_ClampsAtBottom(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(sampleSummaries())
	m.MoveDown()
	if m.SelectedIndex() != 1 {
		t.Errorf("after MoveDown SelectedIndex = %d, want 1", m.SelectedIndex())
	}
	m.MoveDown()
	if m.SelectedIndex() != 1 {
		t.Errorf("MoveDown past end should clamp; got %d, want 1", m.SelectedIndex())
	}
}

func TestSelected_ReturnsChannelAndThread(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(sampleSummaries())
	m.MoveDown()
	chID, threadTS, ok := m.Selected()
	if !ok || chID != "C2" || threadTS != "2.000000" {
		t.Errorf("Selected = (%q, %q, %v); want (C2, 2.000000, true)", chID, threadTS, ok)
	}
}

func TestSetSummaries_PreservesSelectionByThreadTS(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(sampleSummaries())
	m.MoveDown() // selected: thread 2

	// Re-rank: thread 2 moves to position 0, thread 1 to position 1.
	reranked := []cache.ThreadSummary{sampleSummaries()[1], sampleSummaries()[0]}
	m.SetSummaries(reranked)

	if m.SelectedIndex() != 0 {
		t.Errorf("after re-rank SelectedIndex should follow thread 2 to index 0, got %d", m.SelectedIndex())
	}
	chID, _, _ := m.Selected()
	if chID != "C2" {
		t.Errorf("Selected channel should still be C2, got %s", chID)
	}
}

func TestVersion_BumpsOnMutation(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	v0 := m.Version()
	m.SetSummaries(sampleSummaries())
	v1 := m.Version()
	if v1 == v0 {
		t.Errorf("Version did not bump on SetSummaries (v0=%d v1=%d)", v0, v1)
	}
	m.MoveDown()
	v2 := m.Version()
	if v2 == v1 {
		t.Errorf("Version did not bump on MoveDown")
	}
}

func TestView_RendersChannelAndPreview(t *testing.T) {
	m := New(map[string]string{"U1": "alice", "U2": "bob"}, "USELF")
	m.SetSummaries(sampleSummaries())
	out := m.View(40, 60)
	if !strings.Contains(out, "general") {
		t.Errorf("View output missing channel name 'general':\n%s", out)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("View output missing parent preview 'hello world':\n%s", out)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("View output missing resolved author 'alice':\n%s", out)
	}
}

func TestView_EmptyState(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	out := m.View(40, 60)
	if !strings.Contains(strings.ToLower(out), "no threads") {
		t.Errorf("empty View output should mention 'no threads', got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/ui/threadsview/ -v`
Expected: FAIL with "package not found" / undefined symbols.

- [ ] **Step 3: Implement the model**

Create `internal/ui/threadsview/model.go`:

```go
// Package threadsview renders the "Threads" view: a scrollable list of
// thread parents the user is involved in. It does not own any data — the
// App calls SetSummaries with the result of cache.ListInvolvedThreads.
package threadsview

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

type Model struct {
	summaries []cache.ThreadSummary
	selected  int
	yOffset   int
	focused   bool
	version   int64

	userNames  map[string]string
	selfUserID string
}

func New(userNames map[string]string, selfUserID string) Model {
	return Model{userNames: userNames, selfUserID: selfUserID}
}

func (m *Model) Version() int64 { return m.version }
func (m *Model) dirty()          { m.version++ }

func (m *Model) SetUserNames(u map[string]string) {
	m.userNames = u
	m.dirty()
}

func (m *Model) SetSelfUserID(id string) {
	m.selfUserID = id
	m.dirty()
}

func (m *Model) SetFocused(b bool) {
	if m.focused != b {
		m.focused = b
		m.dirty()
	}
}

func (m *Model) Focused() bool { return m.focused }

// SetSummaries replaces the thread list, attempting to preserve the
// previously-selected thread (by channel_id + thread_ts) at its new index.
func (m *Model) SetSummaries(s []cache.ThreadSummary) {
	prevChannel, prevThread := "", ""
	if m.selected >= 0 && m.selected < len(m.summaries) {
		prevChannel = m.summaries[m.selected].ChannelID
		prevThread = m.summaries[m.selected].ThreadTS
	}

	m.summaries = s
	m.selected = 0
	if prevThread != "" {
		for i, ns := range s {
			if ns.ChannelID == prevChannel && ns.ThreadTS == prevThread {
				m.selected = i
				break
			}
		}
	}
	if len(s) == 0 {
		m.selected = 0
	}
	m.dirty()
}

func (m *Model) Summaries() []cache.ThreadSummary { return m.summaries }

func (m *Model) SelectedIndex() int { return m.selected }

// Selected returns the (channelID, threadTS) of the currently highlighted
// row, or ok=false if the list is empty.
func (m *Model) Selected() (channelID, threadTS string, ok bool) {
	if m.selected < 0 || m.selected >= len(m.summaries) {
		return "", "", false
	}
	s := m.summaries[m.selected]
	return s.ChannelID, s.ThreadTS, true
}

func (m *Model) SelectedSummary() (cache.ThreadSummary, bool) {
	if m.selected < 0 || m.selected >= len(m.summaries) {
		return cache.ThreadSummary{}, false
	}
	return m.summaries[m.selected], true
}

func (m *Model) MoveDown() {
	if m.selected < len(m.summaries)-1 {
		m.selected++
		m.dirty()
	}
}

func (m *Model) MoveUp() {
	if m.selected > 0 {
		m.selected--
		m.dirty()
	}
}

func (m *Model) GoToTop() {
	if m.selected != 0 {
		m.selected = 0
		m.dirty()
	}
}

func (m *Model) GoToBottom() {
	if len(m.summaries) > 0 && m.selected != len(m.summaries)-1 {
		m.selected = len(m.summaries) - 1
		m.dirty()
	}
}

func (m *Model) ScrollUp(n int) {
	if n <= 0 {
		return
	}
	m.yOffset -= n
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	m.dirty()
}

func (m *Model) ScrollDown(n int) {
	if n > 0 {
		m.yOffset += n
		m.dirty()
	}
}

// UnreadCount returns the number of unread thread summaries.
func (m *Model) UnreadCount() int {
	n := 0
	for _, s := range m.summaries {
		if s.Unread {
			n++
		}
	}
	return n
}

// View renders the threads list. height/width are the inner content
// dimensions (caller has already accounted for borders).
func (m *Model) View(width, height int) string {
	if len(m.summaries) == 0 {
		empty := styles.MessageMuted.Render("no threads")
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, empty)
	}

	rows := make([]string, 0, len(m.summaries)*4)
	for i, s := range m.summaries {
		card := m.renderRow(s, width, i == m.selected)
		rows = append(rows, card)
		// Blank separator row between cards
		rows = append(rows, "")
	}
	body := strings.Join(rows, "\n")

	// Vertical scroll: clamp yOffset to body length.
	lines := strings.Split(body, "\n")
	if m.yOffset > len(lines)-height {
		m.yOffset = max0(len(lines) - height)
	}
	end := m.yOffset + height
	if end > len(lines) {
		end = len(lines)
	}
	visible := strings.Join(lines[m.yOffset:end], "\n")

	// Pad to height
	visibleLines := strings.Count(visible, "\n") + 1
	if visibleLines < height {
		visible += strings.Repeat("\n", height-visibleLines)
	}
	return visible
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// renderRow renders one thread parent as a 3-line card:
//
//	#channel · author · time   •
//	  > parent text (one line, truncated)
//	  N replies · last by user Hh ago
func (m *Model) renderRow(s cache.ThreadSummary, width int, selected bool) string {
	authorName := m.resolveName(s.ParentUserID)
	if s.ParentUserID == "" {
		authorName = "(parent not loaded)"
	}
	lastBy := m.resolveName(s.LastReplyBy)

	glyph := channelGlyph(s.ChannelType)

	header := fmt.Sprintf("%s%s · %s · %s", glyph, s.ChannelName, authorName, formatRelTime(s.ParentTS))
	if s.Unread {
		header += "  " + styles.UnreadDot.Render("•")
	}

	previewRaw := s.ParentText
	if previewRaw == "" {
		previewRaw = "(parent not loaded)"
	}
	preview := messages.RenderSlackMarkdown(previewRaw, m.userNames)
	preview = strings.ReplaceAll(preview, "\n", " ")
	previewLine := "  > " + truncate.StringWithTail(preview, uint(max0(width-4)), "…")

	footer := fmt.Sprintf("  %d %s · last by %s %s",
		s.ReplyCount, pluralize("reply", "replies", s.ReplyCount), lastBy, formatRelTime(s.LastReplyTS))

	style := styles.MessageText
	if selected {
		style = styles.MessageSelected
	}
	return style.Render(header) + "\n" + style.Render(previewLine) + "\n" + styles.MessageMuted.Render(footer)
}

func (m *Model) resolveName(userID string) string {
	if userID == "" {
		return ""
	}
	if userID == m.selfUserID {
		return "me"
	}
	if n, ok := m.userNames[userID]; ok && n != "" {
		return n
	}
	return userID
}

func channelGlyph(t string) string {
	switch t {
	case "private":
		return "◆"
	case "dm":
		return "● "
	case "group_dm":
		return "● "
	default:
		return "#"
	}
}

func pluralize(singular, plural string, n int) string {
	if n == 1 {
		return singular
	}
	return plural
}
```

- [ ] **Step 4: Add styles needed by threadsview if missing**

Run: `rg "MessageSelected|UnreadDot|MessageMuted|MessageText" internal/ui/styles/`

Expected: each style name exists. If `UnreadDot` does NOT exist, add it to `internal/ui/styles/styles.go`:

```go
// UnreadDot styles the small dot indicator next to unread items.
UnreadDot = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555"))
```

If any other style is missing, add a minimal definition with a sensible fallback color. Run `go build ./...` to confirm.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/threadsview/ -v`
Expected: PASS for all tests.

- [ ] **Step 6: Build the project to verify integration**

Run: `go build ./...`
Expected: success.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/threadsview/ internal/ui/styles/
git commit -m "feat(ui): add threadsview model for the threads list"
```

> **Note on `formatRelTime`:** if no helper exists in the codebase yet, implement one inline at the bottom of `internal/ui/threadsview/model.go`:
>
> ```go
> func formatRelTime(ts string) string {
>     // ts is a Slack timestamp like "1700000000.000000".
>     dot := strings.Index(ts, ".")
>     if dot < 0 { dot = len(ts) }
>     secStr := ts[:dot]
>     sec, err := strconv.ParseInt(secStr, 10, 64)
>     if err != nil { return ts }
>     d := time.Since(time.Unix(sec, 0))
>     switch {
>     case d < time.Minute:    return "just now"
>     case d < time.Hour:      return fmt.Sprintf("%dm ago", int(d.Minutes()))
>     case d < 24*time.Hour:   return fmt.Sprintf("%dh ago", int(d.Hours()))
>     default:                 return fmt.Sprintf("%dd ago", int(d.Hours()/24))
>     }
> }
> ```
>
> Add `"strconv"` and `"time"` to imports if you used the helper.

---

## Task 3: Sidebar — synthetic "Threads" entry

**Files:**
- Modify: `internal/ui/sidebar/model.go`
- Modify: `internal/ui/sidebar/model_test.go`

The sidebar currently navigates a `[]ChannelItem`. We add a synthetic top item (rendered above all sections) without modifying `ChannelItem` itself. Selection-by-index works as before; the synthetic item has selected index `-1` (a sentinel).

- [ ] **Step 1: Write failing tests for the synthetic Threads entry**

Add to `internal/ui/sidebar/model_test.go`:

```go
func TestThreadsItem_DefaultSelected(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "design", Type: "channel"},
	})
	if !m.IsThreadsSelected() {
		t.Errorf("expected Threads entry to be selected by default (top of list)")
	}
}

func TestThreadsItem_MoveDownLeavesIt(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "design", Type: "channel"},
	})
	m.MoveDown()
	if m.IsThreadsSelected() {
		t.Errorf("MoveDown should leave the Threads entry")
	}
	item, ok := m.SelectedItem()
	if !ok || item.ID != "C1" {
		t.Errorf("first channel should be selected, got %+v ok=%v", item, ok)
	}
}

func TestThreadsItem_MoveUpReturnsToIt(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
	})
	m.MoveDown()
	if m.IsThreadsSelected() {
		t.Errorf("precondition: should be on a channel")
	}
	m.MoveUp()
	if !m.IsThreadsSelected() {
		t.Errorf("MoveUp from first channel should land on Threads entry")
	}
}

func TestThreadsItem_UnreadBadgeRenders(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	m.SetThreadsUnreadCount(3)
	out := m.View(20, 10)
	if !strings.Contains(out, "Threads") {
		t.Errorf("View should contain 'Threads': %q", out)
	}
	if !strings.Contains(out, "3") {
		t.Errorf("View should contain unread count '3': %q", out)
	}
}
```

(Add `"strings"` to imports if not present.)

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/ui/sidebar/ -run Threads -v`
Expected: FAIL with `IsThreadsSelected undefined` etc.

- [ ] **Step 3: Add the synthetic Threads entry to the sidebar model**

Modify `internal/ui/sidebar/model.go`. Add fields to the `Model` struct (right after `filtered []int` on line ~110):

```go
	// threadsUnread is the number of unread threads to badge on the synthetic
	// "Threads" item rendered above all sections. -1 means the synthetic item
	// is the currently-selected row (selection sentinel).
	threadsUnread int
	threadsSelected bool
```

Update the constructor `New` (around line 149) to default-select the Threads row:

```go
func New(items []ChannelItem) Model {
	m := Model{items: items, threadsSelected: true}
	m.rebuildFilter()
	return m
}
```

Add the public methods near the other selection methods (around line 180):

```go
// IsThreadsSelected reports whether the synthetic "Threads" row is the
// current selection in the sidebar.
func (m *Model) IsThreadsSelected() bool { return m.threadsSelected }

// SetThreadsUnreadCount updates the badge on the Threads row.
func (m *Model) SetThreadsUnreadCount(n int) {
	if m.threadsUnread != n {
		m.threadsUnread = n
		m.cacheValid = false
		m.dirty()
	}
}

// ThreadsUnreadCount returns the current unread badge count.
func (m *Model) ThreadsUnreadCount() int { return m.threadsUnread }

// SelectThreadsRow forces selection onto the synthetic Threads row.
func (m *Model) SelectThreadsRow() {
	if !m.threadsSelected {
		m.threadsSelected = true
		m.dirty()
	}
}
```

Modify `MoveUp` (around line 188) to step from the first channel to the Threads row:

```go
func (m *Model) MoveUp() {
	if m.threadsSelected {
		return // already at top
	}
	if m.selected == 0 {
		m.threadsSelected = true
		m.dirty()
		return
	}
	m.selected--
	m.dirty()
}
```

Modify `MoveDown` (around line 181) to step off the Threads row:

```go
func (m *Model) MoveDown() {
	if m.threadsSelected {
		if len(m.filtered) == 0 {
			return
		}
		m.threadsSelected = false
		m.selected = 0
		m.dirty()
		return
	}
	if m.selected < len(m.filtered)-1 {
		m.selected++
		m.dirty()
	}
}
```

Modify `GoToTop`:

```go
func (m *Model) GoToTop() {
	m.threadsSelected = true
	m.selected = 0
	m.dirty()
}
```

Modify `SelectedItem` and `SelectedID` to return `false` / "" when on the Threads row:

```go
func (m *Model) SelectedID() string {
	if m.threadsSelected || len(m.filtered) == 0 {
		return ""
	}
	idx := m.filtered[m.selected]
	return m.items[idx].ID
}

func (m *Model) SelectedItem() (ChannelItem, bool) {
	if m.threadsSelected || len(m.filtered) == 0 {
		return ChannelItem{}, false
	}
	idx := m.filtered[m.selected]
	return m.items[idx], true
}
```

Update `SelectByID` to clear the Threads row when a channel is selected:

```go
func (m *Model) SelectByID(id string) {
	for i, idx := range m.filtered {
		if m.items[idx].ID == id {
			if m.threadsSelected {
				m.threadsSelected = false
				m.dirty()
			}
			if m.selected != i {
				m.selected = i
				m.dirty()
			}
			return
		}
	}
}
```

- [ ] **Step 4: Render the Threads row in `buildCache` / `View`**

Locate the render-cache builder in `internal/ui/sidebar/model.go` (search for `buildCache` or the place that constructs `cacheRows`). At the very top of the visible row sequence, prepend a one-line Threads row:

```go
// Synthetic Threads row at the very top of the sidebar.
threadsLabel := "⚑ Threads"
if m.threadsUnread > 0 {
    threadsLabel += fmt.Sprintf("  •%d", m.threadsUnread)
}
threadsNormal := styles.SidebarChannel.Width(width).Render(threadsLabel)
threadsSelected := styles.SidebarChannelSelected.Width(width).Render(threadsLabel)
m.cacheRows = append(m.cacheRows, renderRow{
    normal:   threadsNormal,
    selected: threadsSelected,
    isThreads: true,
})
// Blank separator
m.cacheRows = append(m.cacheRows, renderRow{normal: m.cacheFiller})
```

Also extend the `renderRow` struct (around line 316) with `isThreads bool` and update the row-emission logic in `View` so that when `m.threadsSelected` is true, the Threads row uses its `selected` variant and all channel rows use `normal`. When `m.threadsSelected` is false, channel rows behave as before.

> If the sidebar render code path doesn't pre-cache rows (the implementation may differ from this sketch), follow the same pattern used for existing items: render once with the same style as channel rows, switch on `m.threadsSelected` for the highlight.

Add `"fmt"` to the import block if not already present.

- [ ] **Step 5: Update `ClickAt` to detect the Threads row**

If `ClickAt` (used for mouse-click selection) currently maps a y-coordinate to an item index, update it to return `(ChannelItem{}, false)` when the click lands on the Threads row, AND also expose a separate signal:

```go
// ThreadsRowClicked reports whether the y coordinate lands on the synthetic
// Threads row in the most recently rendered frame.
func (m *Model) ThreadsRowClicked(y int) bool {
    return y == 0 // The Threads row is always the first rendered row.
}
```

The App will check `ThreadsRowClicked` before trying `ClickAt` for normal channel selection.

- [ ] **Step 6: Run sidebar tests**

Run: `go test ./internal/ui/sidebar/ -v`
Expected: PASS — both new tests and all existing tests.

If existing tests break because they assumed `New(items)` selects the first channel: update those assertions to call `MoveDown()` first or call a new `m.threadsSelected = false` setter. **Do not** silently change semantics — the new default is "Threads row is selected on first render". Adapt test fixtures accordingly.

- [ ] **Step 7: Build to confirm integration**

Run: `go build ./...`
Expected: success. (Some App-level test failures are expected and will be fixed in later tasks; only compile errors here block us.)

If the App fails to compile because it called `sidebar.MoveDown` etc. and now those methods behave differently: do not patch the App in this task — that's Task 4. If pure compile errors arise (e.g. removed methods), revert that and keep the surface compatible.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/sidebar/
git commit -m "feat(sidebar): add synthetic 'Threads' top entry with unread badge"
```

---

## Task 4: App — view-mode wiring + tea.Msg types

**Files:**
- Modify: `internal/ui/app.go`

This task introduces `View`, the new tea.Msg types, the threadsview field, and the rendering switch. We do NOT yet wire live updates or the sidebar→view-activate path; those are Tasks 5 and 6.

- [ ] **Step 1: Add the `View` enum, `threadsview.Model` field, and tea.Msg types**

In `internal/ui/app.go`, near the top (next to `Mode`/`Panel` types and the existing tea.Msg block):

```go
// View identifies which top-level content is shown in the message-pane region.
type View int

const (
	ViewChannels View = iota // default: channel messages
	ViewThreads              // threads-view list of involved threads
)
```

Add to the existing tea.Msg block (around line 60):

```go
	// ThreadsViewActivatedMsg is emitted when the user selects the "Threads"
	// row in the sidebar. App switches view to ViewThreads and triggers an
	// initial load.
	ThreadsViewActivatedMsg struct{}

	// ThreadsListLoadedMsg carries the result of cache.ListInvolvedThreads.
	ThreadsListLoadedMsg struct {
		TeamID    string
		Summaries []cache.ThreadSummary
	}

	// ThreadsListDirtyMsg is an internal kick to re-run the query for the
	// active workspace after a debounce window.
	ThreadsListDirtyMsg struct {
		TeamID string
	}
```

Add the import `"github.com/gammons/slk/internal/cache"` to `internal/ui/app.go` if not already imported, plus the new ui package import:

```go
	"github.com/gammons/slk/internal/ui/threadsview"
```

Add a fetcher type next to the existing fetcher types:

```go
// ThreadsListFetchFunc is invoked to (re-)load the involved-threads list
// for a workspace. The implementation is provided by main.go and runs the
// cache query on a goroutine.
type ThreadsListFetchFunc func(teamID string) tea.Msg
```

Add fields to `App`:

```go
	// Threads view
	threadsView          threadsview.Model
	view                 View
	threadsListFetcher   ThreadsListFetchFunc
	threadsDirtyTimer    *time.Timer
	threadsDirtyDebounce time.Duration
```

In the App constructor (search `func New` or `func NewApp`), initialize:

```go
	a.threadsView = threadsview.New(nil, "")
	a.view = ViewChannels
	a.threadsDirtyDebounce = 150 * time.Millisecond
```

Add a setter near the other setters:

```go
// SetThreadsListFetcher wires the function that loads the involved-threads
// list for the active workspace.
func (a *App) SetThreadsListFetcher(f ThreadsListFetchFunc) {
	a.threadsListFetcher = f
}
```

- [ ] **Step 2: Add tea.Msg handlers in the Update switch**

Find the giant `switch msg := msg.(type)` in `Update`. Add three cases:

```go
	case ThreadsViewActivatedMsg:
		a.view = ViewThreads
		a.focusedPanel = PanelMessages
		// Trigger initial load if we have a fetcher and a workspace.
		if a.threadsListFetcher != nil && a.activeTeamID != "" {
			fetcher := a.threadsListFetcher
			team := a.activeTeamID
			cmds = append(cmds, func() tea.Msg { return fetcher(team) })
		}
		// If a row is already selected and we have summaries (re-entry case),
		// open its thread in the right panel.
		cmds = append(cmds, a.openSelectedThreadCmd())

	case ThreadsListLoadedMsg:
		if msg.TeamID != a.activeTeamID {
			break // stale result from a previous workspace
		}
		a.threadsView.SetSummaries(msg.Summaries)
		a.sidebar.SetThreadsUnreadCount(a.threadsView.UnreadCount())
		if a.view == ViewThreads {
			cmds = append(cmds, a.openSelectedThreadCmd())
		}

	case ThreadsListDirtyMsg:
		if msg.TeamID != a.activeTeamID {
			break
		}
		if a.threadsListFetcher != nil {
			fetcher := a.threadsListFetcher
			team := a.activeTeamID
			cmds = append(cmds, func() tea.Msg { return fetcher(team) })
		}
```

Add the helper at the bottom of the file (or near the other thread helpers):

```go
// openSelectedThreadCmd opens the right thread panel on whichever row is
// currently highlighted in the threads view. No-op if the list is empty.
func (a *App) openSelectedThreadCmd() tea.Cmd {
	sum, ok := a.threadsView.SelectedSummary()
	if !ok {
		return nil
	}
	a.threadVisible = true
	a.statusbar.SetInThread(true)
	// Build a parent MessageItem from the cached summary; if the parent
	// isn't loaded the thread panel will display a placeholder until the
	// fetcher returns.
	parent := messages.MessageItem{
		TS:        sum.ParentTS,
		UserID:    sum.ParentUserID,
		UserName:  resolveOrEmpty(a.userNames(), sum.ParentUserID),
		Text:      sum.ParentText,
		Timestamp: "", // formatter not available here; thread panel re-renders on fetch
		ThreadTS:  sum.ThreadTS,
	}
	a.threadPanel.SetThread(parent, nil, sum.ChannelID, sum.ThreadTS)
	a.threadCompose.SetChannel("thread")
	if a.threadFetcher != nil {
		fetcher := a.threadFetcher
		chID, threadTS := sum.ChannelID, sum.ThreadTS
		return func() tea.Msg { return fetcher(chID, threadTS) }
	}
	return nil
}

// userNames returns the App's current user-name resolution map. If the App
// stores it under a different name, alias here. Returns nil-safe map.
func (a *App) userNames() map[string]string {
	// If App already has a userNames field (most likely it does — check
	// existing code for the field name), return it directly. If not,
	// return nil; resolveOrEmpty handles nil maps.
	return a.currentUserNames
}

func resolveOrEmpty(m map[string]string, id string) string {
	if id == "" {
		return ""
	}
	if n, ok := m[id]; ok {
		return n
	}
	return id
}
```

> **Note:** The App likely already stores user names — search for `userNames` or `UserNames` references and use that field name instead of introducing `currentUserNames`. If the existing field is `a.userNames` (a method already exists), drop the helper and just use the field. Adapt the snippet to the actual field name.

- [ ] **Step 3: Switch threadsview into the message-pane region in `View()`**

Find the `(*App) View()` method (~line 2228) where the message pane is rendered. Locate the block that produces `msgPanelOutput` (the rendered message-pane string with border/exactSize). Wrap it:

```go
// Threads view replaces the message pane content when active.
var paneInner string
var paneVersion int64
if a.view == ViewThreads {
    a.threadsView.SetUserNames(a.userNames())
    a.threadsView.SetSelfUserID(a.currentUserID)
    paneInner = a.threadsView.View(msgWidth, contentHeight)
    paneVersion = a.threadsView.Version()
} else {
    paneInner = a.messagepane.View(msgWidth, contentHeight)
    paneVersion = a.messagepane.Version()
}
```

Replace the existing `msgPanelVersion := a.messagepane.Version()` and the `paneInner := a.messagepane.View(...)` calls with these. Keep the panel-cache wrapping (`panelCacheMsgPanel.hit(...)`) using `paneVersion` so caching still works when threadsview output is unchanged.

Also: when `a.view == ViewThreads`, **do not render the bottom compose box**. Find where `a.compose.View(...)` is rendered (the bottom row) and skip it in threads mode:

```go
if a.view == ViewChannels {
    // existing compose rendering
}
```

- [ ] **Step 4: Plumb `j/k`/`gg`/`G` into the threadsview when in ViewThreads**

In `handleDown`, `handleUp`, `handleGoToBottom`, and the `gg` handler — when `a.focusedPanel == PanelMessages && a.view == ViewThreads`, dispatch to `a.threadsView` instead of `a.messagepane`:

```go
func (a *App) handleDown() {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.MoveDown()
	case PanelMessages:
		if a.view == ViewThreads {
			a.threadsView.MoveDown()
		} else {
			a.messagepane.MoveDown()
		}
	case PanelThread:
		a.threadPanel.MoveDown()
	}
}
```

(Apply analogous changes to `handleUp`, `handleGoToBottom`, the `gg` path, and `scrollFocusedPanel`.) After moving the threadsview cursor, also re-open the right thread panel for the new selection. Wrap each threadsview navigation:

```go
case PanelMessages:
    if a.view == ViewThreads {
        a.threadsView.MoveDown()
        // Auto-open the now-selected thread in the right panel.
        // The cmd is collected by the caller via Update — for handleDown
        // (which currently returns nothing), promote it to return tea.Cmd:
        return a.openSelectedThreadCmd()
    }
    a.messagepane.MoveDown()
```

If `handleDown` currently returns nothing, change its signature to `tea.Cmd` and update its single caller in `Update` to append the result to `cmds`.

- [ ] **Step 5: Wire sidebar Enter on Threads row to emit `ThreadsViewActivatedMsg`**

In `handleEnter` (around line 1725):

```go
func (a *App) handleEnter() tea.Cmd {
	if a.focusedPanel == PanelSidebar {
		if a.sidebar.IsThreadsSelected() {
			return func() tea.Msg { return ThreadsViewActivatedMsg{} }
		}
		item, ok := a.sidebar.SelectedItem()
		if ok {
			return func() tea.Msg {
				return ChannelSelectedMsg{ID: item.ID, Name: item.Name}
			}
		}
	}
	// ... existing rest unchanged
}
```

In `ChannelSelectedMsg` handling (around line 690), reset the view back to channels:

```go
case ChannelSelectedMsg:
    a.view = ViewChannels
    a.CloseThread()
    // ... existing rest unchanged
```

- [ ] **Step 6: Build to verify the wiring compiles**

Run: `go build ./...`
Expected: success.

Fix any compile errors (most likely: a method signature change to return `tea.Cmd` from `handleDown` or a missing import).

- [ ] **Step 7: Add an integration test for view-switching**

Append to `internal/ui/app_test.go`:

```go
func TestApp_ThreadsViewActivation(t *testing.T) {
	app := New() // or whatever constructor
	app.SetCurrentUserID("USELF")
	// Seed: one channel item so the sidebar has something below the Threads row.
	app.handleSidebarItems([]sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})

	// Default: ViewChannels and Threads row selected.
	if app.view != ViewChannels {
		t.Fatalf("default view = %v, want ViewChannels", app.view)
	}

	// Activate threads view via the message.
	cmd := app.Update(ThreadsViewActivatedMsg{})
	_ = cmd
	if app.view != ViewThreads {
		t.Fatalf("after activation view = %v, want ViewThreads", app.view)
	}

	// Switching to a channel returns to ViewChannels.
	app.Update(ChannelSelectedMsg{ID: "C1", Name: "general"})
	if app.view != ViewChannels {
		t.Errorf("after ChannelSelectedMsg view = %v, want ViewChannels", app.view)
	}
}
```

Adapt method names to whatever exists in `internal/ui/app_test.go` already (the existing tests show the pattern — copy from a nearby test).

- [ ] **Step 8: Run app tests**

Run: `go test ./internal/ui/ -run Threads -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "feat(app): add View enum, threadsview wiring, and activation flow"
```

---

## Task 5: Live updates — debounced re-query on thread replies

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`

When a `NewMessageMsg` arrives with `ThreadTS != ""`, schedule a `ThreadsListDirtyMsg` after a 150ms debounce.

- [ ] **Step 1: Write a failing integration test**

Append to `internal/ui/app_test.go`:

```go
func TestApp_NewThreadReplyTriggersDirtyMsg(t *testing.T) {
	app := New()
	app.SetCurrentUserID("USELF")
	app.activeTeamID = "T1"

	loaded := make(chan string, 1)
	app.SetThreadsListFetcher(func(teamID string) tea.Msg {
		loaded <- teamID
		return ThreadsListLoadedMsg{TeamID: teamID, Summaries: nil}
	})

	// Activate threads view first, so initial fetch is consumed.
	app.Update(ThreadsViewActivatedMsg{})
	select {
	case <-loaded:
	case <-time.After(time.Second):
		t.Fatal("initial fetch did not fire")
	}

	// Send a thread reply event.
	app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS:       "2.0",
			UserID:   "U2",
			Text:     "reply",
			ThreadTS: "1.0",
		},
	})

	// The dirty timer should fire within ~debounce + slack.
	select {
	case team := <-loaded:
		if team != "T1" {
			t.Errorf("re-fetch teamID = %q, want T1", team)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected re-fetch after thread reply, did not happen")
	}
}
```

(Add `"time"` to imports of the test file if not present.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run TestApp_NewThreadReplyTriggersDirtyMsg -v`
Expected: FAIL (re-fetch never happens).

- [ ] **Step 3: Implement the debouncer**

Add a method to App:

```go
// scheduleThreadsDirty starts (or restarts) the debounce timer that
// triggers a re-query of the involved-threads list. Safe to call from
// any code path that learns of a thread change.
func (a *App) scheduleThreadsDirty() tea.Cmd {
	if a.activeTeamID == "" {
		return nil
	}
	team := a.activeTeamID
	d := a.threadsDirtyDebounce
	if d == 0 {
		d = 150 * time.Millisecond
	}
	// Use tea.Tick for the debounce so the test harness can drive it.
	return tea.Tick(d, func(time.Time) tea.Msg {
		return ThreadsListDirtyMsg{TeamID: team}
	})
}
```

In the `NewMessageMsg` case (around line 724), after the existing routing logic, add:

```go
case NewMessageMsg:
    // ... existing routing code ...
    if msg.Message.ThreadTS != "" {
        cmds = append(cmds, a.scheduleThreadsDirty())
    }
```

- [ ] **Step 4: Run the test**

Run: `go test ./internal/ui/ -run TestApp_NewThreadReplyTriggersDirtyMsg -v`
Expected: PASS.

> If the existing `ThreadsListDirtyMsg` handler (Task 4) does not gather a `tea.Cmd` — confirm: it appends `func() tea.Msg { return fetcher(team) }` to `cmds`. The test calls `Update` synchronously so it relies on the bubbletea program's command dispatch; if the test harness pattern in this codebase calls cmds inline, mirror that. Look at how `TestApp_…` tests already in `app_test.go` exercise `tea.Tick` — copy the technique.

- [ ] **Step 5: Run all UI tests**

Run: `go test ./internal/ui/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "feat(app): debounced threads-list re-query on new thread replies"
```

---

## Task 6: main.go wiring — fetcher + initial load

**Files:**
- Modify: `cmd/slk/main.go`

The fetcher reads from the active workspace's DB and resolves channel metadata. It runs on a goroutine.

- [ ] **Step 1: Add the threads-list fetcher**

In `cmd/slk/main.go` near the other `app.Set…Fetcher` calls (around line 374), add:

```go
		app.SetThreadsListFetcher(func(teamID string) tea.Msg {
			summaries, err := db.ListInvolvedThreads(teamID, client.UserID())
			if err != nil {
				log.Printf("Warning: ListInvolvedThreads(%s): %v", teamID, err)
				return ui.ThreadsListLoadedMsg{TeamID: teamID, Summaries: nil}
			}
			return ui.ThreadsListLoadedMsg{TeamID: teamID, Summaries: summaries}
		})
```

> **Important:** in this codebase the `db`, `client`, and `userNames` variables differ per workspace. Place the `SetThreadsListFetcher` call inside the same per-workspace setup block as `SetThreadFetcher` (around line 374) so each workspace's fetcher closes over the correct `db` and `client`. If `SetThreadsListFetcher` would conflict with multi-workspace state in App (single fetcher field), make it a closure that switches on `teamID` and dispatches to the right per-workspace `db` — search for how `app.SetWorkspaceSwitcher` handles this and mirror it.

- [ ] **Step 2: Trigger an initial load when the active workspace becomes ready**

In the App's `Update` for `WorkspaceReadyMsg` and `WorkspaceSwitchedMsg` (which are processed in `internal/ui/app.go` around lines 810/857), append a fetch command **after** the existing handling:

```go
case WorkspaceReadyMsg:
    // ... existing handling ...
    if a.threadsListFetcher != nil {
        fetcher := a.threadsListFetcher
        team := msg.TeamID
        cmds = append(cmds, func() tea.Msg { return fetcher(team) })
    }
```

(Same addition for `WorkspaceSwitchedMsg`.)

- [ ] **Step 3: Build and run the binary manually**

Run: `make build`
Then run: `./bin/slk` against your existing slk config and verify:
1. The "Threads" entry appears at the top of the channel sidebar
2. Pressing Enter on it opens the threads list in the message pane
3. Selecting (j/k) updates the right thread panel
4. Sending a thread reply re-ranks the list

Expected: all four behaviors work.

> If something is broken, file the bug, do NOT patch in this task — open a debug session and fix in a follow-up commit.

- [ ] **Step 4: Run the full test suite**

Run: `go test ./...`
Expected: PASS for all packages.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go internal/ui/app.go
git commit -m "feat(slk): wire threads-view fetcher and initial load on workspace ready"
```

---

## Task 7: Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/STATUS.md` (if it exists)

- [ ] **Step 1: Document the Threads view in README**

In `README.md`, in the "Channels & Workspaces" or a new "Threads" subsection, add:

```markdown
### Threads view (top of sidebar)
- The `⚑ Threads` entry at the top of the sidebar lists every thread you
  authored, replied to, or were @-mentioned in (per workspace).
- Unread threads are pinned to the top with a `•` marker.
- Selecting the entry replaces the message pane with a scrollable list of
  thread parents; the right thread panel auto-opens for the highlighted row.
- Live updates: new replies in the active workspace re-rank the list and
  update the unread badge.
- v1 is computed from the local SQLite cache, so threads slk has not yet
  seen will not appear until the relevant channel has been opened at least
  once. A future v2 will use Slack's authoritative subscription endpoint.
```

- [ ] **Step 2: Update docs/STATUS.md**

If `docs/STATUS.md` exists and tracks roadmap items, mark "Threads view" as
shipped (or in-progress, depending on convention there).

- [ ] **Step 3: Commit**

```bash
git add README.md docs/STATUS.md
git commit -m "docs: document the threads view"
```

---

## Self-Review

Before declaring done, run through this checklist:

- [ ] `go test ./...` passes
- [ ] `go build ./...` succeeds
- [ ] `make build` produces a working binary
- [ ] `./bin/slk` shows the Threads entry at top of sidebar
- [ ] Activating it shows a list of involved threads
- [ ] Selection (j/k) re-targets the right thread panel
- [ ] Posting a thread reply causes the list to re-rank within ~150ms
- [ ] Switching workspaces refreshes the list for the new workspace
- [ ] Selecting any channel exits Threads view and restores the messages pane

If any item fails, fix it inline, do NOT skip — the spec defines the
acceptance criteria.

---

**Plan ends here.** Spec coverage: §1 UX (Tasks 3, 4), §2 data model (Task 1), §3 components (Tasks 2, 3, 4), §4 live updates (Task 5), §5 keybindings (Task 4 step 4), §6 testing (Tasks 1, 2, 4, 5), §7 main.go wiring (Task 6).
