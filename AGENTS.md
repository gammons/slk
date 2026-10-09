# AGENTS.md

Orientation for anyone — human or agent — writing code in this repo.

**The single most important rule: search before you write.** The most common
defect in this codebase's history is not bugs, it is the same logic implemented
a fourth time because the author did not know the first three existed. See
[Shared code](#shared-code--check-here-before-writing-a-helper) below.

## Build, test, lint

```
go build ./...
go test ./...                 # ~10s
go test ./... -race           # ~47s; this is what CI runs
go vet ./...
golangci-lint run             # v2.13.1, config in .golangci.yml
gofmt -l .                    # must be empty; enforced in CI
```

Those two timings are wall clock with `-count=1` and a warm build cache on an
8-core Linux box, re-measured at the end of Phase 0 (they were `~21s`/`~44s`
before it, on unrecorded hardware — treat them as a shape, not a target).
The shape is what matters: under `-race`, four packages are ~95% of the run —
`internal/ui` 32s, `cmd/slk` 29s, `internal/cache` 19s, `internal/ui/messages`
16s (they overlap, hence a 47s wall). If you are iterating, run the one package
you are changing; `internal/ui` alone is now longer under `-race` than the whole
suite used to be.

Tests are plain `testing.T`, stdlib only. No testify, no gomock, no golden
libraries. White-box (`package ui`, not `package ui_test`) by convention.

## Architecture in one screen

```
cmd/slk/                composition root: wiring, workspace connection,
                        WebSocket event handling, message fetch/cache pipeline
internal/core/          the ports (service interfaces) the TUI calls, and the
                        values the TUI and cmd/slk exchange through them
internal/ui/            bubbletea App: reducers, mode key handlers, view regions
internal/ui/<widget>/   self-contained sub-models (messages, thread, sidebar,
                        compose, and 14 modal packages)
internal/slack/         Slack Web API + browser-protocol WebSocket client
internal/slack/edge/    edgeapi: conditional revalidation, server-side search
internal/bootstrap/     startup fetch orchestration
internal/cache/         SQLite cache (a cache, not a source of truth)
internal/config/        TOML config
internal/demo/          fake workspaces + services behind the hidden
                        `slk --demo` flag; for recording README GIFs only
```

**`wiki/Architecture.md` is stale by roughly 7× and describes a service layer
that no longer exists. Do not trust it.** Current structural documentation:

- `docs/superpowers/plans/2026-09-06-architecture-refactor.md` — the active
  refactor: measured baseline, known problems, phase sequence
- `docs/superpowers/plans/2026-05-23-app-go-solid-refactor.md` — the completed
  `app.go` decomposition; establishes the patterns still in use
- `docs/superpowers/specs/` — one design doc per feature

### Invariants worth knowing

- **`internal/ui` does no I/O of its own.** Slack, SQLite, the filesystem, the
  clipboard, the external editor and launching apps all go through the service
  ports in `internal/core`, which `cmd/slk` wires. No `internal/slack`,
  `slackhttp`, `cache`, `config`, `filedl`, `export`, `editor`, `net/http` or
  `os/exec`; `slack-go` only in `blockkit`, as the data it renders. `internal/ui/boundary_test.go`
  enforces this. The boundary is deliberate; do not breach it.
- **`App.Update` routes through a reducer chain**, not a switch. Add behavior by
  adding to a `reducer_*.go` file, not by extending `Update`. The only step
  outside the chain is the thin `Update` wrapper that records `stackFront`
  (which content pane — messages or thread — last had focus, for the stacked
  thread layout) after every message; do not add other post-chain logic
  there.
- **Per-mode key handling is a table**, `modeHandlers` in
  `internal/ui/mode_handlers.go`. One `mode_*.go` file per mode.
- **SQLite is a cache.** Slack remains authoritative.
- **A map shared across goroutines is never written.** Every map the UI holds
  is either built for it (a copy, like `userNameStore.SnapshotForUI()` or the
  per-message `ExternalUsers` set) or published once and never mutated
  again (`WorkspaceContext.CustomEmoji` / `UserGroups`, swapped whole via
  `atomic.Pointer`). A copy handed to the UI is the UI's from then on: the
  sender must not keep reading it (the UI writes it). The same goes for maps
  captured by a `tea.Cmd` (`saveThreadToFile` clones them). A plain map
  written while another goroutine reads it is a `fatal error: concurrent map
  read and map write`, which bubbletea cannot recover from; it crashed slk on
  2026-10-02 via the user-name map (see `cmd/slk/usernames.go`). Engine state
  several goroutines write is locked (`userNameStore`) instead.

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
| Does message text mention the current user? | `mention.InText(text, selfUserID)` |
| Reaction pill rendering | `messages.ReactionPillText` |
| Date label from a Slack ts | `messages.DateFromTS`, `messages.FormatDateSeparator`, `messages.FormatShortDate` (compact form for a message header: "Today", "Jan 2") |
| Date-qualified timestamp for the selected message header | `messages.LongTimestamp(ts, short)`, `messages.SelectedHeader(rendered, header, ts, short, maxWidth)`; any other header timestamp swap that must fit the width budget: `messages.ReplaceHeaderTimestamp(rendered, header, short, replacement, maxWidth)` |
| mpdm channel name → human name | `slackfmt.FormatMPDMName` |
| Channel-type glyph (`#` / `◆` / `●`) | `messages.ChannelGlyph(chType)` |
| Slack permalink parsing | `slackurl.Parse` |
| Slack ts → `time.Time` (whole seconds, any timezone) | `export.TimeFromTS` |
| A since/until/overlap date range in a timezone, and "is this ts in it?" | `export.NewWindow`, `export.Window.Contains` |
| Sleep out a slack-go rate-limit error (ctx-aware) | `slackclient.WaitOutRateLimit` |
| Page through all channel history in a ts range / a thread's replies in a ts range | `(*slackclient.Client).WalkHistory`, `GetRepliesBetween` |
| Emoji shortcode → glyph | `emoji.Sprint`, `emoji.CodeMap`, `emoji.StripSkinTone` |
| Does Block Kit already render the message body? | `blockkit.RendersBody(blocks)`, `messages.BlocksCarryBody(msg)` |
| Current DND state from a Slack API result | `slack.DNDStateFromStatus` |
| Persist a full Web API profile's custom status and huddle (including clears) | `cmd/slk/applyProfileStatus(teamID, userID, profile, db, send)` after `UpsertUser`, before any resolved-user notice or row repair; `UpsertUser` alone deliberately preserves status on conflict |
| Drop cached thread replies a complete `conversations.replies` response no longer contains (missed deletes, Slackbot ephemerals) | `(*cache.DB).PruneThreadReplies(channelID, threadTS, returnedTS, fetchStartUnix)`; soft-deletes, never touches the parent or rows cached after the fetch began |
| Peer custom status, DND and huddle rendering | `ui/peerstatus` (`Status`, glyph/expiry/summary methods); `messages.AuthorStatusSuffix` for author headers |
| Workspace app/bot classification, from any goroutine | `WorkspaceContext.IsBotUser` / `MarkBotUser`; private synchronized set, never directly read/write a plain bot-ID map |
| Usergroup map helpers | `usergroups.Copy`, `usergroups.Equal`, `usergroups.Display` |
| A workspace's user ID → display name, from any goroutine in `cmd/slk` | `wctx.UserNames` (`*userNameStore`: `Get`/`Set`, `MentionedNames(text)` for one message; `lookupUserCached` / `resolveUserCached` add the SQLite fallback). Just `Set`: hand the UI the map from `SnapshotForUI()` and pass only its version to `NotifyFrom`, which makes every later new or changed name reach the UI as `UserResolvedMsg`. Never keep or read the map you handed over |
| User IDs mentioned in message text | `slackfmt.MentionedUserIDs(text)` |
| Workspace-relative cached external-user flags | `cache.DB.ExternalUsers(workspaceID)` returns a caller-owned map, derived from `User.HomeTeamID`; the legacy boolean is only a fallback within the row's original workspace |
| Slack original-avatar URL → sized CDN URL (e.g. 72px) | `avatar.SizedURL(orig, px)`; returns non-Slack-original URLs unchanged |
| Copy text to the clipboard | `App.clipboardWrite` / `SetClipboardWriter`; `cmd/slk/newClipboardWriter` selects local macOS `pbcopy` or terminal OSC 52 |

### UI chrome

| Need | Use |
|---|---|
| Scrollbar gutter on a rendered pane | `ui/scrollbar.Overlay`, `ui/scrollbar.Visible` |
| Centered modal over a dimmed backdrop | `ui/overlay.DimmedOverlay` |
| Text selection ranges and anchors | `ui/selection` (`Range`, `Anchor`, `LessOrEqual`) |
| Theme colors and styles | `ui/styles` (`Username`, `SelectionStyle`, `SearchHighlightStyle`, `MentionBadgeStyle`, `UserColor`) |
| Window tree geometry | `ui/wintree` |
| Modal geometry / row hit-testing | `boxedOverlay`, `clickableOverlay` (list rows), `pointClickable` (a single glyph, e.g. the profile dialog's 📋) in `internal/ui/reducer_modal_click.go` |
| Channel/DM destination picker for forwarding | `channelfinder.Model.OpenForForwarding()` (joined conversations only); `Open()` restores the normal switcher |
| Render a thread from cache on open, before the fetch lands | `cachedThreadRepliesCmd(threads, chID, threadTS)` (`internal/ui/app.go`); yields a `FromCache` `ThreadRepliesLoadedMsg`, which renders but never marks the thread read — only the fetched result does |
| Bind a composer to the conversation its draft belongs to | `(*compose.Model).SetDraftContext(teamID, channelID, threadTS)` — an empty channel detaches (saves the prior conversation's draft, clears the visible input). `SetActiveChannel` sets only mention context; it does not move draft storage |

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
| The same without a `testing.TB` | `buildTestApp(opts...)` — only for the four legacy builders whose signatures must not change: `newPanelAtApp`, `sixelTestApp`, `makeBenchApp`, `makeWideScrollApp` |
| Size the App | `withSize(w, h)` (direct field assign) / `withWindowSize(w, h)` (real `tea.WindowSizeMsg` resize path). **Not interchangeable** — only the latter sets `forceSixelRepaint`. Mutually last-wins |
| Seed panes and data | `withMessages`, `withChannels`, `withWorkspaces`, `withThreadsView`, `withChannelFinderOpen` |
| Seed App-level state | `withMode`, `withView`, `withActiveChannel`, `withActiveTeam`, `withChannelService`, `withWindowSplit`. **`withActiveChannel` assigns only `a.activeChannelID`** — nothing derives a *name* from an ID, so the messages-pane header renders as a bare `#`, the statusbar as `#`, and the compose placeholder as `Message #...`. A test that asserts on any of those must push the name through the production trio itself; `newGoldenApp` does it via `nameGoldenActiveChannel` (`internal/ui/golden_test.go`) |
| Populate `a.layout` bands / pane caches (needed for mouse hit-testing) | `withRender()` |
| N plain message fixtures | `testMessageItems(n)` |
| Compare or bless a full-screen frame against `testdata/golden/<name>.ansi` | `compareGolden(t, name, got)` (`internal/ui/golden_test.go`) |
| Re-bless goldens | the package-local `-update` flag: `go test ./internal/ui -run TestGolden -update`. It is not defined repo-wide, so `go test ./... -update` fails |
| An `App` with every render nondeterminism pinned (theme, emoji mode, clock) | `newGoldenApp(t, opts...)`, with `goldenMessages()` / `goldenChannels()` as the fixtures |
| Drive a message through the real `Update` chain and render one frame | `updateAndRender(t, a, msg)` (`internal/ui/thread_breadcrumb_test.go`) |
| An App at a given width, resized via `WindowSizeMsg`, channel focused, for thread-layout tests | `stackedApp(t, w, extra...)` (`internal/ui/thread_stacked_test.go`) |
| Assert which of channel / thread the last frame drew | `assertFront(t, a, wantChannel, wantThread)` (same file) |
| Fake one service method on an `App` | `a.setChannelFetcherForTest(fn)` and its siblings, `setUploaderForTest`, `setClipboardReaderForTest`, `setReadStateReaderForTest`, `setDesktopForTest(func(*core.DesktopServiceFuncs))`, `setFilesystemForTest()`, `setEditorForTest()`, `setProfileFetcherForTest(fn)` (`internal/ui/services_helpers_test.go`). Calls for sibling methods of one service compose instead of replacing each other |
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
| An `App` with the main composer mid-upload, a thread open with its own draft, and workspaces T1/T2, counting uploader and workspace-switcher calls | `uploadGuardApp(t)` (`internal/ui/conversation_drafts_safety_test.go`) |
| Observe a compose cursor position or blur state (no getter exists) | `afterKeyValue(c, r)` (same file) |

### Known duplication — do not add to it

These are tracked in the refactor plan and are being consolidated. Do not copy
them as templates:

- **11 `renderBox` implementations** and **7 `visibleWindow`** across the 14
  modal packages. If you are building a modal, expect a shared chrome package to
  land (Phase 4); coordinate rather than adding a twelfth copy.
- **`messages.Model` and `thread.Model`** share 377 verbatim lines and 45
  identically-named methods. `internal/ui/thread/lockstep_test.go` pins *render*
  parity in **one static state only**: 80×20 (`lockstepWidth`/`lockstepHeight`),
  chosen so neither pane scrolls — so no scroll offset, no scrollbar gutter, no
  search-term highlighting and no loading state is compared. The
  scroll/viewport/selection math, which is most of the 377 shared lines, is
  pinned *within* each model (`messages/scrollbar_test.go`,
  `messages/selection_test.go`, `thread/selection_test.go`,
  `thread/model_test.go`) but **not across** them: a change that breaks one
  model's scrolling and not the other's will not fail the lockstep test. Its doc
  comment enumerates **15 verified
  divergences** — that list is the specification Phase 3's pane hooks have to
  satisfy, not a wishlist. If you change one model, change both, and expect the
  lockstep test to tell you when you forgot — within the limits above. Note that
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
behaviour in one static state and carries a documented 15-item divergence list.
Do the same: when
you find a comment standing in for a check, replace it with the check.

**Extract the substrate, not the widget.** Share the uniform part; leave the
divergent part alone. Forcing genuinely different behavior into a common shape
is worse than the duplication it removes.

**Refactoring?** Moving functions is free — two phases of the prior refactor
moved ~3,000 lines with zero test changes. Moving *state* is not: `internal/ui`
test files make **2,905 references to 142 distinct unexported `App` fields and
methods** (type-checked count, not a grep; measured at the end of Phase 0, up
from 1,995 / 121 before it — Phase 0's characterization tests added ~910 of
them). Budget roughly **220 mechanical test-line edits per 10 extractions**, and
expect five members to dominate: `messagepane` (243), `compose` (204),
`focusedPanel` (146), `mode` (129) and `activeChannelID` (125) are 847 of the
total between them. Method and per-member breakdown:
`docs/superpowers/plans/2026-09-06-architecture-refactor.md`, Phase 0 "Achieved".

**Found a bug while refactoring?** Record it, annotate it, raise it separately.
A refactor commit that also changes behavior cannot be reviewed.

## Workflow

Per the README: brainstorm the design first, write tests, then implement.
Designs go to `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md`,
implementation plans to `docs/superpowers/plans/`.

Before opening a PR: `go build ./...`, `go vet ./...`, `go test ./... -race`,
`gofmt -l .` empty.
