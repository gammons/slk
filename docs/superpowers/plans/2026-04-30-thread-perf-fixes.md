# Thread & Threads-List Scroll Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate per-keystroke lag in the thread (replies) side panel and the threads-list view by porting the messages-pane caching pattern, fixing one cache-defeating bug, and debouncing one network round-trip.

**Architecture:** Three independent fix tracks land in sequence, each with its own benchmark gate:
1. **Quick wins** — fix the threadsview cache-key bug and the per-keystroke `conversations.replies` HTTP fetch.
2. **Thread panel rewrite** — pre-build `linesSelected` alongside `linesNormal`, drop `bubbles/viewport.SetContent` in favor of manual yOffset slicing, and cache the chrome (header / separator / parent message). Mirrors `internal/ui/messages/model.go` exactly.
3. **Threads-list row cache** — memoize per-row rendered output keyed on row identity + width + theme version; collapse the loop to visible-window only; precompute styles once.

Each phase is independently mergeable and benchmark-verifiable. Phases 2 and 3 share no code.

**Tech Stack:** Go 1.22+, charm.land/lipgloss/v2, charm.land/bubbles/v2 (currently used in thread; phase 2 removes it from that package), muesli/reflow.

---

## Pre-flight

### Task 0: Read the investigation report

**Files:**
- Read (no edits): this plan's "Background — file:line citations" appendix at the bottom.

- [ ] **Step 1: Open and read the appendix at the end of this document.**

The appendix is the per-keystroke control flow with file:line citations. Every later task references those citations. If you skip this you will not understand why the changes are structured the way they are.

- [ ] **Step 2: Read the cited code blocks in this order:**

```
internal/ui/messages/model.go:95-138       # viewEntry shape (linesNormal + linesSelected)
internal/ui/messages/model.go:272-285      # rationale for dropping bubbles/viewport
internal/ui/messages/model.go:983-1098     # buildCache — the pattern we are porting
internal/ui/messages/model.go:2140-2299    # View — manual visible-window slicing
internal/ui/thread/model.go:824-1016       # current thread.View — what we're replacing
internal/ui/thread/model.go:880-991        # current per-reply + view-level caches
internal/ui/threadsview/model.go:338-506   # current threadsview render path
internal/ui/app.go:4068-4093               # the threadsview cache-key bug
internal/ui/app.go:3074-3118               # openSelectedThreadCmd — the per-j/k HTTP fetch
```

No commit for this task; it is a read-only orientation step.

---

## Phase 1 — Quick Wins

These are small, surgical changes that should each be measurable on their own.

---

### Task 1: Establish thread + threadsview benchmark baselines

**Files:**
- Create: `internal/ui/thread/bench_test.go`
- Create: `internal/ui/threadsview/bench_test.go`

The messages package already has `internal/ui/messages/bench_test.go:1-36` as the canonical scroll benchmark. We mirror that shape so the three packages can be compared.

- [ ] **Step 1: Write the thread-panel scroll benchmark**

Create `internal/ui/thread/bench_test.go`:

```go
package thread

import (
	"fmt"
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
)

// BenchmarkViewScroll simulates rapid j/k scrolling: a thread with many
// replies where only m.selected changes between View() calls. Mirrors
// internal/ui/messages/bench_test.go.
func BenchmarkViewScroll(b *testing.B) {
	parent := messages.MessageItem{
		TS: "1700000000.000000", UserName: "alice", UserID: "U1",
		Text: "Parent message kicking off the thread", Timestamp: "10:00 AM",
	}
	replies := make([]messages.MessageItem, 200)
	for i := range replies {
		replies[i] = messages.MessageItem{
			TS:        fmt.Sprintf("%d.000000", 1700000001+i),
			UserName:  "bob",
			UserID:    "U2",
			Text:      "Reply with **bold** _italic_ and a `code` snippet plus a longer trailing sentence.",
			Timestamp: "10:30 AM",
			ThreadTS:  parent.TS,
		}
	}
	m := New()
	m.SetThread(parent, replies, "C1", parent.TS)

	// Prime caches.
	_ = m.View(40, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			m.MoveUp()
		} else {
			m.MoveDown()
		}
		_ = m.View(40, 100)
	}
}
```

- [ ] **Step 2: Run the thread benchmark to capture the baseline**

Run: `go test -run=^$ -bench=BenchmarkViewScroll -benchmem ./internal/ui/thread/`
Expected: a non-zero ns/op number; record it in your shell history. Do **not** treat any specific value as the gate — Phase 2 will compare against this number.

- [ ] **Step 3: Write the threadsview scroll benchmark**

Create `internal/ui/threadsview/bench_test.go`:

```go
package threadsview

import (
	"fmt"
	"testing"

	"github.com/gammons/slk/internal/cache"
)

// BenchmarkViewScroll simulates j/k scrolling through a long threads list
// where only m.selected changes between View() calls.
func BenchmarkViewScroll(b *testing.B) {
	summaries := make([]cache.ThreadSummary, 200)
	for i := range summaries {
		summaries[i] = cache.ThreadSummary{
			ChannelID:    fmt.Sprintf("C%03d", i),
			ChannelName:  fmt.Sprintf("ch-%03d", i),
			ChannelType:  "channel",
			ThreadTS:     fmt.Sprintf("%d.000000", 1700000000+i),
			ParentUserID: "U1",
			ParentText:   "Parent text with **bold** and `code` and <@U2> mention; medium length.",
			ParentTS:     fmt.Sprintf("%d.000000", 1700000000+i),
			ReplyCount:   3 + i%5,
			LastReplyTS:  fmt.Sprintf("%d.000000", 1700000100+i),
			LastReplyBy:  "U2",
			Unread:       i%4 == 0,
		}
	}
	m := New(map[string]string{"U1": "alice", "U2": "bob"}, "U1")
	m.SetSummaries(summaries)

	// Prime caches.
	_ = m.View(40, 80)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			m.MoveUp()
		} else {
			m.MoveDown()
		}
		_ = m.View(40, 80)
	}
}
```

- [ ] **Step 4: Run the threadsview benchmark to capture the baseline**

Run: `go test -run=^$ -bench=BenchmarkViewScroll -benchmem ./internal/ui/threadsview/`
Expected: a non-zero ns/op number; record it.

- [ ] **Step 5: Capture the messages-pane reference baseline**

Run: `go test -run=^$ -bench=BenchmarkViewScroll -benchmem ./internal/ui/messages/`
Expected: a non-zero ns/op number; record it.

This is the target the thread pane should approach in Phase 2 and the order of magnitude the threads-list should approach in Phase 3.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/thread/bench_test.go internal/ui/threadsview/bench_test.go
git commit -m "test: add scroll benchmarks for thread and threadsview panels"
```

---

### Task 2: Fix the threadsview panel-cache key bug

**Background:** `internal/ui/app.go:4072-4091` reads `tvVersion` on line 4072, then calls `a.threadsView.SetUserNames(a.userNames)` on line 4078 which unconditionally bumps the version (`internal/ui/threadsview/model.go:117-123`), then stores the panel cache under the **old** `tvVersion`. The cache never hits while `a.view == ViewThreads`. This makes every render — keystroke or not — re-run the entire threadsview pipeline.

The fix is two parts:
1. Make `SetUserNames` only dirty when the map actually changed (matches `SetSelfUserID` at `:132-138`).
2. As defense in depth, snapshot `tvVersion` **after** the `Set*` calls so the cache key always reflects the current state.

**Files:**
- Modify: `internal/ui/threadsview/model.go:117-123`
- Modify: `internal/ui/app.go:4072-4091`
- Test: `internal/ui/threadsview/model_test.go` (add a new test case)

- [ ] **Step 1: Write a failing test asserting `SetUserNames` is idempotent**

Append to `internal/ui/threadsview/model_test.go`:

```go
func TestSetUserNames_IdempotentDoesNotBumpVersion(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	names := map[string]string{"U1": "alice", "U2": "bob"}
	m.SetUserNames(names)
	v0 := m.Version()
	m.SetUserNames(names) // same map -> should be a no-op
	if v1 := m.Version(); v1 != v0 {
		t.Errorf("SetUserNames(same map) bumped Version: v0=%d v1=%d", v0, v1)
	}
	// A genuinely different map MUST still bump.
	m.SetUserNames(map[string]string{"U1": "alice", "U2": "carol"})
	if v2 := m.Version(); v2 == v0 {
		t.Errorf("SetUserNames(different map) did NOT bump Version: v0=%d v2=%d", v0, v2)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -run TestSetUserNames_IdempotentDoesNotBumpVersion ./internal/ui/threadsview/ -v`
Expected: FAIL — `SetUserNames(same map) bumped Version`.

- [ ] **Step 3: Make `SetUserNames` equality-checked**

In `internal/ui/threadsview/model.go`, replace lines 117-123:

```go
// SetUserNames replaces the user id -> display name map.
func (m *Model) SetUserNames(names map[string]string) {
	if names == nil {
		names = map[string]string{}
	}
	m.userNames = names
	m.dirty()
}
```

with:

```go
// SetUserNames replaces the user id -> display name map. No-op (no version
// bump) when the new map has the same length and the same key/value pairs as
// the current one — required so the App-level panel cache (app.go:4068-4093)
// can hit on idle re-renders.
func (m *Model) SetUserNames(names map[string]string) {
	if names == nil {
		names = map[string]string{}
	}
	if userNamesEqual(m.userNames, names) {
		return
	}
	m.userNames = names
	m.dirty()
}

// userNamesEqual reports whether two id->name maps have identical contents.
// Used to make SetUserNames idempotent so the panel cache can hit.
func userNamesEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		if vb, ok := b[k]; !ok || vb != va {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Apply the same equality check to `SetChannelNames` (`model.go:127-130`)**

Replace lines 127-130:

```go
func (m *Model) SetChannelNames(names map[string]string) {
	m.channelNames = names
	m.dirty()
}
```

with:

```go
// SetChannelNames sets the channel ID -> name map used to resolve bare
// <#CHANNELID> mentions. No-op when the new map matches the current one.
func (m *Model) SetChannelNames(names map[string]string) {
	if names == nil {
		names = map[string]string{}
	}
	if userNamesEqual(m.channelNames, names) {
		return
	}
	m.channelNames = names
	m.dirty()
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/threadsview/ -v`
Expected: all PASS, including the new `TestSetUserNames_IdempotentDoesNotBumpVersion`.

- [ ] **Step 6: Defense-in-depth — read `tvVersion` AFTER the Set\* calls**

In `internal/ui/app.go`, replace lines 4072-4091:

```go
		tvVersion := a.threadsView.Version()
		if c := &a.panelCacheMsgPanel; !c.hit(tvVersion, msgWidth, contentHeight, msgLayoutKey) {
			msgBorderStyle := styles.UnfocusedBorder.Width(msgWidth)
			if msgFocused {
				msgBorderStyle = styles.FocusedBorder.Width(msgWidth)
			}
			a.threadsView.SetUserNames(a.userNames)
			a.threadsView.SetSelfUserID(a.currentUserID)
			msgContentHeight := contentHeight - 2
			a.layoutMsgHeight = msgContentHeight
			if msgContentHeight < 3 {
				msgContentHeight = 3
			}
			tvView := a.threadsView.View(msgContentHeight, msgWidth-2)
			tvView = messages.ReapplyBgAfterResets(tvView, messages.BgANSI())
			out := exactSize(
				msgBorderStyle.Render(tvView),
				msgWidth+msgBorder, contentHeight,
			)
			c.store(out, tvVersion, msgWidth, contentHeight, msgLayoutKey)
		}
```

with:

```go
		// Push the current id->name maps into the threadsview model BEFORE
		// snapshotting its version. SetUserNames/SetChannelNames are
		// equality-checked (threadsview/model.go), so identical maps are no-
		// ops. Reading Version() after these calls means the panel-cache key
		// reflects the post-Set state — fixes a regression where the cache
		// stored output under a stale version and never hit on subsequent
		// renders.
		a.threadsView.SetUserNames(a.userNames)
		a.threadsView.SetSelfUserID(a.currentUserID)
		tvVersion := a.threadsView.Version()
		if c := &a.panelCacheMsgPanel; !c.hit(tvVersion, msgWidth, contentHeight, msgLayoutKey) {
			msgBorderStyle := styles.UnfocusedBorder.Width(msgWidth)
			if msgFocused {
				msgBorderStyle = styles.FocusedBorder.Width(msgWidth)
			}
			msgContentHeight := contentHeight - 2
			a.layoutMsgHeight = msgContentHeight
			if msgContentHeight < 3 {
				msgContentHeight = 3
			}
			tvView := a.threadsView.View(msgContentHeight, msgWidth-2)
			tvView = messages.ReapplyBgAfterResets(tvView, messages.BgANSI())
			out := exactSize(
				msgBorderStyle.Render(tvView),
				msgWidth+msgBorder, contentHeight,
			)
			c.store(out, tvVersion, msgWidth, contentHeight, msgLayoutKey)
		}
```

- [ ] **Step 7: Verify the panel cache now hits on idle re-renders — write the test**

Append to `internal/ui/threadsview/model_test.go`:

```go
// TestVersion_StableAcrossIdenticalSetCalls is the regression guard for the
// app.go:4068-4093 cache-key bug: pushing the same userNames + selfUserID
// repeatedly must NOT bump Version, otherwise the panel cache can never hit.
func TestVersion_StableAcrossIdenticalSetCalls(t *testing.T) {
	names := map[string]string{"U1": "alice", "U2": "bob"}
	m := New(names, "U1")
	m.SetSummaries(sampleSummaries())
	v0 := m.Version()
	for i := 0; i < 5; i++ {
		m.SetUserNames(names)
		m.SetSelfUserID("U1")
	}
	if v1 := m.Version(); v1 != v0 {
		t.Errorf("Version drifted across identical Set calls: v0=%d v1=%d", v0, v1)
	}
}
```

- [ ] **Step 8: Run the regression guard**

Run: `go test -run TestVersion_StableAcrossIdenticalSetCalls ./internal/ui/threadsview/ -v`
Expected: PASS.

- [ ] **Step 9: Re-run the threadsview scroll benchmark**

Run: `go test -run=^$ -bench=BenchmarkViewScroll -benchmem ./internal/ui/threadsview/`
Expected: ns/op should be **noticeably lower** than the Task 1 baseline. Note that the bench does not exercise app.go's cache directly — but both the bench and the app render path call `m.View()` and the equality-checked Set* paths, and the bench primes the caches once before the loop. The expected drop is from the benchmark already being mostly-cache-friendly; the **bigger** real-world win is the app-level cache now hitting, which the bench cannot measure. Record both numbers in the commit message.

- [ ] **Step 10: Commit**

```bash
git add internal/ui/threadsview/model.go internal/ui/threadsview/model_test.go internal/ui/app.go
git commit -m "fix(threadsview): make Set* idempotent so panel cache can actually hit

SetUserNames and SetChannelNames were dirtying unconditionally, so app.go's
panel cache (keyed on Version) could never hit while ViewThreads was active.
Snapshot Version after pushing maps; equality-check the maps so identical
pushes are no-ops."
```

---

### Task 3: Debounce `openSelectedThreadCmd` so j/k doesn't fire one HTTP request per row

**Background:** `internal/ui/app.go:3074-3118` runs on every j/k while in `ViewThreads`. The dedup at `:3084` only catches re-selecting the same row, so each new row fires `client.GetReplies(...)` (`cmd/slk/main.go:1438`). Hold j down and you flood Slack's rate limit. The fix: keep the synchronous local UI updates immediate (so the right pane shows the parent right away), but coalesce the **network fetch** into a 200ms post-settle debounce.

**Files:**
- Modify: `internal/ui/app.go` — `openSelectedThreadCmd` (3074-3118), App struct (~479-700), Update for new debounce-tick message.

- [ ] **Step 1: Define the debounce-tick message and pending-key field**

In `internal/ui/app.go`, near the other internal message types, add:

```go
// threadFetchDebounceMsg is delivered after the user's threadsview selection
// stops moving for openThreadDebounceDelay. Carries the (channelID, threadTS,
// generation) the user had selected at scheduling time; if the App's current
// pendingThreadFetchGen has advanced past `gen`, the message is dropped — a
// later j/k has scheduled a fresh fetch.
type threadFetchDebounceMsg struct {
	channelID string
	threadTS  string
	gen       uint64
}

const openThreadDebounceDelay = 200 * time.Millisecond
```

In the `App` struct, add:

```go
	// pendingThreadFetchGen is bumped by every openSelectedThreadCmd call.
	// The debounce-tick handler only runs the network fetch when its `gen`
	// matches; older ticks are dropped so a held j produces exactly one
	// fetch (for the row the user finally lands on).
	pendingThreadFetchGen uint64
```

(Place this next to the existing `lastOpenedChannelID` / `lastOpenedThreadTS` fields.)

- [ ] **Step 2: Replace `openSelectedThreadCmd` to schedule a debounced fetch**

In `internal/ui/app.go`, replace lines 3074-3118:

```go
// openSelectedThreadCmd opens the right thread panel on whichever row is
// currently highlighted in the threads view. No-op if the list is empty,
// no thread fetcher is wired, OR the selected thread is already the one
// open in the right panel (dedup: avoids hammering the Slack API and
// clobbering an in-progress read on every j/k press or list reload).
func (a *App) openSelectedThreadCmd() tea.Cmd {
	sum, ok := a.threadsView.SelectedSummary()
	if !ok {
		return nil
	}
	if sum.ChannelID == a.lastOpenedChannelID && sum.ThreadTS == a.lastOpenedThreadTS {
		return nil
	}
	a.lastOpenedChannelID = sum.ChannelID
	a.lastOpenedThreadTS = sum.ThreadTS
	a.threadVisible = true
	a.statusbar.SetInThread(true)
	parent := messages.MessageItem{
		TS:       sum.ParentTS,
		UserID:   sum.ParentUserID,
		UserName: a.userNameFor(sum.ParentUserID),
		Text:     sum.ParentText,
		ThreadTS: sum.ThreadTS,
	}
	a.threadPanel.SetThread(parent, nil, sum.ChannelID, sum.ThreadTS)
	a.threadCompose.SetChannel("thread")
	// ... applyThreadUnreadBoundary / MarkSelectedRead ...
	if a.threadFetcher != nil {
		fetcher := a.threadFetcher
		chID, threadTS := sum.ChannelID, sum.ThreadTS
		return func() tea.Msg { return fetcher(chID, threadTS) }
	}
	return nil
}
```

with:

```go
// openSelectedThreadCmd updates UI state for whichever row the threadsview
// has highlighted (so the right thread panel shows the parent immediately),
// then SCHEDULES the network fetch behind a debounce. Holding j to scroll
// the list previously fired one Slack conversations.replies HTTP call per
// row traversed; now exactly one fetch fires after the cursor settles for
// openThreadDebounceDelay.
func (a *App) openSelectedThreadCmd() tea.Cmd {
	sum, ok := a.threadsView.SelectedSummary()
	if !ok {
		return nil
	}
	if sum.ChannelID == a.lastOpenedChannelID && sum.ThreadTS == a.lastOpenedThreadTS {
		return nil
	}
	a.lastOpenedChannelID = sum.ChannelID
	a.lastOpenedThreadTS = sum.ThreadTS
	a.threadVisible = true
	a.statusbar.SetInThread(true)
	parent := messages.MessageItem{
		TS:       sum.ParentTS,
		UserID:   sum.ParentUserID,
		UserName: a.userNameFor(sum.ParentUserID),
		Text:     sum.ParentText,
		ThreadTS: sum.ThreadTS,
	}
	a.threadPanel.SetThread(parent, nil, sum.ChannelID, sum.ThreadTS)
	a.threadCompose.SetChannel("thread")
	a.applyThreadUnreadBoundary(sum.ChannelID)
	if a.threadsView.MarkSelectedRead() {
		a.sidebar.SetThreadsUnreadCount(a.threadsView.UnreadCount())
	}
	if a.threadFetcher == nil {
		return nil
	}
	a.pendingThreadFetchGen++
	gen := a.pendingThreadFetchGen
	chID, threadTS := sum.ChannelID, sum.ThreadTS
	return tea.Tick(openThreadDebounceDelay, func(time.Time) tea.Msg {
		return threadFetchDebounceMsg{channelID: chID, threadTS: threadTS, gen: gen}
	})
}
```

- [ ] **Step 3: Handle the debounce tick in `Update`**

In `internal/ui/app.go`, find the `tea.Msg` switch in the App's `Update` (search for an existing internal-msg branch like `ThreadsListLoadedMsg`). Add a new case:

```go
	case threadFetchDebounceMsg:
		// Drop stale debounce ticks: a later j/k has scheduled a fresh
		// fetch and bumped the generation past this one.
		if msg.gen != a.pendingThreadFetchGen {
			return a, nil
		}
		// Also drop if the user has navigated away (e.g. switched to a
		// different thread or closed the threads view) since scheduling.
		if msg.channelID != a.lastOpenedChannelID || msg.threadTS != a.lastOpenedThreadTS {
			return a, nil
		}
		if a.threadFetcher == nil {
			return a, nil
		}
		fetcher := a.threadFetcher
		chID, threadTS := msg.channelID, msg.threadTS
		return a, func() tea.Msg { return fetcher(chID, threadTS) }
```

- [ ] **Step 4: Add a regression test for the debounce**

Create a new file `internal/ui/app_threads_debounce_test.go` (the existing `app_test.go` is already 2110 lines):

```go
package ui

import (
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
)

// newTestAppWithThreadsView is the threads-view analogue of
// newTestAppWithMessages (app_selection_test.go:13). Wires three summaries
// into an App, activates ViewThreads, and returns the App for the caller to
// drive openSelectedThreadCmd / Update directly.
func newTestAppWithThreadsView(t *testing.T, summaries []cache.ThreadSummary) *App {
	t.Helper()
	a := NewApp()
	a.width = 120
	a.height = 30
	a.threadsView.SetSummaries(summaries)
	a.view = ViewThreads
	_ = a.View() // populate layout offsets and caches
	return a
}

// TestThreadsViewDebouncesNetworkFetchOnRapidJK guards Task 3:
// holding j down across multiple summaries must collapse to exactly one
// network fetch (against the row the user finally lands on), not one per row.
func TestThreadsViewDebouncesNetworkFetchOnRapidJK(t *testing.T) {
	var fetchCount int32
	var fetchedTS []string
	a := newTestAppWithThreadsView(t, []cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "1.0", ParentText: "p1", ChannelName: "g1"},
		{ChannelID: "C2", ThreadTS: "2.0", ParentText: "p2", ChannelName: "g2"},
		{ChannelID: "C3", ThreadTS: "3.0", ParentText: "p3", ChannelName: "g3"},
	})
	a.threadFetcher = func(channelID, threadTS string) tea.Msg {
		atomic.AddInt32(&fetchCount, 1)
		fetchedTS = append(fetchedTS, threadTS)
		return ThreadRepliesLoadedMsg{ChannelID: channelID, ThreadTS: threadTS}
	}

	// Simulate held-j: three openSelectedThreadCmd invocations interleaved
	// with MoveDown, capturing each returned Cmd. Each Cmd is a tea.Tick
	// scheduled at openThreadDebounceDelay; we don't actually wait for the
	// timer — we synthesize the threadFetchDebounceMsg the tick would emit
	// and feed it into Update. Ticks 1 and 2 carry stale generations and
	// must be dropped; tick 3 must produce the (single) fetcher Cmd.
	var emitted []threadFetchDebounceMsg
	for i := 0; i < 3; i++ {
		cmd := a.openSelectedThreadCmd()
		if cmd != nil {
			// We can't directly observe the tick's payload (it's behind
			// tea.Tick), so we synthesize one matching the App's current
			// (lastOpened*, pendingThreadFetchGen) tuple — the exact
			// payload openSelectedThreadCmd would have scheduled.
			emitted = append(emitted, threadFetchDebounceMsg{
				channelID: a.lastOpenedChannelID,
				threadTS:  a.lastOpenedThreadTS,
				gen:       a.pendingThreadFetchGen,
			})
		}
		a.threadsView.MoveDown()
	}
	// Replay the first two ticks (stale generations) and confirm Update
	// drops them.
	for i := 0; i < len(emitted)-1; i++ {
		stale := threadFetchDebounceMsg{
			channelID: emitted[i].channelID,
			threadTS:  emitted[i].threadTS,
			gen:       emitted[i].gen,
		}
		_, cmd := a.Update(stale)
		if cmd != nil {
			t.Errorf("stale debounce tick %d should be dropped; got cmd %v", i, cmd)
		}
	}
	// Replay the last (current) tick and run its returned fetcher Cmd.
	last := emitted[len(emitted)-1]
	_, cmd := a.Update(last)
	if cmd == nil {
		t.Fatalf("latest debounce tick should return the fetcher Cmd")
	}
	for _, m := range drainBatch(cmd) {
		_, _ = a.Update(m)
	}
	if got := atomic.LoadInt32(&fetchCount); got != 1 {
		t.Errorf("expected exactly 1 network fetch after rapid j/k, got %d (TS=%v)", got, fetchedTS)
	}
}
```

`drainBatch` is the existing helper in `app_selection_test.go:29-44`; it lives in the same `ui` package so this test file can use it directly.

- [ ] **Step 5: Run the test**

Run: `go test -run TestThreadsViewDebouncesNetworkFetchOnRapidJK ./internal/ui/ -v`
Expected: PASS — `expected exactly 1 network fetch ... got 1`.

- [ ] **Step 6: Run all UI tests to confirm no regressions**

Run: `go test ./internal/ui/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "fix(app): debounce thread network fetch on rapid threads-view j/k

openSelectedThreadCmd was firing one client.GetReplies HTTP call per row
traversed while holding j, with the dedup only catching re-selection of the
same row. UI updates remain synchronous; the network fetch now waits 200ms
after the cursor settles, coalescing held-key bursts into one round-trip."
```

---

## Phase 2 — Thread (Replies) Panel: Port the Messages-Pane Caching Pattern

The thread panel currently:
1. Stores only `linesNormal` per reply, building `linesSelected`-equivalent on demand inside `View` for every reply on every j/k (`internal/ui/thread/model.go:957-965`).
2. Re-renders chrome (header + separator + parent message via the full markdown pipeline) on every render (`:835-857`).
3. Uses `bubbles/viewport.Model.SetContent` per render, which `ansi.StringWidth`-walks every line (the messages package documented this at ~55% of CPU and dropped it; see `internal/ui/messages/model.go:272-276`).

We port the messages pattern verbatim: pre-build `linesSelected` alongside `linesNormal` in the per-reply cache; cache the chrome; replace the bubbles viewport with manual yOffset slicing.

---

### Task 4: Pre-build `linesSelected` in the per-reply cache

**Files:**
- Modify: `internal/ui/thread/model.go:20-32` (viewEntry struct)
- Modify: `internal/ui/thread/model.go:879-991` (buildCache + view-level loop)

- [ ] **Step 1: Extend `viewEntry` to carry both bordered variants**

In `internal/ui/thread/model.go`, replace lines 20-32:

```go
// viewEntry is a pre-rendered reply, matching the shape used by
// internal/ui/messages.viewEntry: linesNormal is the bordered styled
// content split on "\n"; linesPlain is the column-aligned mirror of
// the UNBORDERED content; contentColOffset is the column where content
// begins inside the BORDERED viewContent (= 1 for replies, which carry
// the thick left border applied during the viewContent build step).
type viewEntry struct {
	linesNormal      []string
	linesPlain       []messages.PlainLine
	height           int
	replyIdx         int
	contentColOffset int
}
```

with:

```go
// viewEntry is a pre-rendered reply. linesNormal/linesSelected hold the FULLY
// BORDERED rendered content split on "\n" so View() can flatten directly into
// the visible window without any per-frame string scanning, lipgloss render,
// or width measurement. linesPlain mirrors the UNBORDERED content for
// selection extraction. contentColOffset is 1 (the thick left border ▌ that
// linesNormal/linesSelected include).
//
// This shape mirrors internal/ui/messages.viewEntry exactly; keeping them in
// lockstep means scroll and selection logic can be kept in sync.
type viewEntry struct {
	linesNormal      []string
	linesSelected    []string
	linesPlain       []messages.PlainLine
	height           int
	replyIdx         int
	contentColOffset int
}
```

- [ ] **Step 2: Build both variants inside `buildCache`**

In `internal/ui/thread/model.go`, replace lines 879-903 (the per-reply cache build branch):

```go
	if m.cache == nil || m.cacheWidth != width || m.cacheReplyLen != len(m.replies) {
		m.cache = make([]viewEntry, 0, len(m.replies))
		if m.replyIDToIdx == nil {
			m.replyIDToIdx = make(map[string]int, len(m.replies))
		} else {
			for k := range m.replyIDToIdx {
				delete(m.replyIDToIdx, k)
			}
		}
		for i, reply := range m.replies {
			rendered := m.renderThreadMessage(reply, width, m.userNames, m.channelNames, i == m.selected)
			m.cache = append(m.cache, viewEntry{
				linesNormal:      strings.Split(rendered, "\n"),
				linesPlain:       messages.PlainLines(rendered),
				height:           lipgloss.Height(rendered),
				replyIdx:         i,
				contentColOffset: 1, // border applied during viewContent build
			})
			m.replyIDToIdx[reply.TS] = i
		}
		m.cacheWidth = width
		m.cacheReplyLen = len(m.replies)
		m.viewCacheValid = false
	}
```

with:

```go
	if m.cache == nil || m.cacheWidth != width || m.cacheReplyLen != len(m.replies) {
		m.cache = m.cache[:0]
		if m.replyIDToIdx == nil {
			m.replyIDToIdx = make(map[string]int, len(m.replies))
		} else {
			for k := range m.replyIDToIdx {
				delete(m.replyIDToIdx, k)
			}
		}
		// Pre-build border styles ONCE (don't allocate per reply).
		borderFill := lipgloss.NewStyle().Background(styles.Background)
		borderInvis := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
			BorderForeground(styles.Background).BorderBackground(styles.Background)
		borderSelect := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
			BorderForeground(styles.SelectionBorderColor(m.focused)).BorderBackground(styles.Background)
		for i, reply := range m.replies {
			// renderThreadMessage's last arg ("selected") affects only
			// reaction pill styling, not the border. Pass false here; the
			// per-frame border is applied below via borderInvis/Select.
			rendered := m.renderThreadMessage(reply, width, m.userNames, m.channelNames, false)
			filled := borderFill.Width(width - 1).Render(rendered)
			normal := borderInvis.Render(filled)
			selected := borderSelect.Render(filled)
			linesN := strings.Split(normal, "\n")
			linesS := strings.Split(selected, "\n")
			m.cache = append(m.cache, viewEntry{
				linesNormal:      linesN,
				linesSelected:    linesS,
				linesPlain:       messages.PlainLines(filled),
				height:           len(linesN),
				replyIdx:         i,
				contentColOffset: 1,
			})
			m.replyIDToIdx[reply.TS] = i
		}
		m.cacheWidth = width
		m.cacheReplyLen = len(m.replies)
		m.viewCacheValid = false
	}
```

Two important changes:
- The border is now applied **inside** buildCache (once per reply), not in the view-level loop.
- `linesPlain` mirrors `filled` (the unbordered content) — same convention as `internal/ui/messages/model.go:1057-1061`.

> **NOTE:** `renderThreadMessage` previously received `selected` and used it to color reaction pills differently. Audit `renderThreadMessage` (`internal/ui/thread/model.go:1019-1083`) before this step — if `selected` *only* drives border-related styling, dropping it (passing `false` here) is safe. If it drives content-affecting styling (e.g. reaction pill color when the reply is the cursor), you must either keep two cache variants ahead of `filled` or accept that pill styling no longer reacts to selection. The cleanest answer is the latter (the messages pane already does it this way — only the `▌` border indicates selection in messages too). The plan assumes the latter; if the audit shows otherwise, raise it as a checkpoint with the user before continuing.

- [ ] **Step 3: Verify the audit**

Run: `grep -n "selected" internal/ui/thread/model.go | head -40`

Inspect every hit inside `renderThreadMessage` (lines 1019-1083). Confirm `selected` does not affect content. If it does, halt and surface to the human reviewer.

- [ ] **Step 4: Run existing thread tests to catch any regressions**

Run: `go test ./internal/ui/thread/`
Expected: PASS. The view-level loop in `View()` still builds `viewContent` from `linesNormal` (Task 5 changes that), so this step is purely structural.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/thread/model.go
git commit -m "refactor(thread): build linesSelected alongside linesNormal in per-reply cache

Mirrors internal/ui/messages/model.go:1051-1083: bordered output is built
ONCE per reply at cache-build time, not on every j/k. Sets up Task 5 to
flip a slice pointer instead of running lipgloss N times per render."
```

---

### Task 5: Replace the per-render bordered-content loop with a pointer flip

**Files:**
- Modify: `internal/ui/thread/model.go:905-991` (the view-level cache rebuild block)

- [ ] **Step 1: Rewrite the view-level cache to consume the pre-built variants**

In `internal/ui/thread/model.go`, replace lines 905-991 with:

```go
	// View-level cache: viewContent is the flat newline-joined string of all
	// reply rows + inter-reply separators + (optional) "── new ──" landmark,
	// in selection-aware form. Rebuilt only when:
	//   - the per-reply cache was rebuilt (different width or reply count), or
	//   - the user moved the cursor (m.viewSelected != m.selected), or
	//   - the panel height changed (affects scroll math, not content).
	//
	// On a typical j/k, ONLY the "selection moved" branch fires, and the
	// rebuild is a tight loop that picks linesNormal vs linesSelected and
	// joins. No lipgloss render, no Width/Height scan.
	if !m.viewCacheValid || m.viewSelected != m.selected || m.viewWidth != width || m.viewHeight != replyAreaHeight {
		separatorStyle := lipgloss.NewStyle().
			Width(width).
			Background(styles.Background).
			Foreground(styles.Border)
		replySeparator := separatorStyle.Render(strings.Repeat("─", width))

		var newLandmark string
		if m.unreadBoundaryTS != "" {
			landmarkStyle := lipgloss.NewStyle().
				Width(width).
				Background(styles.Background).
				Foreground(styles.Error).
				Bold(true).
				Align(lipgloss.Center)
			newLandmark = landmarkStyle.Render("── new ──")
		}
		landmarkInserted := false

		// entryOffsets / totalLines mirror the FULLY BORDERED viewContent.
		m.entryOffsets = m.entryOffsets[:0]
		var allRows []string
		startLine := 0
		endLine := 0
		currentLine := 0

		for i, e := range m.cache {
			if !landmarkInserted && newLandmark != "" && i < len(m.replies) && m.replies[i].TS > m.unreadBoundaryTS {
				allRows = append(allRows, newLandmark)
				currentLine++
				landmarkInserted = true
			}

			var lines []string
			if i == m.selected {
				lines = e.linesSelected
				startLine = currentLine
				endLine = currentLine + e.height
			} else {
				lines = e.linesNormal
			}
			m.entryOffsets = append(m.entryOffsets, currentLine)
			allRows = append(allRows, lines...)
			currentLine += e.height
			if i < len(m.cache)-1 {
				allRows = append(allRows, replySeparator)
				currentLine++
			}
		}

		m.viewContent = strings.Join(allRows, "\n")
		m.viewSelected = m.selected
		m.viewWidth = width
		m.viewHeight = replyAreaHeight
		m.selectedStartLine = startLine
		m.selectedEndLine = endLine
		m.totalLines = currentLine
		m.viewCacheValid = true
	}
```

The hot loop is now: pick a slice, append it, bump a counter. No `lipgloss.Render`, no `lipgloss.Height`, no per-reply allocations.

- [ ] **Step 2: Run thread tests**

Run: `go test ./internal/ui/thread/`
Expected: PASS. The render output should be byte-identical to before for the same selection + width + content.

- [ ] **Step 3: Run the thread scroll benchmark**

Run: `go test -run=^$ -bench=BenchmarkViewScroll -benchmem ./internal/ui/thread/`
Expected: ns/op should be **substantially lower** than the Task 1 baseline (target: rough order-of-magnitude improvement on the per-reply work; the SetContent cost remains until Task 7).

- [ ] **Step 4: Commit**

```bash
git add internal/ui/thread/model.go
git commit -m "perf(thread): flip pre-bordered slice on j/k instead of re-rendering

The view-level rebuild was running 2 lipgloss.Render and 1 lipgloss.Height
per reply per j/k. Borders are now built once per cache, and the view-level
loop just picks linesSelected vs linesNormal and appends. Mirrors
internal/ui/messages's hot path."
```

---

### Task 6: Cache the chrome (header + separator + parent message)

**Background:** `internal/ui/thread/model.go:835-857` re-renders the header (`lipgloss.NewStyle().Width().Render(...)`), the separator (`strings.Repeat` + Render), AND the **parent message** (full markdown pipeline via `renderThreadMessage`) on every View. The messages pane caches its chrome at `internal/ui/messages/model.go:258-264, 2005-2032`. We add the same here.

**Files:**
- Modify: `internal/ui/thread/model.go` — add chrome cache fields to `Model`; update `dirty()` / `InvalidateCache` / `SetThread`; replace lines 835-857.

- [ ] **Step 1: Add chrome-cache fields to the Model struct**

In `internal/ui/thread/model.go`, locate the Model struct (~lines 36-100). Add these fields next to the existing view-level cache fields:

```go
	// chromeCache holds the rendered "header + separator + parent message +
	// separator" prefix that View() prepends to viewContent. Rebuilt only
	// when its inputs (width, replyCount, parent identity, parent text,
	// userNames, channelNames) change. On a plain j/k it is reused as-is.
	chromeCache       string
	chromeCacheValid  bool
	chromeWidth       int
	chromeReplyCount  int
	chromeParentTS    string
	chromeParentText  string
	chromeUserNamesV  uint64 // hash/version of the userNames map at build time
	chromeChannelNV   uint64 // hash/version of the channelNames map at build time
```

We need a way to detect when `userNames` / `channelNames` change. Either:
- (a) Take an integer "version" counter the App pushes alongside via `SetUserNames`, OR
- (b) Hash the map at build time.

(a) is cleaner. We'll track via a counter local to the model.

Add these helper fields and update `SetUserNames` / `SetChannelNames` to bump them:

```go
	// userNamesV / channelNamesV are bumped every time SetUserNames /
	// SetChannelNames replaces the map. Used by chromeCache (and any other
	// cache that depends on these maps) to detect changes without hashing.
	userNamesV    uint64
	channelNamesV uint64
```

Replace the existing `SetUserNames` / `SetChannelNames` (find them via grep — they invalidate cache). Make them bump these counters when the map identity changes:

```go
func (m *Model) SetUserNames(names map[string]string) {
	if names == nil {
		names = map[string]string{}
	}
	// We compare by identity-ish: the App always passes a fresh map when
	// the underlying data changes (see app.go:3274-3296 SetChannels fan-
	// out), so a new pointer is sufficient signal.
	m.userNames = names
	m.userNamesV++
	m.viewCacheValid = false
	m.chromeCacheValid = false
	m.dirty()
}

func (m *Model) SetChannelNames(names map[string]string) {
	if names == nil {
		names = map[string]string{}
	}
	m.channelNames = names
	m.channelNamesV++
	m.viewCacheValid = false
	m.chromeCacheValid = false
	m.dirty()
}
```

(Use the file's existing function locations rather than appending.)

- [ ] **Step 2: Invalidate the chrome cache from `InvalidateCache` and `SetThread`**

In `internal/ui/thread/model.go`, locate `InvalidateCache` (currently ~115-119):

```go
func (m *Model) InvalidateCache() {
	m.cache = nil
	m.viewCacheValid = false
	m.dirty()
}
```

Replace with:

```go
func (m *Model) InvalidateCache() {
	m.cache = nil
	m.viewCacheValid = false
	m.chromeCacheValid = false
	m.dirty()
}
```

`SetThread` (~126-144) already calls `InvalidateCache` at the end, so no change needed there.

`SetUnreadBoundary` (~151-158) does NOT need to invalidate the chrome cache (the boundary affects only the per-reply landmark, not the chrome). Confirm by inspection.

- [ ] **Step 3: Replace the chrome-rebuild block in `View`**

In `internal/ui/thread/model.go`, the current chrome block runs from line 834 (`// Header`) to line 857 (`m.chromeHeight = chromeHeight`). It has this exact shape:

```go
	// Header
	replyLabel := "replies"
	if len(m.replies) == 1 {
		replyLabel = "reply"
	}
	header := lipgloss.NewStyle().
		Width(width).
		Background(styles.Background).
		Foreground(styles.TextPrimary).
		Bold(true).
		Render(fmt.Sprintf("Thread  %d %s", len(m.replies), replyLabel))

	separator := lipgloss.NewStyle().
		Width(width).
		Background(styles.Background).
		Foreground(styles.Border).
		Render(strings.Repeat("-", width))

	// Parent message
	parentContent := m.renderThreadMessage(m.parent, width, m.userNames, m.channelNames, false)

	chrome := header + "\n" + separator + "\n" + parentContent + "\n" + separator
	chromeHeight := lipgloss.Height(chrome)
	m.chromeHeight = chromeHeight
```

Replace it with:

```go
	// Chrome: header + separator + parent message + separator. Cached because
	// the parent's full markdown pipeline (RenderSlackMarkdown + WordWrap +
	// reactions + attachments) is expensive and identical frame-to-frame on
	// a plain j/k. Mirrors internal/ui/messages's chromeCache.
	chromeReplyCount := len(m.replies)
	parentTS := m.parent.TS
	parentText := m.parent.Text
	if !m.chromeCacheValid ||
		m.chromeWidth != width ||
		m.chromeReplyCount != chromeReplyCount ||
		m.chromeParentTS != parentTS ||
		m.chromeParentText != parentText ||
		m.chromeUserNamesV != m.userNamesV ||
		m.chromeChannelNV != m.channelNamesV {
		replyLabel := "replies"
		if chromeReplyCount == 1 {
			replyLabel = "reply"
		}
		header := lipgloss.NewStyle().
			Width(width).
			Background(styles.Background).
			Foreground(styles.TextPrimary).
			Bold(true).
			Render(fmt.Sprintf("Thread  %d %s", chromeReplyCount, replyLabel))
		separator := lipgloss.NewStyle().
			Width(width).
			Background(styles.Background).
			Foreground(styles.Border).
			Render(strings.Repeat("-", width))
		parentContent := m.renderThreadMessage(m.parent, width, m.userNames, m.channelNames, false)
		m.chromeCache = header + "\n" + separator + "\n" + parentContent + "\n" + separator
		m.chromeHeight = lipgloss.Height(m.chromeCache)
		m.chromeCacheValid = true
		m.chromeWidth = width
		m.chromeReplyCount = chromeReplyCount
		m.chromeParentTS = parentTS
		m.chromeParentText = parentText
		m.chromeUserNamesV = m.userNamesV
		m.chromeChannelNV = m.channelNamesV
	}
	chrome := m.chromeCache
	chromeHeight := m.chromeHeight
```

The `chromeHeight` local is referenced by the next block in the existing function (the `replyAreaHeight := height - chromeHeight` line), so the reassignment from `m.chromeHeight` keeps that calculation working.

- [ ] **Step 4: Adjust `AddReply` / `UpdateMessageInPlace` / `UpdateParentInPlace` to invalidate chrome on parent changes**

These functions exist around `internal/ui/thread/model.go:160-300` (use grep). For each one, audit whether it can change:
- `m.parent.TS` or `m.parent.Text` → invalidate chrome (`m.chromeCacheValid = false`).
- `len(m.replies)` → invalidate chrome.

Specifically:
- `AddReply` (appends to `m.replies`) → `m.chromeCacheValid = false` because `chromeReplyCount` changes.
- `UpdateParentInPlace` → `m.chromeCacheValid = false`.
- `UpdateMessageInPlace` for the parent (when called with parent's TS) → `m.chromeCacheValid = false`. If the function only handles replies, no change needed.

Add the invalidation lines at the end of each affected function, alongside the existing `m.viewCacheValid = false; m.dirty()` calls.

- [ ] **Step 5: Run thread tests**

Run: `go test ./internal/ui/thread/`
Expected: PASS. Output should be byte-identical to before for any given (width, replies, parent, userNames, channelNames) combination.

- [ ] **Step 6: Run the thread scroll benchmark**

Run: `go test -run=^$ -bench=BenchmarkViewScroll -benchmem ./internal/ui/thread/`
Expected: ns/op should drop further (the parent re-render is now skipped on idle scroll).

- [ ] **Step 7: Commit**

```bash
git add internal/ui/thread/model.go
git commit -m "perf(thread): cache chrome (header + separator + parent message)

Parent-message render via the full Slack markdown pipeline was running on
every render. Now cached and invalidated only when width / reply count /
parent identity / parent text / userNames / channelNames change. Mirrors
internal/ui/messages's chromeCache."
```

---

### Task 7: Drop `bubbles/viewport` in favor of manual visible-window slicing

**Background:** `internal/ui/thread/model.go:46` holds a `viewport.Model`, and `:993-997` calls `m.vp.SetWidth/SetHeight/SetContent(viewContent)` on every render. Per the comment at `internal/ui/messages/model.go:272-276`, `SetContent` runs `ansi.StringWidth` over every line of content — measured at ~55% of CPU on j/k in the messages pane, which is why it was removed there. The viewport's `View()` then does another `ansi.StringWidth` per visible line. We replicate the messages-pane manual-slice approach.

**Files:**
- Modify: `internal/ui/thread/model.go` — Model struct (`vp` field), View body (lines 993-1015), GoToTop / GoToBottom / scroll helpers, anywhere else that calls `m.vp.*`.

- [ ] **Step 1: Audit every call site for `m.vp.`**

Run: `grep -n "m\.vp\." internal/ui/thread/model.go`

Make a list. Each call site needs an equivalent operation in terms of `m.yOffset` (an `int` field we'll add). Typical mappings:
- `m.vp.SetYOffset(n)` → `m.yOffset = n`
- `m.vp.YOffset` → `m.yOffset`
- `m.vp.Height` → `m.viewHeight`
- `m.vp.Width` → `m.viewWidth`
- `m.vp.View()` → manual slice of `m.viewContent` from `m.yOffset` over `m.viewHeight` lines

If any call site uses `m.vp.GotoTop` / `m.vp.GotoBottom` / `m.vp.HalfViewDown` / etc., translate to direct `m.yOffset` math.

- [ ] **Step 2: Replace the `vp` field with `yOffset`**

In the Model struct, replace:

```go
	vp                viewport.Model
```

with:

```go
	// yOffset is the first content line currently visible in the reply
	// viewport. Replaces bubbles/viewport: bubbles' SetContent does an
	// ansi.StringWidth scan over every content line per call (~55% of CPU
	// on j/k per internal/ui/messages/model.go:272-276), and we already
	// know our content's line count and width from the buildCache pass.
	yOffset int

	// snappedSelection lets View() avoid yanking yOffset back to the
	// selected reply on every render after the user has manually
	// scrolled. Mirrors internal/ui/messages.snappedSelection.
	snappedSelection int
	hasSnapped       bool
```

- [ ] **Step 3: Replace the View tail (lines 993-1015)**

Replace lines 993-1015 with:

```go
	// Snap yOffset to keep the selected reply visible, but only when the
	// selection has actually changed since the last snap — preserves the
	// user's manual scroll position when no key has moved the cursor.
	if !m.hasSnapped || m.snappedSelection != m.selected {
		if m.selectedStartLine < m.yOffset {
			m.yOffset = m.selectedStartLine
		} else if m.selectedEndLine > m.yOffset+replyAreaHeight {
			m.yOffset = m.selectedEndLine - replyAreaHeight
			if m.yOffset < 0 {
				m.yOffset = 0
			}
		}
		m.snappedSelection = m.selected
		m.hasSnapped = true
	}
	// Clamp.
	maxOffset := m.totalLines - replyAreaHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.yOffset > maxOffset {
		m.yOffset = maxOffset
	}
	if m.yOffset < 0 {
		m.yOffset = 0
	}

	// Slice the visible window directly. m.viewContent is already split
	// per line in buildCache, but stored as a joined string; split once
	// here. (If profiling shows this split on the hot path, switch
	// viewContent to []string.)
	contentLines := strings.Split(m.viewContent, "\n")
	to := m.yOffset + replyAreaHeight
	if to > len(contentLines) {
		to = len(contentLines)
	}
	visible := make([]string, 0, replyAreaHeight)
	visible = append(visible, contentLines[m.yOffset:to]...)
	// Pad to replyAreaHeight with the themed spacer so the panel always
	// fills its allotted height.
	if len(visible) < replyAreaHeight {
		spacer := lipgloss.NewStyle().Width(width).Background(styles.Background).Render("")
		for len(visible) < replyAreaHeight {
			visible = append(visible, spacer)
		}
	}
	visibleContent := strings.Join(visible, "\n")

	// Selection overlay (mouse drag). Operates on the visible slice now,
	// not on the full viewContent — mirrors internal/ui/messages's flow.
	if m.hasSelection {
		visibleContent = m.applySelectionOverlayVisible(visibleContent, m.yOffset, replyAreaHeight)
	}

	result := chrome + "\n" + visibleContent
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Background(styles.Background).Render(result)
}
```

> **NOTE:** `applySelectionOverlay` currently operates on the full `viewContent` string (`internal/ui/thread/model.go:758-820`) and forces a second `vp.SetContent`. The new `applySelectionOverlayVisible` must take the already-windowed slice and the yOffset; copy the messages-pane equivalent (`internal/ui/messages/model.go` — search for the function that takes a `visible` slice and a yOffset). If the messages pane's selection overlay is too coupled to its own data shapes to lift cleanly, ask the human reviewer at this checkpoint how to proceed; the `m.hasSelection` path is rare enough on the keyboard hot path that you can ship Tasks 1-7 with a TODO and iterate on the selection overlay separately.

Also drop the `viewport` import at the top of the file.

- [ ] **Step 4: Update `GoToTop` / `GoToBottom` / `MoveUp` / `MoveDown` / scroll helpers**

Search the file for `m.vp.` references that survive the edits above and translate each:
- `m.vp.GotoTop()` → `m.yOffset = 0; m.snappedSelection = m.selected; m.hasSnapped = true`
- `m.vp.GotoBottom()` → `m.yOffset = max(0, m.totalLines - m.viewHeight); m.snappedSelection = m.selected; m.hasSnapped = true`
- `m.vp.YOffset` reads → `m.yOffset`
- `m.vp.SetYOffset(n)` → `m.yOffset = n; m.hasSnapped = false` (so the next View doesn't immediately yank back to selection)
- `m.vp.HalfViewDown()` → manual `m.yOffset += m.viewHeight / 2; m.hasSnapped = false`
- `m.vp.HalfViewUp()` → manual decrement; clamp to 0.

Some of these `vp.*` calls will be in `app.go` if any are exported through the thread Model. Run `grep -rn "threadPanel\.\(YOffset\|GotoTop\|GotoBottom\|SetYOffset\)" internal/` to find them.

- [ ] **Step 5: Run all thread tests + the full UI suite**

Run: `go test ./internal/ui/thread/ ./internal/ui/`
Expected: PASS. Selection rendering, GoToBottom/GoToTop, drag-selection, and snap-on-selection-change all need to behave the same as before.

- [ ] **Step 6: Run the thread scroll benchmark**

Run: `go test -run=^$ -bench=BenchmarkViewScroll -benchmem ./internal/ui/thread/`
Expected: ns/op should now be **within the same order of magnitude as the messages-pane benchmark from Task 1, Step 5**. If it isn't, profile (`go test -bench=BenchmarkViewScroll -cpuprofile=cpu.out` + `go tool pprof cpu.out`) and surface findings before Task 8.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/thread/model.go internal/ui/app.go
git commit -m "perf(thread): drop bubbles/viewport in favor of manual yOffset slice

bubbles/viewport.SetContent ansi.StringWidth-walks every content line per
call. The messages package already documented this at ~55% CPU on j/k and
removed it; the thread panel was paying the same tax. Now uses manual
yOffset + slice from m.viewContent, matching internal/ui/messages."
```

---

## Phase 3 — Threads-List Row Cache

The threads-list is now panel-cache-friendly thanks to Task 2, but `threadsView.View()` itself still rebuilds **every row** on each cache miss (workspace switch, new WS event, debounce-fetch result), and the per-row work is heavy: `RenderSlackMarkdown` (multi-regex) + 8 fresh `lipgloss.NewStyle()` calls + 3 border renders + 2 time formats + `clipToWidth` (3× `lipgloss.Width`).

Phase 3 adds a per-row render cache and slices to the visible window only.

---

### Task 8: Build the threadsview row cache

**Files:**
- Modify: `internal/ui/threadsview/model.go` — Model struct, render path.

- [ ] **Step 1: Define a `rowEntry` and add cache fields to Model**

In `internal/ui/threadsview/model.go`, add near the top:

```go
// rowEntry holds the rendered output for one thread card so that on a plain
// j/k (which only changes m.selected) View() can flatten precomputed lines
// directly into the visible window without re-running RenderSlackMarkdown,
// formatRelTime, or any lipgloss render. linesNormal/linesSelected mirror
// the messages-pane convention.
type rowEntry struct {
	linesNormal   []string
	linesSelected []string
	height        int // == len(linesNormal); cardContentLines today
	// Inputs the cache was built against — used by buildCache to detect
	// stale entries and rebuild only the rows that changed.
	channelID    string
	threadTS     string
	parentText   string
	lastReplyTS  string
	unread       bool
	parentUserID string
	lastReplyBy  string
}
```

In the Model struct (`~74-94`), add fields:

```go
	// rowCache maps summary index -> precomputed rowEntry. Indexed in lockstep
	// with m.summaries; rebuilt selectively whenever a summary's identity or
	// dependent data has changed. linesNormal / linesSelected exist only for
	// the currently-selected row at the time of the build; non-selected rows
	// keep linesSelected == nil and View() falls back to building it on
	// demand (rare, only when the cursor crosses a row).
	rowCache       []rowEntry
	rowCacheWidth  int
	rowCacheUserNV uint64
	rowCacheChanNV uint64
	// userNamesV / channelNamesV bumped by Set* methods (Task 8 adds them).
	userNamesV    uint64
	channelNamesV uint64
	// separatorLine is the inter-row blank line, rebuilt with rowCache.
	separatorLine string
```

Replace `SetUserNames` / `SetChannelNames` with versions that bump these counters (the equality-checked variants from Task 2 already exist):

```go
func (m *Model) SetUserNames(names map[string]string) {
	if names == nil {
		names = map[string]string{}
	}
	if userNamesEqual(m.userNames, names) {
		return
	}
	m.userNames = names
	m.userNamesV++
	m.dirty()
}

func (m *Model) SetChannelNames(names map[string]string) {
	if names == nil {
		names = map[string]string{}
	}
	if userNamesEqual(m.channelNames, names) {
		return
	}
	m.channelNames = names
	m.channelNamesV++
	m.dirty()
}
```

- [ ] **Step 2: Add `buildRowCache(width int)` mirroring messages.buildCache**

Add a method to the Model:

```go
// buildRowCache rebuilds m.rowCache so it has one rowEntry per summary in
// m.summaries. Each row's linesNormal is the rendered card with the
// "invisible" left border (non-selected); linesSelected is the same content
// with the green ▌ border. Both are pre-split so View() can slice without
// further allocation. Skips per-row rebuilds when the existing rowEntry
// already matches the input identity + maps version.
//
// Mirrors internal/ui/messages.buildCache.
func (m *Model) buildRowCache(width int) {
	mustRebuild := m.rowCacheWidth != width ||
		m.rowCacheUserNV != m.userNamesV ||
		m.rowCacheChanNV != m.channelNamesV ||
		len(m.rowCache) != len(m.summaries)
	// Pre-build styles ONCE (currently rebuilt on every renderCard call).
	muted := lipgloss.NewStyle().Foreground(styles.TextMuted)
	channelName := lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
	unreadDot := lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
	borderInvis := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
		BorderForeground(styles.Background).BorderBackground(styles.Background)
	borderSelect := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
		BorderForeground(styles.Accent).BorderBackground(styles.Background)
	borderFill := lipgloss.NewStyle().Background(styles.Background)
	separatorStyle := lipgloss.NewStyle().Width(width).Background(styles.Background)

	if cap(m.rowCache) < len(m.summaries) {
		m.rowCache = make([]rowEntry, len(m.summaries))
	} else {
		m.rowCache = m.rowCache[:len(m.summaries)]
	}

	for i, s := range m.summaries {
		fresh := mustRebuild ||
			m.rowCache[i].channelID != s.ChannelID ||
			m.rowCache[i].threadTS != s.ThreadTS ||
			m.rowCache[i].parentText != s.ParentText ||
			m.rowCache[i].lastReplyTS != s.LastReplyTS ||
			m.rowCache[i].unread != s.Unread ||
			m.rowCache[i].parentUserID != s.ParentUserID ||
			m.rowCache[i].lastReplyBy != s.LastReplyBy
		if !fresh {
			continue
		}
		// renderCardLines builds the unbordered, unselected content of
		// the card (currently the body of renderCard minus the border
		// wrapping). It returns []string already split per line.
		body := m.renderCardLines(s, width-1, muted, channelName, unreadDot)
		// Apply each border to the joined body, then split.
		filled := borderFill.Width(width - 1).Render(strings.Join(body, "\n"))
		normal := strings.Split(borderInvis.Render(filled), "\n")
		selected := strings.Split(borderSelect.Render(filled), "\n")
		m.rowCache[i] = rowEntry{
			linesNormal:   normal,
			linesSelected: selected,
			height:        len(normal),
			channelID:     s.ChannelID,
			threadTS:      s.ThreadTS,
			parentText:    s.ParentText,
			lastReplyTS:   s.LastReplyTS,
			unread:        s.Unread,
			parentUserID:  s.ParentUserID,
			lastReplyBy:   s.LastReplyBy,
		}
	}

	m.separatorLine = separatorStyle.Render("")
	m.rowCacheWidth = width
	m.rowCacheUserNV = m.userNamesV
	m.rowCacheChanNV = m.channelNamesV
}
```

- [ ] **Step 3: Extract `renderCardLines` from `renderCard`**

Currently `renderCard` (`internal/ui/threadsview/model.go:449-506`) returns a single `string` with the border applied. Refactor it:

- Add a new `renderCardLines(s cache.ThreadSummary, contentWidth int, muted, channelName, unreadDot lipgloss.Style) []string` that returns the **unbordered** body lines (the existing renderCard's content without the final border wrapping). Pass in the pre-built styles so it doesn't rebuild them.
- Keep `renderCard` as a thin wrapper for callers that still want a single styled string (tests, primarily).

This means the body of the function moves into `renderCardLines`, and `renderCard` becomes:

```go
func (m *Model) renderCard(s cache.ThreadSummary, width int, selected bool) string {
	muted := lipgloss.NewStyle().Foreground(styles.TextMuted)
	channelName := lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
	unreadDot := lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
	body := m.renderCardLines(s, width-1, muted, channelName, unreadDot)
	borderFill := lipgloss.NewStyle().Background(styles.Background)
	filled := borderFill.Width(width - 1).Render(strings.Join(body, "\n"))
	if selected {
		return borderSelectStyle().Render(filled)
	}
	return borderInvisStyle().Render(filled)
}
```

This change is mechanical: cut everything in the existing `renderCard` from after the style helper calls down to (but not including) the final border wrapping into the new `renderCardLines` body, swap the `mutedStyle()` etc. helper calls for the passed-in styles, and have the function return `strings.Split(body, "\n")` instead of the joined string.

- [ ] **Step 4: Replace `renderRows` to consume the row cache**

Replace `renderRows` (`internal/ui/threadsview/model.go:425-435`):

```go
func (m *Model) renderRows(width int) []string {
	out := make([]string, 0, len(m.summaries)*cardStride)
	for i, s := range m.summaries {
		out = append(out, strings.Split(m.renderCard(s, width, i == m.selected), "\n")...)
		if i < len(m.summaries)-1 {
			out = append(out, lipgloss.NewStyle().Width(width).Background(styles.Background).Render(""))
		}
	}
	return out
}
```

with:

```go
// renderRows returns the FLAT line list for ALL summaries by reading the
// row cache. Selection is applied by picking linesSelected for the active
// row. Inter-row separators come from m.separatorLine (built in
// buildRowCache). Caller is responsible for invoking buildRowCache first.
func (m *Model) renderRows(width int) []string {
	out := make([]string, 0, len(m.summaries)*cardStride)
	for i := range m.summaries {
		var lines []string
		if i == m.selected {
			lines = m.rowCache[i].linesSelected
		} else {
			lines = m.rowCache[i].linesNormal
		}
		out = append(out, lines...)
		if i < len(m.summaries)-1 {
			out = append(out, m.separatorLine)
		}
	}
	return out
}
```

- [ ] **Step 5: Call `buildRowCache` from `View`**

In `View` (`internal/ui/threadsview/model.go:338-394`), insert at the top of the function (after the empty-state short-circuit):

```go
	m.buildRowCache(width)
```

- [ ] **Step 6: Run threadsview tests**

Run: `go test ./internal/ui/threadsview/`
Expected: PASS. The test `TestView_AllLinesUniformWidth` (`model_test.go:230-243`) is the strongest correctness gate — every line must remain exactly `width` columns wide.

- [ ] **Step 7: Run the threadsview scroll benchmark**

Run: `go test -run=^$ -bench=BenchmarkViewScroll -benchmem ./internal/ui/threadsview/`
Expected: ns/op should be **substantially lower** than the Task 1 baseline (target: order-of-magnitude improvement on the per-keystroke path, since the per-row cache hits on every j/k).

- [ ] **Step 8: Commit**

```bash
git add internal/ui/threadsview/model.go
git commit -m "perf(threadsview): per-row render cache with linesNormal/linesSelected

Was running RenderSlackMarkdown + 8 lipgloss.NewStyle() + 3 border renders
PER ROW, PER RENDER for every summary. Now built once per (row identity,
width, names version) and reused; j/k flips a slice pointer. Mirrors the
internal/ui/messages caching pattern."
```

---

### Task 9: Window the threadsview render to visible rows only

**Background:** Even with Task 8's row cache, `renderRows` still walks every summary and concatenates all rendered rows into the flat line list. With "every thread you've ever participated in" potentially numbering hundreds (`internal/cache/threads.go:36-68` has no `LIMIT`), this is O(N) work per render even when only ~10 cards fit on screen.

We add cumulative-line offsets and slice the visible window, exactly as `internal/ui/messages/model.go:1086-1097` and `:2143-2273` do.

**Files:**
- Modify: `internal/ui/threadsview/model.go` — Model struct (offsets/totalLines), buildRowCache (compute offsets), View (slice visible window).

- [ ] **Step 1: Add offset fields to Model**

```go
	// rowOffsets[i] is the absolute line index of the first line of row i in
	// the FLAT line list (linesNormal of each row + separators). totalLines
	// is the sum. Both are populated by buildRowCache and consumed by View
	// to slice the [yOffset, yOffset+height) visible window without walking
	// every row.
	rowOffsets []int
	totalLines int
```

- [ ] **Step 2: Populate offsets in `buildRowCache`**

At the bottom of `buildRowCache` (after the for-loop), add:

```go
	if cap(m.rowOffsets) < len(m.summaries) {
		m.rowOffsets = make([]int, len(m.summaries))
	} else {
		m.rowOffsets = m.rowOffsets[:len(m.summaries)]
	}
	off := 0
	for i, e := range m.rowCache {
		m.rowOffsets[i] = off
		off += e.height
		if i < len(m.summaries)-1 {
			off++ // separator
		}
	}
	m.totalLines = off
```

- [ ] **Step 3: Replace the body of `View` to slice the visible window**

Replace `View` (`internal/ui/threadsview/model.go:338-394`):

```go
func (m *Model) View(height, width int) string {
	if width < 1 {
		return ""
	}
	if len(m.summaries) == 0 {
		// existing empty-state path
		return m.emptyView(height, width)
	}
	lines := m.renderRows(width)
	// snap, clamp, slice, pad, Join — existing impl
	// ...
}
```

with:

```go
func (m *Model) View(height, width int) string {
	if width < 1 {
		return ""
	}
	if len(m.summaries) == 0 {
		return m.emptyView(height, width)
	}
	m.buildRowCache(width)

	// Snap yOffset to keep the selected card visible.
	if !m.hasSnapped || m.snappedSelection != m.selected {
		startLine := m.rowOffsets[m.selected]
		endLine := startLine + m.rowCache[m.selected].height
		if startLine < m.yOffset {
			m.yOffset = startLine
		} else if endLine > m.yOffset+height {
			m.yOffset = endLine - height
			if m.yOffset < 0 {
				m.yOffset = 0
			}
		}
		m.snappedSelection = m.selected
		m.hasSnapped = true
	}
	maxOffset := m.totalLines - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.yOffset > maxOffset {
		m.yOffset = maxOffset
	}
	if m.yOffset < 0 {
		m.yOffset = 0
	}

	// Walk only the rows whose [start, start+height) range intersects
	// [yOffset, yOffset+height).
	visible := make([]string, 0, height)
	want := height
	for i := range m.summaries {
		if want == 0 {
			break
		}
		entryStart := m.rowOffsets[i]
		entryEnd := entryStart + m.rowCache[i].height
		if entryEnd <= m.yOffset {
			continue
		}
		if entryStart >= m.yOffset+height {
			break
		}
		var lines []string
		if i == m.selected {
			lines = m.rowCache[i].linesSelected
		} else {
			lines = m.rowCache[i].linesNormal
		}
		from := 0
		if entryStart < m.yOffset {
			from = m.yOffset - entryStart
		}
		to := len(lines)
		if entryEnd > m.yOffset+height {
			to = len(lines) - (entryEnd - (m.yOffset + height))
		}
		visible = append(visible, lines[from:to]...)
		want = height - len(visible)
		// Separator (if its line is in the visible window).
		if i < len(m.summaries)-1 && want > 0 {
			sepAbs := entryEnd
			if sepAbs >= m.yOffset && sepAbs < m.yOffset+height {
				visible = append(visible, m.separatorLine)
				want--
			}
		}
	}
	// Pad to height with the separator (background-only) line.
	for len(visible) < height {
		visible = append(visible, m.separatorLine)
	}
	return strings.Join(visible, "\n")
}
```

(`m.emptyView` is the existing empty-state branch; extract it from the current `View` body if it's not already a method.)

- [ ] **Step 4: Run threadsview tests**

Run: `go test ./internal/ui/threadsview/`
Expected: PASS. `TestView_SnapsToSelectedOnOverflow` and `TestView_AllLinesUniformWidth` are the highest-value gates.

- [ ] **Step 5: Run the threadsview scroll benchmark**

Run: `go test -run=^$ -bench=BenchmarkViewScroll -benchmem ./internal/ui/threadsview/`
Expected: ns/op should be **dramatically lower** than the Task 8 result for large summary counts (200 in the bench), since we now visit only ~`height/cardStride` rows per render instead of all 200.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/threadsview/model.go
git commit -m "perf(threadsview): only render visible rows on each View call

Was walking every summary every render; now uses rowOffsets + manual
visible-window slice, mirroring internal/ui/messages.View. With 200 threads
and a 40-line viewport, this is the difference between O(200) and
O(viewport_height/cardStride) work per render."
```

---

## Phase 4 — End-to-End Verification

### Task 10: Full benchmark + integration sanity pass

**Files:**
- Read-only: all benchmarks; manual smoke run of the binary.

- [ ] **Step 1: Run the full benchmark suite for the three packages**

Run:
```bash
go test -run=^$ -bench=BenchmarkViewScroll -benchmem ./internal/ui/messages/ ./internal/ui/thread/ ./internal/ui/threadsview/
```

Expected: thread is in the same order of magnitude as messages; threadsview is lower than the Task 1 baseline by a wide margin. Capture the numbers.

- [ ] **Step 2: Run the full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 3: Build and smoke-test**

Run: `make build`
Expected: `bin/slk` produced, no errors.

Manually run `./bin/slk` against your workspace and:
- Open a long thread; press and hold `j`. Confirm scrolling is smooth (no visible per-keystroke lag).
- Open the threads list (`⚑ Threads`) and hold `j`. Confirm: (a) the right-pane parent updates immediately; (b) network requests do NOT fire per row (watch `~/.local/share/slk/log` if logging is enabled, or just observe Slack rate-limit headers via `:debug`); (c) cursor movement is smooth.

- [ ] **Step 4: Write a short benchmark-results note**

Append to the most recent commit message body, OR add a `docs/superpowers/specs/2026-04-30-thread-perf-results.md` summarizing:
- Baseline ns/op for each of the three benchmarks (from Task 1).
- Final ns/op after all phases.
- Speedup factor.

- [ ] **Step 5: Final commit (only if Step 4 wrote a docs file)**

```bash
git add docs/superpowers/specs/2026-04-30-thread-perf-results.md
git commit -m "docs: record before/after benchmarks for thread perf work"
```

---

## Background — file:line citations

These are the exact references the tasks above depend on. Verified by direct read at plan-write time.

### The cache-defeating bug (Task 2)

```
internal/ui/threadsview/model.go:117-123
  SetUserNames calls m.dirty() unconditionally — no equality check.
internal/ui/threadsview/model.go:132-138
  SetSelfUserID DOES have the equality check; this is the pattern to copy.
internal/ui/app.go:4072-4091
  Reads tvVersion (4072), calls SetUserNames (4078) which bumps to V+1,
  stores the cache under V (4091). Cache never hits.
```

### Per-keystroke HTTP fetch (Task 3)

```
internal/ui/app.go:3074-3118
  openSelectedThreadCmd. Dedup at 3084 only catches re-selecting the
  same row. Returns a tea.Cmd that runs the threadFetcher.
cmd/slk/main.go:654-660, 1438
  threadFetcher wiring -> client.GetReplies — Slack HTTP API call.
```

### Thread panel: bordered content rebuilt every j/k (Tasks 4-5)

```
internal/ui/thread/model.go:880-903
  Per-reply cache; only stores linesNormal.
internal/ui/thread/model.go:906
  View-level cache predicate: invalidates on m.viewSelected != m.selected.
internal/ui/thread/model.go:957-966
  The hot loop: per reply, strings.Join + borderFill.Render +
  borderInvis/Select.Render + lipgloss.Height.
internal/ui/messages/model.go:101-107
  viewEntry shape: linesNormal + linesSelected pre-built.
internal/ui/messages/model.go:1051-1056
  buildCache builds both variants once per cache build.
internal/ui/messages/model.go:2183-2187
  View hot path: just picks one slice or the other.
```

### Thread panel: bubbles/viewport.SetContent every render (Task 7)

```
internal/ui/thread/model.go:46
  m.vp viewport.Model field.
internal/ui/thread/model.go:993-997
  Every render: SetWidth, SetHeight, SetContent.
internal/ui/messages/model.go:272-276
  The comment block that documents this exact 55%-CPU finding.
```

### Thread panel: chrome rebuilt every render (Task 6)

```
internal/ui/thread/model.go:835-857
  Header + separator + parent message rebuilt fresh; runs the full
  RenderSlackMarkdown pipeline on the parent every frame.
internal/ui/thread/model.go:1019-1083
  renderThreadMessage — the heavy pipeline.
internal/ui/messages/render.go:341-379
  RenderSlackMarkdown internals (regex passes + emoji + ReapplyBg).
internal/ui/messages/model.go:258-264, 2005-2032
  chromeCache fields and chrome-rebuild block — the pattern to copy.
```

### Threadsview: no row cache, all rows rendered (Tasks 8-9)

```
internal/ui/threadsview/model.go:34-72
  Style helpers: rebuilt on every call.
internal/ui/threadsview/model.go:425-435
  renderRows: walks every summary unconditionally.
internal/ui/threadsview/model.go:449-506
  renderCard: per row, RenderSlackMarkdown + 3 border renders +
  2 formatRelTime + 8+ NewStyle calls.
internal/ui/threadsview/model.go:338-394
  View: builds the entire flat line list before slicing.
internal/cache/threads.go:36-68, 103-108
  ListInvolvedThreads: no LIMIT; sort.SliceStable in Go.
```
