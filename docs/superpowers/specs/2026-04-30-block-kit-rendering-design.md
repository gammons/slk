# Block Kit & Legacy Attachments Rendering

**Status:** Design
**Date:** 2026-04-30
**Goal:** Make bot/app messages render correctly in slk by parsing and displaying Slack's `blocks` and legacy `attachments` fields, both of which are currently ignored.

---

## Problem

Today the Slack `Message.Blocks` field and the legacy `Message.Attachments` field are dropped on the floor. Only `text`, `files`, and reactions render. The result: bot messages from GitHub, PagerDuty, CI/CD tools, deploy bots, and on-call rotations frequently render as a one-line summary (or empty), with no fields, no buttons, no color, no structure. This is the single largest visual gap between slk and the official Slack client for read-only consumption.

The slack-go dependency already exposes the necessary types; they are simply unused.

## Scope

### In scope (Tier 0 — "make bot messages look right")

**Block types rendered:**
- `section` — text body, optional `fields` 2-column grid, optional `accessory`
- `header` — bold, primary-colored, full width
- `context` — small muted line mixing inline text and small image elements
- `divider` — horizontal rule using theme `Border` color
- `image` — full image block via existing image pipeline + optional title
- `actions` — row of muted, non-interactive control labels

**Section accessories rendered:**
- `image` — small thumbnail, hard-capped at 4 rows × 8 cols
- `button`, `overflow`, `*_select`, `datepicker` — muted label, no interactivity

**Action elements rendered (muted, non-interactive):**
- `button` → `[ Label ]`
- `*_select` → `Placeholder ▾`
- `overflow` → `⋯`
- `datepicker` → `📅 YYYY-MM-DD`

**Legacy `attachments` field** — full visual: colored left border (1 col wide), `pretext` above, then `title` (OSC-8 link if `title_link`), `text`, `fields` 2-column grid, `image_url`, `thumb_url`, `footer` line with optional `footer_icon` and timestamp.

**Interactive hint:** When a message contains any interactive control, append a single muted line `↗ open in Slack to interact` (plain text, no OSC-8 in v1). Once per message, regardless of how many controls exist.

**Unsupported block types:** Render as a single muted placeholder line `[unsupported block: <type>]`.

### Out of scope

- `rich_text` walker. Continue using the `Message.Text` field through `RenderSlackMarkdown` as today. (Today's lists/quotes-in-rich_text limitations remain.)
- `input` blocks (modals only in practice).
- `video`, `file`, `markdown` block types — render as the unsupported placeholder for now.
- Any actual interactivity wiring (button POSTs, modal submission, `block_actions` handling).
- Permalink-resolved OSC-8 link on the `↗ open in Slack` hint. Plain text in v1.

## Design

### Composition order in a rendered message

Top to bottom inside a single `MessageItem`:

1. Username / timestamp header (existing)
2. `Message.Text` rendered via `RenderSlackMarkdown` (existing, preserved)
3. Blocks, in array order (new)
4. Legacy attachments, in array order (new)
5. File attachments via `renderAttachmentBlock` (existing)
6. Thread indicator and reactions (existing)
7. `↗ open in Slack to interact` line, only if any interactive control rendered (new)

`Text` is intentionally NOT skipped when blocks are present. Slack bot messages often populate both, and string-level dedup is fragile. Occasional duplication is accepted for v1.

### Data model

New fields on `internal/ui/messages/MessageItem`:

```go
type MessageItem struct {
    // ...existing fields...
    Blocks            []blockkit.Block             // parsed at ingest
    LegacyAttachments []blockkit.LegacyAttachment  // parsed at ingest
}
```

`internal/cache/Message` schema is unchanged. The existing `RawJSON` column already persists the source of truth, and parsing happens on hydration alongside the existing message-to-`MessageItem` conversion.

`Block` is implemented as a sealed interface (or tagged union, depending on what reads cleanest in Go) with one variant per supported block type plus an `UnknownBlock{Type string}` catch-all. Section accessories and action elements use the same pattern in their own narrower interfaces.

### Module layout

A new package `internal/ui/messages/blockkit/`:

| File | Responsibility |
|---|---|
| `types.go` | `Block`, `LegacyAttachment`, accessory and element types |
| `parse.go` | `Parse(slack.Blocks) []Block`, `ParseAttachments([]slack.Attachment) []LegacyAttachment` |
| `render.go` | `Render(blocks []Block, ctx Context, width int) RenderResult` |
| `attachments.go` | `RenderLegacy(atts []LegacyAttachment, ctx Context, width int) RenderResult` |
| `styles.go` | lipgloss styles consuming the active theme |
| `color.go` | hex / `good`/`warning`/`danger` → theme color mapping |
| `testdata/` | real-world Slack JSON payloads + golden snapshots |

`RenderResult` matches the tuple shape returned by the existing `renderAttachmentBlock` (`internal/ui/messages/model.go:1213`):

```go
type RenderResult struct {
    Lines       []string         // rendered, ANSI-styled
    Flushes     []KittyFlush     // kitty image upload callbacks
    SixelRows   []SixelRow       // sixel passthrough rows
    Height      int              // total rows occupied
    Hits        []HitRect        // clickable regions for image previews
    Interactive bool             // any non-interactive control rendered
}
```

### Integration points

- **Parsing:** `cmd/slk/main.go` `extractAttachments` (around line 1098) gains siblings `extractBlocks` and `extractLegacyAttachments`. All four call sites that build `MessageItem` (lines 1259, 1322, 1386, 1587) populate the new fields.
- **Rendering:** `internal/ui/messages/model.go` `renderMessagePlain` (line 988) calls `blockkit.Render(...)` and `blockkit.RenderLegacy(...)` between steps 2 and 5 of the composition order. The returned tuple flows into the existing flush/sixel/hit aggregation in `viewEntry`.
- **Image hits:** Image blocks register a `HitRect` that dispatches the existing `OpenImagePreviewMsg`. No new app-level message types needed for v1.

### Visual specifications

**`section`**
- Body: `RenderSlackMarkdown` at the available width.
- `fields`: 2-column grid, each cell wrapped to `(width - gutter) / 2`. Label rendered bold-muted, value plain. Falls back to single-column stack when `width < 60`.
- `accessory` placed to the right of the body. Body wraps to `width - accessoryWidth - gutter`. Falls back to below-body when `width < 60`.

**`header`** — bold, theme `Primary` foreground, full width, single line (truncated with ellipsis if needed).

**`context`** — single line, muted, mixing inline `RenderSlackMarkdown` text and small inline image elements (1 row × 2 cols half-block).

**`divider`** — full-width horizontal rule using theme `Border`.

**`image` block** — optional title (small bold) above, image rendered via `ImageContext` at full `max_image_rows`, clickable to full-screen preview. Cache key derived from URL hash.

**`actions`** — row of muted pills wrapped to `width`. Sets `Interactive=true` on `RenderResult`.

**Accessory image** — hard cap 4 rows × 8 cols regardless of `max_image_rows`. Different cache key suffix to allow distinct sizing in the image cache.

**Muted control look** — `text_muted` foreground, `surface_dark` background, no border, single space horizontal padding inside brackets/labels.

**Legacy attachment** — visual:

```
[pretext line, full width, markdown rendered]
█ <title — bold, OSC-8 to title_link if present>
█ <text — markdown rendered>
█
█ Field1 Label    Field1 Value         Field2 Label    Field2 Value
█ Field3 Label    Field3 Value
█
█ <image_url rendered via ImageContext>
█ <footer_icon> footer text · timestamp
```

The `█` glyph is rendered at 1-col width in the attachment's color:
- `good` → theme green
- `warning` → theme warning
- `danger` → theme error
- 6-digit hex → parsed verbatim
- missing → theme `Border`

`thumb_url` placement: small accessory-sized thumbnail to the right of `text`, same cap as section accessories.

**Hint line** (when `Interactive=true`): single line, muted, `↗ open in Slack to interact`. Plain text, no link in v1.

**Unsupported placeholder** — single muted line `[unsupported block: <type>]`.

## Testing strategy

Following existing project patterns (24 test files alongside 31 sources, golden-file approach in render tests):

- **`blockkit/parse_test.go`** — parse real Slack JSON payloads from `testdata/`. Verify field extraction, unknown-type handling, missing-field tolerance.
- **`blockkit/render_test.go`** — render parsed payloads at three fixed widths (60, 100, 140) and snapshot the plain-text output (ANSI stripped) into `testdata/golden/`. Six initial fixtures: GitHub PR notification, PagerDuty alert, deploy approval bot, on-call handoff bot, plain `section` + `fields`, `header` + `divider` + `section` combo.
- **`blockkit/attachments_test.go`** — same pattern for legacy attachments. Verify color-stripe character + style via fixture comparison rather than full ANSI snapshot.
- **Integration:** extend `internal/ui/messages` model tests to assert composition order with mixed `Blocks` + `Attachments` (files) + reactions, and that the cached `viewEntry` is stable across re-renders.
- **Image pipeline:** reuse the existing fake `ImageContext` test scaffolding to verify hit-rect registration and cache-key derivation for both image blocks and accessory thumbnails.

## Risks and accepted tradeoffs

- **Width below ~60 cols** — accessories and field grids collapse to stacked layouts. Breakpoint chosen at 60 cols and applied uniformly.
- **`Text` / leading `section` duplication** — accepted. Some bot messages will show their summary twice. Revisit if reports come in.
- **No permalink on the interactivity hint** — accepted in v1 to avoid coupling rendering to async permalink resolution. Trivial to upgrade later.
- **Animated GIF blocks** — render as static first frame, same as today's file images.
- **`color` field on legacy attachments without theme mapping for arbitrary hex** — parsed verbatim and rendered directly via lipgloss. May clash with some themes; this is a known limitation of letting bots specify their own colors.

## Future work (explicitly deferred)

- `rich_text` walker for ordered/nested lists and `rich_text_quote`
- Real interactivity (button POSTs, modal handling, `block_actions`)
- `markdown`, `video`, `file` block types
- Permalink-resolved OSC-8 on the interactivity hint
- Per-bot rendering tweaks if specific bots need it
