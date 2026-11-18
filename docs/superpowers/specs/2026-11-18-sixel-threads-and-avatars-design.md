# Sixel in the thread pane, and sixel avatars

## Problem

On a sixel terminal two things still drew as half-blocks:

- **Images in the thread pane.** The channel pane records each image's sixel rows and the App paints them out-of-band after the frame. The thread pane never recorded them, so `RenderMessage` spliced the half-block fallback into its lines, and `collectSixelPlacements` looked at the channel windows only. The phase 3 fork kept the thread pane's options as they were (decision P3) and the stacked-layout design returned no thread placements because "the thread already renders images as half-blocks". Neither recorded a reason the thread could not be painted.
- **Avatars.** `internal/avatar` drew avatars with kitty or half-block, never sixel, on the grounds that re-emitting a sixel stream per visible avatar per redraw would dominate the bandwidth budget. That predates the sixel painter, which diffs the placements of each frame against what is on screen and re-sends an image only when its slot, its content key or the text under it (the guard) changes.

## Design

### One placement loop for both panes

The channel pane's per-frame loop moves to `messages.PlaceSixel(frame, offsets, yOffset, height, images, blocked)`: a placement for each image whose whole height is in the window, the fallback grafted onto the visible rows of each cut one. `images(i)` returns the images of list entry `i`, and `blocked` lets the channel pane keep images off its loading and "more below" rows. `sixelEntry` becomes the exported `SixelImage` (fields still unexported) and `EntryPayload.SixelRows` is exported, so the thread pane can carry images it cannot look inside.

### The thread pane

`thread.renderMessage` sets `SixelRows`. Each reply's images ride its `EntryPayload`, the parent's (rendered every frame as entry 0) are taken from that frame's render, and View builds a slice of images index-aligned with the list's entries (nil for the dividers, landmark and spacers) for `PlaceSixel`. The zero-reply branch places the parent's images the same way. `SixelPlacements` is reset at the top of View so no path keeps a vanished image.

The App adds the thread's placements under the condition View draws the thread on (`threadVisible && ThreadWidth > 0`, so side by side, stacked in front, or full screen), through the existing `absoluteWindowSixelPlacements` with `threadSixelRect`: X from `layout.MsgEnd()`, the top border on row 0, and a bottom at the compose region's first row (`1 + PageHeight(PanelThread)`, with `renderThreadRegion`'s minimum of 3), so an image is never painted over the composer.

### Sixel avatars

`avatar.Cache.SetSixel(true)` (sixel terminals only; ignored with kitty) fetches the full-size source, as kitty does, and on load encodes the avatar once with the sixel renderer at its 4×2 footprint, beside the half-block render `Get` still returns. `GetSixel` returns the bytes and a placement key of the user plus a hash of the bytes, so a changed avatar repaints and nothing is hashed per frame.

`core.SixelAvatarService` is an optional extension of `AvatarService`; `App.SetAvatarService` type-asserts for it, so existing services and fakes are untouched. Both panes take a `messages.SixelAvatarFunc`, and with `SixelRows` and an avatar `RenderMessage` blanks the avatar's cells in the text and records a `SixelImage` at row 0, column 1 (right of the border, where `placeAvatarBeside` puts it), the half-block avatar being its fallback. From there it is an image like any other: placed when fully visible, half-block when a pane edge cuts it.

## Cost

An avatar is about 40×40 pixels at a 10×20 cell, a few kilobytes of sixel. The painter sends it when it first appears and when its slot changes, so scrolling re-sends the avatars in view, as it already does for inline images; moving the cursor re-sends the avatars on the two rows whose selection tint changed (the guard). A redraw that changes neither sends nothing. Terminals that cannot afford this can set `show_avatars = false`, which already removes avatars entirely.

## Not changed

- The draft preview still renders without `SixelRows`, so `SpliceSixelFallbacks` stays for it.
- The reply-row small avatars stay kitty-only: a 2×1 sixel image is no more readable than a 2×1 half-block one.
