# slk Architecture Refactor — Tracking Document

> **Status:** Phase 0 designed, not started. Phases 1–5 not designed.
> **Baseline commit:** `4184e60` (main, 2026-09-06)
> **Toolchain:** Go 1.26.5, bubbletea v2, lipgloss v2

This is the coordinating document for a six-phase architecture refactor. It
records the measured baseline, the problems found, the phase sequence, and the
rationale for the ordering.

## How to use this document

Each phase gets its own brainstorm → spec → plan → implement cycle. Do not
execute a phase directly from this document — it states *what* and *why*, not
*how*. Design the phase first, write the spec to
`docs/superpowers/specs/`, write the plan to `docs/superpowers/plans/`, then
implement.

Phases are ordered by dependency, not by value. Later phases assume earlier ones
have landed. In particular, **do not start Phase 3 or Phase 4 before Phase 0 is
complete** — both can break rendering silently, and until Phase 0 lands there is
nothing in the repo that would notice.

Update the status table at the bottom as phases complete.

---

## Measured baseline (2026-09-06, commit `4184e60`)

| Metric | Value |
|---|---|
| Production Go | 64,641 LOC across 235 files |
| Tests | 67,276 LOC across 268 files, 2,551 test funcs |
| Statement coverage, repo-wide | 71.1% |
| Statement coverage, `cmd/slk` | **36.9%** |
| Statement coverage, `internal/ui` | 67.8% |
| `go test ./... -race` | green, 44s |
| Packages with no test file | 1 (`internal/ids`, 64 lines of type declarations) |
| Third-party test libraries | none — pure stdlib `testing` |

Largest source files:

```
4842  cmd/slk/main.go
3559  internal/ui/messages/model.go
3454  internal/ui/app.go
2154  internal/slack/client.go
2072  internal/ui/thread/model.go
1732  internal/ui/sidebar/model.go
1302  internal/ui/compose/model.go
```

### What is already good

Worth stating, because it constrains what should change:

- **The UI/network boundary is clean.** `internal/ui/*.go` imports no
  networking — no `internal/slack`, no `slackhttp`, no `net/http`, no
  `slack-go`. All I/O crosses through five service interfaces in
  `internal/ui/services.go`. Do not undo this.
- **The reducer migration is complete.** `App.Update` (`app.go:597`) is 103
  lines with three residual switch arms; everything else routes through
  `dispatchReducers` over 16 reducers and a `map[Mode]modeHandler`.
- **The Slack wire layer is well tested** — 418 `httptest` servers, plus
  `internal/slackhttp/golden_test.go`, which pins outgoing request shape against
  a redacted capture of the official web client.
- **The prior refactor worked.** `docs/superpowers/plans/2026-05-23-app-go-solid-refactor.md`
  took `app.go` from 6,216 to 2,357 lines across 7 phases. Read it before
  starting any phase here; it establishes the patterns this work continues.

### Documentation warning

`wiki/Architecture.md` claims "~9,300 lines of Go across 31 source files and 24
test files." The real figures are 64,641 across 235 and 268. It is off by
roughly 7× and describes a service layer that no longer exists. It actively
misleads. Rewriting it is Phase 5.

`docs/STATUS.md` is also stale (last updated 2026-05-03, predates windows,
search, grid bootstrap, and file downloads).

---

## Findings

### F1 — `cmd/slk/main.go` is a second application in the composition root

4,842 lines; 44 of 85 functions at 0% coverage.

| Symbol | Lines | Size |
|---|---|---|
| `run()` | 831–2213 | **1,383** (28.6% of the file) |
| `wireCallbacks` (closure literal inside `run`) | 1352–1892 | **541**, wiring 36 callbacks |
| `connectWorkspace` | 2264–2675 | **412**, 16 sequential steps |
| `rtmEventHandler` + 24 methods | 3875–4711 | **837**; 14 methods at 0% coverage |
| `(h) OnMessage` | 3923–4106 | 184, 12 parameters |
| `enrichCachedRow` | 3223–3390 | 168, 9 parameters |
| connect fan-out goroutine (anonymous) | 2001–2167 | 165 |

`convertAndCacheHistory`, `fetchChannelMessages` and `fetchThreadReplies` have
~85% duplicated bodies. They are untestable only because they take a concrete
`*slackclient.Client`.

### F2 — Three concurrency defects in the closure-capture style

These are live bugs, not stylistic complaints.

1. **`activeTeamID` (`main.go:1188`)** — a plain `string`, written from the UI
   goroutine (`:1906`) and from N connect goroutines (`:2035`, `:2042`), read
   from every WebSocket goroutine via the `isActive` closure (`:2062`). No
   synchronization. Redundant with `router.Active().TeamID`.
2. **`workspaces` (`main.go:1187`)** — a plain map written from N connect
   goroutines at `:2022`. Fully redundant with `router.all`, written on the next
   line. Five read sites.
3. **`cfg`** — mutated in place by `SetThemeSaver` and `SetWidthSaver` on the UI
   goroutine, while a copy sharing the `cfg.Workspaces` map by reference lives in
   every `rtmEventHandler` (`:2070`) and is read from WS goroutines.

The doc comment at `main.go:276-278` claims all `router.all` writes precede
`p.Run`. They do not: connect goroutines start at `:2001`, `p.Run()` is at
`:2186`.

### F3 — `messages.Model` and `thread.Model` are a copy-fork

The largest duplication in the repo, documented in the source as intentional.
`internal/ui/thread/model.go:37`:

> *"This shape mirrors internal/ui/messages.viewEntry **exactly**; keeping them
> in lockstep means scroll and selection logic can be kept in sync."*

Measured:

- 45 identically-named exported methods. Adding the 7 renamed twins
  (`AddReply`/`AppendMessage`, `SelectedReply`/`SelectedMessage`,
  `Replies`/`Messages`, `SwapLocalSentReply`/`SwapLocalSent`,
  `RemoveLocalSentReply`/`RemoveLocalSent`, `UpsertSelfSentReply`/`UpsertSelfSent`,
  `ReplyCount`/`len(Messages())`), **52 of thread's 65 exported methods (80%)**
  have a direct counterpart.
- Both structs have exactly 49 fields; **29 names identical**.
- 4 cloned type declarations: `viewEntry`, `reactionEntryHit`, `reactionHitRect`,
  `EmojiContext`.
- **377 of thread/model.go's 782 substantive lines (48%) are verbatim** in
  messages/model.go. `renderThreadMessage` is 85% verbatim a subset of
  `renderMessagePlain`.

It propagates upward. `app.go` carries three hand-written mirror pairs —
`handleReactionNav`/`handleThreadReactionNav` (`:799`/`:819`),
`openPickerFromMessage`/`openPickerFromThread` (`:839`/`:857`),
`toggleReactionOnSelectedMessage`/`toggleReactionOnSelectedThread`
(`:924`/`:952`) — plus mirror arms in `view_messages.go`/`view_thread.go`,
`reducer_mouse.go`, `drag.go`, and `mode_normal.go`.

Genuine divergence is small: 13 thread-specific behaviors (parent row at index
−1, unread boundary, `SetThread` bulk-replace), and thread's use of
`bubbles/viewport` where messages hand-rolls `yOffset`.

### F4 — `App` is still a god-object

107 fields, 131 methods, ~19 concerns. Not a god-*function* problem: the largest
method in `app.go` is `View` at 105 lines.

Nine controllers and five services already demonstrate the extraction pattern.
The un-extracted residue:

- 15 fields of render/perf cache (`renderCache`, `lastScreen`, `lastPanels`,
  `lastStatus`, `lastScreenW/H/Valid`, `scrollPending`, `scrollPanel`,
  `scrollFlushScheduled`)
- 10 directory fields (`userNames`, `externalUsers`, `userGroups`,
  `channelNames`, `emojiCustoms`, `avatarFn`, `workspaceDomains`, …)
- 8 debounce/generation counters

45 `Set*` methods: 21 inject collaborators, 24 push data, 4 are dead in
production (`SetInitialLastReadTS`, `SetChannelFinderItems`, `SetInitialChannel`,
`SetClipboardWriter`). `SetEmojiContext` (29 lines) and `SetUserNames` (24) are
pure fan-out, writing the same value to `messagepane` + every `winModels` entry +
`threadPanel` + `compose` + `threadCompose` + a retained field.

Bootstrap-ordering smell: `SetImageContext` and `SetEmojiContext` are each
called twice from `main.go` (`1130`/`1979`, `1138`/`1983`) because the context
needs `p.Send` and `p` does not exist yet.

### F5 — Smaller, well-bounded duplications

- **`renderBox` appears 5 times**, 143–210 lines each, in `help`,
  `reactionpicker`, `channelfinder`, `themeswitcher`, `workspacefinder`.
- `internal/ui/services.go`: ~250 of 600 lines are mechanical
  `if fn == nil { return zero }` adapter boilerplate over 30 methods.
  `ChannelService`'s own doc comment admits it "mixes three concerns" across its
  12 methods.
- Render god-functions: `thread.View` 514 lines, `messages.viewInternal` 487,
  `messages.renderMessagePlain` 443, `sidebar.buildCache` 327.
- `internal/ui/msgs.go`: 70 message types in one unlabelled block. They already
  map 1:1 onto the `reducer_*.go` files.

### F6 — Test-suite gaps

Covered in detail in the Phase 0 spec. Summary:

- No golden or snapshot tests anywhere. 294 `.View(` calls, 584
  `strings.Contains` assertions.
- `view_composite_test.go`'s `buildViewPanels` duplicates `App.View`'s assembly
  instead of calling it — it can pass while `View` is broken.
- 90.7% of `internal/ui` tests read unexported `App` fields (1,729 occurrences).
- 15 ad-hoc test-app builders.
- 7 of 16 mode handlers at 0% coverage.
- A reproducible load-sensitive flake at `cmd/slk/user_resolver_test.go:78`,
  plus 36 `time.Sleep` and 17 wall-clock deadlines repo-wide.

---

## Ground rules

Apply to every phase.

1. **Behavior-preserving unless the phase says otherwise.** Each phase ships
   green tests with no observable behavior change. F2's concurrency fixes are
   the one deliberate exception, and they are scoped to Phase 2.
2. **Do not fix behavior discovered mid-refactor.** Record it, annotate it,
   raise it separately. A refactor PR that also changes behavior cannot be
   reviewed.
3. **Preserve the UI/network boundary.** `internal/ui` must not gain a
   networking import.
4. **Moving functions is free; moving state costs.** Empirically, from the prior
   refactor: pure code motion produced *zero* test churn across two phases;
   state relocation cost ~150 mechanical test lines per 10 extractions. Budget
   accordingly.
5. **Run `go test ./... -race` before and after every commit.** CI runs it;
   `golangci-lint v2.13.1` and `gofmt` are enforced.
6. **Prefer deleting redundant state over synchronizing it.** F2's `workspaces`
   and `activeTeamID` both have correct existing sources of truth.

---

## Phases

### Phase 0 — Test safety net

**Spec:** [`../specs/2026-09-06-phase0-test-safety-net-design.md`](../specs/2026-09-06-phase0-test-safety-net-design.md)

**Addresses:** F6

Golden tests for `View()` (8 full-screen scenarios, raw ANSI), one
`newTestApp(opts...)` harness with the 15 legacy builders rewritten as wrappers,
flake elimination in 6 files, and table-driven characterization of all 16 mode
handlers.

One production change: `messages.SetNowFunc` clock injection.

**Exit:** `go test ./... -race -count=5` green; 8 goldens that fail on perturbed
layout or styling; all 15 builders wrapping `newTestApp`; every mode handler
above 85% statements, except `mode_normal` and `mode_insert` above 80%.

**Delivery:** 4 PRs — 0b (harness) first, then 0a (goldens), 0c (flakes) and 0d
(modes) in any order.

**Note:** delivers no user-visible value. Its entire return is that Phases 1–4
become verifiable.

---

### Phase 1 — `main.go` mechanical splits

**Addresses:** F1 (partially)

Pure cut-paste. No closure captures involved; every function listed already
takes its dependencies as explicit parameters.

| New file | Source lines | Size |
|---|---|---|
| `cmd/slk/rtm_handler.go` | 3875–4711 | 837 |
| `cmd/slk/history.go` | 2963–3599 | 637 |
| `cmd/slk/attachments.go` | 2676–2761 | 86 |
| `cmd/slk/search.go` | 3600–3705 | 106 |
| `cmd/slk/paths.go` | 3706–3747 | 42 |
| `cmd/slk/usergroups.go` | 2214–2261 | 48 |
| `cmd/slk/presence.go` | 3748–3874 | 127 |

`rtmEventHandler` is constructed in exactly one place (`main.go:2056`) and all
its dependencies are explicit struct fields, so the move is mechanical. Its
existing tests (`event_handler_test.go`, `event_handler_marked_test.go`,
`reconnect_sync_test.go`) already exercise it in isolation.

**Exit:** ~1,900 lines moved out of `main.go`; zero test changes; zero behavior
change; `go test ./... -race` green.

**Risk:** low. This is the phase to do first if you want momentum.

---

### Phase 2 — `main.go` structural

**Addresses:** F1, F2

The valuable half. Ordered by dependency:

1. **Delete `workspaces`** (`main.go:1187`). Redundant with `router.all`. Five
   read sites; three can use `router.ByID`. Removes one unsynchronized map.
2. **Delete `activeTeamID`** (`main.go:1188`). Redundant with
   `router.Active().TeamID`. Removes 17 read sites, 3 write sites, and the
   `isActive` closure race.
3. **Guard `router.all`** with a mutex, and correct the false claim in the doc
   comment at `main.go:276-278`.
4. **Replace the `p *tea.Program` capture with `send func(tea.Msg)`.** `p` is
   declared at `:1186` and assigned at `:1972`; every closure relies on the
   nil-then-set pattern. Inside `wireCallbacks` it is used in only four places
   (`:1490`, `:1501`, `:1693`/`:1716`, `:1829`). `newUserResolver`,
   `membership.New` and `resolveDMNames` already use the indirection. This is
   the change that unblocks step 6.
5. **Extract `run()` phases 1–7** (`:832–1179`, 348 lines) into a startup
   package returning a value struct. It touches none of the shared state — it
   only produces values. Lowest-risk large extraction in the file.
6. **Extract `wireCallbacks`** (541 lines) to `cmd/slk/callbacks.go` with an
   explicit deps struct. Depends on step 4.
7. **Extract the connect fan-out goroutine** (`:2001–2167`, 165 lines) as
   `startWorkspace(...)`. Pulls out the 40-line `rtmEventHandler` literal with it.
8. **Introduce a narrow history-fetch interface** to replace the concrete
   `*slackclient.Client` parameter in the fetch functions. Makes ~350 currently
   untestable lines testable.

**Exit:** `main.go` under ~600 lines; `cmd/slk` coverage above 65%; the three F2
defects gone; `go test ./... -race -count=5` green.

**Risk:** medium. This phase deliberately changes concurrency behavior. The
existing `cmd/slk` tests cover the RTM handler well but nothing covers `run` or
`connectWorkspace` — step 8 is what starts closing that.

---

### Phase 3 — Collapse the `messages`/`thread` fork

**Addresses:** F3

**Prerequisite: Phase 0 must be complete.** This is the change most likely to
break rendering silently, and `thread`'s tests already reach into `m.cache`,
`m.version` and `m.yOffset` heavily.

Extract an `internal/ui/pane` package owning the shared substrate: `[]MessageItem`,
selection index, scroll offset, `viewEntry` cache, selection range, reaction-nav
cursor, and hit-test rects. Provide hooks for the 13 genuinely thread-specific
behaviors.

Note the one real architectural divergence to resolve: `thread` uses
`bubbles/viewport` (`vp`) where `messages` hand-rolls `yOffset`. Pick one.

**Exit:** ~700–900 duplicated lines removed; the 4 cloned type declarations
reduced to one each; the three `app.go` mirror pairs collapsed; mirror arms in
`view_*`, `drag.go`, `reducer_mouse.go` and `mode_normal.go` collapsed; all 8
goldens byte-identical.

**Risk:** high. The goldens are the primary safety mechanism.

---

### Phase 4 — Finish the `App` decomposition

**Addresses:** F4

**Prerequisite: Phase 0 must be complete** (specifically 0b, the harness).

- Extract `renderState` (15 fields), `directory` (10 fields), `debounce` (8
  counters).
- The `directory` extraction also removes most of the 24 data-push setters and
  their fan-out.
- Collapse the 21 injection setters into one `App.Wire(Deps)` or a functional-
  options constructor.
- Delete the 4 dead setters.
- Fold the double `SetImageContext`/`SetEmojiContext` calls into a single
  deferred wiring step.
- Split `msgs.go` (70 types) by family to align 1:1 with the `reducer_*.go` files.
- Delete `view_composite_test.go`'s `buildViewPanels` — superseded by the goldens.

**Exit:** `App` under ~60 fields; setter count roughly halved; all 8 goldens
byte-identical.

**Risk:** medium. State relocation, so expect mechanical test churn — but the
Phase 0b harness is what keeps it to one place rather than fifteen.

---

### Phase 5 — Opportunistic cleanup

**Addresses:** F5, plus the stale documentation

- Shared `renderBox` helper across the 5 modal packages (~800 lines → ~200).
- Generic nil-guard in `services.go` (~250 lines → ~50).
- Split `ChannelService` along the three concerns its own doc names (Slack API /
  local cache / session bookkeeping).
- Break up the render god-functions: `thread.View` (514), `messages.viewInternal`
  (487), `messages.renderMessagePlain` (443), `sidebar.buildCache` (327). Note
  that Phase 3 changes the first three substantially — do this after.
- **Rewrite `wiki/Architecture.md`.** It is 7× off on every figure.
- Refresh or retire `docs/STATUS.md`.

**Risk:** low. Independent items; can be split across contributors.

---

## Status

| Phase | Scope | Spec | Plan | Status |
|---|---|---|---|---|
| 0 | Test safety net | [spec](../specs/2026-09-06-phase0-test-safety-net-design.md) | — | **designed** |
| 1 | `main.go` mechanical splits | — | — | not started |
| 2 | `main.go` structural | — | — | not started |
| 3 | Collapse `messages`/`thread` fork | — | — | not started |
| 4 | Finish `App` decomposition | — | — | not started |
| 5 | Opportunistic cleanup | — | — | not started |

### Target end state

| Metric | Baseline | Target |
|---|---|---|
| `cmd/slk/main.go` | 4,842 lines | < 600 |
| `cmd/slk` coverage | 36.9% | > 65% |
| `internal/ui/app.go` | 3,454 lines | < 2,000 |
| `App` struct fields | 107 | < 60 |
| `App` `Set*` methods | 45 | < 25 |
| `internal/ui/thread/model.go` | 2,072 lines | < 900 |
| Golden tests for `View()` | 0 | 8 |
| Known data races in `cmd/slk` | 3 | 0 |
