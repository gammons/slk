# Phase 0 — Test Safety Net

Design for the first phase of the architecture refactor tracked in
[`docs/superpowers/plans/2026-09-06-architecture-refactor.md`](../plans/2026-09-06-architecture-refactor.md).

## Problem

The codebase is about to undergo four phases of structural refactoring: two on
`cmd/slk/main.go`, one collapsing the `messages`/`thread` copy-fork, and one
finishing the `App` decomposition. The existing test suite protects some of
that work and none of the rest.

What it protects: the dominant test style in `internal/ui` is "send a
`tea.Msg` to `App.Update`, assert on resulting state." That style is immune to
code motion. The prior SOLID refactor proved it — Phase 4 moved 75 message
arms across 14 files (+2,319/−1,519) and Phase 6 split `View` into 9 files
(+722/−403), both with **zero** test changes.

What it does not protect:

1. **Rendering.** There are no golden or snapshot tests anywhere in the repo.
   The entire rendering contract is 294 `.View(` calls checked with 584
   `strings.Contains` assertions. Nothing pins layout, column widths, border
   characters, wrapping, ANSI attributes, or panel composition. Phase 6 of the
   prior refactor restructured `View()` with zero test churn — which means it
   could equally have broken rendering with zero test failures.

   `internal/ui/view_composite_test.go` is worse than absent. Its helper
   `buildViewPanels` duplicates `App.View`'s panel assembly rather than calling
   it, so it can pass while the real path is broken.

2. **`cmd/slk`.** 36.9% statement coverage; 44 of `main.go`'s 85 functions are
   at 0%. `run`, `connectWorkspace`, and 14 of the 24 `rtmEventHandler` methods
   have no tests at all. Phases 1 and 2 operate entirely inside this file.

3. **Keyboard dispatch.** 7 of the 16 mode handlers are at 0% coverage;
   `mode_normal.go` is at 41% and `mode_insert.go` at 53%. Keybindings are the
   product surface of a TUI.

Two structural weaknesses compound this:

- **15 ad-hoc test-app builders** (`newTestAppWithMessages`, `searchTestApp`,
  `newWideTestApp`, `sixelTestApp`, `linkTestApp`, `twoWindowApp`,
  `sameChannelApp`, `newFinderApp`, `newPanelAtApp`, `newTestAppWithThreadsView`,
  `newApp_WithOpenConvCapture`, `openChannelFinder`, `setupAppForTitleTest`,
  `makeBenchApp`, `makeWideScrollApp`). 90.7% of `internal/ui` tests (399 of
  440) read unexported `App` fields — 1,729 occurrences, led by `.messagepane`
  (209), `.activeChannelID` (123) and `.mode` (114). Phase 4 relocates exactly
  those fields, and the construction change would have to be reconciled in
  fifteen places.

- **A live flake.** `TestUserResolver_RequestDoesNotBlockTheCaller`
  (`cmd/slk/user_resolver_test.go:78`) fails roughly one run in five under
  parallel package load — a hard 2-second wall-clock deadline. Repo-wide there
  are 36 `time.Sleep` calls and 17 wall-clock deadline polls. Two "fix flaky
  test" commits are already in recent history; `8eaeba9`'s message reads *"fix
  flaky backoff-expiry test that reddened unrelated PRs."* Red noise during a
  refactor destroys the signal the refactor depends on.

## Goal

Make Phases 1–4 safe to execute. Phase 0 adds no user-visible value; its entire
return is that the subsequent phases can be verified.

## Non-goals

- No production refactoring. The single production change in this phase is a
  clock injection required for deterministic goldens.
- No fixing of behavior discovered while writing characterization tests. Record
  it, annotate it, raise it separately.
- No reworking of the command-drivers (`press`, `drainBatch`, `typeCommand`) or
  the view-mirror helpers. `buildViewPanels` is superseded by the goldens and
  should be deleted in Phase 4, not repaired now.
- No coverage gate in CI. Coverage is a diagnostic here, not a contract.

---

## 1. Determinism contract

A golden render is meaningful only if every global the render path reads is
pinned. `internal/ui` has several, and at least one is already mutated by both
production code and tests.

| Global | Pinned to | Why it matters |
|---|---|---|
| `styles.Apply("dark", config.Theme{})` | dark, no overrides | Package-global. Mutated in production by `mode_theme_switcher.go` and in tests by `reducer_search_test.go:437`. |
| `emoji.SetImageMode(false, 2)` | off | Package-global (`internal/emoji/imagemode.go:24`). When on, emoji render as kitty APC escapes. |
| `messages.SetNowFunc(fixedClock)` | 2026-03-15 12:00 UTC | New. See below. |
| `sidebar.SetNowFunc(fixedClock)` | same instant | Already exists at `internal/ui/sidebar/model.go:500`. |
| `App.spinnerFrame` | `0` | Animation frame appears in the loading overlay and status row. |
| `App.avatarFn` | `nil` | Avatars emit kitty graphics or half-block escapes. |
| `App.imgProtocol` | `imgpkg.ProtoNone` | Inline images emit protocol escapes. |
| `App.nowTimestampFormatter` | fixed string | Used by the optimistic-send path. |

All pins are installed by the harness and reverted with `t.Cleanup`.

Anything that cannot be pinned is a latent nondeterminism bug; the goldens will
surface it as flakiness immediately, which is a useful result rather than a
setback.

### The clock injection

`messages.FormatDateSeparator` (`internal/ui/messages/model.go:3490`) calls
`time.Now()` directly to choose between "Today", "Yesterday", a weekday name,
and an absolute date. A golden containing a "Today" divider would rot at
midnight.

Two options were considered. Dating all fixture messages far in the past would
avoid the problem with no production change, but would leave the
Today/Yesterday/weekday branches ungoldened — and that is precisely the code
that carried the timezone bug fixed in `aef453e`.

**Decision:** add a package-level clock to `internal/ui/messages`, mirroring the
shape the sidebar already uses:

```go
// nowFunc is the clock FormatDateSeparator reads. Tests override it via
// SetNowFunc so day-divider labels are deterministic.
var nowFunc = time.Now

// SetNowFunc injects a clock. Pass nil to revert to time.Now.
func SetNowFunc(fn func() time.Time)
```

This is the only production change in Phase 0.

A broader clock injection threaded through `App` — covering sidebar staleness,
DND expiry, and typing expiry from one seam — is the cleaner long-term design
but is a real refactor and belongs in a later phase.

---

## 2. Golden infrastructure

```
internal/ui/testdata/golden/<scenario>.ansi   raw bytes of a.View().Content
internal/ui/golden_test.go                    harness + scenario table
```

**Storage format:** raw ANSI, exact bytes, trailing newline normalized.

Stripped-text goldens were considered and rejected. Roughly half of what a
`messages`/`thread` merge can break is styling — selection highlight, unread
bold, muted dimming, reaction pills — and none of it survives ANSI stripping.
The `drag_selection` scenario below differs from `base` *only* in escape
sequences.

**Blessing:** `go test ./internal/ui -run TestGolden -update`, gated on a single
`var update = flag.Bool("update", false, ...)`.

**Failure output** is the part that determines whether this suite survives
contact with real use. On mismatch:

- If the ANSI-stripped text differs, print a unified line diff of the stripped
  text. This is the readable, common case.
- If the stripped text is identical, print `content identical, styling differs`
  plus the byte offset of the first difference and a hex window around it.
  Without this branch, a lost selection highlight produces an unreadable wall of
  escape sequences and the reviewer blesses it blind.

**Blank-screen guard:** a separate test asserts each golden is non-empty and its
line count equals the scenario's configured height. Blessing an empty golden is
the standard way a snapshot suite quietly dies.

---

## 3. Scenarios

Eight full-screen goldens. Full-screen rather than per-region because
composition — panel order, width distribution, border placement, overlay
compositing — is what `View()` actually does, and it is what Phases 3 and 4
threaten.

| # | Name | Config | What it pins |
|---|---|---|---|
| 1 | `base` | 120×30, sidebar + messages | Day separator, reactions, attachment, bot message, reply count, unread divider |
| 2 | `thread_open` | 120×30, + thread pane | Three-pane composition, thread chrome |
| 3 | `wide` | 200×50, all panes | Width distribution at scale |
| 4 | `narrow` | 80×24 | `ThreadAutoHidden` collapse and the focus fallback to `PanelMessages` (`app.go:2726`) |
| 5 | `no_sidebar` | 120×30, sidebar hidden | Two-pane composition |
| 6 | `drag_selection` | 120×30, active selection | Selection styling — the main visual output of the drag/selection code Phase 3 touches |
| 7 | `overlay_finder` | 120×30, channel finder open | `applyOverlays` compositing (`view_overlays.go:39`) |
| 8 | `window_split` | 160×40, two wintree leaves | `renderWindowNode` recursion; focused vs unfocused pane borders |

Fixture message content is shared across scenarios and defined once, so a
content tweak re-blesses all eight consistently rather than drifting apart.

---

## 4. Test harness

A single functional-options builder in a new `internal/ui/testapp_test.go`:

```go
func newTestApp(t *testing.T, opts ...testOpt) *App
```

Options are derived from what the 15 existing builders actually vary. Nothing
speculative is added.

| Option | Existing builders that need it |
|---|---|
| `withSize(w, h)` | `newWideTestApp`, `newPanelAtApp`, `makeWideScrollApp`, `sixelTestApp` |
| `withMessages(...)` | `newTestAppWithMessages`, `searchTestApp`, `makeBenchApp` |
| `withChannels(...)` | `newFinderApp`, `newPanelAtApp` |
| `withThreadsView(summaries)` | `newTestAppWithThreadsView` |
| `withWindowSplit(n)` | `twoWindowApp`, `sameChannelApp` |
| `withChannelService(funcs)` | `newFinderApp`, `newApp_WithOpenConvCapture`, `linkTestApp` |
| `withRender()` | ~8 builders (the `_ = a.View()` line that populates `a.layout`) |
| `withMode(m)` | `openChannelFinder` |
| `withActiveChannel(id)` | `setupAppForTitleTest` |

Each of the 15 legacy builders is then reimplemented as a wrapper:

```go
func newTestAppWithMessages(t *testing.T) *App {
	t.Helper()
	return newTestApp(t, withSize(120, 30), withMessages(twoSampleMessages...), withRender())
}
```

**No test body changes.** Helper names and signatures are preserved, so none of
the ~440 test functions is touched. The payoff is Phase 4: when `a.userNames`
becomes `a.directory.userNames`, App construction changes in one place.

Migrating every call site to `newTestApp` directly was considered and rejected —
it would touch hundreds of test functions for no behavioral gain and carries a
real risk of silently altering a test's setup during mechanical edits.

`newGoldenApp` is `newTestApp` plus the section 1 determinism pins, so golden
and non-golden tests share construction and a golden failure cannot be caused by
harness drift.

---

## 5. Flake elimination

**Rule:** a test may wait on a signal indefinitely — `go test`'s own timeout is
the backstop — but may not assert that work completed within a wall-clock
budget.

The one apparent exception, `TestUserResolver_RequestDoesNotBlockTheCaller`, is
not really an exception. Its point is that `Request` does not block, which is a
happens-before relation: assert that `Request` returns while the transport is
still blocked, not that it returns inside two seconds.

Targets, in descending order of observed noise:

| File | Sleeps | Deadlines | Approach |
|---|---|---|---|
| `cmd/slk/user_resolver_test.go` | 8 | 6 | The confirmed flake. Signal from the fake batcher's flush instead of `Sleep(batchWindow + 300ms)`; convert the 2s deadline to a happens-before assertion |
| `cmd/slk/thread_subscriptions_test.go` | 3 | 10 | Gate on channel receive |
| `internal/slack/membership/manager_test.go` | 7 | 5 | Inject a clock for backoff expiry (already has one flaky-fix commit, `8eaeba9`) |
| `internal/avatar/avatar_test.go` | 6 | 3 | Signal from `SetOnReady` instead of sleeping |
| `internal/wake/detector_test.go` | 3 | 2 | `fakeClock` already exists; finish the job |
| `internal/emoji/place_test.go` | 3 | 5 | Signal on the ready callback |

Most `time.Now().Add(...)` occurrences in `internal/ui` are fixture data (DND
end timestamps), not deadlines, and are left alone.

---

## 6. Mode characterization

**These are characterization tests, not specification tests.** They record what
the code does today so that refactoring cannot change it silently. Where a table
entry documents behavior that looks wrong, it is recorded as-is with a `// BUG?:`
annotation and raised separately. Fixing behavior inside a safety-net change
defeats the purpose of the safety net.

One file per mode, `internal/ui/mode_<name>_test.go`, each a table:

```go
tests := []struct {
	name     string
	setup    func(*App)   // preconditions beyond the harness default
	key      tea.KeyMsg
	wantMode Mode
	assert   func(*testing.T, *App, tea.Cmd)
}{...}
```

Dispatch goes through `dispatchModeKey(a, key)` (`mode_handlers.go:92`) rather
than `App.Update`, so a failure localizes to the handler rather than to the
reducer chain. Each case runs against a fresh `newTestApp`.

There are **16** modes (`mode.go`), all 16 registered in `modeHandlers`:

| Mode | Statements | Current coverage |
|---|---|---|
| `mode_normal` | 131 | 41% |
| `mode_insert` | 128 | 53% |
| `mode_reaction_picker` | 31 | 0% |
| `mode_theme_switcher` | 23 | 0% (mutates global `styles`; needs cleanup) |
| `mode_presence_menu` | 23 | 0% |
| `mode_new_message` | 21 | 0% |
| `mode_presence_snooze` | 20 | 0% |
| `mode_workspace_finder` | 19 | 0% |
| `mode_reactions_view` | 9 | 0% |
| `command`, `search`, `channel_finder`, `confirm`, `help`, `link_picker`, `workspace_search` | — | partial |

Two findings are anticipated and are results rather than failures:

- Some keys will prove dead, handled by a reducer before `dispatchModeKey` is
  reached. The table makes that visible.
- `handleNormalMode` is the fallback for unregistered modes
  (`mode_handlers.go:96`), so some modal handlers may leak normal-mode keys.
  Recording that is the point. All 16 modes are currently registered, so this
  fallback should be unreachable in practice — a test asserting that is worth
  including.

---

## Success criteria

Phase 0 is complete when all of the following hold:

1. `go test ./... -race -count=5` is green — five consecutive full runs, no flake.
2. Eight golden files exist, and deliberately perturbing panel order, a border
   character, or the selection style makes the corresponding golden fail.
3. All 15 legacy builders are wrappers over `newTestApp`, with no changes inside
   any test body.
4. Every one of the 16 mode handlers is above 85% statement coverage, except
   `mode_normal` and `mode_insert`, which must be above 80% (they are 3–6×
   larger than the rest and contain rarely-reachable arms).
5. `internal/ui` package coverage is above its 67.8% baseline.
6. Exactly one production change in the phase: `messages.SetNowFunc`.

`cmd/slk` coverage is explicitly *not* a Phase 0 criterion; raising it is
Phase 1 and 2's job, and it depends on extractions that Phase 0 does not make.

## Delivery

Four independent PRs off a `refactor/phase0-safety-net` worktree:

| PR | Contents |
|---|---|
| **0b** | `newTestApp` + 15 wrappers |
| **0a** | Golden infrastructure + 8 scenarios + `messages.SetNowFunc` |
| **0c** | Flake fixes (6 files) |
| **0d** | Mode characterization (16 files) |

0b lands first: it is pure mechanical refactoring with no new assertions, and 0a
builds `newGoldenApp` on top of it. 0c and 0d are independent of both and of
each other.
