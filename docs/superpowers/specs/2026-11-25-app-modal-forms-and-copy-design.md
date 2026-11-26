# App modal form fields and text copy

**Date:** 2026-11-25
**Status:** Implemented
**Builds on:** `2026-11-25-app-modals-design.md`

## Problem

A modal whose view carries `input` blocks (the AI Budget tool's budget
increase form, for example) drew every field as
`[unsupported block: input]`. The dialog was unreadable: field labels,
current values, placeholders and hints were all lost.

The dialog was also not selectable. A drag inside a modal was consumed as a
click, so text could not be copied even though every message pane copies a
drag selection on release.

## Scope

1. **Read-only input fields.** Render `input` blocks with their label, the
   value the view carries or the placeholder the app would show, and the
   hint. Forms still cannot be filled in or submitted; this is display only.
2. **Text selection and copy in an app modal.** A drag inside the dialog body
   highlights text and copies it on release, with the same clipboard and
   status-bar path as a message pane. Buttons keep their click behavior.

Not supported: editing, validation, submission, selects opening their
options, or mouse-wheel/keyboard scrolling changing the selection.

## Design

### Input fields

`core/blocks.InputBlock` carries the block id, label, hint, placeholder,
value, element kind (`plain_text_input`, `static_select`, ...), and the
optional/multiline flags. `blockkit.parseInput` fills it from slack-go's
`InputBlock`, extracting the element's placeholder/initial value.

`appendInput` draws:

- the label in the field-label style, with `(optional)` appended when Slack
  marks it so;
- for a plain-text input, one row per wrapped line of the value (or the
  placeholder in muted italics, or an em dash), prefixed by a muted `▌ ` bar;
- for other element kinds, the existing control label (`static_select ▾`,
  `📅 date`, ...);
- the hint, wrapped and muted.

`RendersBody` treats an input with any label/value/placeholder as body
content, as it does for the other blocks.

### Selection

The dialog body is a list of rendered lines, not an `entrylist`, but the
column model is the same: `entrylist.PlainLines` gives a plain mirror with a
column-to-byte map, and `entrylist.DisplayWidth` / `SliceColumns` measure and
slice it. `appview.Model` keeps:

- `plain`, rebuilt in `View` from the block renderer's lines;
- a `selection.Range` in content line/column coordinates, plus `hasSel`;
- `lastBodyTop` and `lastLeftPad`, the box-local geometry of the body, so a
  click can be mapped back to a content line and column.

`View` paints a pinned or active selection onto the visible body rows with
`styles.SelectionStyle()`, using the same `ansi.Cut` + `SliceColumns` splice
as `entrylist`'s overlay. Scrolling moves the window; the selection stays on
its content lines.

`BeginSelectionAt`, `ExtendSelectionAt`, `Selecting`, `EndSelection` and
`SelectionText` are the model's selection API. A drag outside the body clamps
to the nearest body cell. A new view clears the selection.

### Mouse routing

- A left press inside the dialog first goes through the existing button point
  hit-test. A button press wins. Otherwise `modalClickTarget.onInside` starts
  a selection at the box-local cell.
- `dragState.Handle` intercepts `MouseMotionMsg` / `MouseReleaseMsg` while
  the mode is `ModeAppView`: motion extends the modal selection, release
  copies it (`clipboardWrite` + `statusbar.CopiedMsg`) and returns, so the
  pane drag FSM never sees these events.
- A click outside the box still dismisses the dialog; a click inside that is
  not in the body (title, padding, footer) starts nothing.

## Tests

- `blockkit`: parse an input block (label, hint, placeholder, initial value);
  render shows label, value/placeholder and hint and never the unsupported
  marker; a value wins over the placeholder.
- `appview`: a drag over a known cell copies exactly that text; a click
  outside the body does not select; a drag past the edges clamps; a new view
  clears the selection.
- `ui`: end-to-end through `Update`: open a modal, press-drag-release over
  body text, the clipboard receives it and the status bar shows the copy
  notice.

## Files

- `internal/core/blocks/blocks.go`: `InputBlock`.
- `internal/ui/messages/blockkit/parse.go`, `render.go`, `types.go`: parse
  and draw it.
- `internal/ui/appview/select.go` (new) and `model.go`: selection state,
  highlight, geometry.
- `internal/ui/reducer_modal_click.go`: `onInside` for the app view.
- `internal/ui/drag.go`, `internal/ui/app_view.go`: motion/release routing and
  the copy.
