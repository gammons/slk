# Stronger mode/selection visuals and inline images in threads

**Status:** design, awaiting implementation plan
**Date:** 2026-05-02

## Problem

Three rough edges in the slk UI:

1. **Insert-mode is easy to miss.** Today, the only visual cues that the user is composing a message are (a) a green " INSERT " pill in the bottom-left of the status bar and (b) the compose box's `▌` left border switching from gray to blue. Users routinely lose track of which mode they're in.
2. **The selected-message highlight is too subtle.** A single bright-green `▌` left border marks the selected row. It's easy to lose the cursor when scrolling, especially after focus changes or when the surrounding messages are visually busy.
3. **Threads don't render inline images.** The thread side panel calls `messages.RenderAttachments`, which emits a plain-text `[Image] <url>` line. The full inline-image pipeline (kitty / sixel / halfblock with async fetch, placeholders, and LRU cache) lives only in `messages/model.go` and is tied to `messages.Model` state.

## Goals

- Insert-mode and selected-message highlights are unmistakable on every built-in theme without adding new chrome rows or new keybindings.
- The visual language is **consistent**: tinted background = "you're acting on this", whether that's the row you're navigating or the box you're typing in.
- Thread replies render attached images inline using the same protocol-detection and caching pipeline as the main messages pane.
- No new theme files need touching to ship the visual changes; tints derive automatically from each theme's existing colors with an optional override hook for art-directed themes.

## Non-goals

- Click-to-preview from a thread image, or `O` / `v` opening the preview overlay from a selected thread reply. The preview overlay stays a messages-pane feature in v1.
- Block Kit / legacy attachment rendering in the thread panel. Threads keep current text-only behavior for non-image attachments.
- New keybindings or compose-flow changes (placeholder, focus rules, status-bar text).
- New config keys in v1. Optional theme-level overrides can land later without breaking anything.
- Changing the selected-message clipboard contract (`linesPlain` and `contentColOffset = 1` continue to mirror un-bordered content).

## Design

### 1. Compose-box insert-mode tint

When the compose box is focused (which is 1:1 with `ModeInsert`), it gains a tinted background fill in addition to its existing left border. The border itself shifts from `Primary` (blue) to `Accent` (bright green) so it shares the visual language of the selected message.

Changes:

- `internal/ui/styles/styles.go`:
  - Add a derived color, computed at theme-apply time:
    - `ComposeInsertBG = mix(Accent, Background, α=0.15)` where `mix` is a straight-line RGB interpolation.
  - `ComposeInsert` style:
    - `BorderForeground` switches from `Primary` to `Accent`.
    - Add `Background(ComposeInsertBG)`.
  - Optional override hook: if a theme TOML sets `colors.compose_insert_bg`, that color replaces the derived value.
- `internal/ui/compose/model.go:1049-1082`: extend the inner textarea style so the tinted background fills the whole compose body — including behind the cursor row — not just the lipgloss padding.

No changes to status bar, placeholder, focus plumbing, or keybindings.

### 2. Selected-message tinted row

The selected row keeps the bright-green `▌` left border and adds a tinted background fill spanning the full row width. When the panel is unfocused, the tint and border both dim to a `TextMuted`-based mix.

Changes:

- `internal/ui/styles/styles.go`:
  - New helper `SelectionTintColor(focused bool) color.Color`:
    - Focused: `mix(Accent, Background, α=0.15)` (same value as `ComposeInsertBG`).
    - Unfocused: `mix(TextMuted, Background, α=0.15)`.
  - The existing `SelectionBorderColor(focused)` is unchanged.
- `internal/ui/messages/model.go:1041-1126` (`buildCacheStyles` / `renderMessageEntry`):
  - The `borderSelect` style adds `.Background(SelectionTintColor(m.focused))`.
  - `renderMessageEntry` ensures every wrapped line of `linesSelected` is padded to the full content width before the border is applied, so the tint reaches the right edge cleanly. `linesNormal` is unchanged.
- `internal/ui/thread/model.go:980-1009`: identical change to its `borderSelect` style and per-line padding.
- `internal/ui/threadsview/model.go:50-72`: switch from hard-coded `styles.Accent` to `styles.SelectionBorderColor(focused)` for the border, and add `.Background(SelectionTintColor(focused))`. This also fixes the existing inconsistency where the threads list's selection didn't dim when the panel lost focus.

No changes to selection navigation, clipboard contract, or hit-testing.

### 3. Inline images in the thread side panel (render-only)

A new shared package owns the inline-image rendering pipeline. Both panels embed an instance.

#### New package: `internal/ui/imgrender`

Moves out of `messages/`:

- `renderAttachmentBlock` → `(*Renderer).RenderBlock(att Attachment, ts string, width, rowCursor, attIdx, contentColBase int) BlockResult`
- `computeImageTarget`
- `buildPlaceholder`
- Related private helpers (thumbnail URL picker, sixel sentinel construction, kitty key formatting, etc.)

`BlockResult` carries the rendered lines, optional hit-rect, and any per-frame flush callback the caller should accumulate.

`Renderer` struct:

```go
type Renderer struct {
    Ctx            ImageContext // moved verbatim from internal/ui/messages
    fetching       map[string]struct{}
    failed         map[string]struct{}
    flushes        []func(io.Writer) error // per-frame kitty escape callbacks
    sixelSentinels map[int]sixelEntry
}
```

Methods:

- `SetContext(ctx ImageContext)`
- `BeginFrame()` — clears per-frame flushes and sixel-sentinel rows.
- `EndFrame() (flushes []func(io.Writer) error, sentinels map[int]sixelEntry)` — returned to the caller's viewport View().
- `RenderBlock(...)` — the renamed `renderAttachmentBlock`.
- `OnImageReady(msg ImageReadyMsg) (matched bool)` — clears the file from `fetching`, returns whether this renderer had a pending fetch for it.
- `OnImageFailed(msg ImageFailedMsg) (matched bool)` — same, populates `failed`.

The `ImageContext` type and the `ImageReadyMsg` / `ImageFailedMsg` Bubble Tea messages move from `internal/ui/messages` to `internal/ui/imgrender`. All callers in `internal/ui/messages`, `internal/ui/thread`, and `internal/ui/app` are updated to import from the new package; no compatibility re-exports are kept.

#### `messages.Model`

- Replaces its `imgCtx`, `fetchingImages`, `failedImages`, per-frame flush slice, and sixel-sentinel map with a single `*imgrender.Renderer` field.
- `renderMessagePlain` calls `m.imgRenderer.RenderBlock(...)`. The returned hit-rect feeds the existing per-entry `hits` slice unchanged — `App.HitTest` and click-to-preview behavior are byte-for-byte identical.
- `BeginFrame` / `EndFrame` are called around the cache rebuild path the same way the inline state was managed before.

#### `thread.Model`

- New field `imgRenderer *imgrender.Renderer` and matching `SetImageContext(ctx imgrender.ImageContext)` setter.
- `renderThreadMessage` (`thread/model.go:1203-1206`) replaces the `messages.RenderAttachments(msg.Attachments)` call with a loop:
  - For each `att` in `msg.Attachments`, call `m.imgRenderer.RenderBlock(...)` and append its lines to the message body.
  - **Discard** the returned hit-rect in v1 (render-only).
- `BeginFrame` / `EndFrame` wrap the thread cache rebuild so kitty escape callbacks and sixel sentinel rows emit through the thread viewport's `View()` output the same way they do for the messages pane.
- v1 cache invalidation strategy on `ImageReadyMsg` matching a thread reply: full `thread.InvalidateCache()`. Per-entry partial rebuild can come later as an optimization.

#### `App` wiring

- At startup and on workspace switch, `App` calls `a.thread.SetImageContext(...)` immediately after `a.messages.SetImageContext(...)` with the same `ImageContext`.
- The `ImageReadyMsg` / `ImageFailedMsg` handlers in `app.go` forward to **both** the messages model and the thread model. Each calls its renderer's `OnImageReady` / `OnImageFailed`; whichever returned `matched == true` triggers its own cache rebuild.
- No change to `App.HitTest` (still consults messages-pane only).
- No change to the `O` / `v` key handlers (still operate on the messages-pane selection).

#### README update

The "Threads side panel renders attachments as text (`[Image] <url>`); inline rendering there is on the roadmap" caveat is updated:

> Threads render images inline using the same pipeline as the main messages pane. Click-to-preview and `O` / `v` from a thread reply are still messages-pane only.

### 4. Tint derivation helper

A single private helper in `internal/ui/styles` does the work for both §1 and §2:

```go
// mixColors returns a straight-line RGB interpolation between fg and bg,
// where alpha is the share of fg (0.0 = bg, 1.0 = fg).
func mixColors(fg, bg color.Color, alpha float64) color.Color
```

Called once at theme-apply time to populate `ComposeInsertBG`, and called by `SelectionTintColor` (which can either cache its result or recompute — both are cheap).

If a theme TOML sets `colors.compose_insert_bg` or `colors.selection_bg`, the derived value is replaced by the explicit one. These keys are not documented in v1 (they are an internal escape hatch) but are reserved.

The chosen α = 0.15 is a starting point — verifying it works on every built-in theme is part of the implementation's snapshot tests (§5). It can be tuned per-direction if needed (e.g., a different α for the unfocused selection tint).

## Testing

- **Unit tests** live alongside the new `internal/ui/imgrender` package: placeholder rendering, target-cell computation, fetcher failure paths, kitty / sixel / halfblock branches, and `OnImageReady` / `OnImageFailed` matching. The existing image-rendering tests in `internal/ui/messages/*_test.go` move (not copy) into `internal/ui/imgrender` and are adapted to call the new package directly. Tests that exercise messages-pane behavior beyond image rendering (e.g. hit-rect plumbing, click-to-preview routing) stay where they are.
- **Tint helper test** confirms `mixColors(Accent, Background, 0.15)` produces a stable RGB across the 12 built-in themes (locks the default α). One test row per built-in theme, asserting the derived hex values.
- **Snapshot / golden tests** for the messages and thread renderers cover:
  - Tinted compose box with placeholder text (focused vs. unfocused).
  - Tinted selected message in the messages pane (focused vs. unfocused).
  - Tinted selected reply in the thread panel.
  - Selected card in the threads list (focused vs. unfocused — also locks in the new dim-on-blur behavior).
- **Thread image rendering tests** mirror the existing messages-pane image tests: placeholder during fetch, halfblock fallback, full cache hit. Click-to-preview tests are skipped (out of scope for v1).
- Existing clipboard / drag-to-copy tests for the messages pane (`internal/ui/messages/selection_test.go:166-169`, `internal/ui/thread/selection_test.go:52-53`) continue to pass unmodified — the tinted background does not change `linesPlain` or `contentColOffset`.

## Rollout

1. Land the styles helper and tint derivation, gated behind unit tests.
2. Land the compose-box and selected-message visual changes (§1, §2).
3. Land the `imgrender` package extraction with messages-pane behavior unchanged (`messages.Model` migrates internally; no observable diff except in tests).
4. Wire `imgrender.Renderer` into `thread.Model` and update `App` for `ImageReadyMsg` routing (§3).
5. Update README's image-rendering caveats.

Each step is independently mergeable.
