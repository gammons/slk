# Block Kit & Legacy Attachments Rendering — Implementation Plan (Overview)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render Slack `blocks` and legacy `attachments` so bot/app messages (GitHub, PagerDuty, CI, deploy bots) display with their structure, fields, color stripes, and visible-but-disabled controls instead of the current empty/one-line fallback.

**Architecture:** A new self-contained `internal/ui/messages/blockkit/` package owns parsing (slack-go → typed sealed-interface tree), per-block-type rendering, and theme-aware styling. The package returns the same `(lines, flushes, sixelRows, height, hits)` tuple shape as the existing `renderAttachmentBlock` so it plugs into `renderMessagePlain` as two new render passes (blocks first, legacy attachments second) between the body text and the file-attachment pass. Image rendering reuses the existing `ImageContext`/`Fetcher` pipeline with a URL-hash-derived cache key. Interactive controls render as muted, non-functional labels with a single `↗ open in Slack to interact` hint line appended once per message. `rich_text` blocks are NOT walked — the `Message.Text` fallback continues handling them as today.

**Tech Stack:** Go 1.22+, `github.com/slack-go/slack` v0.23.0 (already a dep), `charm.land/lipgloss/v2` (already a dep), `crypto/sha1` (stdlib) for URL→cache-key derivation. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-04-30-block-kit-rendering-design.md`

---

## Phase Files

| Phase | File | Summary |
|---|---|---|
| 1 | `01-data-model-and-parsing.md` | Scaffold `blockkit` package; parse slack-go `Blocks` and `Attachments` into typed structs; thread new fields through `MessageItem` and the four `cmd/slk/main.go` ingestion sites. Renderer is a no-op stub. |
| 2 | `02-simple-blocks.md` | Render `divider`, `header`, `unsupported`, then `section` text body + 2-column fields grid, then non-image `section` accessory (muted labels), then `context`. |
| 3 | `03-actions-and-images.md` | Render `actions` block as muted labels (sets `Interactive=true`); render `image` block via `ImageContext`; render `section` image accessory side-by-side with body, smaller cap. |
| 4 | `04-legacy-attachments.md` | Color-stripe + pretext + title + text + footer; then fields grid + `image_url` + `thumb_url`. Color resolution (`good`/`warning`/`danger`/hex). |
| 5 | `05-integration.md` | Wire `blockkit.Render` and `blockkit.RenderLegacy` into `renderMessagePlain`; append the `↗ open in Slack to interact` hint when `Interactive=true`; end-to-end tests against six real bot JSON fixtures; build + manual smoke. |

Each phase is independently mergeable. The feature is dark until Phase 5 wires it in. Phases 1–4 add code that nothing calls yet, fully unit-tested in isolation.

---

## File Structure (cumulative across all phases)

**New files:**
- `internal/ui/messages/blockkit/types.go` — `Block` sealed interface, concrete block structs, accessory/element types, `RenderResult`, `Context`
- `internal/ui/messages/blockkit/parse.go` — `Parse(slack.Blocks) []Block`, `ParseAttachments([]slack.Attachment) []LegacyAttachment`
- `internal/ui/messages/blockkit/parse_test.go`
- `internal/ui/messages/blockkit/render.go` — `Render(blocks []Block, ctx Context, width int) RenderResult` and per-block-type renderers
- `internal/ui/messages/blockkit/render_test.go`
- `internal/ui/messages/blockkit/attachments.go` — `RenderLegacy([]LegacyAttachment, Context, width) RenderResult` and helpers
- `internal/ui/messages/blockkit/attachments_test.go`
- `internal/ui/messages/blockkit/styles.go` — lipgloss styles consuming the active theme
- `internal/ui/messages/blockkit/color.go` — `good`/`warning`/`danger`/hex color resolution
- `internal/ui/messages/blockkit/color_test.go`
- `internal/ui/messages/blockkit/image.go` — block-image fetch/render helper (URL-hash cache key, single-image-no-thumbs flow)
- `internal/ui/messages/blockkit/integration_test.go` — six fixture-based end-to-end tests
- `internal/ui/messages/blockkit/testdata/github_pr.json`
- `internal/ui/messages/blockkit/testdata/pagerduty_alert.json`
- `internal/ui/messages/blockkit/testdata/deploy_approval.json`
- `internal/ui/messages/blockkit/testdata/oncall_handoff.json`
- `internal/ui/messages/blockkit/testdata/section_with_fields.json`
- `internal/ui/messages/blockkit/testdata/header_divider_section.json`

**Modified files:**
- `internal/ui/messages/model.go` — add `Blocks []blockkit.Block` and `LegacyAttachments []blockkit.LegacyAttachment` to `MessageItem`; call `blockkit.Render` and `blockkit.RenderLegacy` from `renderMessagePlain` between the body text and the file-attachment passes; aggregate flushes/sixelRows/hits from the new render passes; append the hint line when any pass set `Interactive=true`.
- `cmd/slk/main.go` — add `extractBlocks(slack.Blocks) []blockkit.Block` and `extractLegacyAttachments([]slack.Attachment) []blockkit.LegacyAttachment`; populate the two new `MessageItem` fields at all four ingestion sites (`fetchOlderMessages`, `fetchChannelMessages`, `fetchThreadReplies`, and the upload-completion path around line 1587).
- `internal/cache/messages.go` — no schema change. The existing `RawJSON` column already persists the source of truth.

**No changes:** `internal/cache/db.go` (schema), `internal/slack/events.go` (raw event handling — the typed `slack.Message` already exposes `Blocks` and `Attachments`), `internal/image/*` (the existing pipeline is reused via `ImageContext`).

---

## Conventions used throughout the plan

- **Tests follow the existing project style.** No golden-file snapshots. Use `ansi.Strip(...)` from `github.com/charmbracelet/x/ansi` and assert specific substrings — same pattern as `internal/ui/messages/render_test.go`. JSON fixtures in `testdata/` for the integration tests only (parsing block payloads by hand in Go literals would bloat the test code).
- **TDD discipline:** every code-bearing task starts with a failing test (`go test ./internal/ui/messages/blockkit/... -run TestName -v` showing FAIL), then minimal implementation, then re-run showing PASS, then commit.
- **Commits are scoped per-task.** Commit message format: `feat(blockkit): <what>` for additive work; `feat(messages): wire blockkit into renderMessagePlain` for the integration commit.
- **Run `make build` after every implementation task** before committing — the codebase compiles fast and a broken commit is easy to make when threading types through four call sites.
- **Width handling:** all renderers accept `width int` (the available content width — already accounts for avatar gutter and border) and produce lines that consume EXACTLY that many display columns or fewer. The 60-col breakpoint for collapse-to-stacked layouts is a constant `narrowBreakpoint = 60` defined in `render.go`.
- **Color in tests:** assert visible *characters* (`█`, `[`, `▾`, etc.) and substring text, not ANSI codes. Lipgloss style output is unstable across versions; the plain-text shape is stable.
