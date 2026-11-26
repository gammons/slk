# Inline emoji images on sixel terminals

**Date:** 2026-11-25
**Status:** Implemented

## Problem

On a sixel terminal, slk draws avatars, message images and Block Kit images
as real images, but standard and workspace custom emoji inside message text
still draw as glyphs (standard) or as their `:name:` text (custom). Custom
emoji have no Unicode glyph, so `:thanks-ty:` is readable but not the image
Slack shows.

The emoji-image path already exists for kitty: `emoji.Place` returns a kitty
**unicode placeholder** string, which encodes the image ID and the position
in the cells themselves, so the terminal draws the image wherever the
placeholder is written. Sixel cannot do that: a sixel DCS stream is painted
at the cursor when it is written, and slk's renderer cannot write it inline
(the line would have to be re-emitted on every redraw and could never be
erased).

Sixel images in slk therefore go through the placement pipeline:
`imgpkg.SixelEntry` / `messages.SixelImage` carry the payload and a row and
column, and `PlaceSixel` (after `entrylist.Layout`) publishes
`imgpkg.SixelPaint`s that the App paints out-of-band, erases and clips.
Inline emoji need the same treatment.

## Design

### 1. Marker substitution while rendering

When sixel inline emoji is enabled (terminal protocol sixel, `emoji_images`
on, a fetcher, image cells N), `renderEmojiWith` uses a new sink instead of
the kitty path. For each `emoji.Token`:

- resolve the token's URL through the existing token pass (`ResolveEmojiToTokens`,
  which already handles shortcodes, codepoint names and workspace customs);
- **only workspace custom emoji get a marker.** Standard emoji keep their
  Unicode glyph: they already render well, have no missing content, and
  turning every emoji in history into an image would fetch a payload per
  occurrence for no readability gain;
- ask the sink to place the URL. Warm payload: the sink records the sixel
  payload and returns a **marker encoding the image's index**: for two-cell
  images a lead rune (U+E000) plus an index rune (U+E100 + n), and for
  one-cell images the index rune alone. The marker's width is exactly the
  image footprint, and the encoded index means a marker split by a wrap or a
  literal rune in the message cannot consume another image's placement. A
  cold payload starts the existing deduplicated fetch (with `CellTarget` =
  N x 1 cells) and returns the same marker; a later `EmojiImageReadyMsg`
  re-renders the message and the warm path takes over.
- a URL known to have failed keeps the token's plain text (`:name:`).
- resolution runs only on the visible text: OSC 8 control sequences (a
  link's hidden target) are copied verbatim, so a shortcode-looking path
  segment never becomes an emoji or an image. Standard emoji in the
  fallback path keep the old tone-stripping rule (`:+1::skin-tone-5:`
  still draws 👍).

The marker runes are Private Use Area characters. The body is not shown
with them: `RenderMessage` resolves every marker before returning.

### 2. Resolution to placements

`RenderMessage` resolves the markers **before** `placeAvatarBeside` adds
the avatar gutter, so a column is content-relative and the later
`contentColBase` stamp (1, or 6 beside an avatar) is applied exactly once:

- a marker's index picks its image from the sink, so a split or stray
  rune cannot shift another image. A marker whose runes a wrap put on two
  rows is dropped (its cells stay blank); a literal lead rune not followed
  by an index is the caller's text and stays;
- compute the display column with `ansi.StringWidth` of the line up to that
  point;
- replace the marker with N spaces, so the plain mirror, clipboard and
  selection never see PUA characters;
- append a `messages.SixelImage` with `row`, `col`, `width = N`, `height
  = 1`, the payload key and bytes. Cold entries (no bytes yet) are skipped;
  the reserved spaces stay until the ready message rebuilds the message.

Rows are content rows, the coordinate system `SixelRows` already uses; both
panes feed them to `PlaceSixel` unchanged.

A one-row image is fully visible exactly when its row is in the window, so
partial-visibility handling is the existing one and no fallback row is
needed (a blocked row draws its blank reservation rather than a halfblock
blob).

### 3. Fetching and caching

The sink reuses the emoji fetch machinery: `EmojiCacheKey` (the `E-` URL
hash), `spawnEmojiFetch` and its negative cache. The pane's image context
already calls `ConfigurePrerender(protocol)` on the shared fetcher, so a
fetch with `CellTarget` prerenders the sixel payload; the warm path reads
it with `Fetcher.Prerendered(key, cells x 1, ProtoSixel)` and runs its
`OnFlush` into a buffer once, off the render path.

`EmojiImageReadyMsg` already invalidates the rows whose text uses the URL
(`messageUsesEmojiURL`), which now includes codepoint-form names after the
emoji-name fix; a cold custom emoji therefore fills in on the next frame.

### 4. Startup switch

`cmd/slk/main.go`, after the interactive protocol probe settles, calls
`emoji.SetSixelInlineMode(enabled, cells)` with enabled =
`appearance.emoji_images == "on" && proto == ProtoSixel`. The kitty path
keeps `ImageModeActive`; the two are exclusive.

## Scope

First version: **workspace custom emoji in message body text** (the mrkdwn
rendered by `RenderMessage`'s body options), in both content panes.
Standard emoji keep their glyphs. Headers, reaction pills, the picker,
composer autocomplete, sidebar/status emoji and Block Kit text keep their
current rendering; they can adopt the same sink later by passing it in their
render options.

Not supported: sixel inline emoji on partial rows (the row is either drawn
or not), and terminals whose sixel probe fails remain on glyphs/text.

## Tests

- `internal/emoji`: marker width equals the image cells; a warm prerender
  returns a marker plus the payload bytes; a failed URL returns no marker.
- `internal/ui/messages`: a message body with a custom emoji renders N
  spaces and one `SixelImage` at the right row/column (with and without an
  avatar); multiple emoji on one row; a cold emoji reserves spaces without
  a placement and does not leak sentinels into `Plain` or a text selection;
  ordinary `:name:` text and a failed image stay text.
- Existing sixel image tests keep passing.

## Files

- `internal/emoji/inline_sixel.go` (new): marker, sink, payload capture.
- `internal/ui/messages/render.go`: `RenderSlackMarkdownOpts.InlineSixel`;
  `renderEmojiWith` branch.
- `internal/ui/messages/render_message.go`: allocate the sink for the body,
  resolve sentinels, append `SixelRows`.
- `cmd/slk/main.go`: `SetSixelInlineMode`.
- `AGENTS.md`: table rows for the sink and the startup switch.
