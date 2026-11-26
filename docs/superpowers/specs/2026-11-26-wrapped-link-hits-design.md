# Wrapped links stay clickable

## Problem

A long link label is hard-wrapped by `messages.WordWrap`. `ansi.Hardwrap`
keeps the OSC 8 open marker on the first row and the close marker on the last,
so the middle rows carry no marker. `ScanOsc8Spans` only registers a span that
opens and closes on one row, so clicking a wrapped link segment fell through
to the message (opening its thread) instead of opening the URL.

## Fix

`wrapLine` now builds its wrapped output in a local buffer and passes it
through `reopenOsc8Lines`. That walks the rows, tracks the hyperlink open from
the previous row, and puts an open/close pair on every row of a wrapped link.
The visible text is unchanged; the added markers are zero-width. Each row is
then a complete span, so `ScanOsc8Spans` records the label's own columns on
every row - not the pane border or avatar gutter before it - and every segment
opens the same URL.

The reordering is a no-op for lines without hyperlinks, and the fast path for
lines that already fit is untouched.

## Tests

- `TestWordWrapReopensWrappedLinks`: a long URL wraps into one span per row,
  all with the original URL.
- `TestWordWrapReopensSpacedLinkLabels`: a label containing spaces wraps the
  same way.
