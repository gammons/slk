# Inline Image Rendering — Design

**Date:** 2026-04-29
**Status:** Draft
**Owner:** @gammons

## Summary

Render Slack image-file attachments inline in the messages pane, with click-to-fullscreen preview. Use the kitty graphics protocol on capable terminals, sixel on terminals that support it, and the existing half-block (`▀`) renderer everywhere else. Reuse the same renderer for avatars (always half-block for performance).

This spec replaces the placeholder description in `2026-04-23-slack-tui-design.md:401-432` and the STATUS.md line "Inline image rendering (Kitty graphics > Sixel > fallback)".

## Goals

- Auto-render image attachments inline as they scroll into view, lazy-loaded.
- Cap inline images to ~20 terminal rows tall, fit within the message-pane width, aspect preserved.
- Click on an inline image (or press `O` on the selected message) to open a full-screen in-app preview.
- Support kitty graphics protocol (kitty, ghostty, recent WezTerm), sixel (foot, mlterm, opted-in xterm/wezterm), and half-block fallback (any 24-bit-color terminal).
- Reuse one image-rendering pipeline for both inline message images and avatars.
- Migrate the existing `~/.cache/slk/avatars/` files into the unified `~/.cache/slk/images/` cache.

## Non-Goals

These are out of scope for v1; each could be its own spec later:

- Link unfurl previews (rendering `og:image` from URL-only messages).
- Non-image attachments (PDFs, videos, audio) — they continue to render as `[File] <url>` text.
- Inline rendering in the threads sidebar — threads keep `[Image] <url>` for v1.
- Animated GIF playback — only the first frame is rendered for v1.
- Save-to-disk keybind for images.
- Drag-to-copy text selection across image regions — image rows are unselectable empty space in the selection layer.
- Active terminal-capability querying (XTSMGRAPHICS, kitty `?` query at startup beyond a single version probe).

## Decisions Summary

| Decision | Choice |
|---|---|
| Protocol scope | Kitty + Sixel + half-block fallback |
| Render trigger | Auto-render, lazy-loaded as messages scroll into view |
| Inline size cap | 20 rows tall, fit message width, aspect preserved |
| Click behavior | Mouse click or `O` opens full-screen preview overlay |
| Preview region | Covers messages + thread panes; sidebar + status bar visible |
| Capability detection | Env-var heuristics + `[appearance] image_protocol` config override |
| tmux behavior | Force half-block when `TMUX` is set |
| Thumb selection | Smallest Slack `Thumb*` ≥ target render box; `Thumb1024` for preview |
| Cache eviction | LRU by total size, default 200 MB cap |
| Animated GIFs | First-frame static for v1 |
| Avatars | Always half-block, regardless of detected protocol |
| Loading placeholder | Reserved-height block with centered `⏳ Loading <filename>...` |
| Sixel encoder | `github.com/mattn/go-sixel` dependency |

## Architecture

A new package `internal/image/` owns the entire image lifecycle. It exposes a small typed interface to the UI layer; the UI does not need to know which protocol is active.

```
internal/image/
  capability.go     // protocol detection: Off | HalfBlock | Sixel | Kitty
  cellmetrics.go    // pixels-per-cell estimation (env vars, ioctl, fallback)
  cache.go          // ~/.cache/slk/images/, LRU by size, 200 MB cap
  fetcher.go        // HTTP GET, decode PNG/JPEG/GIF[0], downscale, single-flight
  renderer.go       // top-level Render type and Renderer interface
  halfblock.go      // ▀ encoder, generalized from internal/avatar
  kitty.go          // kitty graphics with unicode-placeholder placement
  sixel.go          // wraps mattn/go-sixel, fits-fully-on-screen gating
  preview.go        // full-screen overlay sub-component
```

### Top-level data shape

```go
type Render struct {
    Cells    image.Point   // (cols, rows) in terminal cells
    Lines    []string      // exactly Cells.Y rows; baked into viewEntry
    Fallback []string      // halfblock fallback for sixel partial-visibility
    OnFlush  func(io.Writer) error // optional pre-frame side effect (kitty upload)
    ID       uint32        // protocol-specific image ID; 0 if none
}

type Renderer interface {
    Render(img image.Image, target image.Point) Render
}
```

**Invariant:** `Lines` is always exactly `Cells.Y` entries long, each `Cells.X` cells wide (per `lipgloss.Width`). The messages-pane render cache treats them like any other line — no protocol awareness needed in the slicer.

## Capability Detection

`Detect(env, cfg) Protocol` runs at startup once.

1. If `cfg.ImageProtocol != "auto"`, honor it directly. (`"off"` disables image rendering completely; the UI falls back to today's `[Image] <url>` text.)
2. If `TMUX` is set → `ProtoHalfBlock`. No tmux-passthrough heuristics in v1.
3. If `KITTY_WINDOW_ID` is set, or `TERM == "xterm-kitty"`, or `TERM_PROGRAM` is `"ghostty"` or `"WezTerm"` → `ProtoKitty`.
4. If `TERM_PROGRAM == "iTerm.app"` and version ≥ 3.5 → `ProtoKitty` *but* with a known limitation: iTerm2 does not support unicode placeholders, so the runtime version probe (below) falls it back to half-block.
5. If `TERM` matches `foot|mlterm` → `ProtoSixel`. (Conservative — not auto-enabled for `xterm-256color` claimants without an additional `XTERM_VERSION` or explicit opt-in.)
6. Otherwise → `ProtoHalfBlock`.

**Kitty version probe:** if `Detect` returned `ProtoKitty`, send a small probe at startup (transmit a 1×1 image with `q=2` and request a status reply, with a 200 ms timeout). On timeout or error, downgrade to `ProtoHalfBlock` and log it. iTerm2 fails this probe today and downgrades.

Prior art for env-var-only detection: `internal/emoji/terminal.go:30`.

## Cell Metrics

`cellmetrics.Estimate() (pxW, pxH int)`:

- Honor explicit `$COLORTERM_CELL_WIDTH` / `$COLORTERM_CELL_HEIGHT` if set.
- Try `unix.IoctlGetWinsize` — modern terminals return pixel dimensions in `ws_xpixel`/`ws_ypixel`. Divide by `ws_col`/`ws_row` to get per-cell pixels.
- Fall back to `(8, 16)`.

Used by:
- `Fetcher.PickThumb` to choose the smallest Slack thumb ≥ the target render box.
- `Renderer.Render` to set kitty/sixel pixel scaling.

## Image Cache

`internal/image/cache.go`:

```go
type Cache struct {
    dir    string         // ~/.cache/slk/images/
    capMB  int64          // default 200
    mu     sync.Mutex
    index  map[string]*entry
    lru    *list.List
}

func (c *Cache) Get(key string) (path string, ok bool)
func (c *Cache) Put(key string, ext string, data []byte) (path string, err error)
func (c *Cache) Open(key string) (io.ReadCloser, bool, error)
```

**Key format:**
- Slack file thumb: `<file_id>-<thumb_size>` (e.g. `F0123ABCD-720`).
- Avatar: `avatar-<userID>`.

**Eviction:** on `Put`, if `total + new > capMB`, delete oldest-mtime entries until under the cap. Single entries larger than the cap are written + served for the session, then evicted on the next sweep with a logged warning. `Get` updates atime + LRU position.

**Index load:** at startup, `os.ReadDir(dir)` + stat each entry. Keeps RAM proportional to file count, not bytes.

**Cap config:** `[cache] max_image_cache_mb = 200`.

**Migration:** on first run after this feature ships, `~/.cache/slk/avatars/<userID>.img` files are renamed to `~/.cache/slk/images/avatar-<userID>.png`. The migration is idempotent (skip if target already exists) and runs once at startup before the rest of the cache index loads. The empty `~/.cache/slk/avatars/` directory is left in place.

## Fetcher

`internal/image/fetcher.go`:

```go
type FetchRequest struct {
    Key    string
    URL    string
    Target image.Point  // pixels
}

type FetchResult struct {
    Img    image.Image
    Source string
    Mime   string
}

func (f *Fetcher) Fetch(ctx context.Context, req FetchRequest) (FetchResult, error)
```

- Cache hit → decode from disk; cache miss → HTTP GET → `Cache.Put` → decode.
- Decoders registered: `image/png`, `image/jpeg`, `image/gif` (frame 0 via `gif.DecodeAll`).
- Always downscale to fit `Target`; never upscale (terminal would just blur).
- Downscaling: `golang.org/x/image/draw.BiLinear` (already a dep).
- `singleflight.Group` deduplicates concurrent fetches by `Key` — important for avatars (same user across many adjacent messages).
- HTTP: 10-second per-request timeout, no auth headers (Slack thumbs are unauthenticated). Same User-Agent slk uses elsewhere.
- Errors return; the renderer falls back to the OSC 8 text line for that attachment.

### PickThumb

```go
// PickThumb selects the smallest Slack Thumb* whose pixel dimensions are
// >= target on both axes. Falls back to the largest available if none satisfy.
func PickThumb(file slack.File, target image.Point) (url string, suffix string)
```

Source data: `slack.File.Thumb360`/`Thumb720`/`Thumb1024` plus `*W`/`*H` companions. Inputs come from `slack-go` (`go/pkg/mod/github.com/slack-go/slack@v0.23.0/files.go:26-101`).

Inline rendering uses cell-metrics-derived pixel target. The full-screen preview always asks for `Thumb1024`.

The `url_private` (original) is **not** used — it requires Slack auth headers and is out of scope for v1.

## Renderer

### HalfBlock (`halfblock.go`)

Generalized from `internal/avatar/avatar.go:155-185`:

- Resize to `(target.X, target.Y * 2)` pixels.
- For each cell: `▀` with `fg = top sub-pixel`, `bg = bottom sub-pixel` as 24-bit ANSI (`\x1b[38;2;…m\x1b[48;2;…m▀`).
- Each row appended to `Lines`. `Fallback = Lines`. `OnFlush = nil`. `ID = 0`.
- Output post-processed by the existing `ReapplyBgAfterResets` (`render.go:234`) so the surrounding theme bg/fg do not leak.

### Kitty (`kitty.go`)

Uses **unicode-placeholder** placement mode. This is the only kitty mode that interacts cleanly with the custom scroller and partial-visibility scrolling.

**Image upload (in `OnFlush`, fired once per session per `(file_id, target_cells)`):**

```
\x1b_Ga=t,f=100,t=d,i=<ID>,U=1,q=2;<base64-PNG-bytes>\x1b\\
```

- `a=t` transmit, `f=100` PNG, `t=d` direct (no temp file), `i=<ID>` minted by an `imageRegistry` singleton, `U=1` enables unicode-placeholder mode, `q=2` quiet.
- An `imageRegistry` keyed on `(file_id, target_cells)` ensures each image is uploaded at most once per session. Subsequent renders only emit placeholder rows.

**Per-row content (in `Lines`):**

Each line is `target.X` cells of `U+10EEEE` with diacritic combining marks encoding `(image_row, image_col)`, all wrapped in a 24-bit-color SGR encoding the upper byte of the image ID. Per kitty's spec.

When the terminal sees these placeholder cells in the framebuffer, it overlays the corresponding image-pixel region. **Scrolling, partial visibility, clipping, and z-order all work** because the cells are normal text from the scroller's POV.

**Limitations:**
- Requires kitty 0.20+ (Aug 2021). Older versions fail the startup probe and fall back to half-block.
- iTerm2 ≥ 3.5 implements kitty graphics but **does not** support unicode placeholders. iTerm2 falls back to half-block. Document this in the spec and revisit if iTerm2 adds support.

### Sixel (`sixel.go`)

Sixel has no image IDs and no unicode-placeholder equivalent.

**Approach:** generate sixel bytes per `(file_id, target_cells)`, store once. Per-frame, lines containing the image emit the sixel byte stream **only when the image fits entirely in the visible viewport**. If any row is clipped at top or bottom, that frame uses the pre-computed half-block `Fallback` rows for the same image.

**Lines content:**

`Lines[0]` is prefixed with a zero-width sentinel marker (a private-use codepoint `U+E0001` reserved for slk) followed by spaces. `Lines[1..N-1]` are pure spaces of width `target.X`. The messages-pane line writer in `Model.View()` recognizes the sentinel and emits the sixel byte stream + advances the cursor `target.Y` rows; for partial-visibility frames, it emits the corresponding `Fallback` rows instead.

**Encoding:** `github.com/mattn/go-sixel` — pure-Go, MIT-licensed, octree color quantization built in. Adds one dependency.

**Acknowledged tradeoff:** images flip between sixel and half-block as they scroll past the viewport edge. Predictable but visible. The alternative (re-encode cropped sixel per scroll position) would regress scroll perf significantly.

### Avatar Rendering

Avatars are images with `target = (4, 2)` cells. They use the same `Renderer` interface, but **always go through the half-block renderer** regardless of detected protocol:

- For kitty: avatars *could* use unicode placeholders, but the visual upgrade at 4×2 cells is small and the placement coordination cost is non-trivial.
- For sixel: per-frame re-emission of sixel bytes for every visible avatar would dominate the redraw budget.
- Half-block at 24-bit color is already the existing avatar look, and it composes with text trivially.

This means `internal/avatar` only needs the half-block path. Pixel output is identical to today's avatars. The user-visible difference is purely the cache location (avatars now live in the unified `~/.cache/slk/images/`).

## Integration: Messages Pane

### Attachment struct (`internal/ui/messages/model.go:36`)

```go
type Attachment struct {
    Kind   string  // "image" | "file"
    Name   string
    URL    string  // permalink, used for OSC 8 hyperlink and ProtoOff fallback

    // Populated only for Kind == "image":
    FileID string
    Mime   string
    Thumbs []ThumbSpec  // sorted ascending by max(W, H)
}

type ThumbSpec struct {
    URL  string
    W, H int
}
```

`extractAttachments` (`cmd/slk/main.go:958`) is extended to populate `FileID`, `Mime`, and `Thumbs` from `slack.File`'s thumb fields. The existing `pickAttachmentURL` is preserved for the OSC 8 hyperlink and the `ProtoOff` text fallback.

### renderMessagePlain integration

The current call to `RenderAttachments` (`internal/ui/messages/model.go:795-798`) is replaced with `renderAttachmentBlock(att, ctx)`. It returns one of:

- The current `[Image] <url>` OSC 8 line — when `Protocol == ProtoOff`, when `Kind == "file"`, or when the message-pane width is too narrow for any image rendering.
- A reserved-height placeholder block — when `Kind == "image"` and the image is not yet cached or decoded. The block is `target.Y` rows tall (same height as the eventual image), filled with theme `surface_dark` background, with a centered `⏳ Loading <filename>...` indicator on the middle row.
- The renderer's `Render.Lines` baked in directly — when the image is cached and decoded.

**Stable height invariant:** the block reserves the final image's height *from the moment the message is first rendered*, so the user never sees a scroll jump when the image arrives. Image dimensions are known up-front from `Thumbs[i].W/H` even before download.

### viewEntry cache (`model.go:67-84`)

```go
type viewEntry struct {
    linesNormal   []string
    linesSelected []string
    linesPlain    []string
    height        int
    msgIdx        int

    // New: per-frame side effects (kitty image uploads).
    flushes []func(io.Writer) error
    // New: per-row sixel sentinels — map of row index -> sixel bytes + fallback rows.
    sixelRows map[int]sixelEntry
}

type sixelEntry struct {
    bytes    []byte
    fallback []string
}
```

`buildCache` (`model.go:603`) collects `OnFlush` callbacks from each rendered image into `flushes`, and indexes any sentinel-prefixed rows into `sixelRows`.

### Model.View() (`model.go:1175`)

- Walks the visible window of entries as today.
- Per visible entry, runs all `flushes` once (deduplicated against an `emittedThisFrame` set keyed by image ID), before emitting the line bytes.
- For lines that appear in `sixelRows`, the writer checks whether *all* rows of that image's footprint are within the visible window. If yes, emits the sixel byte stream and advances the cursor by `target.Y` rows, skipping ahead. If no, emits the corresponding `fallback` rows in normal text mode.

### linesPlain (selection layer)

Image rows in `linesPlain` are pure spaces of width `target.X`. Drag-to-copy ignores them — image regions are intentionally unselectable.

## Integration: Avatars

`internal/avatar/avatar.go` keeps its public API (`Cache`, `NewCache`, `Preload`, `PreloadSync`, `Get`) but its internals delegate:

- `loadAndRender` calls `image.Fetcher.Fetch` with `target = (4, 2)` cells (in pixels via `cellmetrics`).
- `renderHalfBlock` is **deleted** from this package and moved into `internal/image/halfblock.go`. The avatar package calls `image.HalfBlock.Render(img, image.Pt(4, 2))`.
- The `Cache` struct's per-userID `renders map[string]string` continues to memo-ize the produced ANSI string in RAM, keyed on `(userID, theme)` — same as today.

Pixel output is identical to today, verified by golden tests.

## Lazy-Load Coordination

A `prefetcher` goroutine watches the messages-pane viewport state.

- The messages model exposes `yOffset`, `entryOffsets`, and `version`. The prefetcher subscribes to a channel that fires (with 50 ms debounce) on any change.
- For each `MessageItem` in the visible range ± 2 screens of overscroll, if its image attachments are not yet cached + decoded, kick off `Fetcher.Fetch` calls.
- On completion, send a `tea.Msg(ImageReadyMsg{Channel, TS, AttachmentIdx})` into the bubbletea program.
- The messages model handles `ImageReadyMsg` by calling `m.invalidateMessage(channel, ts)` (a new method that clears that single message's `viewEntry` from the cache and bumps `version`). The next frame re-renders that one message with the decoded image baked in. Reserved-height placeholder ensures no scroll jump.

## Full-Screen Preview

`internal/image/preview.go`:

```go
type Preview struct {
    fileID    string
    name      string
    sourceURL string  // e.g. Thumb1024
    img       image.Image
    rendered  Render
    err       error
    loading   bool
}

func (p *Preview) Init() tea.Cmd
func (p *Preview) Update(msg tea.Msg) (Preview, tea.Cmd)
func (p *Preview) View(width, height int) string
```

**App wiring:** `internal/ui/app.go` gains a `previewOverlay *image.Preview` field. When non-nil, `app.View()` composes its `View()` over the messages + thread region; the sidebar and status bar remain visible. Z-order: sidebar | preview-overlay | status bar.

**Open paths:**
1. **Mouse click** on a cell within an inline image's footprint → `OpenImagePreviewMsg`.
2. **`O` keybind** on the selected message → `OpenImagePreviewMsg` for the first image attachment, or cycles if multiple.

**Mouse hit-testing:** the messages model collects a `[]hitRect` during `View()` — each entry records `(rowStart, rowEnd, colStart, colEnd, fileID, attachmentIdx)`. On `tea.MouseMsg{Action: Press, Button: Left}`, the model checks the hit map first; if no image hit, falls through to the existing drag-to-copy handler.

**Close paths:**
- `Esc` or `q` → close.
- `Enter` → close + `xdg-open`/`open`/`start` the cached image file.

**Sizing:** preview fills its overlay region minus 1 row top + 1 row bottom for a thin caption (`<filename>  •  <W>x<H>  •  <size>`). Image scaled to fit, aspect preserved, centered.

**Rendering:** uses the active protocol. Kitty uses unicode placeholders within the overlay region. Sixel emits bytes once on open and re-emits on each redraw (overlay doesn't scroll, so cost is bounded). Half-block paints into the overlay region.

**Decoded-image lifecycle:** preview keeps its decoded `image.Image` for the lifetime of the overlay; closing drops it. The disk-cached file remains in `image.Cache` so reopening is instant.

## Config

```toml
[appearance]
image_protocol = "auto"  # "auto" | "kitty" | "sixel" | "halfblock" | "off"
max_image_rows = 20      # cap inline image height in terminal rows

[cache]
max_image_cache_mb = 200
```

All four keys are optional with the documented defaults. Existing configs load unchanged.

## Keybindings (additions)

| Key | Mode | Action |
|---|---|---|
| `O` | Normal (message) | Open full-screen image preview for selected message |
| `Esc` / `q` | Preview overlay | Close overlay |
| `Enter` | Preview overlay | Open in system image viewer |
| Left-click | Any | On a rendered inline image, opens full-screen preview |

Added to the README keybindings table when this ships.

## Testing Strategy

**Package-local tests in `internal/image/`:**

- `cache_test.go` — LRU eviction by mtime, atime updates on `Get`, single-flight dedup of concurrent fetches, oversize entry handling, migration idempotence.
- `cellmetrics_test.go` — env var precedence, ioctl path mocked, `(8,16)` fallback.
- `capability_test.go` — env-var matrix → expected protocol; tmux override; config override.
- `halfblock_test.go` — golden ANSI output for a 4×4 known PNG (avatar pixel parity check).
- `kitty_test.go` — encode a known image, verify upload-escape format and placeholder-row diacritic encoding match kitty's spec.
- `sixel_test.go` — wraps `mattn/go-sixel`; round-trip a generated PNG and verify byte structure.
- `fetcher_test.go` — `httptest.Server` for HTTP path; thumb-picker selection logic.
- `preview_test.go` — open/close/resize state transitions; sizing math.

**UI integration tests (`internal/ui/messages/`):**

- Stable-height test: insert a message with a not-yet-loaded image, assert `viewEntry.height`. Send `ImageReadyMsg`, assert height unchanged.
- Hit-map test: build a known cache, simulate a `tea.MouseMsg` at a cell inside an image, assert `OpenImagePreviewMsg` is emitted.
- Selection test: simulate drag-to-copy across an image region, assert `linesPlain` for that region is empty/spaces.

**Avatar parity test:** golden ANSI output before/after the avatar refactor must be byte-identical for a known input PNG.

**Test images:** golden tests generate their own tiny PNGs in-test rather than committing binary fixtures. Keeps the repo lean.

## Risks

- **iTerm2 limitation.** Kitty graphics in iTerm2 ≥ 3.5 do not support unicode placeholders. iTerm2 falls back to half-block via the startup probe. Document and revisit if iTerm2 adds upstream support.
- **Sixel partial-visibility flicker.** Images flip between sixel and half-block as they scroll past the viewport edge. Acknowledged tradeoff; accepted to keep scroll perf intact.
- **Kitty version probe** can hang on terminals that claim kitty but ignore queries. 200 ms timeout mitigates; on timeout, downgrade to half-block.
- **Cell-metrics fallback `(8, 16)`** may produce slightly off-target downscales on hi-dpi terminals. Mitigated because we always pick a Slack thumb ≥ target — worst case is some over-resolution that the renderer downscales correctly.
- **Lazy-load races.** A user scrolling rapidly could fire many `Fetcher.Fetch` calls. `singleflight` dedupes per key; bounded-overscroll (± 2 screens) bounds the working set. Cancellation: requests use a context tied to channel-switch / quit, not to viewport position — once kicked off, a request runs to completion or cache.
- **Migration failure.** If avatar-cache migration fails partway, both old and new files may exist. The migration is idempotent (skip if target exists) and a partial run is recoverable on next startup.
- **`mattn/go-sixel` dependency.** Adds one third-party dep. Pure Go, MIT, ~600 LOC, well-maintained. Vendoring is an option if maintenance becomes a concern.

## Implementation Sequence

The work lands in eight independently-mergeable PRs, each gated by tests:

1. **`internal/image` skeleton** — capability detection, cell-metrics, half-block renderer, cache, fetcher, thumb-picker. Pure-package tests only; no UI integration.
2. **Avatar package refactor** — delegate storage to `image.Cache`, rendering to `image.HalfBlock`. One-time migration. Pixel-identical output verified by golden tests.
3. **Kitty renderer** — unicode-placeholder mode, version probe, image registry.
4. **Sixel renderer** — wrap `mattn/go-sixel`, fits-fully-on-screen gating, half-block fallback path.
5. **Inline image rendering in messages pane** — `Attachment` extension, `extractAttachments` updates, `renderMessagePlain` integration, reserved-height placeholder, lazy-load coordinator + `ImageReadyMsg`. Half-block-only for first cut.
6. **Wire kitty + sixel through messages pane** — per-frame flush invocation, sentinel marker handling, partial-visibility fallback.
7. **Full-screen preview overlay** — `O` keybind, mouse click hit-testing, `Esc`/`Enter` handling, system-viewer launch.
8. **Documentation + STATUS.md update + README keybindings table.**

Each PR is shippable on its own. The feature is dark until step 5 and progressively richer through 6 and 7.

## References

- Original 3-tier plan: `docs/superpowers/specs/2026-04-23-slack-tui-design.md:401-432`
- Paste-image-upload (companion outbound spec): `docs/superpowers/specs/2026-04-29-paste-image-upload-design.md`
- Avatar half-block precedent: `internal/avatar/avatar.go:155-185`
- Custom scroller rationale: `internal/ui/messages/model.go:122-126`
- Env-var detection precedent: `internal/emoji/terminal.go:30`
- Slack `slack.File` thumb fields: `~/go/pkg/mod/github.com/slack-go/slack@v0.23.0/files.go:26-101`
- Kitty graphics protocol: https://sw.kovidgoyal.net/kitty/graphics-protocol/
- Kitty unicode placeholders: https://sw.kovidgoyal.net/kitty/graphics-protocol/#unicode-placeholders
- Sixel encoder: https://github.com/mattn/go-sixel
