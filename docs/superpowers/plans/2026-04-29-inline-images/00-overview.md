# Inline Image Rendering — Implementation Plan (Overview)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render Slack image-file attachments inline in the messages pane with click-to-fullscreen preview, using kitty graphics > sixel > half-block protocol fallback. Reuses the same renderer for avatars (always half-block).

**Architecture:** A new `internal/image` package owns capability detection, an LRU disk cache (`~/.cache/slk/images/`), an HTTP fetcher with single-flight dedup, and three protocol-specific renderers behind a unified `Render` struct. The messages-pane render cache absorbs image rows as opaque text. Lazy-loading prefetches images as they enter the viewport; reserved-height placeholders prevent scroll jumps. A new full-screen preview overlay opens on click or `O` keybind.

**Tech Stack:** Go 1.22+, `golang.org/x/image/draw` (already a dep) for downscaling, `github.com/mattn/go-sixel` (new dep) for sixel encoding, `golang.org/x/sync/singleflight` for fetch dedup, `golang.org/x/sys/unix` for ioctl-based pixel-size queries, charm.land/bubbletea/v2, `image/png`/`image/jpeg`/`image/gif` (stdlib).

**Spec:** `docs/superpowers/specs/2026-04-29-inline-image-rendering-design.md`

---

## Phase Files

| Phase | File | Summary |
|---|---|---|
| 1 | `01-image-package-foundation.md` | New `internal/image` package: capability, cellmetrics, cache, fetcher, half-block renderer, thumb-picker. Pure-package, no UI. |
| 2 | `02-avatar-refactor.md` | Refactor `internal/avatar` to delegate to `internal/image`. One-time disk migration. Pixel-identical golden test. |
| 3 | `03-kitty-renderer.md` | Kitty graphics with unicode-placeholder placement, image registry, version probe. |
| 4 | `04-sixel-renderer.md` | Sixel encoder via `mattn/go-sixel` with half-block fallback for partial visibility. |
| 5 | `05-inline-images-messages-pane.md` | `Attachment` extension, `extractAttachments` updates, render integration, reserved-height placeholder, lazy-load prefetcher, `ImageReadyMsg`. Half-block-only at this step. |
| 6 | `06-wire-kitty-sixel.md` | Per-frame flush invocation, sentinel marker handling, sixel partial-visibility fallback wired through `Model.View()`. |
| 7 | `07-fullscreen-preview.md` | `Preview` overlay sub-component, `O` keybind, mouse hit-testing, `Esc`/`Enter`, `xdg-open` integration, app-level overlay composition. |
| 8 | `08-docs.md` | README keybindings, README features, STATUS.md update, config example. |

Each phase is **independently mergeable**. The feature is dark until Phase 5; richer through 6 and 7.

---

## File Structure (cumulative across all phases)

**New files:**
- `internal/image/capability.go` — protocol detection
- `internal/image/capability_test.go`
- `internal/image/cellmetrics.go` — px/cell estimation
- `internal/image/cellmetrics_test.go`
- `internal/image/cache.go` — LRU disk cache
- `internal/image/cache_test.go`
- `internal/image/fetcher.go` — HTTP + decode + downscale + singleflight + thumb-picker
- `internal/image/fetcher_test.go`
- `internal/image/renderer.go` — `Render`, `Renderer`, top-level `Render(...)` dispatcher
- `internal/image/halfblock.go` — half-block renderer
- `internal/image/halfblock_test.go`
- `internal/image/kitty.go` — kitty graphics renderer + image registry
- `internal/image/kitty_test.go`
- `internal/image/sixel.go` — sixel renderer (wraps `mattn/go-sixel`)
- `internal/image/sixel_test.go`
- `internal/image/preview.go` — full-screen preview component
- `internal/image/preview_test.go`
- `internal/image/migrate.go` — one-time avatar disk migration
- `internal/image/migrate_test.go`
- `internal/image/testdata/` — small generated PNGs for golden tests (created at test time, not committed)

**Modified files:**
- `go.mod`, `go.sum` — add `github.com/mattn/go-sixel`, `golang.org/x/sync`, `golang.org/x/sys`.
- `internal/avatar/avatar.go` — delegate storage + rendering to `internal/image`.
- `internal/avatar/avatar_test.go` — golden parity test (new) + adapt existing.
- `internal/ui/messages/model.go` — extend `Attachment`, `viewEntry`, `renderAttachmentBlock`, `invalidateMessage`, mouse hit-map, `ImageReadyMsg` arm.
- `internal/ui/messages/render.go` — keep `RenderAttachments` for `ProtoOff` fallback path.
- `internal/ui/messages/model_test.go` — height-stability test, hit-map test, selection test for image rows.
- `internal/ui/thread/model.go` — leave attachment rendering as text (out of scope for v1).
- `internal/ui/app.go` — `previewOverlay` field, overlay composition in `View()`, `Ctrl+W`/`O`/`Esc` routing while overlay open.
- `internal/ui/app_test.go` — preview overlay open/close test.
- `cmd/slk/main.go` — construct `image.Cache`/`Fetcher`/`Renderer`, run avatar migration, populate `Attachment.FileID`/`Mime`/`Thumbs` in `extractAttachments`, wire prefetcher.
- `internal/config/config.go` — add `Appearance.ImageProtocol`, `Appearance.MaxImageRows`, `Cache.MaxImageCacheMB`.
- `internal/config/config_test.go` — defaults + override tests.
- `README.md` — keybindings table, features section, config example.
- `docs/STATUS.md` — mark inline image rendering shipped.

---

## Sequencing Rationale

Bottom-up. Phase 1 builds the substrate (`internal/image`) with no UI integration. Phase 2 proves the package via the avatar refactor (lowest-risk consumer, golden-test verifies pixel parity). Phase 3 and Phase 4 add the two pixel protocols **in parallel** — neither depends on the other; either can land first. Phase 5 wires inline images into the messages pane using only the half-block path so the entire integration shape lands without depending on Phase 3/4. Phase 6 wires kitty + sixel through the messages pane (depends on 3, 4, 5). Phase 7 adds the preview overlay (depends on 5). Phase 8 is docs (depends on all).

If only Phase 5 ships, users get half-block inline images in every terminal. If Phase 6 ships, kitty/sixel users get pixels. If Phase 7 ships, click-to-preview works. Each is a coherent shippable point.

---

## Test Conventions

- Go test files live next to the code under test.
- Golden tests generate their own tiny PNGs in-test; no binary fixtures committed.
- HTTP tests use `httptest.NewServer`.
- Tests use the standard library `testing` only — no testify, matching codebase convention.
- Each phase's tasks finish with `make test` passing (or the equivalent `go test ./...`).

---

## Commit Convention

Each task ends with a commit. Commits use Conventional Commits prefixes already in this repo's history: `feat:`, `refactor:`, `test:`, `docs:`, `chore:`.

---

Continue to `01-image-package-foundation.md`.
