# Paste-to-Upload (Smart `Ctrl+V`)

**Date:** 2026-04-29
**Status:** Design

## Overview

In insert mode, `Ctrl+V` becomes a smart-paste action that inspects the
OS clipboard and dispatches based on contents:

1. **Image bytes** (PNG via `clipboard.FmtImage`) → attach as a pending
   image with auto-generated filename `slk-paste-<timestamp>.png`.
2. **Single-line file path** (absolute, `~/`-relative, or `./`-relative,
   that resolves to an existing regular file) → attach the file by path
   with its original basename.
3. **Anything else** → fall through to ordinary text paste into the
   textarea (the existing bracketed-paste-equivalent path).

Pending attachments render as chips above the textarea. Multiple
attachments + an optional caption are sent together via Slack's V2
file-upload API (`UploadFile` in slack-go v0.23.0, which transparently
handles `getUploadURLExternal` → PUT → `completeUploadExternal`). The
status bar shows `Uploading N/M…` during upload and `Sent` or
`Upload failed: <reason>` on completion. Files appear in the channel
via the existing WebSocket echo path; no optimistic local rendering.

Linux is a hard dependency: builds need `libx11-dev` for cgo and the
runtime needs X11 or Wayland with `wl-clipboard`. macOS and Windows
work natively. Headless or otherwise unsupported environments are
detected at startup via `clipboard.Init()` failure, after which
`Ctrl+V` silently falls through to text paste.

Individual files are capped at 10 MB.

## Goals

- Paste a screenshot or copied image into a channel or thread with one
  keystroke.
- Paste a copied file path with the same keystroke (option-3 fallback
  baked into option-1's flow).
- Multiple attachments + caption in a single send.
- Errors and progress surfaced in the status bar; no blocking overlays.
- Reads correctly route to whichever pane (channel or thread) is
  focused.

## Non-Goals

- Drag-and-drop file uploads. (Terminals don't deliver drag events
  consistently.)
- File downloads from Slack (separate roadmap item).
- Inline image rendering (separate roadmap item).
- Pasting non-image binary clipboard contents (e.g., PDFs sometimes
  land on the clipboard as bytes — we only handle image MIME for
  binary; binaries via path).
- Editing existing messages to add attachments (`chat.update` doesn't
  support files; trying it from edit mode is refused with a toast).
- Mid-upload cancellation (HTTP PUT in progress can't be torn down
  cleanly).

## User-Facing Behavior

### `Ctrl+V` in insert mode

The user is composing a message (channel pane or thread reply pane).
They press `Ctrl+V`. slk inspects the clipboard:

1. If image bytes are present and ≤ 10 MB:
   - Attach with filename `slk-paste-2026-04-29-15-30-45.png` (RFC3339
     date-time, colons replaced with hyphens).
   - Status bar toast: `Attached: slk-paste-…png (243 KB)`.
   - Chip appears above the textarea.
2. Else if clipboard text is a single line and resolves to an existing
   regular file (after `~/` expansion) ≤ 10 MB:
   - Attach with the file's basename.
   - Status bar toast: `Attached: report.pdf (820 KB)`.
   - Chip appears above the textarea.
3. Else:
   - Fall through to text paste — the clipboard text gets inserted
     into the textarea via the existing compose update path.

Refusal cases (each shows a toast and does not attach):
- Image > 10 MB: `Image too large (12.3 MB > 10 MB limit)`.
- File path > 10 MB: `File too large`.
- Image bytes are 0: `Empty file`.

### Pending-attachment chips

When the compose has any pending attachments, a one-row chip area
renders above the textarea, full width:

```
┌──────────────────────────────────────────────────────────┐
│ [📎 screenshot.png 243 KB] [📎 report.pdf 820 KB]         │
│ Heads up — design review notes attached                  │
└──────────────────────────────────────────────────────────┘
```

- Chip format: `📎 <filename> <human-size>`. Attachments are listed
  left-to-right in insertion order. Newest is the rightmost.
- Wrapping: if chips would overflow the row, additional chips wrap to
  more rows below the first (lipgloss handles this; there's no
  truncation at the chip-row level beyond per-chip filename truncation
  to a reasonable max — e.g., 32 chars + ellipsis).
- During upload, the chip row renders with muted background/foreground
  (the `uploading` flag) so the user sees they're in-flight.

### Sending with attachments

Pressing Enter in insert mode while attachments are present:

- The textarea text becomes the caption (Slack's `initial_comment`).
- Caption + attachments are dispatched via `UploadAttachmentsMsg`.
- `compose.SetUploading(true)`; chips greyed.
- Status bar shows `Uploading 1/N…`, advancing as each file completes.
- Esc during upload: ignored, with toast `Upload in progress`.

On completion:

- Success: status bar toast `Sent` (2s); compose clears attachments
  and text; uploading flag resets. The Slack WebSocket echo arrives
  and renders the message + files normally via the existing
  `extractAttachments` pipeline at `cmd/slk/main.go:883`.
- Failure: status bar toast `Upload failed: <truncated reason>` (3s);
  attachments and caption remain in the compose; user retries by
  pressing Enter again or removes chips and tries different inputs.
  If the failure happened mid-batch (say file 2 of 3), file 1 is on
  Slack already and the user sees it via WS echo; only the remaining
  attachments stay in the compose for retry.

### Removing chips

Backspace, when the textarea is empty *and* the cursor is at column 0
*and* there is at least one pending attachment, removes the most
recently added attachment.

- Status bar toast: `Removed: <filename>` (1.5s).
- The chip row collapses when the last attachment is removed.

### Caption semantics

- 0 attachments: existing send/reply path, unchanged.
- ≥ 1 attachment + non-empty caption: caption attaches to the **last**
  file's `InitialComment`. Slack groups files shared in a single
  `completeUploadExternal` call into one message thread, so all files
  + the caption appear as a single Slack post. (When sequential
  uploads can't be batched into one share, attaching the caption to
  the last file is the closest approximation.)
- ≥ 1 attachment + empty caption: files post with no caption, same
  grouping.

### Linux dependency

The README's "Install / Build from source" section grows a note that
Linux requires `libx11-dev` (Debian/Ubuntu) or equivalent at build
time, plus a clipboard manager at runtime (X11 default, or
`wl-clipboard` for Wayland). Headless Linux builds will compile but
log a warning at startup; `Ctrl+V` falls back to text paste.

## Architecture

### New direct dependency: `golang.design/x/clipboard`

Add to `go.mod` as a direct require. Initialize once at `App` startup
(or in `cmd/slk/main.go` before the program starts) via
`clipboard.Init()`:

```go
clipboardAvailable := true
if err := clipboard.Init(); err != nil {
    log.Printf("Warning: clipboard init failed (%v); Ctrl+V image paste disabled", err)
    clipboardAvailable = false
}
app.SetClipboardAvailable(clipboardAvailable)
```

`golang.org/x/image` is already a transitive dep; `image/png` is
already imported in `internal/avatar`, so PNG validation is reusable
(though we don't decode-then-re-encode — we pass the bytes through).

### Slack client extension — `internal/slack/client.go`

Extend the `SlackAPI` interface (lines 21–43) with:

```go
UploadFileV2Context(ctx context.Context, params slack.UploadFileV2Parameters) (*slack.FileSummary, error)
```

Note: in slack-go v0.23.0 the canonical method is named `UploadFile`
(legacy V1 was retired). The interface method should mirror that name
for clarity.

Add the wrapper next to `SendMessage` (line 408):

```go
// UploadFile uploads a file to a channel (and optional thread). It
// uses Slack's V2 external-upload flow under the hood
// (getUploadURLExternal → PUT → completeUploadExternal).
//
// caption, when non-empty, attaches as the file's initial_comment.
// Slack groups files completed in a single share into one message;
// for multi-file uploads we currently call this once per file, so
// the caption should be attached to the last file only.
func (c *Client) UploadFile(
    ctx context.Context,
    channelID, threadTS, filename string,
    r io.Reader,
    size int64,
    caption string,
) (*slack.FileSummary, error) {
    // FileSize is int in slack-go v0.23.0; we accept int64 from the
    // caller (matching os.FileInfo.Size()) and narrow here. The 10 MB
    // upstream cap means int range is never exceeded on supported
    // platforms.
    params := slack.UploadFileV2Parameters{
        Filename: filename,
        Reader:   r,
        FileSize: int(size),
        Channel:  channelID,
    }
    if threadTS != "" {
        params.ThreadTimestamp = threadTS
    }
    if caption != "" {
        params.InitialComment = caption
    }
    f, err := c.api.UploadFileV2Context(ctx, params)
    if err != nil {
        return nil, fmt.Errorf("uploading file %q: %w", filename, err)
    }
    return f, nil
}
```

### Pending-attachment state on `compose.Model`

In `internal/ui/compose/model.go`:

```go
// PendingAttachment is a file (or in-memory image) waiting to be
// uploaded with the next send. Bytes and Path are mutually exclusive:
// Bytes is set for clipboard-pasted images; Path is set for
// file-path-pasted files (read at upload time, not at attach time).
type PendingAttachment struct {
    Filename string
    Bytes    []byte // non-nil for clipboard images
    Path     string // non-empty for file-path attachments
    Mime     string
    Size     int64
}

// New fields on Model:
type Model struct {
    // ... existing fields ...
    pending   []PendingAttachment
    uploading bool
}
```

New methods:

```go
func (m *Model) AddAttachment(a PendingAttachment)
func (m *Model) RemoveLastAttachment() (PendingAttachment, bool)
func (m *Model) Attachments() []PendingAttachment // returns a copy
func (m *Model) ClearAttachments()
func (m *Model) SetUploading(on bool)
func (m *Model) Uploading() bool
```

`View()` (current line 657) is modified to render a chip row above the
textarea when `len(m.pending) > 0`. The `uploading` flag swaps the
chip styles to muted variants. Width math accounts for the chip row's
height when reporting cursor row to the parent layout.

`Update()` (current line 181) intercepts Backspace when:
- `m.uploading == false`
- The textarea value is empty
- The cursor column is 0
- `len(m.pending) > 0`

In that case it calls `RemoveLastAttachment()` and returns the
removed attachment (and a status-bar toast cmd) instead of forwarding
to the textarea.

### New tea.Msg types in `internal/ui/app.go`

Adjacent to `SendMessageMsg`:

```go
type UploadAttachmentsMsg struct {
    ChannelID   string
    ThreadTS    string
    Caption     string
    Attachments []compose.PendingAttachment
}

type UploadProgressMsg struct {
    Done  int
    Total int
}

type UploadResultMsg struct {
    Err error
}

type UploadFunc func(channelID, threadTS, caption string, attachments []compose.PendingAttachment) tea.Cmd

// New field on App:
uploader UploadFunc

func (a *App) SetUploader(fn UploadFunc) { a.uploader = fn }
```

`UploadFunc` returns a `tea.Cmd` so the implementation can stream
per-file `UploadProgressMsg` ticks using `program.Send` (the same
pattern the RTM event handler already uses) before returning the
final `UploadResultMsg`. The cmd's return value is the terminal
result; intermediate progress arrives out-of-band via `program.Send`
to avoid blocking on a sequenced batch.

### Wiring in `cmd/slk/main.go`

Adjacent to `app.SetMessageSender(...)` (line 349):

```go
app.SetUploader(func(channelID, threadTS, caption string, attachments []compose.PendingAttachment) tea.Cmd {
    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
        defer cancel()

        // Slack groups files shared in a single completeUploadExternal
        // call. UploadFileV2 currently does one share per call, so we
        // attach the caption to the LAST file only and accept that
        // multi-file paste creates one message per file (with the
        // last carrying the caption).
        for i, att := range attachments {
            program.Send(ui.UploadProgressMsg{Done: i, Total: len(attachments)})

            var reader io.Reader
            if att.Bytes != nil {
                reader = bytes.NewReader(att.Bytes)
            } else {
                f, err := os.Open(att.Path)
                if err != nil {
                    return ui.UploadResultMsg{Err: fmt.Errorf("opening %s: %w", att.Filename, err)}
                }
                defer f.Close()
                reader = f
            }

            currentCaption := ""
            if i == len(attachments)-1 {
                currentCaption = caption
            }

            _, err := client.UploadFile(ctx, channelID, threadTS, att.Filename, reader, att.Size, currentCaption)
            if err != nil {
                return ui.UploadResultMsg{Err: fmt.Errorf("uploading %s (%d/%d): %w", att.Filename, i+1, len(attachments), err)}
            }
        }
        return ui.UploadResultMsg{Err: nil}
    }
})
```

### Smart-paste handler in `app.go`

A new branch in `handleInsertMode` for `Ctrl+V`. Locate the existing
`code := msg.Key().Code` / `mod := msg.Key().Mod` lines (around 1432).
Add a check before the `isSend`/`isNewline` switching:

```go
isPaste := code == 'v' && mod == tea.ModCtrl
if isPaste {
    return a.smartPaste()
}
```

`smartPaste` resolves which compose is active (channel vs. thread)
based on `focusedPanel` and `threadVisible`:

```go
func (a *App) smartPaste() tea.Cmd {
    if !a.clipboardAvailable {
        return nil // text-paste fallback would happen via the textarea forwarding,
                   // but Ctrl+V isn't a textarea-native key; just no-op
    }

    target := &a.compose
    if a.focusedPanel == PanelThread && a.threadVisible {
        target = &a.threadCompose
    }

    // 1. Image bytes
    if imgBytes := clipboard.Read(clipboard.FmtImage); len(imgBytes) > 0 {
        const maxSize = 10 * 1024 * 1024
        if len(imgBytes) > maxSize {
            return toastCmd(fmt.Sprintf("Image too large (%s > 10 MB limit)", humanSize(int64(len(imgBytes)))))
        }
        if len(imgBytes) == 0 {
            return toastCmd("Empty file")
        }
        filename := "slk-paste-" + time.Now().Format("2006-01-02-15-04-05") + ".png"
        target.AddAttachment(compose.PendingAttachment{
            Filename: filename,
            Bytes:    imgBytes,
            Mime:     "image/png",
            Size:     int64(len(imgBytes)),
        })
        return toastCmd(fmt.Sprintf("Attached: %s (%s)", filename, humanSize(int64(len(imgBytes)))))
    }

    // 2. File path
    text := string(clipboard.Read(clipboard.FmtText))
    if path, ok := resolveFilePath(text); ok {
        info, err := os.Stat(path)
        if err == nil && info.Mode().IsRegular() {
            const maxSize = 10 * 1024 * 1024
            if info.Size() > maxSize {
                return toastCmd("File too large")
            }
            if info.Size() == 0 {
                return toastCmd("Empty file")
            }
            target.AddAttachment(compose.PendingAttachment{
                Filename: filepath.Base(path),
                Path:     path,
                Mime:     mime.TypeByExtension(filepath.Ext(path)),
                Size:     info.Size(),
            })
            return toastCmd(fmt.Sprintf("Attached: %s (%s)", filepath.Base(path), humanSize(info.Size())))
        }
    }

    // 3. Text fallback — forward to the active compose's textarea.
    return a.pasteTextIntoCompose(text)
}

// resolveFilePath: trim space; reject if multi-line or > 4096 chars;
// expand "~/"; require absolute or ./-relative path.
func resolveFilePath(text string) (string, bool) {
    s := strings.TrimSpace(text)
    if s == "" || strings.ContainsAny(s, "\n\r") || len(s) > 4096 {
        return "", false
    }
    if strings.HasPrefix(s, "~/") {
        home, err := os.UserHomeDir()
        if err != nil {
            return "", false
        }
        s = filepath.Join(home, s[2:])
    }
    if !filepath.IsAbs(s) && !strings.HasPrefix(s, "./") {
        return "", false
    }
    return filepath.Clean(s), true
}
```

`humanSize`, `toastCmd`, and `pasteTextIntoCompose` are small helpers
added in this work or reused if equivalent helpers already exist. The
toast pattern mirrors `statusbar.PermalinkCopiedMsg` — set toast,
schedule `CopiedClearMsg` after 2s.

### Send-with-attachments in `handleInsertMode`

In the existing channel-send and thread-send `isSend` blocks (around
lines 1455 and 1491), prepend an attachments check:

```go
if isSend {
    if len(target.Attachments()) > 0 {
        return a.submitWithAttachments(target)
    }
    // ... existing text-only send/reply logic ...
}
```

Where `submitWithAttachments`:

```go
func (a *App) submitWithAttachments(c *compose.Model) tea.Cmd {
    if a.editing.active {
        return toastCmd("Cannot attach files to an edit (send a new message)")
    }
    attachments := c.Attachments()
    caption := strings.TrimSpace(c.Value())
    var channelID, threadTS string
    if c == &a.threadCompose {
        channelID = a.threadPanel.ChannelID()
        threadTS = a.threadPanel.ThreadTS()
    } else {
        channelID = a.activeChannelID
        threadTS = ""
    }
    if channelID == "" || a.uploader == nil {
        return toastCmd("Cannot upload: no active channel")
    }
    c.SetUploading(true)
    cmd := a.uploader(channelID, threadTS, caption, attachments)
    return tea.Batch(
        cmd,
        toastCmd(fmt.Sprintf("Uploading 0/%d…", len(attachments))),
    )
}
```

`UploadProgressMsg` and `UploadResultMsg` are handled in
`App.Update`:

```go
case UploadProgressMsg:
    a.statusbar.SetToast(fmt.Sprintf("Uploading %d/%d…", msg.Done, msg.Total))

case UploadResultMsg:
    a.compose.SetUploading(false)
    a.threadCompose.SetUploading(false)
    if msg.Err != nil {
        cmds = append(cmds, toastCmd("Upload failed: "+truncateReason(msg.Err.Error(), 40)))
        // Attachments stay; user retries by pressing Enter.
        return a, tea.Batch(cmds...)
    }
    a.compose.ClearAttachments()
    a.threadCompose.ClearAttachments()
    a.compose.Reset()
    a.threadCompose.Reset()
    cmds = append(cmds, toastCmd("Sent"))
```

(Calling Reset on both is fine — only one had content.)

### Esc-during-upload, channel/workspace switch during upload

In `handleInsertMode`'s top-of-function Esc check, before the existing
edit-cancel check, add:

```go
if (a.compose.Uploading() || a.threadCompose.Uploading()) && key.Matches(msg, a.keys.Escape) {
    return toastCmd("Upload in progress")
}
```

In `case ChannelSelectedMsg:` and `case WorkspaceSwitchedMsg:`, before
the existing logic:

```go
if a.compose.Uploading() || a.threadCompose.Uploading() {
    cmds = append(cmds, toastCmd("Upload in progress"))
    break // refuse the switch
}
```

## Data Flow

### Smart-paste

```
press Ctrl+V (insert mode)
  ↓
handleInsertMode → smartPaste
  - clipboardAvailable? (set at startup from clipboard.Init)
    no → no-op
  - clipboard.Read(FmtImage) returns bytes?
    yes → size check → AddAttachment + toast
  - else clipboard.Read(FmtText) → resolveFilePath → os.Stat regular file?
    yes → size check → AddAttachment(by Path) + toast
  - else → pasteTextIntoCompose(text)
```

### Send with attachments

```
press Enter (insert mode, len(attachments) > 0)
  ↓
handleInsertMode isSend → submitWithAttachments
  - editing.active? → refuse with toast
  - resolve channelID + threadTS by which compose
  - compose.SetUploading(true) (chips render greyed, Esc refused)
  - emit UploadAttachmentsMsg via uploader(...)
  - status bar shows initial "Uploading 0/N…"
  ↓
uploader (in main.go) iterates attachments:
  - per file: program.Send(UploadProgressMsg{Done:i, Total:N})
  - per file: client.UploadFile (caption only on last)
  - first error → return UploadResultMsg{Err}
  - all success → return UploadResultMsg{Err: nil}
  ↓
App.Update receives UploadProgressMsg → updates toast
App.Update receives UploadResultMsg:
  - SetUploading(false) on both composes
  - success: ClearAttachments + Reset + "Sent" toast
  - failure: keep attachments, "Upload failed: …" toast
  ↓
WS echo arrives → existing extractAttachments → message renders
```

### Backspace removes chip

```
press Backspace (insert mode, textarea empty, col 0, len(pending) > 0)
  ↓
compose.Update detects state → RemoveLastAttachment
  ↓
return removed PendingAttachment (caller emits "Removed: <name>" toast)
```

## Error Handling & Edge Cases

- `clipboard.Init()` failure at startup: log warning, set
  `clipboardAvailable = false`. `Ctrl+V` smart-paste is a no-op
  thereafter (text-paste via the textarea's normal Update isn't
  triggered by Ctrl+V — Ctrl+V isn't a printable character).
- `clipboard.Read(FmtImage)` returns empty bytes (the common case):
  fall through to text-path detection.
- `clipboard.Read(FmtText)` returns empty: text fallback inserts
  nothing; effectively a no-op.
- File-path heuristic rejects: directories, symlink loops,
  non-regular files (devices, pipes, sockets), paths with embedded
  newlines, paths > 4096 chars, paths that don't expand to absolute
  or `./`-relative form.
- Image > 10 MB or file > 10 MB: refused with sized toast, no attach.
- Image bytes exactly 0 / file exactly 0 bytes: refused with `Empty
  file` toast.
- Permission denied on file read at upload time: caught in
  `os.Open`, surfaces as upload error.
- Network failure mid-upload: surfaces as `UploadResultMsg{Err}`,
  attachments stay for retry.
- Slack auth/scope rejection (`not_allowed_token_type`,
  `invalid_auth`): same path — error toast surfaces the Slack reason.
- Partial multi-file failure: abort on first error; toast indicates
  position (`Upload failed: uploading screenshot.png (2/3): …`);
  successfully-uploaded earlier files arrive via WS echo and stay; the
  remaining attachments stay in the compose.
- Channel/workspace switch during upload: refused with `Upload in
  progress` toast; switch deferred until upload result arrives.
- Esc during upload: refused with `Upload in progress` toast.
- Concurrent edit-mode + paste: Ctrl+V still attaches to the edit
  compose. Submitting with attachments while editing is refused with
  `Cannot attach files to an edit (send a new message)`. Backspace-
  removes-chip works. Esc cancels the edit and discards the
  attachments (consistent with the existing edit-cancel behavior).
- Successful send: clear attachments and text on both composes
  (cheap and harmless; only one had content).
- Failed send: leave attachments and text intact for retry.
- WSMessageDeletedMsg / NewMessageMsg arrive during upload: render
  normally; upload state is independent.

## Testing

### Unit
- `internal/ui/compose/model_test.go`:
  - `AddAttachment` / `RemoveLastAttachment` / `Attachments` /
    `ClearAttachments` lifecycle.
  - `SetUploading(true/false)` and `Uploading()` round-trip.
  - View() renders chip row when attachments present, collapses when
    cleared.
  - Backspace at column 0 of empty textarea with attachments calls
    `RemoveLastAttachment` (does not forward to textarea).
- `internal/slack/client_test.go`:
  - `UploadFile` calls `UploadFileV2Context` on the SlackAPI mock with
    correct (channelID, threadTS, filename, reader, size, caption).
  - On error, the wrapped error contains the filename.
- `internal/ui/app_test.go`:
  - `smartPaste`: image present (≤ limit) → attaches; > limit →
    refuses; not present + valid path → attaches by path; invalid
    path → text fallback; empty clipboard → no-op.
  - `submitWithAttachments`: emits `UploadAttachmentsMsg`, not
    `SendMessageMsg`; refuses during edit mode.
  - `UploadProgressMsg` updates status bar toast.
  - `UploadResultMsg{Err: nil}` clears attachments and emits "Sent".
  - `UploadResultMsg{Err: x}` keeps attachments and emits failure
    toast.
  - Esc during upload refused with `Upload in progress`.
  - Channel switch during upload refused.

### Integration
- `cmd/slk/main_test.go` (or local equivalent):
  - `uploader` setter wires through to `client.UploadFile`.
  - Multi-attachment iteration: caption attached to last only.
  - Failure-at-N short-circuits with correct error.

### Manual smoke
- Take a screenshot, paste into a channel; verify Slack web shows the
  file with caption.
- Paste into a thread; verify the file lands in the thread, not the
  channel.
- Paste a file path (`~/Downloads/foo.pdf`); verify upload as PDF
  with original filename.
- Paste a malformed path; verify text-paste fallback inserts the
  literal string into the textarea.
- Paste a 12 MB image; verify refusal toast.
- Paste two images in a row; verify both chips appear; send; verify
  both files in Slack.
- Backspace-remove second chip; send; verify only one file uploaded.
- Disconnect network mid-upload; verify failure toast and chips
  remain for retry.
- Run on macOS, Linux X11, Linux Wayland (with `wl-clipboard`
  installed), and a headless Linux build to confirm
  `clipboard.Init()` fallback.

## Files Touched

| File                                        | Change                          |
|---------------------------------------------|---------------------------------|
| `go.mod`, `go.sum`                          | Add `golang.design/x/clipboard` |
| `internal/slack/client.go`                  | `UploadFileV2Context` on `SlackAPI`; `UploadFile` wrapper |
| `internal/slack/client_test.go`             | Tests for new wrapper           |
| `internal/ui/compose/model.go`              | `PendingAttachment`, pending field, uploading flag, methods, View() chip row, Update() Backspace |
| `internal/ui/compose/model_test.go`         | Tests for the above             |
| `internal/ui/app.go`                        | New tea.Msg types, setter, `smartPaste`, `submitWithAttachments`, Update arms, Esc/switch guards |
| `internal/ui/app_test.go`                   | Tests for the above             |
| `cmd/slk/main.go`                           | `clipboard.Init()`, `SetUploader` wiring |
| `README.md`                                 | Document Linux build deps + `Ctrl+V` paste behavior |
| `docs/STATUS.md`                            | Mark file uploads (paste path) implemented |

## Open Questions

None.

## References

- Slack-go v0.23.0 file-upload: `slack@v0.23.0/files.go` —
  `UploadFileV2`, `UploadFileV2Context`, `UploadFileV2Parameters`,
  `CompleteUploadExternalParameters`.
- Existing read-side pipeline: `cmd/slk/main.go:883` (`extractAttachments`).
- Toast pattern reused: `statusbar.PermalinkCopiedMsg` in
  `internal/ui/statusbar/model.go`.
- Edit-mode interaction: design at
  `docs/superpowers/specs/2026-04-29-edit-delete-messages-design.md`
  and implementation at branch `feature/edit-delete-messages` (now
  merged).
- Clipboard library:
  https://pkg.go.dev/golang.design/x/clipboard.
- Linux clipboard runtime requirements: `wl-clipboard` (Wayland) or
  `xclip`/`xsel` (X11), plus `libx11-dev` at build time for cgo.
