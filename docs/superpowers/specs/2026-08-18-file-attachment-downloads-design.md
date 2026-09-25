# File Attachment Names & Downloads — Design

Date: 2026-08-18
Status: Approved

## Problem

Non-image file attachments (CSV, PDF, etc.) render as `[File] <url>` with no
filename, and there is no way to download a file for local viewing from within
slk. The raw URL is noisy and the permalink requires switching to a browser.

## Goals

1. Render file attachments with their filename: `[File] report.csv`, with the
   filename as an OSC-8 hyperlink to the Slack permalink (no raw URL).
2. A `d` keybinding downloads the selected message's file attachment(s) via
   authenticated request and opens them in the OS default app.
3. Multiple attachments on one message open a picker modal, mirroring the `o`
   key / linkpicker UX.

## Non-goals

- Viewing file contents inside slk (CSV tables, PDF rendering).
- Configurable download directory; files go to a temp/cache dir.
- Changing image attachment behavior (images already have preview + `O`).
- Browser-based flow via permalink remains available by clicking the OSC-8
  link in the terminal.

## Design

### 1. Generalized picker (internal/ui/linkpicker)

The existing linkpicker (model.go + view.go, ~175 lines) is generalized into a
reusable chooser rather than duplicating it as a separate filepicker package.

- `Model.Open` gains a title parameter (or the model stores a title set at
  open time): "Open link" for links, "Download file" for files.
- File rows render as filename (+ human-readable size when available); link
  rows render as today (label + URL, `[slk]` badge for in-app permalinks).
- The App records the picker session kind (`links` or `files`) when opening.
  The mode handler (`handleLinkPickerMode`) dispatches on Enter (the mode
  keeps its existing `ModeLinkPicker` name to minimize churn):
  - links → `OpenLinkMsg` (existing routing in reducer_links.go)
  - files → new `DownloadFileMsg`
- File picker items carry the chosen attachment (or its index into the
  selected message's attachments) so Enter can hand it to the downloader.
- Esc/q close without choosing, as today.

### 2. Download pipeline (internal/filedl)

New package with one exported type:

```go
type Downloader struct { /* http client, auth resolver, dest dir */ }
func (d *Downloader) Download(att messages.Attachment) (path string, err error)
```

- **Auth**: reuses the `TeamAuth` (xoxc Bearer + `d` cookie) mechanism used by
  `internal/image/fetcher.go` for files.slack.com. The auth-resolution logic
  (per-team map + foreign-team fallback retry) is currently private to the
  image fetcher; extract it minimally so both image and file downloads share
  the same code path (e.g. an auth-resolver interface the fetcher satisfies,
  or a small shared helper in internal/slackhttp). Keep the extraction
  minimal — no fetcher refactor beyond exposing auth resolution.
- **Destination**: `filepath.Join(os.TempDir(), "slk-files")`, created on
  first use. Filename = Slack title/name (sanitized for path separators and
  control characters); on collision append `-2`, `-3`, etc. Never overwrite.
- **Failure modes**: HTTP error, 403/auth failure, disk error → `ToastMsg`
  with a short message. Success → `launchOS(path)` (existing
  xdg-open/open/rundll32 helper in internal/ui/app.go) + toast
  "Downloaded <name>".
- Download runs as a `tea.Cmd` (async, off the UI thread), mirroring
  `openURLCmd`.

### 3. Rendering + data model

- `messages.Attachment` gains:
  - `DownloadURL string` — the auth-gated `URLPrivate`, populated for all
    files (images too). `URL` stays the user-facing permalink for
    clickability.
  - `Size int64` — Slack's `File.Size`, for the picker row.
- `renderSingleAttachment` (internal/ui/messages/render.go) becomes
  `[File] report.csv` / `[Image] photo.png`: marker in muted bold, filename
  styled as the OSC-8 hyperlink to the permalink, no raw URL. Falls back to
  the URL when `Name` is empty.
- `extractAttachments` (cmd/slk/main.go) populates the new fields.
- The existing comment about omitting filenames (render.go) is removed/updated
  — that rationale predates the name-as-link format.

### 4. Keybinding + flow

- New normal-mode binding: `d` — "download file" (`d` is free; `D` = delete,
  `ctrl+d` = half-page down).
- `d` on the selected message (messages pane or thread panel, mirroring
  `openLinksOfSelected`): collect non-image file attachments:
  - 0 → toast "No files in message"
  - 1 → dispatch `DownloadFileMsg` directly
  - 2+ → open the generalized picker with file rows; Enter dispatches
    `DownloadFileMsg`
- Help overlay gains the `d` entry.

### 5. Testing

- `internal/filedl`: `httptest.Server`-based tests — Authorization/cookie
  headers asserted, filename sanitization, collision suffixing, HTTP error
  propagation.
- Generalized picker: extend linkpicker model tests for title/file rows;
  dispatch behavior per session kind.
- `renderSingleAttachment`: new output format, empty-name fallback.
- `extractAttachments`: new fields populated (extend
  cmd/slk/attachments_test.go).
- App-level `d` key tests mirroring internal/ui/open_links_test.go: 0/1/2+
  files, Enter dispatch, Esc close.
- Existing tests asserting the old `[File] <url>` format are updated.

## Error handling summary

| Failure | Behavior |
|---|---|
| No files in message | Toast "No files in message" |
| HTTP/auth/disk error on download | Toast with short error |
| OS open fails | Logged; toast "Failed to open file" |
| Empty attachment name | Render falls back to URL |
