# slk Architecture Refactor — Tracking Document

> **Status:** Phase 0 **complete**. Phases 1–5 not designed.
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
| Statement coverage, `internal/ui` | 67.8% (**79.8%** after Phase 0) |
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

(The `renderBox` duplication originally recorded here proved to be much larger
than first counted; it is promoted to F6.)

- `internal/ui/services.go`: ~250 of 600 lines are mechanical
  `if fn == nil { return zero }` adapter boilerplate over 30 methods.
  `ChannelService`'s own doc comment admits it "mixes three concerns" across its
  12 methods.
- Render god-functions: `thread.View` 514 lines, `messages.viewInternal` 487,
  `messages.renderMessagePlain` 443, `sidebar.buildCache` 327.
- `internal/ui/msgs.go`: 70 message types in one unlabelled block. They already
  map 1:1 onto the `reducer_*.go` files.

### F6 — The modal-widget cluster: 13 packages, 5,295 lines, one widget

`internal/ui` contains 13 modal packages that are variations on the same
component: a centered bordered box containing a filter prompt and a windowed,
selectable list.

```
channelfinder 720   newmessagepicker 656   searchresults 617   reactionpicker 557
help 489            themeswitcher 379      presencemenu 375    workspacefinder 372
mentionpicker 335   emojipicker 237        linkpicker 200      confirmprompt 180
channelpicker 178                                              total 5,295
```

There are **11 `renderBox` implementations** (143–210 lines each) and **7
`visibleWindow`** implementations. An undeclared interface runs through them:

| Method | Impls | Distinct signatures | Classification |
|---|---|---|---|
| `Close()` | 14 | 1 | chrome |
| `IsVisible() bool` | 14 | 1 (receiver varies) | chrome |
| `BoxSize(w, h) (int, int)` | 9 | 1 (receiver varies) | chrome |
| `ClickRow(w, h, localY) bool` | 8 | 1 | chrome |
| `Open(...)` | 13 | **6** | behavior — legitimately diverges |
| `HandleKey(string)` | 11 | **9 return types** | behavior — legitimately diverges |

The seam is clean: **chrome is uniform, behavior is not.** Two of the four
chrome methods are *already* declared as interfaces — `boxedOverlay` and
`clickableOverlay` at `internal/ui/reducer_modal_click.go:31,38` — but they are
used only for mouse-click routing and never for rendering. The abstraction was
discovered and then not applied.

Consolidation target is the chrome (box rendering, list windowing, scrollbar
integration, geometry), not the widgets. `Open` and `HandleKey` stay
per-package.

### F7 — Duplication recurs because copy-then-adapt is the cheapest correct path

Measured with `dupl` (github.com/mibk/dupl) across all 234 non-generated source
files:

| Threshold | Clone pairs found |
|---|---|
| 150 tokens | 5 |
| 100 tokens | 11 |
| 75 tokens | 22 |
| 50 tokens | 89 |

**Automated clone detection misses this codebase's expensive duplication.** At
75 tokens it finds the `main.go` history triple (`3014-3065` ≡ `3445-3496` ≡
`3528-3579`) and 3 modal pairs. Against F3 — 377 verbatim lines, 45
identically-named methods — it finds exactly **one** 34-line region
(`messages/model.go:2095-2129` ≡ `thread/model.go:1973-2006`). Against F6's
13-package cluster it finds 3 pairs.

The reason is that duplication here is *copy-then-adapt*: names change, a field
is added, a branch is reordered. Token-sequence detectors need long contiguous
identical runs; adaptation fragments them below any useful threshold.

Three root causes, none of which a linter addresses:

1. **No shared home.** No modal-chrome package exists. Building modal #14, the
   fastest correct path is to copy modal #13. Creating the shared package is a
   bigger change than the feature that needs it, so nobody does.
2. **No map.** The repo has no `AGENTS.md`, `CLAUDE.md`, or `CONTRIBUTING.md`.
   Every contributor — and every agent session — starts cold, unaware that
   `messages.WordWrap`, `internal/text.Fold`, `ui/scrollbar.Overlay` or
   `ui/overlay.DimmedOverlay` exist.
3. **Divergence is invisible.** `thread/model.go:37` asks humans to keep two
   files in lockstep. A comment cannot fail a build.

### F8 — Test-suite gaps

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

**Addresses:** F8, plus F7 mitigation (lockstep test)

Golden tests for `View()` (8 full-screen scenarios, raw ANSI), one
`newTestApp(opts...)` harness with the 15 legacy builders rewritten as wrappers,
flake elimination in 6 files, table-driven characterization of all 16 mode
handlers, and a lockstep test pinning `messages`/`thread` render parity until
Phase 3 merges them.

One production change: `messages.SetNowFunc` clock injection.

**Exit:** `go test ./... -race -count=5` green; 8 goldens that fail on perturbed
layout or styling; all 15 builders wrapping `newTestApp`; every mode handler
above 85% statements, except `mode_normal` and `mode_insert` above 80%.

**Delivery:** 4 PRs — 0b (harness) first, then 0a (goldens), 0c (flakes) and 0d
(modes) in any order.

**Note:** delivers no user-visible value. Its return is that Phases **3–5**
become verifiable — not Phases 1–4, as this note originally claimed.

- **Phase 1** needs nothing from it: it is pure cut-paste with no closure
  captures, and it moves no state.
- **Phase 2 is the gap, and it is a real one.** It deliberately changes
  concurrency behaviour in `cmd/slk`, which sits at 36.9% statement coverage,
  and Phase 0 raised none of it: `cmd/slk` coverage is explicitly *not* a Phase
  0 criterion (see the spec's exit criteria — "raising it is Phase 1 and 2's
  job"). Two `cmd/slk` test files were converted from timing waits to signals
  (`user_resolver_test.go`, `thread_subscriptions_test.go`), which removes
  flakes but adds no coverage. Nothing covers `run` or `connectWorkspace`.
  Phase 2 must budget for building its own safety net as step 0.
- **Phases 3, 4 and 5** are what Phase 0 actually protects: the 8 goldens, the
  16 mode-handler tables and the lockstep divergence list all bear directly on
  them.

#### Achieved (branch `refactor/phase0-safety-net`, 17 tasks)

| Exit criterion | Bar | Achieved |
|---|---|---|
| `go test ./... -race -count=5` | PASS | **PASS** (exit 0) |
| Goldens in `internal/ui/testdata/golden/` | 8 | **8** |
| Goldens fail on a perturbed layout | must fail | **7 of 8** scenarios failed on a swapped rail/sidebar append order; `no_sidebar` correctly passed (it appends neither) |
| Goldens fail on perturbed styling | must fail | **yes**, and on the styling-only diff branch: byte 4934, SGR `48;2;74;158;255` → `48;2;255;0;255`, no fall-through to the text tier |
| `internal/ui` statement coverage | > 67.8% | **67.8% → 79.8%** |
| Every `handle*Mode` | > 85% (80% for normal/insert) | **all 16 ≥ 95.0%**. Lowest: `handlePresenceCustomSnoozeMode` 95.0%, `handleNormalMode` 96.0%, `handleWorkspaceSearchMode` 96.0% |
| `handleNormalMode` | > 80% | **40.0% → 96.0%** |
| `handleInsertMode` | > 80% | **53.1% → 100.0%** |
| Production `.go` changes | exactly 1 | **1**: `internal/ui/messages/model.go` (+22/-1), the `nowFunc` / `SetNowFunc` clock injection |
| Lint | clean | `gofmt -l .` empty, `go vet ./...` clean, `golangci-lint run` 0 issues |

#### Cost Phase 0 imposed on Phase 5 — measured, not estimated

Phase 0's tests are white-box `package ui` tests, so they read `App`'s
unexported fields and call its unexported methods directly. That is the same
coupling this document already warns about, and Phase 0 materially increased it.

**Method:** a type-checked count, not a grep. `golang.org/x/tools/go/packages`
loads `internal/ui` with `Tests: true`; every `ast.SelectorExpr` in a `_test.go`
file whose receiver resolves to `ui.App` or `*ui.App` and whose selected
identifier is unexported is counted once. Comments, strings and same-named
members of other types are excluded by construction, and the receiver's variable
name is irrelevant. Counts cover unexported **fields and methods** alike.

| | `main` (79c78b9) | after Phase 0 | delta |
|---|---|---|---|
| References from `internal/ui` test files | 1,995 | **2,905** | +910 (×1.46) |
| Distinct unexported `App` members referenced | 121 | **142** | +21 |

Of that, **914 references to 68 distinct members** are in the 18 test files
Phase 0 added; the modified files net out to roughly zero. Heaviest new
coupling: `compose` +88 (204 total), `threadCompose` +50 (62), `layout` +47
(109), `help` +45 (58), `sidebar` +43 (103), `focusedPanel` +43 (146),
`messagepane` +35 (243). Twenty-one members are newly reachable from tests at
all, including `workspaceFinder`, `themeSwitcher`, `reactionPicker`,
`presenceMenu` and `pickerKind` — each previously at zero.

**Consequence for Phase 5** ("Finish `App` decomposition") and for any Phase 2/3
field relocation: the per-extraction test-edit cost is now roughly **1.5× the
pre-Phase-0 figure**. `AGENTS.md`'s "~150 mechanical test-line edits per 10
extractions" rule of thumb predates this branch; budget nearer 220, and expect
`compose`, `messagepane`, `focusedPanel`, `mode` and `activeChannelID` to
dominate — those five alone account for 847 of the 2,905 references. This is a
real cost of the safety net, not an accident: characterizing 16 mode handlers
requires observing state that `App` exposes no getters for. Phase 5 should plan
to add getters (or move the state) ahead of the mechanical edit, not during it.

The lockstep test (`internal/ui/thread/lockstep_test.go`) documents **15
verified divergences** between `messages.Model` and `thread.Model`. That list is
Phase 3's pane-hook specification — read it before designing Phase 3.

Phase 0 found production defects it deliberately did not fix — the
one-production-change budget forbade it, and a characterization test that also
changes behaviour cannot be reviewed. They are filed as **issues #181, #182 and
#184–#194**: two rendering defects (status-row overrun, thread separator glyph),
three user-facing keybinding defects, two input-filter defects, an emoji-picker
ranking defect, plus dead code, stale comments, missing test seams, a swallowed
error in `internal/slack/membership`, a missing `dirty()` in `compose`, and a
picker state-clearing asymmetry. (#193 and #194 were filed during the
whole-branch review, which found three `// BUG?:` rows with no issue behind
them; the third — the reaction picker's `up`/`down` asymmetry — was examined and
is not a defect, and its row now says so.) Several are
pinned in place by `// BUG?:` characterization rows that must be **re-pinned,
not deleted**, when the bug is fixed; each issue names what to re-bless.

Two of these matter to later phases specifically: **#192** (`compose` render
version) is the seam Phase 4 needs before it can safely move the theme-switcher
calls, and **#190** item 4 (a `Mode` count sentinel) is the seam Phase 5 needs
before it can add a mode without risking an unregistered handler.

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

### Phase 4 — De-duplication: extract the modal chrome substrate

**Addresses:** F6, and structurally F7 cause #1

**Prerequisite: Phases 0 and 3 must be complete.** Phase 3's pane extraction
solves the same shape of problem one layer down (shared list substrate,
divergent behavior hooks); its outcome should inform this design rather than the
two being invented independently.

The seam is already established by the data: **chrome is uniform across all 13
modal packages; behavior is not.**

Extract a modal-chrome package owning:

- box rendering — replaces 11 `renderBox` implementations (143–210 lines each)
- list windowing — replaces 7 `visibleWindow` implementations
- geometry (`BoxSize`) and row hit-testing (`ClickRow`), promoting the existing
  `boxedOverlay`/`clickableOverlay` interfaces
  (`internal/ui/reducer_modal_click.go:31,38`) from mouse-routing-only to the
  package's real contract
- visibility (`Open`/`Close`/`IsVisible` state)
- scrollbar integration (`ui/scrollbar.Overlay`) and dimmed backdrop
  (`ui/overlay.DimmedOverlay`), which already exist and are inconsistently used

**Explicitly out of scope:** `Open(...)` has 6 distinct signatures and
`HandleKey(string)` has 9 distinct return types across the cluster. These encode
genuine per-widget behavior. Do **not** unify them — forcing them into a common
shape is the classic over-abstraction failure and would be worse than the
duplication.

**Exit:** modal cluster below ~3,000 lines (from 5,295); one `renderBox`; one
`visibleWindow`; every modal package satisfying a declared chrome interface with
a compile-time assertion; all 8 goldens byte-identical except
`overlay_finder`, which is re-blessed once with a reviewed diff.

**Risk:** medium-high. Touches 13 packages and the `overlay_finder` golden is
expected to change. Consolidate incrementally — one modal migrated per commit,
goldens green between each.

---

### Phase 5 — Finish the `App` decomposition

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

### Phase 6 — Opportunistic cleanup

**Addresses:** F5, plus the stale documentation

- Generic nil-guard in `services.go` (~250 lines → ~50).
- Split `ChannelService` along the three concerns its own doc names (Slack API /
  local cache / session bookkeeping).
- Break up the render god-functions: `thread.View` (514), `messages.viewInternal`
  (487), `messages.renderMessagePlain` (443), `sidebar.buildCache` (327). Note
  that Phase 3 changes the first three substantially — do this after.
- **Rewrite `wiki/Architecture.md`.** It is 7× off on every figure.
- Refresh or retire `docs/STATUS.md`.
- Enable `dupl` in `.golangci.yml` at a 100-token threshold, warn-only. See the
  caveat below — this is deliberately last and deliberately low-expectation.

**Risk:** low. Independent items; can be split across contributors.

---

## Standing practice: preventing re-duplication

Derived from F7. These are not phases; they apply continuously.

### 1. `AGENTS.md` is the map — keep it current

`AGENTS.md` at the repo root lists where shared code lives. It exists because
the dominant cause of duplication here is discovery cost, not laziness.

**When you add a reusable helper, add it to `AGENTS.md` in the same commit.** An
unlisted helper will be re-implemented by the next contributor. When `AGENTS.md`
and the code disagree, the code wins and `AGENTS.md` is a bug.

### 2. Search before you write

Before writing any helper, check `AGENTS.md`, then grep. The specific traps this
codebase has already fallen into: text wrapping, box rendering, list windowing,
scrollbars, date formatting, mrkdwn flattening, case-folding, ID formatting.

### 3. Prefer a declared interface over a "keep in sync" comment

If two implementations must stay parallel, express it as an interface with a
compile-time assertion (`var _ Chrome = (*Model)(nil)`) or a lockstep test that
feeds both the same input and compares output. `thread/model.go:37` is the
counter-example: a comment asking humans to maintain a 377-line invariant.

### 4. Extract the substrate, not the widget

The uniform part is worth sharing; the divergent part is not. F6's split —
chrome uniform, `Open`/`HandleKey` divergent — is the model. Forcing divergent
behavior into a common shape is worse than the duplication it removes.

### 5. `dupl` is a backstop, not a safety net

Measured against this codebase: at a 75-token threshold `dupl` finds 22 clone
pairs and misses nearly all of F3 (1 of ~45 duplicated methods) and most of F6
(3 of 13 packages). It catches verbatim copies; this codebase's duplication is
copy-then-adapt, which defeats token-sequence matching. Enable it, but do not
treat a green `dupl` run as evidence of anything.

Reproduce the measurement with:

```
go install github.com/mibk/dupl@latest
dupl -t 75 -plumbing $(find . -name '*.go' \
  -not -path './vendor/*' -not -path './.worktrees/*' \
  -not -name '*_test.go' -not -name '*_gen.go')
```

---

## Status

| Phase | Scope | Prereqs | Spec | Plan | Status |
|---|---|---|---|---|---|
| 0 | Test safety net | — | [spec](../specs/2026-09-06-phase0-test-safety-net-design.md) | [plan](2026-09-06-phase0-test-safety-net.md) | **complete** |
| 1 | `main.go` mechanical splits | — | — | — | not started |
| 2 | `main.go` structural | 1 | — | — | not started |
| 3 | Collapse `messages`/`thread` fork | 0 | — | — | not started |
| 4 | Modal chrome substrate | 0, 3 | — | — | not started |
| 5 | Finish `App` decomposition | 0b | — | — | not started |
| 6 | Opportunistic cleanup | 3, 4 | — | — | not started |

Standing practice (`AGENTS.md`, search-before-write, declared interfaces over
sync comments, substrate-not-widget, `dupl` as backstop) applies from now, not
at a phase boundary.

### Target end state

| Metric | Baseline | Target |
|---|---|---|
| `cmd/slk/main.go` | 4,842 lines | < 600 |
| `cmd/slk` coverage | 36.9% | > 65% |
| `internal/ui/app.go` | 3,454 lines | < 2,000 |
| `App` struct fields | 107 | < 60 |
| `App` `Set*` methods | 45 | < 25 |
| `internal/ui/thread/model.go` | 2,072 lines | < 900 |
| Modal-widget cluster (13 pkgs) | 5,295 lines | < 3,000 |
| `renderBox` implementations | 11 | 1 |
| `visibleWindow` implementations | 7 | 1 |
| Golden tests for `View()` | 0 | 8 — **done (Phase 0)** |
| Known data races in `cmd/slk` | 3 | 0 |
