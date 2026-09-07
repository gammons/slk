# Phase 0 — Test Safety Net Implementation Plan

> **Status: COMPLETE.** All 17 tasks implemented and reviewed on branch
> `refactor/phase0-safety-net` (base `79c78b9`). All nine exit criteria in
> [Verification](#verification-phase-exit-criteria) below are ticked with their
> achieved numbers. Summary of results is in the tracking document's Phase 0
> section.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the test safety net that makes Phases 1–5 of the architecture refactor verifiable: golden tests for `View()`, one test-app harness, flake elimination, mode-handler characterization, and a `messages`/`thread` lockstep test.

**Architecture:** Five independent PRs. 0b (harness) lands first because 0a builds on it. Everything is additive test code except one production change: a package-level clock in `internal/ui/messages`. No behavior changes.

**Tech Stack:** Go 1.26.5, bubbletea v2 (`charm.land/bubbletea/v2`), lipgloss v2, `github.com/charmbracelet/x/ansi` for ANSI stripping. Tests are plain `testing.T`, stdlib only, white-box (`package ui`).

**Spec:** [`../specs/2026-09-06-phase0-test-safety-net-design.md`](../specs/2026-09-06-phase0-test-safety-net-design.md)
**Tracking:** [`2026-09-06-architecture-refactor.md`](2026-09-06-architecture-refactor.md)

## Global Constraints

- Go 1.26.5. No new third-party dependencies — stdlib `testing` only. `github.com/charmbracelet/x/ansi` is already a direct dependency and may be used.
- Tests are white-box: `package ui`, `package messages`, not `package ui_test`.
- `gofmt -l .` must be empty. CI enforces it.
- CI runs `go test ./... -race`. Every task must leave it green.
- No `t.Parallel()` anywhere in this repo — do not introduce it. `styles` and `emoji` hold process-global mutable state.
- Exactly one production (non-`_test.go`) change is permitted in this entire plan: `internal/ui/messages` gains `nowFunc` + `SetNowFunc`. Any other production edit is out of scope — record it and raise it separately.
- Characterization tests record **current** behavior. If behavior looks wrong, add a `// BUG?:` comment and keep the assertion matching reality. Do not fix it here.
- Commit after every task. Use conventional-commit prefixes (`test:`, `refactor:`, `feat:`) matching repo style.
- **Partial completion is permitted but must be escalated, never silent.** Two tasks (2 and 11) name items that may prove impossible within the one-production-change budget. If you cannot complete a task item, you must: (a) write the specific item and the blocking reason into your report file, (b) return status `DONE_WITH_CONCERNS`, not `DONE`. A success criterion missed without a written exception is a failed task. Do not exceed the one-production-change budget to avoid escalating — escalate instead.

## File Structure

**PR 0b — harness**
- Create `internal/ui/testapp_test.go` — `newTestApp(t, opts...)` + option funcs + shared fixtures
- Modify 11 existing test files to convert their local builders into wrappers

**PR 0a — goldens**
- Modify `internal/ui/messages/model.go` — add `nowFunc` / `SetNowFunc` (the one production change)
- Create `internal/ui/messages/clock_test.go` — clock injection tests
- Create `internal/ui/golden_test.go` — `-update` flag, compare helper, `newGoldenApp`, scenario table
- Create `internal/ui/testdata/golden/*.ansi` — 8 golden files

**PR 0c — flakes**
- Modify `cmd/slk/user_resolver_test.go`, `cmd/slk/thread_subscriptions_test.go`, `internal/slack/membership/manager_test.go`, `internal/avatar/avatar_test.go`, `internal/wake/detector_test.go`, `internal/emoji/place_test.go`

**PR 0d — mode characterization**
- Create `internal/ui/modekeys_test.go` — shared table runner
- Create/modify one `internal/ui/mode_<name>_test.go` per mode

**PR 0e — lockstep**
- Create `internal/ui/thread/lockstep_test.go`

---

## PR 0b — Test harness

### Task 1: `newTestApp` builder

**Files:**
- Create: `internal/ui/testapp_test.go`

**Interfaces:**
- Consumes: nothing (first task)
- Produces:
  - `func newTestApp(t testing.TB, opts ...testOpt) *App` — `testing.TB`, not `*testing.T`, so the two benchmark builders (`makeBenchApp`, `makeWideScrollApp`) can route through it in Task 2
  - `type testOpt func(*testAppCfg)`
  - `func withSize(w, h int) testOpt`
  - `func withMessages(msgs ...messages.MessageItem) testOpt`
  - `func withChannels(items ...sidebar.ChannelItem) testOpt`
  - `func withMode(m Mode) testOpt`
  - `func withActiveChannel(id string) testOpt`
  - `func withRender() testOpt`
  - `func withChannelService(f ChannelServiceFuncs) testOpt`
  - `func withWindowSplit(dir wintree.Dir) testOpt`
  - `func withThreadsView(sums []cache.ThreadSummary) testOpt`
  - `func sampleMessages(n int) []messages.MessageItem`

- [ ] **Step 1: Write the failing test**

Create `internal/ui/testapp_test.go` with the test first (the builder comes in step 3):

```go
package ui

import (
	"fmt"
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/wintree"
)

func TestNewTestApp_Defaults(t *testing.T) {
	a := newTestApp(t)
	if a == nil {
		t.Fatal("newTestApp returned nil")
	}
	if a.width != 120 || a.height != 30 {
		t.Errorf("default size = %dx%d, want 120x30", a.width, a.height)
	}
	if a.mode != ModeNormal {
		t.Errorf("default mode = %v, want ModeNormal", a.mode)
	}
}

func TestNewTestApp_WithSizeAndMessages(t *testing.T) {
	a := newTestApp(t, withSize(200, 50), withMessages(sampleMessages(3)...))
	if a.width != 200 || a.height != 50 {
		t.Errorf("size = %dx%d, want 200x50", a.width, a.height)
	}
	if got := len(a.messagepane.Messages()); got != 3 {
		t.Errorf("message count = %d, want 3", got)
	}
}

func TestNewTestApp_WithRenderPopulatesLayout(t *testing.T) {
	a := newTestApp(t, withRender())
	if a.layout.sidebarEnd == 0 {
		t.Error("withRender did not populate layout bands")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui -run TestNewTestApp -v`
Expected: FAIL — `undefined: newTestApp`, `undefined: withSize`, `undefined: withMessages`, `undefined: withRender`, `undefined: sampleMessages`

- [ ] **Step 3: Write the builder**

Append to `internal/ui/testapp_test.go`:

```go
// testAppCfg is the accumulated configuration a testOpt mutates.
// Zero value is the default App: 120x30, Normal mode, no data.
type testAppCfg struct {
	w, h          int
	msgs          []messages.MessageItem
	channels      []sidebar.ChannelItem
	mode          Mode
	activeChannel string
	render        bool
	chanSvc       *ChannelServiceFuncs
	splits        []wintree.Dir
	threadSums    []cache.ThreadSummary
	hasThreadSums bool
}

type testOpt func(*testAppCfg)

func withSize(w, h int) testOpt { return func(c *testAppCfg) { c.w, c.h = w, h } }

func withMessages(msgs ...messages.MessageItem) testOpt {
	return func(c *testAppCfg) { c.msgs = msgs }
}

func withChannels(items ...sidebar.ChannelItem) testOpt {
	return func(c *testAppCfg) { c.channels = items }
}

func withMode(m Mode) testOpt { return func(c *testAppCfg) { c.mode = m } }

func withActiveChannel(id string) testOpt {
	return func(c *testAppCfg) { c.activeChannel = id }
}

// withRender calls View() once so a.layout bands and pane caches are
// populated. Required by any test that does mouse hit-testing.
func withRender() testOpt { return func(c *testAppCfg) { c.render = true } }

func withChannelService(f ChannelServiceFuncs) testOpt {
	return func(c *testAppCfg) { c.chanSvc = &f }
}

// withWindowSplit splits the window tree once per call, in order.
func withWindowSplit(dir wintree.Dir) testOpt {
	return func(c *testAppCfg) { c.splits = append(c.splits, dir) }
}

func withThreadsView(sums []cache.ThreadSummary) testOpt {
	return func(c *testAppCfg) { c.threadSums, c.hasThreadSums = sums, true }
}

// newTestApp builds an App for tests. Options are applied in a fixed
// order regardless of argument order: size, data, services, splits,
// mode, then render. This keeps construction deterministic — a test
// cannot accidentally depend on option ordering.
//
// Takes testing.TB rather than *testing.T so benchmarks can use it too
// (app_bench_test.go's builders route through this in Task 2).
func newTestApp(t testing.TB, opts ...testOpt) *App {
	t.Helper()
	cfg := testAppCfg{w: 120, h: 30, mode: ModeNormal}
	for _, o := range opts {
		o(&cfg)
	}

	a := NewApp()
	a.width, a.height = cfg.w, cfg.h

	if len(cfg.channels) > 0 {
		a.SetChannels(cfg.channels)
	}
	if cfg.chanSvc != nil {
		a.SetChannelService(NewChannelService(*cfg.chanSvc))
	}
	if len(cfg.msgs) > 0 {
		a.messagepane.SetMessages(cfg.msgs)
	}
	if cfg.hasThreadSums {
		a.threadsView.SetSummaries(cfg.threadSums)
	}
	if cfg.activeChannel != "" {
		a.activeChannelID = cfg.activeChannel
	}
	for _, d := range cfg.splits {
		_ = a.splitWindow(d)
	}
	if cfg.mode != ModeNormal {
		a.SetMode(cfg.mode)
	}
	if cfg.render {
		_ = a.View()
	}
	return a
}

// sampleMessages builds n plain messages with distinct TS values and
// greppable text ("msg-1", "msg-2", ...). Deliberately minimal: no
// reactions, attachments, or date grouping. Tests that need those
// build their own items.
func sampleMessages(n int) []messages.MessageItem {
	out := make([]messages.MessageItem, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, messages.MessageItem{
			TS:        fmt.Sprintf("%d.0", i),
			UserID:    "U1",
			UserName:  "alice",
			Text:      fmt.Sprintf("msg-%d", i),
			Timestamp: "1:00 PM",
		})
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui -run TestNewTestApp -v`
Expected: PASS — 3 tests

If `a.threadsView.SetSummaries` or `a.splitWindow` has a different signature than assumed, fix the call to match the real one — do not change production code.

- [ ] **Step 5: Verify nothing else broke**

Run: `go test ./internal/ui -race`
Expected: PASS (all existing tests still green — this task is purely additive)

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/ui/testapp_test.go
git add internal/ui/testapp_test.go
git commit -m "test(ui): add newTestApp options builder

Single source of truth for App construction in tests. Phase 4 of the
architecture refactor relocates App fields; without this, that change
would have to be reconciled across 15 ad-hoc builders."
```

---

### Task 2: Convert the 15 legacy builders to wrappers

**Files:**
- Modify: `internal/ui/app_selection_test.go:13` (`newTestAppWithMessages`)
- Modify: `internal/ui/app_threads_debounce_test.go:16` (`newTestAppWithThreadsView`)
- Modify: `internal/ui/reducer_search_test.go:22` (`searchTestApp`)
- Modify: `internal/ui/windows_test.go:10` (`newWideTestApp`)
- Modify: `internal/ui/channel_finder_search_test.go:12` (`newFinderApp`)
- Modify: `internal/ui/app_panelat_test.go:31` (`newPanelAtApp`)
- Modify: `internal/ui/sixelpaint_test.go:370` (`sixelTestApp`)
- Modify: `internal/ui/reducer_links_test.go:14` (`linkTestApp`)
- Modify: `internal/ui/fanout_test.go:23,85` (`twoWindowApp`, `sameChannelApp`)
- Modify: `internal/ui/reducer_modal_click_test.go:15` (`openChannelFinder`)
- Modify: `internal/ui/reducer_new_message_test.go:10` (`newApp_WithOpenConvCapture`)
- Modify: `internal/ui/app_bench_test.go:16,85` (`makeBenchApp`, `makeWideScrollApp`)
- Modify: `internal/ui/app_test.go:4215` (`setupAppForTitleTest`)

**Interfaces:**
- Consumes: everything Task 1 produces
- Produces: no new symbols. All 15 builder names and signatures are preserved exactly.

**Critical constraint:** do not change any test body. Only the builder bodies change. If a builder cannot be expressed with the current options, **add an option to Task 1's file** rather than changing the test that uses it.

- [ ] **Step 1: Establish the baseline**

Run: `go test ./internal/ui -race -count=1 2>&1 | tail -5`
Expected: PASS. Record the test count — it must be identical at the end.

- [ ] **Step 2: Convert one builder and verify**

Start with the simplest. In `internal/ui/app_selection_test.go`, replace the body of `newTestAppWithMessages` (currently lines 13–24) with:

```go
func newTestAppWithMessages(t *testing.T) *App {
	t.Helper()
	return newTestApp(t,
		withSize(120, 30),
		withMessages(
			messages.MessageItem{TS: "1.0", UserName: "alice", UserID: "U1", Text: "hello world", Timestamp: "1:00 PM"},
			messages.MessageItem{TS: "2.0", UserName: "bob", UserID: "U2", Text: "second message", Timestamp: "1:01 PM"},
		),
		withRender(),
	)
}
```

The exact `MessageItem` values must be preserved verbatim from the original — tests assert on "hello world" and on selection offsets that depend on this content.

- [ ] **Step 3: Verify that file's tests still pass**

Run: `go test ./internal/ui -run 'TestApp_(Drag|PlainClick|Copied|AutoScroll|FocusNext|SetMode|ToggleSidebar)' -v`
Expected: PASS — all tests in `app_selection_test.go`

- [ ] **Step 4: Repeat for the remaining 14 builders, one at a time**

For each: read the current body, express it with `newTestApp` options, run that file's tests, move on. Do **not** batch — a silent setup change is the main risk in this task and batching hides which conversion caused it.

Order (simplest first): `newPanelAtApp`, `sixelTestApp`, `newWideTestApp`, `linkTestApp`, `searchTestApp`, `openChannelFinder`, `newFinderApp`, `newApp_WithOpenConvCapture`, `newTestAppWithThreadsView`, `twoWindowApp`, `sameChannelApp`, `makeBenchApp`, `makeWideScrollApp`, `setupAppForTitleTest`.

Notes on the tricky ones:

- `newPanelAtApp()` and `sixelTestApp()` take no `*testing.T`. Keep their signatures. `newTestApp` needs a `*testing.T` only for `t.Helper()`, so give these a `t` parameter **only if** you can update their call sites without touching assertions — otherwise leave these two unconverted and record why in the commit message. Two unconverted builders is an acceptable outcome; changing test bodies is not.
- `twoWindowApp` / `sameChannelApp` return `(*App, wintree.LeafID, wintree.LeafID)`. Build with `withWindowSplit(wintree.SplitSideBySide)` and capture `a.focusedWin` between operations exactly as the originals do.
- `makeBenchApp` / `makeWideScrollApp` are used by benchmarks (`*testing.B`). Task 1 already declares `newTestApp(t testing.TB, ...)` for exactly this reason, so both convert without any signature change.

- [ ] **Step 5: Verify the full suite and the test count**

Run: `go test ./internal/ui -race -count=1 2>&1 | tail -5`
Expected: PASS with the same test count as Step 1.

Run: `git diff --stat`
Expected: only builder bodies changed. Confirm with `git diff` that no line inside a `func Test*` body was modified.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/ui/
go test ./internal/ui -race
git add internal/ui/
git commit -m "test(ui): route legacy test-app builders through newTestApp

The 15 ad-hoc builders become thin wrappers. No test body changes; all
helper names and signatures preserved. Consolidates App construction so
Phase 4 field relocations change one place instead of fifteen."
```

---

## PR 0a — Golden tests

### Task 3: Clock injection in `internal/ui/messages`

**Files:**
- Modify: `internal/ui/messages/model.go:3490` (`FormatDateSeparator`)
- Create: `internal/ui/messages/clock_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `func messages.SetNowFunc(fn func() time.Time)` — package-level clock override. `nil` reverts to `time.Now`.

**This is the only production change in the entire plan.**

- [ ] **Step 1: Write the failing test**

Create `internal/ui/messages/clock_test.go`:

```go
package messages

import (
	"testing"
	"time"
)

// fixedClock is the instant every golden and clock-dependent test
// anchors to: Sunday 2026-03-15 12:00 UTC.
func fixedClock() time.Time {
	return time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
}

func TestSetNowFunc_TodayAndYesterday(t *testing.T) {
	SetNowFunc(fixedClock)
	t.Cleanup(func() { SetNowFunc(nil) })

	if got := FormatDateSeparator("2026-03-15"); got != "Today" {
		t.Errorf("2026-03-15 = %q, want \"Today\"", got)
	}
	if got := FormatDateSeparator("2026-03-14"); got != "Yesterday" {
		t.Errorf("2026-03-14 = %q, want \"Yesterday\"", got)
	}
}

func TestSetNowFunc_WeekdayWithinAWeek(t *testing.T) {
	SetNowFunc(fixedClock)
	t.Cleanup(func() { SetNowFunc(nil) })

	// 2026-03-11 is 4 days before 2026-03-15, so days<7 -> weekday name.
	if got := FormatDateSeparator("2026-03-11"); got != "Wednesday" {
		t.Errorf("2026-03-11 = %q, want \"Wednesday\"", got)
	}
}

func TestSetNowFunc_NilRevertsToTimeNow(t *testing.T) {
	SetNowFunc(fixedClock)
	SetNowFunc(nil)

	today := time.Now().UTC().Format("2006-01-02")
	if got := FormatDateSeparator(today); got != "Today" {
		t.Errorf("after SetNowFunc(nil), today = %q, want \"Today\"", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/messages -run TestSetNowFunc -v`
Expected: FAIL — `undefined: SetNowFunc`

- [ ] **Step 3: Add the clock**

In `internal/ui/messages/model.go`, immediately above `func FormatDateSeparator` (line 3490), insert:

```go
// nowFunc is the clock FormatDateSeparator reads. Production leaves it
// as time.Now; tests override it via SetNowFunc so that day-divider
// labels ("Today", "Yesterday", weekday names) are deterministic.
//
// Mirrors the injectable clock sidebar.Model already carries
// (internal/ui/sidebar/model.go:500). Not guarded by a mutex: it is
// set from test setup before any render and reverted in t.Cleanup,
// and this package's tests do not run in parallel.
var nowFunc = time.Now

// SetNowFunc injects a clock for tests. Pass nil to revert to time.Now.
func SetNowFunc(fn func() time.Time) {
	if fn == nil {
		nowFunc = time.Now
		return
	}
	nowFunc = fn
}
```

Then change the single `time.Now()` call inside `FormatDateSeparator` (currently line 3497) from:

```go
	now := time.Now()
```

to:

```go
	now := nowFunc()
```

Leave every other `time.Now()` in the package alone — they are perf instrumentation (`bwT0`, `plT0`, `bodyT0`, `reactT0`, `bkT0`, `lgT0`, `attT0`), not render-visible.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/messages -run TestSetNowFunc -v`
Expected: PASS — 3 tests

Run: `go test ./internal/ui/messages -race`
Expected: PASS — in particular `TestFormatDateSeparatorAcrossTimezones` must still pass unchanged.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ui/messages/
git add internal/ui/messages/model.go internal/ui/messages/clock_test.go
git commit -m "feat(messages): injectable clock for FormatDateSeparator

FormatDateSeparator picks Today/Yesterday/weekday/absolute from
time.Now, so a golden containing a day divider would rot at midnight.
Mirrors the existing sidebar.Model.SetNowFunc shape.

Only production change in Phase 0 of the architecture refactor."
```

---

### Task 4: Golden compare helper

**Files:**
- Create: `internal/ui/golden_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `var updateGolden = flag.Bool("update", false, ...)`
  - `func compareGolden(t *testing.T, name, got string)`
  - `func firstLineDiff(want, got string) string`
  - `func firstByteDiff(want, got string) string`

The helper is written and tested *before* any scenario exists, because a
golden suite whose failure output is unreadable gets blessed blind.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/golden_test.go`:

```go
package ui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstLineDiff_ReportsFirstDifferingLine(t *testing.T) {
	want := "alpha\nbravo\ncharlie"
	got := "alpha\nBRAVO\ncharlie"
	out := firstLineDiff(want, got)
	if !strings.Contains(out, "line 2") {
		t.Errorf("expected line 2 in %q", out)
	}
	if !strings.Contains(out, "bravo") || !strings.Contains(out, "BRAVO") {
		t.Errorf("expected both values in %q", out)
	}
}

func TestFirstLineDiff_ReportsLineCountMismatch(t *testing.T) {
	out := firstLineDiff("a\nb", "a\nb\nc")
	if !strings.Contains(out, "line count") {
		t.Errorf("expected line-count message in %q", out)
	}
}

func TestFirstLineDiff_EmptyWhenEqual(t *testing.T) {
	if out := firstLineDiff("same", "same"); out != "" {
		t.Errorf("expected empty diff, got %q", out)
	}
}

func TestFirstByteDiff_ReportsOffsetAndHex(t *testing.T) {
	want := "plain \x1b[31mred\x1b[0m"
	got := "plain \x1b[32mred\x1b[0m"
	out := firstByteDiff(want, got)
	if !strings.Contains(out, "byte 8") {
		t.Errorf("expected byte offset 8 in %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui -run 'TestFirst(Line|Byte)Diff' -v`
Expected: FAIL — `undefined: firstLineDiff`, `undefined: firstByteDiff`

- [ ] **Step 3: Write the helper**

Append to `internal/ui/golden_test.go`:

```go
// updateGolden re-blesses every golden file this run touches.
//   go test ./internal/ui -run TestGolden -update
var updateGolden = flag.Bool("update", false, "rewrite golden files from current output")

// goldenDir is where .ansi goldens live, relative to this package.
const goldenDir = "testdata/golden"

// compareGolden asserts got matches testdata/golden/<name>.ansi byte
// for byte, or rewrites it under -update.
//
// Failure output is deliberately two-tier. Goldens store raw ANSI, so a
// naive diff of a styling-only regression is an unreadable wall of
// escape sequences and gets blessed without being read. When the
// stripped text matches, we say so explicitly and point at the byte.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(goldenDir, name+".ansi")

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		t.Logf("updated %s (%d bytes, %d lines)", path, len(got), strings.Count(got, "\n")+1)
		return
	}

	wantB, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing or unreadable: %v\n"+
			"bless it with: go test ./internal/ui -run TestGolden -update", path, err)
	}
	want := string(wantB)
	if want == got {
		return
	}

	if d := firstLineDiff(stripANSI(want), stripANSI(got)); d != "" {
		t.Errorf("golden %s: rendered text differs\n%s", path, d)
		return
	}

	t.Errorf("golden %s: content identical, STYLING differs\n%s\n"+
		"A style regression (selection highlight, unread bold, muted dim) "+
		"is the usual cause. Do not bless this without reading it.",
		path, firstByteDiff(want, got))
}

// firstLineDiff returns a human-readable description of the first
// differing line, or "" when the inputs are equal.
func firstLineDiff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	n := len(wl)
	if len(gl) < n {
		n = len(gl)
	}
	for i := 0; i < n; i++ {
		if wl[i] != gl[i] {
			return fmt.Sprintf("first difference at line %d:\n  want: %q\n  got:  %q", i+1, wl[i], gl[i])
		}
	}
	if len(wl) != len(gl) {
		return fmt.Sprintf("line count differs: want %d lines, got %d", len(wl), len(gl))
	}
	return ""
}

// firstByteDiff locates the first differing byte and prints a quoted
// window around it in both inputs.
func firstByteDiff(want, got string) string {
	n := len(want)
	if len(got) < n {
		n = len(got)
	}
	for i := 0; i < n; i++ {
		if want[i] != got[i] {
			lo := i - 40
			if lo < 0 {
				lo = 0
			}
			hiW, hiG := i+40, i+40
			if hiW > len(want) {
				hiW = len(want)
			}
			if hiG > len(got) {
				hiG = len(got)
			}
			return fmt.Sprintf("first differing byte %d:\n  want: %q\n  got:  %q",
				i, want[lo:hiW], got[lo:hiG])
		}
	}
	return fmt.Sprintf("byte length differs: want %d, got %d", len(want), len(got))
}
```

- [ ] **Step 4: Add the ANSI strip helper**

`stripANSI` is used above. Add it to the same file:

```go
// stripANSI removes SGR/OSC sequences so a text-level diff is readable.
func stripANSI(s string) string { return ansi.Strip(s) }
```

and add `"github.com/charmbracelet/x/ansi"` to the import block. This is
already a direct dependency (`internal/ui/help/footer_test.go:14` uses it).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui -run 'TestFirst(Line|Byte)Diff' -v`
Expected: PASS — 4 tests

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/ui/golden_test.go
git add internal/ui/golden_test.go
git commit -m "test(ui): golden compare helper with two-tier diff output

Goldens store raw ANSI. A styling-only regression diffed naively is an
unreadable wall of escapes that gets blessed without review, so when
stripped text matches we say so explicitly and point at the byte."
```

---

### Task 5: `newGoldenApp` determinism pins

**Files:**
- Modify: `internal/ui/golden_test.go`

**Interfaces:**
- Consumes: `newTestApp` (Task 1), `messages.SetNowFunc` (Task 3)
- Produces: `func newGoldenApp(t *testing.T, opts ...testOpt) *App`

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/golden_test.go`:

```go
func TestNewGoldenApp_IsDeterministic(t *testing.T) {
	a1 := newGoldenApp(t, withMessages(goldenMessages()...), withRender())
	a2 := newGoldenApp(t, withMessages(goldenMessages()...), withRender())
	if a1.View().Content != a2.View().Content {
		t.Error("two identically-configured golden apps rendered differently")
	}
}

func TestNewGoldenApp_PinsDateSeparatorClock(t *testing.T) {
	// goldenMessages dates rows on 2026-03-15, the pinned "today".
	a := newGoldenApp(t, withMessages(goldenMessages()...), withRender())
	if !strings.Contains(stripANSI(a.View().Content), "Today") {
		t.Error("expected a 'Today' day divider under the pinned clock")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui -run TestNewGoldenApp -v`
Expected: FAIL — `undefined: newGoldenApp`, `undefined: goldenMessages`

- [ ] **Step 3: Write `newGoldenApp` and the shared fixture**

Append to `internal/ui/golden_test.go`:

```go
// goldenClock is the instant every golden anchors to:
// Sunday 2026-03-15 12:00 UTC.
func goldenClock() time.Time {
	return time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
}

// newGoldenApp is newTestApp plus every global the render path reads,
// pinned and reverted. A golden is only meaningful if these are fixed;
// anything not pinned here shows up as golden flakiness.
func newGoldenApp(t *testing.T, opts ...testOpt) *App {
	t.Helper()

	// Theme: package-global, mutated in production by
	// mode_theme_switcher.go and in tests by reducer_search_test.go:437.
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	// Emoji image mode: package-global. When on, emoji render as kitty
	// APC escapes.
	emoji.SetImageMode(false, 2)
	t.Cleanup(func() { emoji.SetImageMode(false, 2) })

	// Day-divider clock.
	messages.SetNowFunc(goldenClock)
	t.Cleanup(func() { messages.SetNowFunc(nil) })

	// Sidebar staleness clock.
	a := newTestApp(t, opts...)
	a.sidebar.SetNowFunc(goldenClock)

	// Per-App nondeterminism.
	a.spinnerFrame = 0
	a.avatarFn = nil
	a.imgProtocol = imgpkg.ProtoNone
	a.SetNowTimestampFormatter(func() string { return "12:00 PM" })

	return a
}

// goldenMessages is the shared fixture for every scenario: one message
// per interesting render branch, so a single content edit re-blesses
// all scenarios consistently instead of letting them drift.
func goldenMessages() []messages.MessageItem {
	return []messages.MessageItem{
		{
			TS: "1710460800.000100", UserID: "U1", UserName: "alice",
			Text: "morning all", Timestamp: "9:00 AM", DateStr: "2026-03-14",
		},
		{
			TS: "1710547200.000100", UserID: "U2", UserName: "bob",
			Text: "shipped the thing", Timestamp: "9:00 AM", DateStr: "2026-03-15",
			Reactions: []messages.ReactionItem{
				{Emoji: "tada", Count: 3, UserIDs: []string{"U1", "U3", "U4"}},
				{Emoji: "eyes", Count: 1, HasReacted: true, UserIDs: []string{"U1"}},
			},
		},
		{
			TS: "1710547260.000100", UserID: "U3", UserName: "carol",
			Text: "nice — see thread", Timestamp: "9:01 AM", DateStr: "2026-03-15",
			ThreadTS: "1710547260.000100", ReplyCount: 4,
		},
		{
			TS: "1710547320.000100", UserID: "B1", UserName: "deploybot",
			Text: "build #421 green", Timestamp: "9:02 AM", DateStr: "2026-03-15",
			Attachments: []messages.Attachment{
				{Kind: "file", Name: "build.log", URL: "https://example.invalid/build.log", Size: 20480},
			},
		},
		{
			TS: "1710547380.000100", UserID: "U1", UserName: "alice",
			Text: "this line is deliberately long enough that it must wrap at every width the golden scenarios exercise, which is what pins the wrapping behaviour",
			Timestamp: "9:03 AM", DateStr: "2026-03-15", IsEdited: true,
		},
	}
}

// goldenChannels is the shared sidebar fixture.
func goldenChannels() []sidebar.ChannelItem {
	return []sidebar.ChannelItem{
		{ID: "C1", Name: "general", Type: "channel", Section: "Channels"},
		{ID: "C2", Name: "engineering", Type: "channel", Section: "Channels", IsStarred: true},
		{ID: "C3", Name: "muted-noise", Type: "channel", Section: "Channels", IsMuted: true},
		{ID: "D1", Name: "bob", Type: "dm", Section: "DMs", Presence: "active", DMUserID: "U2"},
		{ID: "D2", Name: "carol", Type: "dm", Section: "DMs", Presence: "away", DMUserID: "U3"},
	}
}
```

Add to the import block: `"time"`, `"github.com/gammons/slk/internal/config"`,
`"github.com/gammons/slk/internal/emoji"`, `imgpkg "github.com/gammons/slk/internal/image"`,
`"github.com/gammons/slk/internal/ui/messages"`, `"github.com/gammons/slk/internal/ui/sidebar"`,
`"github.com/gammons/slk/internal/ui/styles"`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui -run TestNewGoldenApp -v`
Expected: PASS — 2 tests

If `a.sidebar.SetNowFunc` is not reachable (unexported field on a value, not pointer), use whatever accessor `internal/ui/sidebar` exposes; do not add production code.

- [ ] **Step 5: Verify determinism under repetition**

Run: `go test ./internal/ui -run TestNewGoldenApp -count=20`
Expected: PASS — 20 consecutive runs. Any failure here is unpinned global state; find it now, not after 8 goldens exist.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/ui/golden_test.go
git add internal/ui/golden_test.go
git commit -m "test(ui): newGoldenApp with pinned render globals

Pins theme, emoji image mode, both clocks, spinner frame, avatar func
and image protocol. Shared message and channel fixtures so scenarios
re-bless consistently instead of drifting apart."
```

---

### Task 6: Scenario table and the first four goldens

**Files:**
- Modify: `internal/ui/golden_test.go`
- Create: `internal/ui/testdata/golden/base.ansi`
- Create: `internal/ui/testdata/golden/thread_open.ansi`
- Create: `internal/ui/testdata/golden/wide.ansi`
- Create: `internal/ui/testdata/golden/narrow.ansi`

**Interfaces:**
- Consumes: `newGoldenApp`, `goldenMessages`, `goldenChannels`, `compareGolden`
- Produces: `func TestGolden(t *testing.T)` — the table other tasks extend; `type goldenScenario struct{ name string; w, h int; build func(*testing.T) *App }`

- [ ] **Step 1: Write the scenario table**

Append to `internal/ui/golden_test.go`:

```go
// goldenScenario is one full-screen render pinned to a file.
// Full-screen rather than per-region because composition — panel
// order, width distribution, border placement, overlay compositing —
// is what View() actually does, and is what the later refactor phases
// threaten.
type goldenScenario struct {
	name  string
	w, h  int
	build func(t *testing.T) *App
}

func goldenScenarios() []goldenScenario {
	return []goldenScenario{
		{
			name: "base", w: 120, h: 30,
			build: func(t *testing.T) *App {
				return newGoldenApp(t,
					withSize(120, 30),
					withChannels(goldenChannels()...),
					withMessages(goldenMessages()...),
					withActiveChannel("C1"),
					withRender(),
				)
			},
		},
		{
			name: "thread_open", w: 120, h: 30,
			build: func(t *testing.T) *App {
				a := newGoldenApp(t,
					withSize(120, 30),
					withChannels(goldenChannels()...),
					withMessages(goldenMessages()...),
					withActiveChannel("C1"),
				)
				msgs := goldenMessages()
				a.threadPanel.SetThread(msgs[2], msgs[3:5], "C1", "1710547260.000100")
				a.threadVisible = true
				_ = a.View()
				return a
			},
		},
		{
			name: "wide", w: 200, h: 50,
			build: func(t *testing.T) *App {
				a := newGoldenApp(t,
					withSize(200, 50),
					withChannels(goldenChannels()...),
					withMessages(goldenMessages()...),
					withActiveChannel("C1"),
				)
				msgs := goldenMessages()
				a.threadPanel.SetThread(msgs[2], msgs[3:5], "C1", "1710547260.000100")
				a.threadVisible = true
				_ = a.View()
				return a
			},
		},
		{
			// 80x24 forces layout.Compute to set ThreadAutoHidden, which
			// View() acts on at app.go:2726 by clearing threadVisible and
			// falling focus back to PanelMessages.
			name: "narrow", w: 80, h: 24,
			build: func(t *testing.T) *App {
				a := newGoldenApp(t,
					withSize(80, 24),
					withChannels(goldenChannels()...),
					withMessages(goldenMessages()...),
					withActiveChannel("C1"),
				)
				a.threadVisible = true
				a.focusedPanel = PanelThread
				_ = a.View()
				return a
			},
		},
	}
}

func TestGolden(t *testing.T) {
	for _, sc := range goldenScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			a := sc.build(t)
			compareGolden(t, sc.name, a.View().Content)
		})
	}
}
```

- [ ] **Step 2: Run to verify it fails with a clear message**

Run: `go test ./internal/ui -run TestGolden -v`
Expected: FAIL — 4 subtests, each `golden testdata/golden/<name>.ansi missing or unreadable ... bless it with: go test ./internal/ui -run TestGolden -update`

This confirms the missing-golden path produces an actionable message rather than a panic.

- [ ] **Step 3: Bless the goldens**

Run: `go test ./internal/ui -run TestGolden -update -v`
Expected: PASS — 4 subtests, each logging `updated testdata/golden/<name>.ansi (N bytes, M lines)`

- [ ] **Step 4: Read the goldens before trusting them**

Run: `for f in internal/ui/testdata/golden/*.ansi; do echo "=== $f ==="; sed 's/\x1b\[[0-9;]*m//g' "$f"; done`

Inspect each by eye. Verify:
- `base` shows the sidebar with `general`/`engineering`/`muted-noise`/`bob`/`carol`, a `Yesterday` and a `Today` divider, the reaction pills, the file attachment, and the wrapped long line.
- `thread_open` has three panes.
- `wide` is 200 columns.
- `narrow` has **no** thread pane (auto-hidden) and is 24 lines.

A golden that does not show what you expect is a bug caught now rather than a wrong baseline frozen forever. If one is wrong, fix the scenario and re-bless.

- [ ] **Step 5: Verify they now pass and are stable**

Run: `go test ./internal/ui -run TestGolden -count=10`
Expected: PASS — 10 consecutive runs

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/ui/golden_test.go
git add internal/ui/golden_test.go internal/ui/testdata/golden/
git commit -m "test(ui): golden scenarios base, thread_open, wide, narrow

Full-screen View() snapshots in raw ANSI. narrow pins the
ThreadAutoHidden collapse and the focus fallback at app.go:2726."
```

---

### Task 7: The remaining four goldens

**Files:**
- Modify: `internal/ui/golden_test.go`
- Create: `internal/ui/testdata/golden/no_sidebar.ansi`
- Create: `internal/ui/testdata/golden/drag_selection.ansi`
- Create: `internal/ui/testdata/golden/overlay_finder.ansi`
- Create: `internal/ui/testdata/golden/window_split.ansi`

**Interfaces:**
- Consumes: everything from Task 6
- Produces: no new symbols — four more entries in `goldenScenarios()`

- [ ] **Step 1: Add the four scenarios**

Add these entries to the slice returned by `goldenScenarios()`, before the closing `}`:

```go
		{
			name: "no_sidebar", w: 120, h: 30,
			build: func(t *testing.T) *App {
				a := newGoldenApp(t,
					withSize(120, 30),
					withChannels(goldenChannels()...),
					withMessages(goldenMessages()...),
					withActiveChannel("C1"),
				)
				a.sidebarVisible = false
				a.focusedPanel = PanelMessages
				_ = a.View()
				return a
			},
		},
		{
			// The whole point of storing raw ANSI: this scenario's diff
			// from "base" is entirely escape sequences.
			name: "drag_selection", w: 120, h: 30,
			build: func(t *testing.T) *App {
				a := newGoldenApp(t,
					withSize(120, 30),
					withChannels(goldenChannels()...),
					withMessages(goldenMessages()...),
					withActiveChannel("C1"),
					withRender(),
				)
				x := a.layout.sidebarEnd + 2
				y := 4
				_, _ = a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				_, _ = a.Update(tea.MouseMotionMsg{X: x + 25, Y: y + 2, Button: tea.MouseLeft})
				_ = a.View()
				return a
			},
		},
		{
			name: "overlay_finder", w: 120, h: 30,
			build: func(t *testing.T) *App {
				a := newGoldenApp(t,
					withSize(120, 30),
					withChannels(goldenChannels()...),
					withMessages(goldenMessages()...),
					withActiveChannel("C1"),
				)
				a.channelFinder.SetItems([]channelfinder.Item{
					{ID: "C1", Name: "general", Type: "channel", Joined: true, LastVisited: 300},
					{ID: "C2", Name: "engineering", Type: "channel", Joined: true, LastVisited: 200},
					{ID: "C3", Name: "muted-noise", Type: "channel", Joined: true, LastVisited: 100},
				})
				a.channelFinder.Open()
				a.SetMode(ModeChannelFinder)
				_ = a.View()
				return a
			},
		},
		{
			name: "window_split", w: 160, h: 40,
			build: func(t *testing.T) *App {
				a := newGoldenApp(t,
					withSize(160, 40),
					withChannels(goldenChannels()...),
					withActiveChannel("C1"),
				)
				_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
				a.messagepane.SetMessages(goldenMessages())
				_ = a.splitWindow(wintree.SplitSideBySide)
				_, _ = a.Update(ChannelSelectedMsg{ID: "C2", Name: "engineering", Type: "channel"})
				a.messagepane.SetMessages(goldenMessages()[:2])
				_ = a.View()
				return a
			},
		},
```

Add to the import block: `tea "charm.land/bubbletea/v2"`,
`"github.com/gammons/slk/internal/ui/channelfinder"`,
`"github.com/gammons/slk/internal/ui/wintree"`.

- [ ] **Step 2: Bless and inspect**

Run: `go test ./internal/ui -run TestGolden -update -v`
Expected: PASS — 8 subtests

Run: `for f in no_sidebar drag_selection overlay_finder window_split; do echo "=== $f ==="; sed 's/\x1b\[[0-9;]*m//g' internal/ui/testdata/golden/$f.ansi; done`

Verify by eye:
- `no_sidebar` has no channel list.
- `drag_selection` stripped output should be **nearly identical to `base`** — the difference is in the escapes. Confirm with:
  `diff <(sed 's/\x1b\[[0-9;]*m//g' internal/ui/testdata/golden/base.ansi) <(sed 's/\x1b\[[0-9;]*m//g' internal/ui/testdata/golden/drag_selection.ansi)`
  A small or empty diff here is *correct*, and is exactly why the golden stores raw bytes.
- `overlay_finder` shows a centered box over a dimmed background.
- `window_split` shows two message panes side by side with different borders (focused vs unfocused).

- [ ] **Step 3: Verify `drag_selection` actually captured a selection**

Run: `grep -c $'\x1b' internal/ui/testdata/golden/drag_selection.ansi`
Then compare byte sizes:
`wc -c internal/ui/testdata/golden/base.ansi internal/ui/testdata/golden/drag_selection.ansi`

Expected: `drag_selection.ansi` is **larger** than `base.ansi`. If they are byte-identical, the drag did not register — fix the coordinates (see `app_selection_test.go:53-57` for the offset reasoning) and re-bless. A `drag_selection` golden identical to `base` pins nothing.

- [ ] **Step 4: Run the whole suite**

Run: `go test ./internal/ui -race -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ui/golden_test.go
git add internal/ui/golden_test.go internal/ui/testdata/golden/
git commit -m "test(ui): golden scenarios no_sidebar, drag_selection, overlay_finder, window_split

drag_selection differs from base only in escape sequences, which is the
case that justifies storing raw ANSI rather than stripped text."
```

---

### Task 8: Blank-screen guard and perturbation proof

**Files:**
- Modify: `internal/ui/golden_test.go`

**Interfaces:**
- Consumes: `goldenScenarios`
- Produces: `func TestGoldenFilesAreWellFormed(t *testing.T)`

A snapshot suite dies when someone blesses an empty file. This guard makes
that impossible, and the perturbation check proves the goldens can actually fail.

- [ ] **Step 1: Write the guard test**

Append to `internal/ui/golden_test.go`:

```go
// TestGoldenFilesAreWellFormed catches the classic snapshot-suite death:
// a golden blessed from a blank or truncated render. Every golden must
// be non-empty and exactly as tall as its scenario.
func TestGoldenFilesAreWellFormed(t *testing.T) {
	for _, sc := range goldenScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			path := filepath.Join(goldenDir, sc.name+".ansi")
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			if len(b) == 0 {
				t.Fatalf("%s is empty", path)
			}
			lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
			if len(lines) != sc.h {
				t.Errorf("%s has %d lines, want %d (scenario height)", path, len(lines), sc.h)
			}
			if strings.TrimSpace(stripANSI(string(b))) == "" {
				t.Errorf("%s renders as entirely blank", path)
			}
		})
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test ./internal/ui -run TestGoldenFilesAreWellFormed -v`
Expected: PASS — 8 subtests

If a height assertion fails, the scenario's declared `h` disagrees with what
`View()` produced. Determine which is right before changing either.

- [ ] **Step 3: Prove the goldens can fail — layout perturbation**

Temporarily reverse the panel order in `internal/ui/app.go`'s `View()` (swap
two `panels = append(...)` calls), then:

Run: `go test ./internal/ui -run TestGolden -v`
Expected: **FAIL** on multiple scenarios with a readable `first difference at line N` message.

Revert the change:
Run: `git checkout internal/ui/app.go`
Run: `go test ./internal/ui -run TestGolden`
Expected: PASS

- [ ] **Step 4: Prove the goldens can fail — style perturbation**

This is the important one, because it exercises the styling-only branch.
Temporarily change `SelectionStyle()` in `internal/ui/styles/styles.go` to use a
different background color, then:

Run: `go test ./internal/ui -run 'TestGolden/drag_selection' -v`
Expected: **FAIL** with `content identical, STYLING differs` and a `first differing byte` window.

If it instead reports a text diff, the perturbation changed layout too — pick a
pure-color change. If it **passes**, `drag_selection` is not capturing selection
styling; go back to Task 7 Step 3.

Revert:
Run: `git checkout internal/ui/styles/styles.go`
Run: `go test ./internal/ui -run TestGolden`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ui/golden_test.go
git status --short   # must show ONLY golden_test.go; both perturbations reverted
git add internal/ui/golden_test.go
git commit -m "test(ui): well-formedness guard for golden files

Asserts each golden is non-empty, exactly its scenario height, and not
blank after ANSI stripping. Verified both failure modes by perturbation:
panel reorder trips the text diff, selection-style change trips the
styling-only branch."
```

---

## PR 0c — Flake elimination

**The rule for every task in this PR:** a test may wait on a signal
indefinitely — `go test`'s own timeout is the backstop — but may not assert
that work completed within a wall-clock budget. Replace `time.Sleep` +
"probably done by now" with a channel the code under test closes.

**CORRECTION (post-Task-9, confirmed by two independent reviews). The
"signal from the fake's method" idiom below is WRONG and reintroduces the
race it was meant to remove.** `userResolver.flush` calls
`batcher.UsersInfo` at `cmd/slk/main.go:514` but only reaches
`applyEdgeUser` — which writes the cache row — at `:537`. A channel closed
inside the fake's `UsersInfo` therefore fires strictly *before* any row
exists.

**Signal from the last thing PRODUCTION code invokes on the path under
test, not from the fake's entry point.** For the resolver that is the
`send` callback: both `resolveOne` (upsert `:461` → send `:471`) and
`applyEdgeUser` (upsert → send `:616`) write and then send, so observing
the `ui.UserResolvedMsg` proves the write landed.

Prefer barriers production reaches *unconditionally*. `resolveOne`'s error
branch returns without sending, so a barrier on its `send` has no sender if
a round trip fails — acceptable against a static `httptest` server, but a
hang rather than a failure.

**Also: do not grep only for `time.Sleep`/`time.After`.** Task 9's real
flake was invisible to that grep — it was `t.TempDir` cleanup racing
goroutines that outlived the test body
(`TempDir RemoveAll cleanup: directory not empty`). Look for un-awaited
async work, not just timing constructs.

### Task 9: `cmd/slk/user_resolver_test.go`

**Files:**
- Modify: `cmd/slk/user_resolver_test.go` (8 `time.Sleep`, 6 `time.After`)

**Interfaces:**
- Consumes: nothing
- Produces: no shared helper. Each wait is an inline `<-ch` receive.

**Pre-flight amendment (agreed before execution):** an earlier draft of this
plan introduced an `awaitClose(t, ch, what)` helper and instructed Tasks 11–12
to copy it into three more packages. That is verbatim duplication of a logic
block — the exact anti-pattern this refactor exists to remove — for a helper
whose body is a single channel receive. **Do not create it.** Write the receive
inline at each call site with a comment stating why there is no timeout.

- [ ] **Step 1: Reproduce the flake**

Run: `go test ./cmd/slk -run TestUserResolver_RequestDoesNotBlockTheCaller -race -count=30`
Expected: mostly PASS. Then reproduce under load — run the full suite concurrently in another shell while repeating:
`go test ./... -race -count=1 & go test ./cmd/slk -run TestUserResolver -race -count=20`
Expected: at least one FAIL. Record the failure output; you need it to know the fix worked.

- [ ] **Step 2: Establish the inline wait idiom**

There is no helper. At each wait site, write the receive directly with a comment
naming the event. Use this form throughout Tasks 9–12:

```go
	// No timeout by design: `go test` already imposes one (default 10m),
	// and a wall-clock budget inside the test is exactly what made this
	// load-sensitive. A hang here produces a goroutine dump naming the
	// stuck test, which beats "expected 2 calls, got 1".
	<-b.flushed // edge users/info batch flushed
```

- [ ] **Step 3: Convert the batch-window sleeps**

Every `time.Sleep(userResolverBatchWindow + 300*time.Millisecond)` is waiting
for `userResolver.flush` to run. Replace with a signal from the fake batcher.
Find the fake batcher type in this file and add a `flushed chan struct{}`
field, closing it at the end of its `UsersInfo` implementation:

```go
type fakeBatcher struct {
	mu      sync.Mutex
	calls   [][]string
	users   []edge.User
	err     error
	flushed chan struct{} // closed after the first UsersInfo call returns
	once    sync.Once
}

func (f *fakeBatcher) UsersInfo(ctx context.Context, updated map[string]int64) ([]edge.User, error) {
	f.mu.Lock()
	ids := make([]string, 0, len(updated))
	for id := range updated {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	f.calls = append(f.calls, ids)
	f.mu.Unlock()
	f.once.Do(func() {
		if f.flushed != nil {
			close(f.flushed)
		}
	})
	return f.users, f.err
}
```

Adapt to the fake's actual current shape — do not replace fields it already has.

Then at each call site, replace:

```go
	time.Sleep(userResolverBatchWindow + 300*time.Millisecond)
```

with:

```go
	<-b.flushed // edge users/info batch flushed
```

- [ ] **Step 4: Convert `TestUserResolver_RequestDoesNotBlockTheCaller`**

This is the confirmed flake. Its current shape asserts completion inside a hard
2s `time.After`. Its actual claim is a happens-before relation: `Request`
returns while the transport is still blocked. Rewrite it as:

```go
func TestUserResolver_RequestDoesNotBlockTheCaller(t *testing.T) {
	db := newTestDB(t)

	// release gates the server handler. Nothing may complete until we
	// close it, so if Request returned, it provably did not wait.
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U1","name":"alice"}}`))
	}))
	defer srv.Close()
	defer close(release)

	r := newUserResolver("T1", testClientFor(t, srv), db, avatar.NewCache(nil, nil, false), nil, nil, nil)

	returned := make(chan struct{})
	go func() {
		for i := 0; i < userResolverConcurrency*4; i++ {
			r.Request(fmt.Sprintf("U%d", i))
		}
		close(returned)
	}()

	// The handler is still blocked on `release`, so this close can only
	// happen if Request never waited on the network.
	<-returned // Request calls returned while the transport is still blocked
}
```

Adapt `testClientFor` / `newUserResolver` arguments to this file's existing
helpers — the shape above is the assertion structure, not necessarily the exact
constructor call.

- [ ] **Step 5: Convert the remaining sleeps in the file**

Work through the remaining `time.Sleep` and `time.After` occurrences the same
way. For each, ask: *what event is this waiting for?* Then signal that event.

Run: `grep -n 'time.Sleep\|time.After' cmd/slk/user_resolver_test.go`
Expected when done: no matches, or only occurrences with a comment justifying
why a duration is semantically required.

- [ ] **Step 6: Verify under load**

Run: `go test ./cmd/slk -run TestUserResolver -race -count=50`
Expected: PASS, 50/50

Run the loaded reproduction from Step 1 again.
Expected: PASS — the failure no longer reproduces.

- [ ] **Step 7: Commit**

```bash
gofmt -w cmd/slk/user_resolver_test.go
git add cmd/slk/user_resolver_test.go
git commit -m "test(resolver): replace wall-clock waits with signals

TestUserResolver_RequestDoesNotBlockTheCaller failed roughly 1 run in 5
under parallel package load: it asserted completion inside a hard 2s
deadline. Its real claim is a happens-before relation, so it now asserts
Request returns while the transport is still blocked.

Batch-window sleeps become a channel the fake batcher closes."
```

---

### Task 10: `cmd/slk/thread_subscriptions_test.go`

**Files:**
- Modify: `cmd/slk/thread_subscriptions_test.go` (3 `time.Sleep`, 10 `time.After`)

**Interfaces:**
- Consumes: the inline-receive idiom established in Task 9
- Produces: nothing new

- [ ] **Step 1: Inventory the waits**

Run: `grep -n 'time.Sleep\|time.After\|time.Now().Add' cmd/slk/thread_subscriptions_test.go`

For each hit, identify the event being awaited. The `fakeSubscriptions` type in
this file is the natural signalling point for most of them.

- [ ] **Step 2: Add signal channels to `fakeSubscriptions`**

Give it a `called chan struct{}` closed via `sync.Once` on first invocation,
mirroring the `fakeBatcher` change in Task 9 Step 3.

- [ ] **Step 3: Replace each wait with an inline receive**

Replace every `select { case <-done: case <-time.After(...): t.Fatal("timeout") }`
with a bare `<-done` plus a comment naming the event.

- [ ] **Step 4: Verify**

Run: `grep -c 'time.Sleep\|time.After' cmd/slk/thread_subscriptions_test.go`
Expected: `0`

Run: `go test ./cmd/slk -run TestThreadSub -race -count=50`
Expected: PASS, 50/50

- [ ] **Step 5: Commit**

```bash
gofmt -w cmd/slk/thread_subscriptions_test.go
git add cmd/slk/thread_subscriptions_test.go
git commit -m "test(threadsubs): replace wall-clock waits with signals"
```

---

### Task 11: `internal/slack/membership/manager_test.go`

**Files:**
- Modify: `internal/slack/membership/manager_test.go` (7 `time.Sleep`, 5 `time.After`)

**Interfaces:**
- Consumes: the inline-receive idiom from Task 9
- Produces: nothing shared

This file already has one flaky-fix commit against it (`8eaeba9`, *"fix flaky
backoff-expiry test that reddened unrelated PRs"*). The backoff-expiry tests are
sleeping to let a real duration elapse, which needs a clock rather than a signal.

- [ ] **Step 1: Determine whether the manager already has a clock seam**

Run: `grep -n 'nowFn\|now()\|time.Now\|Clock' internal/slack/membership/*.go`

If a clock seam exists, use it. **If it does not, do not add one** — that is a
production change and this plan permits exactly one. Instead, reduce the backoff
constants' impact by asserting on ordering rather than elapsed time, or record
the file as partially converted and raise the clock injection as a separate
issue.

- [ ] **Step 2: Convert the non-backoff waits**

The waits that are "wait for the fetch goroutine to finish" convert to signals
exactly as in Task 9, using an inline `<-ch` receive. Do not add a helper.

- [ ] **Step 3: Verify**

Run: `go test ./internal/slack/membership -race -count=50`
Expected: PASS, 50/50

- [ ] **Step 4: Commit**

```bash
gofmt -w internal/slack/membership/
git add internal/slack/membership/manager_test.go
git commit -m "test(membership): replace completion sleeps with signals

Backoff-expiry tests still elapse real time; converting those needs a
clock seam on the manager, which is a production change and out of
scope for Phase 0. Raised separately."
```

---

### Task 12: `avatar`, `wake`, and `emoji/place` tests

**Files:**
- Modify: `internal/avatar/avatar_test.go` (6 `time.Sleep`, 3 `time.After`)
- Modify: `internal/wake/detector_test.go` (3 `time.Sleep`, 2 `time.After`)
- Modify: `internal/emoji/place_test.go` (3 `time.Sleep`, 5 `time.After`)

**Interfaces:**
- Consumes: nothing
- Produces: nothing shared — inline receives only

- [ ] **Step 1: `internal/avatar` — signal from `SetOnReady`**

`avatar.Cache` already has a `SetOnReady` callback. Every sleep in this file is
waiting for a preload to complete. Replace with:

```go
ready := make(chan struct{})
var once sync.Once
c.SetOnReady(func(userID string) {
	once.Do(func() { close(ready) })
})
c.Preload("U1", srv.URL+"/a.png")
<-ready // avatar preload completed
```

Run: `go test ./internal/avatar -race -count=50`
Expected: PASS, 50/50

- [ ] **Step 2: `internal/wake` — use the existing `fakeClock`**

Run: `grep -n 'fakeClock' internal/wake/*_test.go`

A `fakeClock` already exists in this package. The remaining sleeps are places
that did not adopt it. Convert them to advance the fake clock instead of
sleeping.

Run: `go test ./internal/wake -race -count=50`
Expected: PASS, 50/50

- [ ] **Step 3: `internal/emoji/place` — signal from the ready callback**

`emoji.Place` takes a `PlaceContext` with a `SendMsg` callback. Close a channel
from a test `SendMsg` instead of sleeping.

Run: `go test ./internal/emoji -race -count=50`
Expected: PASS, 50/50

- [ ] **Step 4: Full-suite stability check — the PR 0c exit gate**

Run: `go test ./... -race -count=5`
Expected: PASS. This is success criterion #1 for the whole phase.

If anything still flakes, find it now:
`for i in $(seq 1 10); do go test ./... -race -count=1 2>&1 | grep -E '^(FAIL|---)' ; done`

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/avatar/ internal/wake/ internal/emoji/
git add internal/avatar/avatar_test.go internal/wake/detector_test.go internal/emoji/place_test.go
git commit -m "test: replace remaining wall-clock waits with signals

avatar signals from SetOnReady, wake uses its existing fakeClock, emoji
signals from the PlaceContext callback. go test ./... -race -count=5 green."
```

---

## PR 0e — `messages`/`thread` lockstep

### Task 13: Lockstep test

**Files:**
- Create: `internal/ui/thread/lockstep_test.go`

**Interfaces:**
- Consumes: `messages.SetNowFunc` (Task 3)
- Produces: `func TestLockstep_SharedRenderBehaviour(t *testing.T)`, plus a
  package-level doc comment enumerating the intended divergences

`internal/ui/thread/model.go:37` documents a hand-maintained invariant: 377
verbatim lines and 45 identically-named methods kept in sync by a comment. This
test converts that comment into something that fails. Phase 3 deletes it when
the two models merge — that lifecycle is intended.

The enumerated divergence list this test produces **is the specification for
Phase 3's hooks**, so treat writing it as design work.

- [ ] **Step 1: Write the test**

Create `internal/ui/thread/lockstep_test.go`:

```go
package thread

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/ui/messages"
)

// lockstepClock matches internal/ui's golden clock so day dividers are
// stable in both models.
func lockstepClock() time.Time {
	return time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
}

// lockstepItems exercises only behaviour BOTH models claim to share:
// plain rows, a day divider, reaction pills, and long-line wrapping.
// Deliberately excludes thread-only concepts (parent row, unread
// boundary) and channel-only concepts (reply counts).
func lockstepItems() []messages.MessageItem {
	return []messages.MessageItem{
		{TS: "1710460800.000100", UserID: "U1", UserName: "alice",
			Text: "first", Timestamp: "9:00 AM", DateStr: "2026-03-14"},
		{TS: "1710547200.000100", UserID: "U2", UserName: "bob",
			Text: "second", Timestamp: "9:00 AM", DateStr: "2026-03-15",
			Reactions: []messages.ReactionItem{{Emoji: "tada", Count: 2, UserIDs: []string{"U1", "U3"}}}},
		{TS: "1710547260.000100", UserID: "U3", UserName: "carol",
			Text: "a deliberately long line that has to wrap identically in both panes or the two renderers have drifted apart",
			Timestamp: "9:01 AM", DateStr: "2026-03-15"},
	}
}

// TestLockstep_SharedRenderBehaviour pins the parity that
// thread/model.go:37 currently asks humans to maintain by hand.
//
// It asserts on the SHARED SUBSET only. Every legitimate divergence is
// enumerated in the "Documented divergences" comment below; that list
// is the contract Phase 3's pane extraction must honour.
func TestLockstep_SharedRenderBehaviour(t *testing.T) {
	messages.SetNowFunc(lockstepClock)
	t.Cleanup(func() { messages.SetNowFunc(nil) })

	items := lockstepItems()

	const w, h = 80, 20

	mm := messages.New(items, "general")
	mm.SetFocused(true)
	msgOut := ansi.Strip(mm.View(h, w))

	tm := New()
	tm.SetThread(items[0], items[1:], "C1", "1710460800.000100")
	tm.SetFocused(true)
	thrOut := ansi.Strip(tm.View(h, w))

	// Every message body must appear in both.
	for _, it := range items {
		head := it.Text
		if len(head) > 30 {
			head = head[:30]
		}
		if !strings.Contains(msgOut, head) {
			t.Errorf("messages pane missing %q", head)
		}
		if !strings.Contains(thrOut, head) {
			t.Errorf("thread pane missing %q", head)
		}
	}

	// The day divider label must be identical in both.
	if strings.Contains(msgOut, "Today") != strings.Contains(thrOut, "Today") {
		t.Errorf("day-divider parity broken:\nmessages has Today=%v\nthread has Today=%v",
			strings.Contains(msgOut, "Today"), strings.Contains(thrOut, "Today"))
	}

	// Reaction pills must render the same emoji and count in both.
	if strings.Contains(msgOut, "tada") != strings.Contains(thrOut, "tada") {
		t.Errorf("reaction-pill parity broken")
	}

	// Wrapping: the long line must break at the same column, so the
	// wrapped remainder must appear in both.
	const tail = "have drifted apart"
	if strings.Contains(msgOut, tail) != strings.Contains(thrOut, tail) {
		t.Errorf("wrap parity broken: messages=%v thread=%v",
			strings.Contains(msgOut, tail), strings.Contains(thrOut, tail))
	}
}

// Documented divergences between messages.Model and thread.Model.
//
// This list is the specification for the divergence hooks Phase 3's
// shared pane package must provide. Everything NOT listed here is
// expected to render identically and is asserted above.
//
// Adding an entry is a design decision: it declares "this difference is
// intended." If behaviour drifts and you cannot justify it as intended,
// the fix belongs in the model, not in this list.
//
//  1. Parent row — thread renders a parent message at a pseudo-index
//     above the replies; messages has no such row.
//  2. Unread boundary — thread draws a "── new ──" divider from
//     SetUnreadBoundary; messages uses SetLastReadTS differently.
//  3. Reply counts — messages renders "N replies" affordances; thread
//     does not (it IS the thread).
//  4. Scroll mechanism — thread uses bubbles/viewport; messages
//     hand-rolls yOffset. Phase 3 must pick one.
//  5. Channel chrome — messages renders a channel header with topic;
//     thread renders thread chrome.
//  6. Search terms — messages supports SetSearchTerms highlighting;
//     thread does not.
//  7. Loading spinner — messages has SetLoading/SetSpinnerFrame; thread
//     does not.
```

**Pre-flight amendment (agreed before execution):** an earlier draft made the
divergence list a `TestLockstep_DocumentedDivergences` function that asserted
only that a hardcoded slice was non-empty. It exercised no production code — a
test that asserts nothing. The list is valuable as Phase 3 input, so it stays as
the doc comment above `TestLockstep_SharedRenderBehaviour`. **Do not write it as
a test.**

- [ ] **Step 2: Run and adapt to real signatures**

Run: `go test ./internal/ui/thread -run TestLockstep -v`

The constructor and `View` signatures above are best-effort. Verify them:
`grep -n 'func New(' internal/ui/thread/model.go internal/ui/messages/model.go`
`grep -n 'func (m \*Model) View(' internal/ui/thread/model.go internal/ui/messages/model.go`

Adapt the calls to match. Do **not** change production signatures.

Expected once adapted: PASS — 1 test.

- [ ] **Step 3: Handle genuine parity failures**

If a shared-subset assertion fails, you have found a real drift between the two
models. **Do not fix the model.** Either:
- Add it to the "Documented divergences" comment with a written
  justification, if the difference is intended; or
- Add a `// BUG?:` comment, keep the assertion failing-as-skipped via
  `t.Skip` with the reason, and raise it separately.

Record whichever you chose in the commit message. This information is Phase 3
input.

- [ ] **Step 4: Verify**

Run: `go test ./internal/ui/thread -race -count=10`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ui/thread/lockstep_test.go
git add internal/ui/thread/lockstep_test.go
git commit -m "test(thread): lockstep parity test with messages.Model

thread/model.go:37 asks humans to keep 377 lines in sync with
messages.Model by hand. This asserts the shared subset — message bodies,
day dividers, reaction pills, wrap points — and enumerates every
intended divergence.

The divergence list is the specification for Phase 3's pane hooks.
Phase 3 deletes this file when the two models merge."
```

---

## PR 0d — Mode characterization

**These are characterization tests, not specification tests.** They record what
the code does *today*. Where behavior looks wrong, add `// BUG?:` and keep the
assertion matching reality.

Two findings are expected results, not failures:
- Some keys will prove dead — handled by a reducer before `dispatchModeKey` runs.
- All 16 modes are registered in `modeHandlers`, so the `handleNormalMode`
  fallback at `mode_handlers.go:96` should be unreachable. Task 14 asserts that.

### Task 14: Table runner and the registration invariant

**Files:**
- Create: `internal/ui/modekeys_test.go`

**Interfaces:**
- Consumes: `newTestApp` (Task 1)
- Produces:
  - `type keyCase struct{ name string; setup func(*App); key tea.KeyMsg; wantMode Mode; assert func(*testing.T, *App, tea.Cmd) }`
  - `func runKeyCases(t *testing.T, mode Mode, cases []keyCase)`
  - `func keyPress(r rune) tea.KeyMsg`
  - `func keyCode(c rune) tea.KeyMsg`

- [ ] **Step 1: Write the failing test**

Create `internal/ui/modekeys_test.go`:

```go
package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestEveryModeHasAHandler pins the registration invariant. handleKey
// falls back to handleNormalMode for unregistered modes
// (mode_handlers.go:96); that fallback should be unreachable because
// all 16 modes are registered. If someone adds a Mode without a
// handler, modal keys would silently leak into normal-mode behaviour.
func TestEveryModeHasAHandler(t *testing.T) {
	all := []Mode{
		ModeNormal, ModeInsert, ModeCommand, ModeSearch,
		ModeChannelFinder, ModeReactionPicker, ModeWorkspaceFinder,
		ModeThemeSwitcher, ModePresenceMenu, ModePresenceCustomSnooze,
		ModeConfirm, ModeHelp, ModeNewMessage, ModeReactionsView,
		ModeLinkPicker, ModeWorkspaceSearch,
	}
	if len(modeHandlers) != len(all) {
		t.Errorf("modeHandlers has %d entries, want %d", len(modeHandlers), len(all))
	}
	for _, m := range all {
		if _, ok := modeHandlers[m]; !ok {
			t.Errorf("mode %v (%s) has no handler; keys would fall back to Normal", m, m)
		}
	}
}

func TestRunKeyCases_Harness(t *testing.T) {
	runKeyCases(t, ModeNormal, []keyCase{
		{
			name:     "unbound key leaves mode unchanged",
			key:      keyPress('\''),
			wantMode: ModeNormal,
		},
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui -run 'TestEveryModeHasAHandler|TestRunKeyCases' -v`
Expected: FAIL — `undefined: runKeyCases`, `undefined: keyCase`, `undefined: keyPress`

- [ ] **Step 3: Write the runner**

Append to `internal/ui/modekeys_test.go`:

```go
// keyCase is one characterization row: given this precondition and
// this key, the handler leaves the App in this state.
type keyCase struct {
	name string
	// setup runs after construction, before dispatch. nil means the
	// harness default is the precondition.
	setup func(*App)
	key   tea.KeyMsg
	// wantMode is the mode AFTER dispatch.
	wantMode Mode
	// assert checks anything beyond mode. nil means mode is the whole
	// assertion.
	assert func(t *testing.T, a *App, cmd tea.Cmd)
	// opts are extra construction options for this case.
	opts []testOpt
}

// runKeyCases drives each case through dispatchModeKey directly rather
// than App.Update, so a failure localises to the mode handler instead
// of to the reducer chain that runs ahead of it.
func runKeyCases(t *testing.T, mode Mode, cases []keyCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := append([]testOpt{withSize(120, 30)}, tc.opts...)
			a := newTestApp(t, opts...)
			a.mode = mode
			if tc.setup != nil {
				tc.setup(a)
			}
			cmd := dispatchModeKey(a, tc.key)
			if a.mode != tc.wantMode {
				t.Errorf("mode after %v = %v (%s), want %v (%s)",
					tc.key, a.mode, a.mode, tc.wantMode, tc.wantMode)
			}
			if tc.assert != nil {
				tc.assert(t, a, cmd)
			}
		})
	}
}

// keyPress builds a printable-rune key message.
func keyPress(r rune) tea.KeyMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// keyCode builds a special-key message (tea.KeyEnter, tea.KeyEscape,
// tea.KeyUp, ...).
func keyCode(c rune) tea.KeyMsg {
	return tea.KeyPressMsg{Code: c}
}
```

Note: `a.mode = mode` sets the field directly rather than calling `SetMode`,
because `SetMode` (app.go:1640) has side effects (clearing selections) that
would contaminate the precondition. If a case needs those side effects, do it
in `setup`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui -run 'TestEveryModeHasAHandler|TestRunKeyCases' -v`
Expected: PASS — 2 tests

If `tea.KeyPressMsg{Code: r, Text: ...}` does not compile, check the v2 key API:
`grep -n 'tea.KeyPressMsg{' internal/ui/*_test.go | head -5` and copy the shape
already in use.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ui/modekeys_test.go
git add internal/ui/modekeys_test.go
git commit -m "test(ui): mode key-dispatch table runner

Drives dispatchModeKey directly so failures localise to the handler
rather than the reducer chain. Asserts all 16 modes are registered,
making the handleNormalMode fallback at mode_handlers.go:96 unreachable."
```

---

### Task 15: The nine zero-coverage modal handlers

**Files:**
- Create: `internal/ui/mode_reactions_view_test.go`
- Create: `internal/ui/mode_help_test.go`
- Create: `internal/ui/mode_workspace_finder_test.go`
- Create: `internal/ui/mode_presence_snooze_test.go`
- Create: `internal/ui/mode_new_message_test.go`
- Create: `internal/ui/mode_presence_menu_test.go`
- Create: `internal/ui/mode_theme_switcher_test.go`
- Create: `internal/ui/mode_reaction_picker_test.go`
- Create: `internal/ui/mode_link_picker_test.go`

**Interfaces:**
- Consumes: `runKeyCases`, `keyCase`, `keyPress`, `keyCode` (Task 14)
- Produces: one `Test<Mode>Keys` function per file

**Procedure for each mode — this is data-dependent work, so it is a procedure
with a worked example rather than 9 pre-written tables:**

1. Read `internal/ui/mode_<name>.go` in full. It is 9–31 statements.
2. Enumerate every key it branches on, plus one key it does *not* handle.
3. For each, write a `keyCase` asserting the resulting mode and the specific
   observable effect.
4. Run with `-cover` and confirm the handler is above 85%.

- [ ] **Step 1: Worked example — `mode_reactions_view.go`**

The handler (all 15 lines of it) normalises esc/up/down, forwards to
`a.reactionsView.HandleKey`, then drops to Normal when the overlay reports
itself invisible. Create `internal/ui/mode_reactions_view_test.go`:

```go
package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestReactionsViewModeKeys(t *testing.T) {
	openView := func(a *App) {
		a.openReactionsView()
	}

	runKeyCases(t, ModeReactionsView, []keyCase{
		{
			name:     "esc closes the overlay and returns to Normal",
			setup:    openView,
			key:      tea.KeyPressMsg{Code: tea.KeyEscape},
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.reactionsView.IsVisible() {
					t.Error("overlay still visible after esc")
				}
			},
		},
		{
			name:     "q closes the overlay and returns to Normal",
			setup:    openView,
			key:      keyPress('q'),
			wantMode: ModeNormal,
		},
		{
			name:     "down scrolls without leaving the mode",
			setup:    openView,
			key:      tea.KeyPressMsg{Code: tea.KeyDown},
			wantMode: ModeReactionsView,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.reactionsView.IsVisible() {
					t.Error("down should not close the overlay")
				}
			},
		},
		{
			name:     "up scrolls without leaving the mode",
			setup:    openView,
			key:      tea.KeyPressMsg{Code: tea.KeyUp},
			wantMode: ModeReactionsView,
		},
		{
			name:     "handler always returns a nil cmd",
			setup:    openView,
			key:      keyPress('x'),
			wantMode: ModeReactionsView,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("expected nil cmd, got %T", cmd)
				}
			},
		},
	})
}
```

Run: `go test ./internal/ui -run TestReactionsViewModeKeys -v -coverprofile=/tmp/c.out`
Then: `go tool cover -func=/tmp/c.out | grep handleReactionsViewMode`
Expected: PASS, and coverage for `handleReactionsViewMode` at 100%.

If `a.openReactionsView()` needs a selected message to work, add
`opts: []testOpt{withMessages(sampleMessages(2)...), withRender()}` to the cases
and select a row in `setup`.

- [ ] **Step 2: Repeat for the other eight**

In ascending size order so the pattern is established on easy ones first:
`mode_reactions_view` (9 stmts, done above), `mode_workspace_finder` (19),
`mode_presence_snooze` (20), `mode_new_message` (21), `mode_presence_menu` (23),
`mode_theme_switcher` (23), `mode_reaction_picker` (31), `mode_help`,
`mode_link_picker`.

**`mode_theme_switcher` needs cleanup** — it calls `styles.Apply`, which is
process-global. Add to every case's `setup`:

```go
setup: func(a *App) {
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
	a.themeSwitcher.Open()
},
```

Otherwise a theme-switcher test will change the theme for every test that runs
after it in the same process — including the goldens.

- [ ] **Step 3: Verify coverage for all nine**

Run:
```bash
go test ./internal/ui -coverprofile=/tmp/c.out >/dev/null
go tool cover -func=/tmp/c.out | grep -E 'handle(ReactionsView|Help|WorkspaceFinder|PresenceCustomSnooze|NewMessage|PresenceMenu|ThemeSwitcher|ReactionPicker|LinkPicker)Mode'
```
Expected: every listed handler above 85.0%.

- [ ] **Step 4: Confirm the goldens still pass**

Run: `go test ./internal/ui -run TestGolden -count=3`
Expected: PASS. A failure here means a mode test leaked global state — most
likely the theme. Fix the cleanup, not the golden.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ui/
go test ./internal/ui -race
git add internal/ui/mode_*_test.go
git commit -m "test(ui): characterize the nine zero-coverage mode handlers

Table-driven, one file per mode, driven through dispatchModeKey.
Records current behaviour; no behaviour changed. Theme-switcher cases
restore the global styles state so they cannot contaminate the goldens."
```

---

### Task 16: `mode_normal` characterization

**Files:**
- Create: `internal/ui/mode_normal_keys_test.go`

(Named `_keys_test.go` because `mode_normal_command_test.go` already exists and
must not be disturbed.)

**Interfaces:**
- Consumes: `runKeyCases`, `keyCase`, `keyPress`, `keyCode`
- Produces: `func TestNormalModeKeys(t *testing.T)`

`handleNormalMode` is `internal/ui/mode_normal.go:39-321` — 283 lines, 131
statements, currently 41% covered. It is the largest handler and the primary
user-facing keymap.

- [ ] **Step 1: Enumerate the keymap**

Run: `sed -n '39,321p' internal/ui/mode_normal.go`

Write down every `case` arm. Group them:
- navigation: `j`, `k`, `g`, `G`, arrows, `ctrl+d`, `ctrl+u`
- panel focus: `tab`, `shift+tab`, `ctrl+w` chords
- mode entry: `i`, `:`, `/`, `?`, `t` (channel finder), `ctrl+t`, `ctrl+n`
- message actions: `enter` (thread), `y` (copy), `o` (links), `d` (download),
  `e` (edit), `r` (react), `u` (mark unread), `L` (reactions view)
- toggles: `s` (sidebar), thread close
- quit: `q`, `ctrl+c`

Confirm the list against the source — do not trust this summary, it is a
starting point.

- [ ] **Step 2: Write a case per key**

For each key, assert the mode transition and the one observable effect that
distinguishes it. Use `opts` to supply preconditions:

```go
func TestNormalModeKeys(t *testing.T) {
	withMsgs := []testOpt{
		withSize(120, 30),
		withMessages(sampleMessages(5)...),
		withActiveChannel("C1"),
		withRender(),
	}

	runKeyCases(t, ModeNormal, []keyCase{
		{
			name: "j moves selection down",
			opts: withMsgs,
			key:  keyPress('j'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.messagepane.SelectedIndex(); got != 1 {
					t.Errorf("selected index = %d, want 1", got)
				}
			},
		},
		{
			name: "i enters insert mode",
			opts: withMsgs,
			key:  keyPress('i'),
			wantMode: ModeInsert,
		},
		{
			name: ": enters command mode",
			opts: withMsgs,
			key:  keyPress(':'),
			wantMode: ModeCommand,
		},
		// ... one case per enumerated key
	})
}
```

- [ ] **Step 3: Record dead keys explicitly**

Some keys will not reach `handleNormalMode` because a reducer claims the message
first. When a case fails because the key had no effect, verify by checking
whether a `reducer_*.go` handles that message type. If so, record it:

```go
		{
			// BUG?: `X` appears in handleNormalMode but reduceIO claims
			// this message type first (reducer_io.go:NNN), so this arm
			// is unreachable through App.Update. Characterized here at
			// the handler level only.
			name: "X is unreachable via Update",
			...
		},
```

This is a finding worth having, not a problem to hide.

- [ ] **Step 4: Verify coverage**

Run:
```bash
go test ./internal/ui -coverprofile=/tmp/c.out >/dev/null
go tool cover -func=/tmp/c.out | grep handleNormalMode
```
Expected: above 80.0%

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/ui/mode_normal_keys_test.go
git add internal/ui/mode_normal_keys_test.go
git commit -m "test(ui): characterize handleNormalMode keymap

283 lines, 131 statements, was 41% covered. One table row per key,
recording current behaviour. Unreachable arms annotated with BUG? and
the reducer that claims the message first."
```

---

### Task 17: `mode_insert` and the remaining partial modes

**Files:**
- Create: `internal/ui/mode_insert_keys_test.go`
- Create: `internal/ui/mode_search_test.go`
- Create: `internal/ui/mode_channel_finder_test.go`
- Create: `internal/ui/mode_confirm_test.go`
- Create: `internal/ui/mode_workspace_search_test.go`
- Modify: `internal/ui/mode_command_test.go` (fill gaps; do not rewrite)

**Interfaces:**
- Consumes: `runKeyCases`, `keyCase`, `keyPress`, `keyCode`
- Produces: `TestInsertModeKeys`, `TestSearchModeKeys`, `TestChannelFinderModeKeys`, `TestConfirmModeKeys`, `TestWorkspaceSearchModeKeys`

- [ ] **Step 1: `mode_insert` (209 lines, 128 statements, 53% covered)**

Run: `sed -n '40,248p' internal/ui/mode_insert.go` and enumerate.

Expected groups: printable-rune insertion, `backspace`, `enter` (submit),
`shift+enter` / `alt+enter` (newline), `esc` (exit to Normal), `ctrl+v` (paste),
`tab` (completion), mention/emoji/channel picker triggers.

Preconditions need the compose model populated. Use `setup` to type into
`a.compose` before dispatching.

- [ ] **Step 2: The four finder-style modes**

`mode_search`, `mode_channel_finder`, `mode_workspace_search` all route through
`normalizeFinderKey` (`mode_handlers.go:71`). Assert that normalization
explicitly — it is shared logic and a good place for a regression:

```go
{
	name: "up normalizes to the finder's 'up' string",
	setup: func(a *App) { a.channelFinder.Open() },
	key: tea.KeyPressMsg{Code: tea.KeyUp},
	wantMode: ModeChannelFinder,
	assert: func(t *testing.T, a *App, _ tea.Cmd) {
		// selection moved => the normalized key reached the widget
	},
},
```

- [ ] **Step 3: `mode_confirm`**

Small. Assert `y`/`enter` runs the confirm callback and `n`/`esc` does not, and
that both return to the prior mode.

- [ ] **Step 4: Fill `mode_command_test.go` gaps**

Read the existing file first. Add cases only for uncovered arms — do not
restructure the tests that are already there.

- [ ] **Step 5: Verify the PR 0d exit gate**

Run:
```bash
go test ./internal/ui -coverprofile=/tmp/c.out >/dev/null
go tool cover -func=/tmp/c.out | grep -E 'internal/ui/mode_' | sort -k3 -n
```
Expected: every `handle*Mode` function above 85.0%, except `handleNormalMode`
and `handleInsertMode` which must be above 80.0%.

Run: `go tool cover -func=/tmp/c.out | tail -1`
Expected: total above the 67.8% baseline for `internal/ui`.

- [ ] **Step 6: Full verification**

Run: `go test ./... -race -count=5`
Expected: PASS

Run: `gofmt -l .`
Expected: empty

Run: `go vet ./...`
Expected: no output

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/ui/
git add internal/ui/mode_*_test.go
git commit -m "test(ui): characterize insert, search, finder and confirm modes

Completes key-dispatch coverage for all 16 mode handlers. Asserts
normalizeFinderKey's mapping explicitly since three modes share it."
```

---

## Verification: phase exit criteria

Run all of these before declaring Phase 0 done. Each maps to a numbered success
criterion in the spec.

- [x] **1. No flakes** — PASS, exit 0.

```bash
go test ./... -race -count=5
```
Expected: PASS, five consecutive full runs.

- [x] **2. Goldens exist and can fail** — 8 goldens. Layout perturbation failed 7 of 8 (`no_sidebar` correctly passed, it appends neither rail nor sidebar); style perturbation hit the styling-only diff tier at byte 4934, `48;2;74;158;255` → `48;2;255;0;255`, with no fall-through to the text tier. Both reverted.

```bash
ls internal/ui/testdata/golden/*.ansi | wc -l    # expect 8
go test ./internal/ui -run TestGolden -count=3   # expect PASS
```
Plus the two perturbation checks from Task 8 Steps 3–4, re-run and re-reverted.

- [x] **3. Builders are wrappers, test bodies untouched** — all 15 converted (13 textual, 2 transitive).

```bash
git diff main --stat -- internal/ui/
git diff main -- internal/ui/ | grep -E '^[+-]' | grep -v '^[+-][+-]'
```
Expected: no modified line inside a `func Test*` body.

- [x] **4. Mode coverage** — all 16 `handle*Mode` at **≥ 95.0%**, well clear of the 85%/80% bars. Lowest three: `handlePresenceCustomSnoozeMode` 95.0%, `handleNormalMode` 96.0%, `handleWorkspaceSearchMode` 96.0%. `handleNormalMode` 40.0% → 96.0%; `handleInsertMode` 53.1% → 100.0%.

```bash
go test ./internal/ui -coverprofile=/tmp/c.out >/dev/null
go tool cover -func=/tmp/c.out | grep -E 'handle[A-Za-z]+Mode'
```
Expected: all above 85%, except `handleNormalMode` / `handleInsertMode` above 80%.

- [x] **5. `internal/ui` coverage above baseline** — **67.8% → 79.8%**.

```bash
go tool cover -func=/tmp/c.out | tail -1
```
Expected: above 67.8%.

- [x] **6. Exactly one production change** — `internal/ui/messages/model.go` only, +22/-1, the `nowFunc` / `SetNowFunc` addition and the single call-site substitution.

```bash
git diff main --stat -- '*.go' ':!*_test.go'
```
Expected: only `internal/ui/messages/model.go`, and only the `nowFunc` /
`SetNowFunc` addition plus the single `time.Now()` → `nowFunc()` substitution.

- [x] **7. Lockstep test exists and enumerates divergences** — PASS. The doc comment lists **15 verified divergences** (up from the 7 the brief predicted: 5 confirmed, 1 refined, 1 corrected, 8 added). That list is Phase 3's hook specification.

```bash
go test ./internal/ui/thread -run TestLockstep -v
```
Expected: PASS. Confirm the "Documented divergences" comment lists every
intended difference found during implementation.

- [x] **8. Lint clean** — `gofmt -l .` empty, `go vet ./...` no output, `golangci-lint run` 0 issues.

```bash
gofmt -l .        # expect empty
go vet ./...      # expect no output
golangci-lint run # expect no output
```

- [x] **9. Update the tracking document**

Mark Phase 0 complete in
`docs/superpowers/plans/2026-09-06-architecture-refactor.md`'s status table and
record the achieved numbers (coverage, golden count) next to the baseline.

---

## Plan self-review notes

Checked against the spec:

| Spec section | Task |
|---|---|
| 1. Determinism contract | 5 |
| 2. Golden infrastructure | 4 (helper), 6 (table), 8 (guard) |
| 3. Scenarios (8) | 6 (four), 7 (four) |
| 4. Test harness | 1, 2 |
| 5. Flake elimination (6 files) | 9, 10, 11, 12 |
| 6. Mode characterization (16) | 14, 15, 16, 17 |
| 7. Lockstep test | 13 |
| Success criteria 1–7 | Verification section |

**Known soft spots, flagged deliberately rather than papered over:**

- Tasks 15–17 give a procedure and a worked example rather than 16 pre-written
  tables. The contents depend on reading each `mode_*.go`; writing speculative
  assertions here would produce tables that do not compile. The worked example
  in Task 15 Step 1 is complete and real.
- Several API shapes (`thread.New`, `Model.View`, `tea.KeyPressMsg` literal
  form, `threadsView.SetSummaries`) are best-effort from grep. Each task that
  uses one includes a verification step and an instruction to adapt to the real
  signature without touching production code.
- Task 11 may not fully convert the membership backoff tests, because doing so
  needs a clock seam that would be a second production change. The task says to
  stop and raise it separately rather than exceed the phase's scope.

