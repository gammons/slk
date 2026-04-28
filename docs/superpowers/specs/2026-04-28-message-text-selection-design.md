# Mouse Drag-Select & Auto-Copy in Message Panes

**Date:** 2026-04-28
**Status:** Design (approved)

## Problem

`slk` enables `tea.MouseModeCellMotion`, which captures all mouse events
inside the alternate screen. As a side effect, the terminal emulator's
native click-and-drag text selection is unavailable inside the app: users
cannot select message text the way they can in any other terminal program
(or in `opencode`-style TUIs).

We want the same UX users already expect from `opencode`: drag with the
mouse to highlight a region of message text, release the mouse to copy it
to the system clipboard. No modifier keys, no extra commands.

## User-Visible Behavior

- In the **messages pane** and **thread pane**, pressing the left mouse
  button anchors a selection at the clicked cell.
- Holding the button and moving the mouse extends the selection. Selected
  cells are highlighted with a reverse / theme-accent overlay.
- Dragging within ~1 row of the top or bottom edge of the pane auto-scrolls
  that pane and continues to extend the selection while the button is
  held.
- Releasing the button:
  - If the selection covers ≥1 character, the plain-text contents are
    written to the system clipboard via OSC 52, and the status bar shows
    a brief toast: `Copied N chars`.
  - If no drag occurred (a plain click), selection clears and the existing
    click-to-focus / click-to-select-message behavior runs as before.
- Selection is cleared by: starting a new drag, pressing `Esc` in normal
  mode, switching channels or threads, or any keypress that mutates focus
  (the existing focus-mutating handlers gain a single `clearSelection()`
  call).
- Selection persists across scrolling and across newly-arriving messages
  by anchoring its endpoints to message IDs (Slack message timestamps).
- Sidebar, workspace rail, compose box, and status bar are out of scope:
  drags there fall through to existing handlers untouched.

### Copy fidelity (resolved)

The clipboard receives plain visible text:

- ANSI styling is stripped.
- Emoji glyphs stay as glyphs (`🚀`, not `:rocket:`).
- Mentions stay in their rendered form (`@alice`, not `<@U123>`).
- OSC 8 hyperlinks: the display text is copied; the underlying URL is
  dropped. (Users who want raw URLs already have other affordances; we can
  revisit if it becomes a real complaint.)

## Architecture

Two coordinated additions, plus a small amount of glue in `App`.

### A. New `internal/ui/selection/` package

A pure, panel-agnostic data structure with no UI dependencies.

```go
package selection

// Anchor identifies one endpoint of a selection.
//   MessageID is the Slack TS of the anchored message, or "" when the
//     anchor sits on a non-message row (e.g. a date separator). An
//     anchor on a separator is treated as a line boundary, not a
//     character position.
//   Line is the 0-indexed line within that message's rendered block
//     (after wrapping).
//   Col is the display column inside that line; columns are 0-indexed
//     and measured in display cells (wide chars occupy 2).
type Anchor struct {
    MessageID string
    Line      int
    Col       int
}

// Range is a half-open [Start, End) selection. Endpoints may be in any
// order; consumers must call Normalize before iterating.
//   Active is true while the user is still dragging. Renderers use this
//   to decide whether to draw the live highlight.
type Range struct {
    Start  Anchor
    End    Anchor
    Active bool
}

func (r Range) Normalize() (lo, hi Anchor)   // ordered (lo ≤ hi)
func (r Range) IsEmpty() bool                // Start == End
func (r Range) Contains(a Anchor) bool       // half-open membership
```

Tested in isolation. No knowledge of `lipgloss`, `tea`, message structs,
or rendered cache.

### B. `Selectable` capability on `messages.Model` and `thread.Model`

The render cache (`viewEntry`) gains one parallel slice per entry.

```go
type viewEntry struct {
    linesNormal   []string  // existing: pre-styled lines for normal cursor
    linesSelected []string  // existing: pre-styled lines for cursor-selected message
    linesPlain    []string  // NEW: ANSI-stripped, column-aligned plain text
    height        int
    msgIdx        int
}
```

`linesPlain[i]` represents the same line as `linesNormal[i]`, ANSI-stripped
and padded so that **byte cluster N corresponds to display column N**:

- Each ASCII / narrow rune is one cluster occupying one column.
- Each wide rune (CJK, most emoji) is one cluster occupying columns N
  and N+1; column N+1 holds a zero-width sentinel that is dropped during
  text extraction. The sentinel is internal to the package and never
  exposed.
- Trailing render padding (background-fill spaces) is preserved in
  `linesPlain` so column math stays consistent, but is right-trimmed on
  extraction.

The `Model` exposes:

```go
func (m *Model) BeginSelectionAt(viewportY, x int)
func (m *Model) ExtendSelectionAt(viewportY, x int)
func (m *Model) EndSelection() (text string, ok bool)
func (m *Model) ClearSelection()
func (m *Model) HasSelection() bool
func (m *Model) ScrollHintForDrag(viewportY int) int   // -1, 0, +1

// Test/debug accessor:
func (m *Model) SelectionText() string
```

Coordinates passed in are pane-local (after the existing `-1` border
adjustment that `ClickAt` already does). Out-of-range Y values clamp to
the nearest row inside the rendered area.

A small `messageIDToEntryIdx map[string]int` is populated during
`buildCache` so `(MessageID, Line, Col)` resolves to absolute line indices
in O(1).

### C. App-level glue (`internal/ui/app.go`)

A small drag FSM lives on `App`:

```go
type dragState struct {
    panel            Panel  // PanelMessages or PanelThread; "" when idle
    pressX, pressY   int    // pane-local coordinates of the press
    lastX, lastY     int    // most recent motion coordinates
    moved            bool   // true after the first MouseMotionMsg
    autoScrollActive bool
}
```

Event handling:

- `tea.MouseClickMsg` (left button, inside messages or thread pane):
  - Record press coordinates in `dragState`.
  - Call the panel's `BeginSelectionAt(...)`. The selection is created
    but `Active=true` and zero-width — the existing click-to-focus and
    click-to-select-message behavior still runs.
- `tea.MouseMotionMsg` (left button held, same panel as the press):
  - Set `moved = true` on the first such message.
  - Call `ExtendSelectionAt(...)`.
  - If `ScrollHintForDrag(y)` is non-zero, schedule (or refresh) a
    `tea.Tick(50ms)` that scrolls one line in that direction and
    re-extends. The tick stops when the cursor leaves the edge or the
    button is released.
- `tea.MouseMotionMsg` outside the originating panel:
  - Clamp the motion coordinates to the panel's bounding rect; the
    selection cannot escape the panel it started in.
- `tea.MouseReleaseMsg`:
  - If `dragState.moved`: call `EndSelection()`. If non-empty, return
    `tea.Batch(tea.SetClipboard(text), statusbar.CopiedMsg{N: len(...)})`.
    Selection becomes inactive but the highlight persists until cleared.
  - If not moved: fall through to the existing single-click handler
    (`ClickAt`), and clear any prior selection.

Existing focus-mutating events (channel switch, thread open/close, key
input that changes mode) gain a single `clearSelection()` call.

## Rendering Selection Highlight

The current render is fast because lines are pre-styled strings sliced
into the viewport. Re-applying selection per-frame must stay cheap.

**Approach: viewport-time overlay, never cache mutation.**

In `View()`, after computing the visible window, for each visible line
that intersects the active selection range:

```
prefix   := ansi.Truncate(linesNormal[i], colLo, "")
selected := selectionStyle.Render(linesPlain[i][colLo:colHi])
suffix   := ansi.TruncateLeft(linesNormal[i], colHi, "")
visible[row] = prefix + selected + suffix
```

using `github.com/charmbracelet/x/ansi` (already an indirect dep) for
cell-accurate truncation. Lines wholly inside the selection use one
full-line render (`selectionStyle.Width(width).Render(linesPlain[i])`).

Selection style is a new pair of theme entries:

```go
SelectionBackground lipgloss.Color // default: theme accent mixed with surface
SelectionForeground lipgloss.Color // default: theme text
```

If a theme doesn't define them, the style falls back to
`lipgloss.NewStyle().Reverse(true)`, which honors whatever foreground /
background lipgloss is rendering against.

**Cost analysis.** Only visible lines that actually intersect the
selection are re-composed. A typical drag covers fewer than 30 lines.
`buildCache` is never invalidated by selection changes — we keep reusing
the same `linesNormal` / `linesPlain` slices. Mouse motion already arrives
much faster than scrolling, so this is the right place to spend cycles.

## Plain-Text Extraction & Clipboard

`EndSelection()` walks the cache from the normalized `lo` to `hi`:

- For each line wholly inside the range, append the right-trimmed
  `linesPlain[i]` followed by `"\n"`.
- For the first / last partial line, slice `linesPlain[i][colLo:colHi]`
  before appending.
- Wide-char continuation sentinels are dropped during the slice.
- A single trailing newline is stripped from the assembled string.

The result is delivered with `tea.SetClipboard(text)`, which emits an
OSC 52 escape. No native-clipboard dependency is added.

OSC 52 has no reliable round-trip in practice, so we do not detect
whether the terminal accepted the write. Terminals known to need an
opt-in setting (tmux `set-clipboard`, some Kitty configs, screen, etc.)
are documented in the README's troubleshooting section. The status bar
toast says `Copied N chars` on the assumption it worked; failure is rare
enough that a probe-and-fallback is overkill for v1.

## Anchoring Across Scroll & New Messages

Each anchor stores `(MessageID, Line, Col)`. Resolution to absolute cache
coordinates happens through the new `messageIDToEntryIdx` map populated
in `buildCache`. Cache rebuilds (new message, width change, channel
switch within the same model) re-resolve anchors on the fly.

If a message referenced by an endpoint is deleted, that endpoint
collapses to the nearest surviving anchor (the next message above for
`lo`, the next message below for `hi`). If neither side has a surviving
neighbor, the selection clears.

A channel switch always clears selection — anchors are scoped to a
single channel's message stream. A thread open/close does not clear
selection in the messages pane (and vice versa).

## Testing

- **`internal/ui/selection/`** — unit tests for `Normalize`, `IsEmpty`,
  `Contains`, plus a property-style test that random pairs of anchors
  always normalize to a `lo ≤ hi` ordering.
- **`internal/ui/messages/`**:
  - Build a model with synthetic messages (including wide-char and
    emoji content), drive `BeginSelectionAt` → `ExtendSelectionAt` →
    `EndSelection`, and assert the returned plain text. Rendering goes
    through the real `buildCache` so wide-char alignment is exercised
    end-to-end.
  - Selection survives `AddMessage` (new top, new bottom) — anchor
    endpoints stay on the same characters.
  - Click-without-drag returns no clipboard text and runs the existing
    `ClickAt` selection.
  - `ScrollHintForDrag` returns `-1` at the top edge, `+1` at the
    bottom, `0` in the middle.
  - Selection clears when the channel changes; persists when the cache
    is rebuilt for a width change.
- **`internal/ui/thread/`** mirrors the messages-pane suite for the
  thread panel.
- **`internal/ui/app_test.go`** — feed a sequence of `MouseClickMsg`,
  two `MouseMotionMsg`, `MouseReleaseMsg` into `App.Update`. Assert a
  `tea.SetClipboard` command is returned in the batch and a `CopiedMsg`
  is delivered to the status bar.

## Out of Scope (Explicit YAGNI)

- Selection in sidebar, workspace rail, compose, or status bar.
- Markdown-source copy (Slack-syntax reconstruction).
- Native-clipboard fallback (xclip / wl-copy / pbcopy / golang.design/x/clipboard).
- Keyboard-driven visual mode (`v`, `V`, `y`). The `selection` package
  is designed so this can be layered on later without changing its API.
- Selection that spans both the messages pane and the thread pane.
- Right-click context menu, "copy as quote", "copy permalink".

## Files Touched (Summary)

- `internal/ui/selection/` — new package (3 files: `selection.go`,
  `selection_test.go`, plus a small `doc.go`).
- `internal/ui/messages/model.go` — `viewEntry.linesPlain`, the
  `messageIDToEntryIdx` map, the `Selectable` methods, and the
  selection-overlay branch inside `View()`.
- `internal/ui/messages/render.go` — emit `linesPlain` alongside
  `linesNormal` / `linesSelected` during `buildCache`.
- `internal/ui/thread/model.go` — same set of changes as
  `messages/model.go`. (If duplication starts hurting, a follow-up may
  factor a shared mixin; not in scope here.)
- `internal/ui/app.go` — `dragState`, `MouseMotionMsg` /
  `MouseReleaseMsg` handlers, auto-scroll tick, batched
  `tea.SetClipboard` + `CopiedMsg` on release.
- `internal/ui/styles/themes.go` + `themes_test.go` — new
  `SelectionBackground` / `SelectionForeground` colors with sensible
  fallbacks.
- `internal/ui/statusbar/model.go` — accept `CopiedMsg` and show a
  short-lived toast.
- `README.md` — one paragraph on drag-to-copy, OSC 52 caveats, and the
  short list of terminals that need an opt-in for clipboard writes.
