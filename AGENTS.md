# AGENTS.md

Orientation for anyone — human or agent — writing code in this repo.

**The single most important rule: search before you write.** The most common
defect in this codebase's history is not bugs, it is the same logic implemented
a fourth time because the author did not know the first three existed. See
[Shared code](#shared-code--check-here-before-writing-a-helper) below.

## Build, test, lint

```
go build ./...
go test ./...                 # ~21s
go test ./... -race           # ~44s; this is what CI runs
go vet ./...
golangci-lint run             # v2.13.1, config in .golangci.yml
gofmt -l .                    # must be empty; enforced in CI
```

Tests are plain `testing.T`, stdlib only. No testify, no gomock, no golden
libraries. White-box (`package ui`, not `package ui_test`) by convention.

## Architecture in one screen

```
cmd/slk/                composition root: wiring, workspace connection,
                        WebSocket event handling, message fetch/cache pipeline
internal/ui/            bubbletea App: reducers, mode key handlers, view regions
internal/ui/<widget>/   self-contained sub-models (messages, thread, sidebar,
                        compose, and 13 modal packages)
internal/slack/         Slack Web API + browser-protocol WebSocket client
internal/slack/edge/    edgeapi: conditional revalidation, server-side search
internal/bootstrap/     startup fetch orchestration
internal/cache/         SQLite cache (a cache, not a source of truth)
internal/config/        TOML config
```

**`wiki/Architecture.md` is stale by roughly 7× and describes a service layer
that no longer exists. Do not trust it.** Current structural documentation:

- `docs/superpowers/plans/2026-09-06-architecture-refactor.md` — the active
  refactor: measured baseline, known problems, phase sequence
- `docs/superpowers/plans/2026-05-23-app-go-solid-refactor.md` — the completed
  `app.go` decomposition; establishes the patterns still in use
- `docs/superpowers/specs/` — one design doc per feature

### Invariants worth knowing

- **`internal/ui` must not import networking.** No `internal/slack`, no
  `slackhttp`, no `net/http`, no `slack-go`. All I/O crosses through the five
  service interfaces in `internal/ui/services.go`. This boundary is deliberate;
  do not breach it.
- **`App.Update` routes through a reducer chain**, not a switch. Add behavior by
  adding to a `reducer_*.go` file, not by extending `Update`.
- **Per-mode key handling is a table**, `modeHandlers` in
  `internal/ui/mode_handlers.go`. One `mode_*.go` file per mode.
- **SQLite is a cache.** Slack remains authoritative.

## Shared code — check here before writing a helper

If you are about to write text wrapping, box drawing, list windowing,
scrollbars, date formatting, case folding, or ID formatting: it already exists.

### Text and rendering

| Need | Use |
|---|---|
| Word wrap to a width | `messages.WordWrap(s, limit)` |
| Plain-text line segmentation (grapheme-correct) | `messages.PlainLines`, `messages.DisplayWidthOfPlain`, `messages.SliceColumns` |
| Display width of a string (emoji-aware) | `emoji.Width(s)` |
| Case/accent-insensitive fold for matching | `text.Fold(s)` |
| Slack mrkdwn → plain text | `messages.FlattenMrkdwn`, `messages.FlattenMrkdwnWithUserGroups` |
| Search-term highlighting (ANSI/OSC-safe) | `messages.HighlightSearchTerms`, `messages.SearchHighlightSGR` |
| Extract links from message text | `messages.ExtractLinks` |
| Reaction pill rendering | `messages.ReactionPillText` |
| Date label from a Slack ts | `messages.DateFromTS`, `messages.FormatDateSeparator` |
| mpdm channel name → human name | `slackfmt.FormatMPDMName` |
| Slack permalink parsing | `slackurl.Parse` |
| Emoji shortcode → glyph | `emoji.Sprint`, `emoji.CodeMap`, `emoji.StripSkinTone` |
| Usergroup map helpers | `usergroups.Copy`, `usergroups.Equal`, `usergroups.Display` |

### UI chrome

| Need | Use |
|---|---|
| Scrollbar gutter on a rendered pane | `ui/scrollbar.Overlay`, `ui/scrollbar.Visible` |
| Centered modal over a dimmed backdrop | `ui/overlay.DimmedOverlay` |
| Text selection ranges and anchors | `ui/selection` (`Range`, `Anchor`, `LessOrEqual`) |
| Theme colors and styles | `ui/styles` (`Username`, `SelectionStyle`, `SearchHighlightStyle`, `UserColor`) |
| Window tree geometry | `ui/wintree` |
| Modal geometry / row hit-testing | `boxedOverlay`, `clickableOverlay` in `internal/ui/reducer_modal_click.go` |

### Test helpers

Everything here is unexported and lives in a `_test.go` file, so it is
reachable only from the package that declares it (all of these are
`package ui` unless noted). They are listed because the failure mode this
file exists to prevent — writing a sixteenth ad-hoc test-app builder — is
exactly what happened before Phase 0 consolidated them. Symbols are
greppable by name; no line numbers, because these files move.

| Need | Use |
|---|---|
| Build an `App` for a test or benchmark | `newTestApp(t, opts...)` (`internal/ui/testapp_test.go`) |
| The same without a `testing.TB` | `buildTestApp(opts...)` — only for the two legacy builders whose signatures cannot gain a `*testing.T` |
| Size the App | `withSize(w, h)` (direct field assign) / `withWindowSize(w, h)` (real `tea.WindowSizeMsg` resize path). **Not interchangeable** — only the latter sets `forceSixelRepaint`. Mutually last-wins |
| Seed panes and data | `withMessages`, `withChannels`, `withWorkspaces`, `withThreadsView`, `withChannelFinderOpen` |
| Seed App-level state | `withMode`, `withView`, `withActiveChannel`, `withActiveTeam`, `withChannelService`, `withWindowSplit` |
| Populate `a.layout` bands / pane caches (needed for mouse hit-testing) | `withRender()` |
| N plain message fixtures | `testMessageItems(n)` |
| Compare or bless a full-screen frame against `testdata/golden/<name>.ansi` | `compareGolden(t, name, got)` (`internal/ui/golden_test.go`) |
| Re-bless goldens | the package-local `-update` flag: `go test ./internal/ui -run TestGolden -update`. It is not defined repo-wide, so `go test ./... -update` fails |
| An `App` with every render nondeterminism pinned (theme, emoji mode, clock) | `newGoldenApp(t, opts...)`, with `goldenMessages()` / `goldenChannels()` as the fixtures |
| Table-drive a mode handler's keys | `runKeyCases(t, mode, []keyCase{...})` (`internal/ui/modekeys_test.go`). Calls `dispatchModeKey` directly, so it **bypasses** the reducer chain and the `ctrl+c` / bootstrap / scroll-flush gates ahead of it |
| Build a key message for such a table | `keyPress(r)` printable rune, `keyCode(c)` special key, `keyMod(c, mod)` modified key |
| Count the rows a finder-style modal is showing | `modalRows(bs)` (`internal/ui/mode_workspace_finder_test.go`) — `h - 7`; floors at 1, and is **not** valid for `newmessagepicker` |
| Read the highlighted (`▌`) row out of a rendered modal box | `modalHighlightedRow(t, box)` (same file) |
| Compare two `color.Color` without `==` panicking on a non-comparable dynamic type | `colorEqual(a, b)` (`internal/ui/mode_theme_switcher_test.go`; `package styles` has its own twin in `styles/styles_test.go`) |
| Observe a status-bar toast (no getter exists) | `statusbarText(a)` (`internal/ui/mode_presence_snooze_test.go`) |
| Record status-setter invocations | the `statusCall` struct (same file) |
| A fixture with real unread channels either side of the active one | `unreadOpts()` + `seedUnreads(t, a)` (`internal/ui/mode_normal_keys_test.go`) |
| Establish a focused pane with an asserted selection | `focusMessages(t, a)`, `focusThreadPanel(t, a)` (same file) |
| Park the message viewport at an exact `yOffset` | `scrollTo(off)` (same file) |
| Make nav-history entries resolvable | `navLookupOpt()` (same file) |
| Run only the first command of a `tea.Batch` (skip a 2s tick) | `firstBatchCmd(t, cmd)` (`internal/ui/mode_insert_keys_test.go`) |
| Observe a compose cursor position or blur state (no getter exists) | `afterKeyValue(c, r)` (same file) |

### Known duplication — do not add to it

These are tracked in the refactor plan and are being consolidated. Do not copy
them as templates:

- **11 `renderBox` implementations** and **7 `visibleWindow`** across the 13
  modal packages. If you are building a modal, expect a shared chrome package to
  land (Phase 4); coordinate rather than adding a twelfth copy.
- **`messages.Model` and `thread.Model`** share 377 verbatim lines and 45
  identically-named methods. `internal/ui/thread/lockstep_test.go` pins the
  parity that is genuinely shared, and its doc comment enumerates **15 verified
  divergences** — that list is the specification Phase 3's pane hooks have to
  satisfy, not a wishlist. If you change one model, change both, and expect the
  lockstep test to tell you when you forgot. Note that
  `TestLockstep_ReactionHitTestFrames` is a tripwire that fires on
  *convergence*: Phase 3 must delete it, not satisfy it.
- **`convertAndCacheHistory` / `fetchChannelMessages` / `fetchThreadReplies`** in
  `cmd/slk/main.go` have ~85% duplicated bodies.

## Conventions

**Adding a reusable helper?** Add it to the tables above in the same commit. An
unlisted helper gets re-implemented. When this file and the code disagree, the
code is right and this file is a bug — fix it.

**Two implementations that must stay parallel?** Express it as an interface with
a compile-time assertion (`var _ Chrome = (*Model)(nil)`) or a lockstep test.
Not a comment. The comment at `internal/ui/thread/model.go:35-36` — "this shape
mirrors `internal/ui/messages.viewEntry` exactly; keeping them in lockstep
means…" — was the counter-example: prose asking humans to maintain a 377-line
invariant by hand. Phase 0 discharged it. The invariant now has a test,
`internal/ui/thread/lockstep_test.go`, which asserts the shared render
behaviour and carries a documented 15-item divergence list. Do the same: when
you find a comment standing in for a check, replace it with the check.

**Extract the substrate, not the widget.** Share the uniform part; leave the
divergent part alone. Forcing genuinely different behavior into a common shape
is worse than the duplication it removes.

**Refactoring?** Moving functions is free — two phases of the prior refactor
moved ~3,000 lines with zero test changes. Moving *state* costs roughly 150
mechanical test-line edits per 10 extractions, because ~90% of `internal/ui`
tests read unexported `App` fields directly.

**Found a bug while refactoring?** Record it, annotate it, raise it separately.
A refactor commit that also changes behavior cannot be reviewed.

## Workflow

Per the README: brainstorm the design first, write tests, then implement.
Designs go to `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md`,
implementation plans to `docs/superpowers/plans/`.

Before opening a PR: `go build ./...`, `go vet ./...`, `go test ./... -race`,
`gofmt -l .` empty.
