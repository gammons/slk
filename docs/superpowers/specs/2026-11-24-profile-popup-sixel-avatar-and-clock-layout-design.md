# Profile popup: sixel avatar + clocks follow `appearance.timestamp_format`

Date: 2026-11-24

Two defects in the user-profile dialog, found together.

## 1. The avatar stayed half-block on sixel terminals

2fddd692 wired sixel avatars into the two content panes only: the panes
take a `messages.SixelAvatarFunc`, `RenderMessage` blanks the avatar's
4×2 cells and records a `SixelImage`, and `PlaceSixel` turns those into
painter placements. The profile dialog drew its avatar from
`a.avatarFn(userID)` — on sixel, the half-block render (4 cols × 2 rows,
the 4×4-in-half-blocks blob) — and had no placement path at all.

Worse, `collectSixelPlacements` returns nil while any overlay is open
("sixel is painted after the text, so any placement published now would
land on top of the modal"), so even the panes' avatars erase under the
popup.

### The fix

The dialog owns its avatar's slot, so the "lands on top of the modal"
hazard is exactly right for it:

- `userProfileLive()` fills new `userprofile.Live.AvatarSixelKey` /
  `AvatarSixel` fields from `a.sixelAvatarFn`, after `avatarFn` so the
  lazy preload has run (both come from the same preload; on kitty /
  kitty-emulated terminals `sixelAvatarFn` is nil and nothing changes —
  placeholders already work there through `DimmedOverlay` +
  `resolveKittyFrame`).
- When both the half-block render and sixel bytes are present,
  `nameRows` draws blank background-colored cells instead of the
  half-block text (`blankAvatar`, mirroring `RenderMessage`'s blanking),
  and `layout` records the avatar's box-local spot (body row 0, inner
  column 0 — same arithmetic as the email icon's `iconRow`/`iconCol`).
- `ViewOverlay` computes where the box landed with
  `overlay.Centered` — the centering formula `DimmedOverlay` composites
  with, extracted so the two cannot drift — and stashes an absolute
  `imgpkg.SixelPaint` on the model, exposed as `(*Model).SixelPaint()`,
  the same render-time-stash pattern as `lastLive`/`lastClickTarget`.
- `collectSixelPlacements`'s overlay branch now returns the profile
  modal's placement when it has one, else keeps the blanket withhold.
  The dialog is read-only with no stacking modals (`e`/`c` copy and
  toast), so nothing can sit over the slot. On close, the modal's
  placement vanishes and the painter repaints the panes'.

The card can outgrow a tiny terminal, and the painter draws exactly
where it is told — never clipped. So `avatarPaint` returns nil unless
the whole footprint lands inside the screen (`Row+Rows <= termH &&
Col+Cols <= termW`), and `ViewOverlay` then re-runs layout with the
sixel fields stripped: the half-block avatar draws and clips with the
card's text, instead of blanked cells with no image over them. The two
avatars share one footprint, so the box — and the fit that just failed —
cannot change between the passes. Wide terminals never hit this: the box
is clamped to `termW-4`, the header rows are never dropped, and the
centering clamps at 0.

## 2. The dialog's clocks ignored `appearance.timestamp_format`

`formatLocalTime` (the "Local time" row) and the DND row's "until"
clock hardcoded `"3:04 PM"`. The configured layout already had a home —
`messages.SetDateTokenTimeLayout` seeds a package var at startup from
`appearance.timestamp_format` — but with no getter. Exported it as
`messages.TimestampLayout()` and both clocks (plus the DM header's DND
suffix in `presence.go`, which hardcoded `"15:04"` for the same defect)
now format with it. No new wiring: the seed at `cmd/slk/main.go` already
runs for the `<!date` tokens, and every clock follows one setting.

## Tests

- `userprofile/sixelavatar_test.go`: with sixel bytes, the header rows
  show blanks where the half-block avatar was and `SixelPaint()` reports
  the placement on exactly those cells (cross-checked against a
  half-block frame rendered with the same footprint); without them, the
  half-block draws and nothing is placed; both parts required.
- `sixelpaint_profile_test.go` (package ui): with the modal open, the
  frame's one placement is the avatar's (key `avatar-U9`, 2×4, on the
  blocky variant's cells); without a sixel service, none; after close,
  none.
- `localtime_test.go` / `view_test.go`: both clocks follow a configured
  `15:04` layout (global restored via `t.Cleanup`).
