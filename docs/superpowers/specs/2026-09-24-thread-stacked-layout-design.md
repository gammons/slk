# Thread Stacked Layout and Breadcrumb

**Issues:** [#223](https://github.com/gammons/slk/issues/223),
[#244](https://github.com/gammons/slk/issues/244)
**Supersedes:** PR #246 (mkozjak), PR #149 (piotrsynowiec)
**Branch:** `feat/thread-stacked-layout`

## Problem

Opening a thread at a common terminal width appears to do nothing. `Enter`
sets `threadVisible`, then the next `App.View()` asks `panelLayout.Compute`
for widths, gets `ThreadAutoHidden`, and clears `threadVisible` and thread
focus from inside the render (`app.go:3101-3107`). With the default 6-column
rail and 30-column sidebar the thread only survives at 124 columns and up, so
the 120-column default cannot open a thread at all. The status bar keeps
saying `> Thread` (the stale state #149 diagnosed), and the reply fetch that
lands afterwards is discarded because the pane is no longer visible.

Where the thread does show, it is too narrow to read: 35% of the content area,
which is 35–56 columns at typical widths.

PR #246 lowers the effective threshold to 112 columns by clamping the thread to
its 30-column minimum. That fixes the default width but keeps a 30-column
thread and keeps auto-hide below 112. This design replaces its layout logic.

## Goals

- A thread is readable when open: at least 80 columns wide whenever it shares
  the screen.
- A thread can be opened and replied to at any terminal width. Nothing is
  auto-hidden.
- It is always obvious that you are looking at a thread, not the channel.
- `View()` no longer mutates thread state.

## Non-goals

- A modal or overlay presentation of the thread. Considered and dropped in
  favour of the thread taking over the channel pane's space.
- Consolidating the three existing private `channelGlyph` helpers
  (`messages`, `statusbar`, `threadsview`). Noted as a follow-up.
- Any change to `thread.Model` beyond its header, or to `messages.Model`
  beyond exporting one helper. The Phase 3 fork collapse is unaffected.

## Relationship to in-flight refactors

None of the architecture-refactor phases or RFC #236 block this.

- **Phase 2** is confined to `cmd/slk`; this is confined to `internal/ui`.
- **Phase 3** (messages/thread fork): `thread.Model` only gains a header
  input. The header takes the channel *type* rather than a formatted title,
  which is what `thread/lockstep_test.go` divergence 5 says a Phase 3 chrome
  hook needs.
- **Phase 4** (modal chrome): no modal is added.
- **RFC #236**: removing the render-time mutation is a step in the RFC's
  direction ("`View` is not pure"). The layout gains one input, which the RFC's
  eventual `SetSize` migration absorbs.

## Design

### 1. Layout

**Pane space** is the width left for the channel and thread panes after the
rail, the sidebar (and its border), and both panes' 2-column borders.

| State | Layout |
|---|---|
| No thread open | Unchanged: the channel pane takes the whole content area. |
| Thread open, pane space ≥ 40 + 80 | **Side by side.** Thread width is `max(35% of the content area, 80)`, capped so the channel keeps 40. |
| Thread open, pane space < 120 | **Stacked.** One pane — whichever is in front — takes the whole content area; the other is not drawn. |

Constants: `minMsgWidth = 40` (unchanged), `minThreadW` 30 → **80**. The
existing `floorPaneW = 10` floor applies to whichever pane is drawn when
stacked, so behaviour below the floor matches today's.

Worked widths (sidebar shown; "width" is the pane's `ThreadWidth`/`MsgWidth`,
content is 2 columns less):

| Terminal | Today (`main`) | This design |
|---|---|---|
| 300 | channel 167 / thread 91 | unchanged (35% already exceeds 80) |
| 200 | channel 102 / thread 56 | channel 78 / thread 80 |
| 162 | channel 77 / thread 43 | channel 40 / thread 80 (both minima exactly) |
| 161 | channel 76 / thread 43 | stacked, 121 |
| 120 | thread auto-hidden | stacked, 80 |
| 80 | thread auto-hidden | stacked, 40 |

Side by side needs **≥ 162 columns** with the sidebar shown, **≥ 130** with it
hidden. The thread is wider than today on large screens; that is intended.

### 2. Which pane is in front

`Compute` gains a `threadFront bool` argument. The caller derives it; `Compute`
stays pure geometry.

```
threadFront = threadVisible &&
    (focusedPanel == PanelThread ||
     (focusedPanel != PanelMessages && stackFront == PanelThread))
```

`stackFront Panel` is one new `App` field recording the content pane
(`PanelMessages` or `PanelThread`) that last had focus, so focusing the sidebar
leaves the last content pane showing. Its zero value (`PanelWorkspace`) reads
as "channel in front".

It is recorded in **one place**. `App.Update`'s body moves to an unexported
`update`; `Update` calls it and then sets `stackFront = focusedPanel` whenever
`focusedPanel` is `PanelMessages` or `PanelThread`. This avoids touching the ~30
sites that assign `focusedPanel`.

An unexported `func (a *App) computeFrame() panelLayoutFrame` wraps the
`Compute` call with the App's current inputs. `App.View` uses it, and so do the
test files that today duplicate the six-argument call
(`winmodels_test.go`, `view_window_region_test.go`, `sixelpaint_test.go`,
`view_composite_test.go`, `view_messages_border_test.go`), so they cannot pass
the wrong `threadFront`.

### 3. Removing render-time mutation

Delete `panelLayoutFrame.ThreadAutoHidden` and the block at
`app.go:3101-3107` that consumes it. `View()` no longer writes
`threadVisible` or `focusedPanel`. This is the #223/#244 fix and makes #149's
toast unnecessary.

### 4. Rendering

`App.View`:

- Draws the channel/windows region only when `frame.MsgWidth > 0`. The thread
  region's existing `frame.ThreadWidth > 0` gate already covers the other case.
- With the thread in front, the thread covers the whole windows area, split
  windows included. The window tree is untouched and reappears on Tab or close.
- `collectSixelPlacements` returns no placements when `frame.MsgWidth == 0`.
  The thread already renders images as half-blocks; the image preview overlay
  is unchanged.

Render caches key on width already, so switching between side by side and
stacked needs no cache changes.

Column bands fall out of the geometry. Thread in front: `msgEnd == sidebarEnd`
and `threadEnd == width`. Channel in front: `threadEnd == msgEnd`, as when no
thread is open.

### 5. Breadcrumb

The thread header row `Thread  3 replies` (`thread/model.go:1367`) is replaced,
in **both** layouts, by:

```
# general › Thread from Alice · 3 replies                          esc close
```

- `# general` uses the channel pane header's format (`glyph + " " + name`), so
  in stacked mode the breadcrumb echoes the header of the pane it replaced.
  Glyphs: `#` public, `◆` private, `●` DM / group DM.
- `Thread from Alice` names the parent's author (`parent.UserName`). If that is
  empty, `from …` is omitted.
- `· N replies` keeps today's pluralisation (`reply` at 1).
- `esc close` is right-aligned with at least two spaces before it.
- Styling: `# general ›` in `styles.TextMuted`; `Thread from Alice` bold in
  `styles.Accent`; `· N replies` in `styles.TextPrimary`; `esc close` in
  `styles.TextMuted`.

When the content width is too small, parts are dropped in this order:

1. the `esc close` hint;
2. `from Alice`;
3. the channel name is truncated with `…`.

`Thread · N replies` is always kept. Example: at 80×24 the stacked pane's
content is 38 columns, so it renders `# general › Thread · 3 replies`.

The header stays two rows (breadcrumb plus separator), so `chromeHeight` and
every thread hit-test frame are unchanged.

**Inputs.** New setter `thread.Model.SetBreadcrumb(channelName, channelType
string)`, called next to the existing `threadCompose.SetChannel` in
`openThreadPanel` and `openSelectedThreadCmd`:

- name: `threadComposeChannelName(channelID)`, which the reply placeholder
  already uses;
- type: the threads-view summary's `ChannelType` when opened from the Threads
  view; otherwise the matching `sidebar.Items()` entry; otherwise `""`, which
  renders `#`.

The chrome cache predicate gains the breadcrumb name, type and parent author.

**Shared helper.** `messages.channelGlyph` is exported as
`messages.ChannelGlyph` and used by `thread`, rather than adding a fourth copy.
`AGENTS.md` gains a row for it.

### 6. Input

No key handler changes.

- **Tab / Shift-Tab, `h` / `l`**: `FocusNext`/`FocusPrev` already cycle
  sidebar → channel → thread. When stacked, moving between channel and thread
  switches which is drawn; the thread stays open and the status bar keeps
  `> Thread`.
- **Esc / `q`**: `CloseThread` already clears `threadVisible` and focuses the
  channel.
- **Threads view**: activation focuses the list, so the list stays in front;
  `j`/`k` update the thread behind it; `Enter` focuses `PanelThread`
  (`app_test.go:886`) and brings it forward; `i` already moves to the thread
  compose.
- **Resize**: stacking is derived each frame; narrowing while the channel is
  focused puts the thread behind it, still open.

**Mouse**: no changes. The hidden pane's band has zero width, so the
`x < MsgEnd()` / `x < ThreadEnd()` chains in `reducer_mouse.go` and
`panelLayout.PanelAt` route to the visible pane with correct pane-local
coordinates. Verified by tests (below), not assumed.

**Windows**: one fix. `windowBounds()` (`windows.go:50`) computes the layout
with `threadFront = false`, since that is the area the windows fill whenever
they are drawn. Otherwise, with the thread in front, `ctrl+w s`/`v` would
refuse with "Not enough room" and `ctrl+w h/j/k/l` would navigate zero-width
rects. Splitting focuses the new window (channel comes forward, thread stays
open); focusing another window closes the thread, as the window-management
spec already requires.

Unchanged: `/` and `n`/`N` stay no-ops while the thread is focused; modal
overlays draw over whichever pane is shown.

## Testing

**Layout** (`internal/ui/panellayout_test.go`, new):

- Table: widths 80, 120, 140, 161, 162, 200, 300 with the sidebar shown and
  129, 130 with it hidden, each with `threadFront` true and false. Asserts
  `MsgWidth`, `ThreadWidth`, borders and the four band ends.
- Sweep: widths 20–300 × sidebar shown/hidden × `threadFront`. Asserts no
  negative widths; exactly one content pane when stacked; `ThreadWidth ≥ 80`
  whenever side by side; bands contiguous and ending at the terminal width
  whenever the drawn pane is above `floorPaneW`.

**App-level**, through the real `a.Update` chain, sized with `withWindowSize`:

- 120 columns: Enter opens the thread in front; Tab shows the channel with the
  thread still open; Tab brings the thread back; Esc closes it.
- Threads view at 120: the list stays in front on activation; Enter brings the
  thread forward.
- Render purity: two consecutive `View()` calls leave `threadVisible`,
  `focusedPanel` and `stackFront` unchanged.
- Sidebar focus: with the thread in front, focusing the sidebar keeps the
  thread drawn.
- Mouse, thread in front: click, wheel and drag land on the thread at the
  expected pane-local coordinates.
- Windows, thread in front: `windowBounds()` equals the channel-in-front
  bounds and `ctrl+w v` splits.

**Breadcrumb** (`internal/ui/thread`): full text; each shortening step at
chosen widths; glyph per channel type; `reply`/`replies`; missing author;
`chromeHeight` stays 2; cache invalidates on each new input.

**Goldens** (still 8; each re-bless is a reviewed diff):

| Scenario | Change |
|---|---|
| `base` | unchanged |
| `thread_open` | 140 → **120** columns, thread focused: stacked takeover at the default width |
| `wide` 200×50 | re-blessed: 80-column thread, breadcrumb |
| `narrow` 80×24 | re-blessed: 40-column stacked thread, shortened breadcrumb |

`autoHidesThread`, `goldenThreadMinWidth`,
`TestGolden_NarrowAutoHidesThreadPane` and
`TestGolden_ThreadScenariosAreWideEnough` are deleted with the behaviour they
pin. They are replaced by a per-scenario `layout` field (`sideBySide`,
`stackedThread`, `stackedChannel`, or none) asserted against the built App's
`computeFrame()`, which keeps their purpose: a scenario whose name promises a
thread cannot silently render none.

**Docs**: `AGENTS.md` (`messages.ChannelGlyph`); divergence 5 text in
`thread/lockstep_test.go`; the `panellayout.go` package comment.

**Verification**: `go build ./...`, `go vet ./...`, `go test ./... -race`,
`golangci-lint run`, `gofmt -l .` empty.

## Delivery

One PR from `feat/thread-stacked-layout`, in three milestones:

1. Layout, `stackFront`, render changes, breadcrumb, removal of auto-hide —
   a runnable build. **Stop for a hands-on feel test before continuing.**
2. `windowBounds` fix; mouse and window tests.
3. Stale-reference sweep and full verification. (Goldens were re-blessed
   in the task whose change moved them — Tasks 1–3 of the plan — so
   every commit stays green.)

The PR description closes #223 and #244, states that it supersedes #246 and
#149, and credits mkozjak (the `Enter`-through-`Update` test pattern and the
120-column `thread_open` golden) and piotrsynowiec (the stale `> Thread`
diagnosis).
