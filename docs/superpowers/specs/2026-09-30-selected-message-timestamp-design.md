# Selected-message long timestamp

## Purpose

Make it obvious *when* a message was sent, especially in threads, where a
reply can be days newer than its parent and the nearest day divider is
often off-screen. Today every header shows only `msg.Timestamp`, formatted
once in `cmd/slk/history.go:formatTimestamp` with
`appearance.timestamp_format` (default `3:04 PM`): no date at all.

## UX

The **selected** message's header shows a date-qualified timestamp; every
other row keeps the short one. Nothing to open, no new key. Moving the
selection with `j`/`k` (or clicking) moves the long timestamp with it.

```
  Priya Raman  3:40 PM                      ← unselected
▌ Sam Ortiz  Tue Sep 29, 3:42 PM            ← selected, same year
▌ Sam Ortiz  Mon Sep 29 2025, 3:42 PM       ← selected, earlier year
```

- Applies in both the messages pane and the thread panel, including the
  thread **parent** when it is selected.
- Applies whether or not the pane has focus: the unfocused selection
  still marks "the message you were on", and the long form is useful
  there too.
- The time part is `msg.Timestamp` unchanged, so the user's
  `timestamp_format` still governs it. Only a date prefix is added.
- **Except while a mouse text selection exists** in that pane (during a
  drag, and while the finished selection stays pinned until cleared):
  the selected row then shows the short timestamp. See "Amendment:
  text selection" below.

### Format

`LongTimestamp(ts, short)`:

| Case | Output |
|---|---|
| Message's local year == current year (`nowFunc()`) | `Mon Jan 2, ` + short → `Tue Sep 29, 3:42 PM` |
| Earlier (or later) year | `Mon Jan 2 2006, ` + short → `Mon Sep 29 2025, 3:42 PM` |
| `ts` unparseable, or `short` empty | `short` unchanged |

No relative words ("Yesterday") — the point is an exact date, and the day
dividers already provide the relative view. No seconds, zone, or edit time
(out of scope; see below).

### Width fallback

The selected and unselected variants of a row **must have identical
height**: `entryOffsets`, `totalLines`, scroll snapping and every
row-indexed hit map (reactions, images, sixel rows) are computed once per
entry and shared by both variants. The header line is not wrapped by the
renderer itself, but the per-variant `Width(width-1)` fill would wrap an
over-long line and add a row.

So the long form is used only when the header line, with the long
timestamp substituted, still fits in the content width the body is
wrapped to (`contentWidth` in each renderer). Otherwise the selected row
shows the short timestamp, exactly as today. Width is measured with
`lipgloss.Width` on the assembled header (username + status suffix + gap
+ timestamp + `(edited)` mark), so emoji in status suffixes are counted
correctly.

## Mechanism

### Why not "re-render on selection change"

Both panes pre-render each message once into two cached variants,
`linesNormal` and `linesSelected`, derived from a single `rendered`
string (`messages.Model.renderMessageEntry`; the reply loop in
`thread.Model`'s cache build). `j`/`k` only switches which cached variant
the flatten loop picks; it does not re-render. The long timestamp
therefore has to be baked into `linesSelected` at cache-build time. This
keeps selection movement O(1) and needs no new invalidation.

### Shared helpers (`internal/ui/messages`)

Both panes use these, so the header behaves the same in both (per AGENTS.md,
"if you change one model, change both"):

```go
// LongTimestamp returns short prefixed with the message's local date
// ("Tue Sep 29, " or "Mon Sep 29 2025, " when not the current year).
// Returns short unchanged when ts is unparseable or short is empty.
func LongTimestamp(ts, short string) string

// SelectedHeader returns rendered with the header's styled short
// timestamp replaced by the styled long one, provided the resulting
// header line is no wider than maxWidth. Otherwise returns rendered
// unchanged.
func SelectedHeader(rendered, header string, ts, short string, maxWidth int) string
```

`SelectedHeader` replaces the **first** occurrence of
`styles.Timestamp.Render(short)` in `rendered`. The first occurrence is
always the header: the only earlier styled text is the thread-broadcast
label (`↳ replied to a thread`), which has different text; `(edited)`
and body text come after. The year check reads the existing
package-level `nowFunc`, so `SetNowFunc` makes tests deterministic.

### Per-pane wiring

The substitution happens *before* the selected variant is filled and
bordered, and before `RepaintBgToSelectionTint`, so the tint is applied
to the long timestamp like any other span:

- `messages.Model.renderMessageEntry`: `renderedSel :=
  SelectedHeader(rendered, …)`; `renderedTinted` is derived from
  `renderedSel` instead of `rendered`. `linesNormal` and `linesPlain`
  still derive from the unmodified `rendered`, so clipboard text is
  unchanged. (Mouse→column mapping is *not* unchanged on the selected
  row while it shows the long form; see "Amendment: text selection".)
- `thread.Model` reply cache loop: same change at the same point.
- `thread.Model` parent: rendered outside the cache with
  `parentIsSelected`; apply `SelectedHeader` when `parentIsSelected`,
  before the selected border. `m.parentEntry.linesPlain` stays built
  from the unmodified content.

`renderMessagePlain` / `renderThreadMessage` need to expose the header
string (or enough to rebuild it) and their `contentWidth` so the caller
can check the width. Prefer returning them alongside the existing results
rather than recomputing, so the fit check uses exactly what was rendered.

### Invariants preserved

- `len(linesSelected) == len(linesNormal)` for every entry (enforced by
  the width fallback; asserted in tests).
- Plain text / clipboard / text selection read the short form. The long
  form is display-only, and is not displayed while a text selection
  exists.
- No I/O, no new service calls: everything derives from `msg.TS`,
  already on every `MessageItem`.
- `internal/ui/thread/lockstep_test.go` compares the two panes in one
  static state; both change identically, so it should still pass. If it
  compares selected rows and the thread parent differs, update the
  divergence list rather than forcing a match.

### Amendment: text selection (found during implementation)

The mouse drag-selection overlay (`messages.Model.applySelectionToRows`
and its thread twin) builds each highlighted row by cutting the
*displayed* row at columns taken from `linesPlain` and splicing in
`linesPlain` text. That requires the displayed row and its plain mirror
to be column-aligned. With the long form on screen and the short form in
`linesPlain`, dragging across the selected header garbled it
(`bob  9:00 AM` → `bob  Sun  AM`). A press also moves the selection
cursor to the clicked message, so nearly every drag crosses that row.

Resolution (chosen over "put the date after the time" and "copy the long
form"): while `hasSelection`, the selected row draws a short-timestamp
selected variant, i.e. exactly today's row, so the overlay, the format and
the short-form clipboard all stay as specified.

- Each entry keeps its unmodified content (`shortSrc`) when the selected
  variant carries the long form; `selectedLines` renders
  `linesSelectedShort` from it on first use and memoises it. Building it
  eagerly for every entry made full cache builds ~45–100% slower.
- The thread parent skips `SelectedHeader` while `hasSelection`; the
  thread view cache key includes `hasSelection`.
- `messages.Model` bumps `Version()` on `hasSelection` transitions only
  (begin from none, empty end, clear), so the App's bordered-render
  cache re-renders once per drag, never per cell of motion.

## Testing

Stdlib `testing`, white-box, test-first.

`internal/ui/messages`:
- `LongTimestamp`: same year; earlier year; unparseable `ts`; empty
  `short`. Clock pinned with `SetNowFunc`, restored with `t.Cleanup`.
- `SelectedHeader`: substitutes when it fits; returns input unchanged
  when the long header exceeds `maxWidth`; replaces only the header
  occurrence when the body contains the same time text.
- Model: the selected row's rendered view contains `Tue Sep 29, 3:42 PM`
  and unselected rows don't; after `MoveUp` the long form moves with the
  selection; at a narrow width the selected row shows the short form and
  `linesSelected`/`linesNormal` heights match for every entry;
  `SelectionText` of a copied selected row contains the short form only.

`internal/ui/thread`:
- The same model tests for a selected reply.
- Selected parent shows the long form; unselected parent shows short.

`internal/ui`:
- Goldens `base`, `thread_open` (and any others with a selected row) will
  change on purpose. Re-bless with
  `go test ./internal/ui -run TestGolden -update` and review the diff to
  confirm only the selected header changed.

Before merging: `go build ./...`, `go vet ./...`, `go test ./... -race`,
`gofmt -l .` empty, `golangci-lint run`.

## Documentation

Add `messages.LongTimestamp` / `messages.SelectedHeader` to the "Text and
rendering" table in `AGENTS.md` in the same commit.

## Out of scope

- Seconds, time zone, relative age ("2h ago").
- Edit time (Slack's `edited.ts` is not currently carried on
  `MessageItem`).
- Changing unselected headers or `timestamp_format` semantics.
- A message-info modal.
