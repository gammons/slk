# Activity card links, clipping and copy

**Date:** 2026-11-26
**Status:** Implemented

## Problem

An Activity card renders the hydrated message preview with the same
mrkdwn renderer as a message body, so a link in it gets the underlined
link style and an OSC 8 hyperlink. But:

- the card has no link hit-testing or click routing, so clicking a link
  selected/opened the card instead of opening the link;
- `clipToWidth` used a reflow truncator that cut ANSI/OSC sequences, so a
  long card line could lose half an OSC 8 sequence (a strange
  highlight and a broken hyperlink);
- `y` (copy message) and `o` (open links) acted on the hidden message
  pane's selection, not the Activity card on screen.

## Design

- `activityview.Model` records the OSC 8 spans of the card lines it last
  drew (`messages.ScanOsc8Spans`) as `activityLinkHit`s in the list's
  flat-line coordinates. `HitTestLink(rowY, col)` answers a pane-local
  click.
- `clipToWidth` uses `x/ansi.Truncate`, which keeps escape sequences
  whole, so a clipped card still has one complete OSC 8 opener and
  closer.
- `reduceMouseClick`'s Activity branch asks `HitTestLink` before
  `ClickAt`: a link opens (`OpenLinkMsg`), anything else selects/opens
  the card as before.
- `Model.SelectedBody` exposes the selected card's hydrated message.
  `copyMessageOfSelected` (`y`) copies its body, and `openLinksOfSelected`
  (`o`) extracts its links, both before the pane-selection path. The
  pane helper `copyMessageText` and the link-list picker are shared with
  the message panes.

## Tests

- `activityview`: a link in a card preview is found by
  `HitTestLink` at the drawn column and not past its label;
  `clipToWidth` leaves an OSC 8 pair whole and ends in an ellipsis.
- `ui`: a click on a card's link returns `OpenLinkMsg`; `y` copies the
  selected card's body and `o` opens its link rather than acting on the
  hidden pane.

## Files

- `internal/ui/activityview/model.go`: link hits, `HitTestLink`,
  `SelectedBody`, ANSI-safe `clipToWidth`.
- `internal/ui/reducer_mouse.go`: link-first click routing.
- `internal/ui/app.go`: `copyMessageText` / `openLinkList` shared with
  the Activity card branches.
