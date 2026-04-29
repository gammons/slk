# Paste-to-Upload Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `Ctrl+V` smart-paste to slk's compose so users can paste images, file paths, or text from the OS clipboard, with multi-attachment uploads via Slack's V2 file-upload API.

**Architecture:** A new `golang.design/x/clipboard` dependency reads PNG image bytes or text from the OS clipboard. A compose-layer `PendingAttachment` slice tracks files between `Ctrl+V` and Enter. The Slack client wraps `UploadFileV2Context`. Status-bar toasts surface progress and errors. Server-confirmed via WS echo (no optimistic rendering), matching the edit/delete feature's pattern.

**Tech Stack:** Go 1.22+, charm.land/bubbletea/v2, charm.land/bubbles/v2 (textarea), charm.land/lipgloss/v2, slack-go/slack v0.23.0, golang.design/x/clipboard v0.7+, SQLite via `internal/cache`. Linux requires `libx11-dev` at build time.

---

## File Structure

**New files:**
- (none — clipboard logic lives in app.go alongside other I/O dispatchers)

**Modified files:**
- `go.mod`, `go.sum` — add `golang.design/x/clipboard`.
- `internal/slack/client.go` — extend `SlackAPI` interface, add `UploadFile` wrapper.
- `internal/slack/client_test.go` — tests for the wrapper.
- `internal/ui/compose/model.go` — `PendingAttachment` type, `pending` slice, `uploading` flag, accessor/mutator methods, modified `View()`, modified `Update()` for backspace-removes-chip.
- `internal/ui/compose/model_test.go` — tests for the new methods + chip rendering + backspace behavior.
- `internal/ui/app.go` — new tea.Msg types + `UploadFunc`; `clipboardAvailable` field; `smartPaste`, `submitWithAttachments`, `humanSize`, `resolveFilePath`, `toastCmd` helpers; `Ctrl+V` insert-mode branch; `UploadProgressMsg` + `UploadResultMsg` Update arms; Esc/channel-switch/workspace-switch guards during upload.
- `internal/ui/app_test.go` — tests for smart-paste decision logic, send-with-attachments routing, upload result handling, guards during upload.
- `cmd/slk/main.go` — `clipboard.Init()` at startup, `app.SetClipboardAvailable(...)`, `app.SetUploader(...)` callback.
- `README.md` — `Ctrl+V` keybinding row + Linux build dependency note.
- `docs/STATUS.md` — mark file uploads (paste path) implemented.

---

## Sequencing Rationale

Bottom-up. Slack-client wrapper first (independently testable). Then compose-layer state (independently testable). Then statusbar toast types (declarative). Then app.go orchestration (depends on all of the above). Then cmd/slk wiring. Finally docs.

---

## Task 1: Add `UploadFile` wrapper to slack client

**Files:**
- Modify: `internal/slack/client.go` — extend `SlackAPI` interface (lines 21–43), add wrapper near `SendMessage` (line 408).
- Test: `internal/slack/client_test.go`

The slack-go v0.23.0 `*slack.Client` has `UploadFileV2Context(ctx, slack.UploadFileV2Parameters) (*slack.FileSummary, error)`. We wrap it through the testable `SlackAPI` interface.

- [ ] **Step 1: Write failing test**

First, look at the existing client_test.go to confirm the mock pattern. The mock likely embeds `*slack.Client` or implements `SlackAPI` directly. Append to `internal/slack/client_test.go` (using whatever mock pattern is already established):

```go
func TestUploadFile_Success(t *testing.T) {
	mock := newMockSlackAPI()
	mock.uploadFileV2Result = &slack.FileSummary{ID: "F123", Title: "screenshot.png"}

	c := &Client{api: mock}

	r := strings.NewReader("fake-png-bytes")
	f, err := c.UploadFile(context.Background(), "C1", "", "screenshot.png", r, 14, "look at this")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.ID != "F123" {
		t.Errorf("expected FileSummary ID F123, got %q", f.ID)
	}
	if mock.lastUploadParams.Channel != "C1" {
		t.Errorf("expected Channel=C1, got %q", mock.lastUploadParams.Channel)
	}
	if mock.lastUploadParams.Filename != "screenshot.png" {
		t.Errorf("expected Filename=screenshot.png, got %q", mock.lastUploadParams.Filename)
	}
	if mock.lastUploadParams.FileSize != 14 {
		t.Errorf("expected FileSize=14, got %d", mock.lastUploadParams.FileSize)
	}
	if mock.lastUploadParams.InitialComment != "look at this" {
		t.Errorf("expected InitialComment, got %q", mock.lastUploadParams.InitialComment)
	}
	if mock.lastUploadParams.ThreadTimestamp != "" {
		t.Errorf("expected empty ThreadTimestamp, got %q", mock.lastUploadParams.ThreadTimestamp)
	}
}

func TestUploadFile_Thread(t *testing.T) {
	mock := newMockSlackAPI()
	mock.uploadFileV2Result = &slack.FileSummary{ID: "F124"}

	c := &Client{api: mock}
	_, err := c.UploadFile(context.Background(), "C1", "1700000000.000100", "x.png",
		strings.NewReader("x"), 1, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.lastUploadParams.ThreadTimestamp != "1700000000.000100" {
		t.Errorf("expected ThreadTimestamp set, got %q", mock.lastUploadParams.ThreadTimestamp)
	}
	if mock.lastUploadParams.InitialComment != "" {
		t.Errorf("expected empty InitialComment, got %q", mock.lastUploadParams.InitialComment)
	}
}

func TestUploadFile_ErrorWraps(t *testing.T) {
	mock := newMockSlackAPI()
	mock.uploadFileV2Err = errors.New("not_authorized")

	c := &Client{api: mock}
	_, err := c.UploadFile(context.Background(), "C1", "", "x.png",
		strings.NewReader("x"), 1, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "x.png") {
		t.Errorf("expected error to mention filename, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "not_authorized") {
		t.Errorf("expected error to wrap underlying, got %q", err.Error())
	}
}
```

If the mock pattern in client_test.go is different (e.g., uses `httptest`), adapt. The key invariants to test are: parameters (channel, filename, size, threadTS, caption) are passed through correctly, and errors are wrapped with the filename.

You may need to extend the mock struct with three new fields:

```go
type mockSlackAPI struct {
    // ... existing fields ...
    uploadFileV2Result *slack.FileSummary
    uploadFileV2Err    error
    lastUploadParams   slack.UploadFileV2Parameters
}

func (m *mockSlackAPI) UploadFileV2Context(ctx context.Context, params slack.UploadFileV2Parameters) (*slack.FileSummary, error) {
    m.lastUploadParams = params
    return m.uploadFileV2Result, m.uploadFileV2Err
}
```

Add `errors` and `strings` imports if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/slack/ -run TestUploadFile -v`
Expected: FAIL — `UploadFile` and/or `UploadFileV2Context` undefined.

- [ ] **Step 3: Add to the SlackAPI interface**

In `internal/slack/client.go`, find the `SlackAPI` interface (lines 21–43). Add this method line, alphabetized or at the end (match existing style):

```go
UploadFileV2Context(ctx context.Context, params slack.UploadFileV2Parameters) (*slack.FileSummary, error)
```

- [ ] **Step 4: Add the wrapper method**

In the same file, add this after `SendMessage` (around line 414):

```go
// UploadFile uploads a single file to a channel (and optional thread)
// using Slack's V2 external-upload flow. The slack-go library's
// UploadFileV2Context handles the three internal steps
// (getUploadURLExternal → PUT → completeUploadExternal).
//
// caption, when non-empty, is attached as the file's initial_comment.
// For multi-file batches the caller should set caption on the LAST
// file only (Slack groups files completed in one share into one
// message; sequential single-file uploads can't be grouped).
//
// size is int64 (matching os.FileInfo.Size()) and is narrowed to int
// for slack-go. Callers must enforce a reasonable upper bound; this
// wrapper does not.
func (c *Client) UploadFile(
	ctx context.Context,
	channelID, threadTS, filename string,
	r io.Reader,
	size int64,
	caption string,
) (*slack.FileSummary, error) {
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

If `io` isn't already imported in `client.go`, add it.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/slack/ -v`
Expected: PASS for all tests including the three new ones.

Then run `go build ./...` — should succeed (the new method is unused by other packages so far, which is fine).

- [ ] **Step 6: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "feat(slack): add UploadFile wrapper using V2 external-upload flow"
```

---

## Task 2: Add `PendingAttachment` and `pending` state to compose.Model

**Files:**
- Modify: `internal/ui/compose/model.go`
- Test: `internal/ui/compose/model_test.go`

This task adds the data layer only — no chip rendering yet (that's Task 3) and no Backspace behavior yet (Task 4). Smaller TDD slices.

- [ ] **Step 1: Write failing tests**

Append to `internal/ui/compose/model_test.go`:

```go
func TestAddAttachment_AppendsToPending(t *testing.T) {
	m := New("general")
	att := PendingAttachment{Filename: "a.png", Bytes: []byte("x"), Mime: "image/png", Size: 1}
	m.AddAttachment(att)

	got := m.Attachments()
	if len(got) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(got))
	}
	if got[0].Filename != "a.png" {
		t.Errorf("expected filename a.png, got %q", got[0].Filename)
	}
}

func TestAttachments_ReturnsCopy(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "a.png", Bytes: []byte("x"), Size: 1})
	got := m.Attachments()
	got[0].Filename = "MUTATED"

	again := m.Attachments()
	if again[0].Filename != "a.png" {
		t.Errorf("Attachments() must return a copy; got mutation: %q", again[0].Filename)
	}
}

func TestRemoveLastAttachment(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "a.png", Size: 1})
	m.AddAttachment(PendingAttachment{Filename: "b.png", Size: 2})

	removed, ok := m.RemoveLastAttachment()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if removed.Filename != "b.png" {
		t.Errorf("expected to remove b.png, got %q", removed.Filename)
	}
	if len(m.Attachments()) != 1 {
		t.Errorf("expected 1 remaining, got %d", len(m.Attachments()))
	}
}

func TestRemoveLastAttachment_Empty(t *testing.T) {
	m := New("general")
	_, ok := m.RemoveLastAttachment()
	if ok {
		t.Error("expected ok=false on empty pending")
	}
}

func TestClearAttachments(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "a.png", Size: 1})
	m.AddAttachment(PendingAttachment{Filename: "b.png", Size: 2})
	m.ClearAttachments()
	if len(m.Attachments()) != 0 {
		t.Errorf("expected empty after Clear, got %d", len(m.Attachments()))
	}
}

func TestSetUploading(t *testing.T) {
	m := New("general")
	if m.Uploading() {
		t.Error("expected !Uploading() initially")
	}
	m.SetUploading(true)
	if !m.Uploading() {
		t.Error("expected Uploading() after SetUploading(true)")
	}
	m.SetUploading(false)
	if m.Uploading() {
		t.Error("expected !Uploading() after SetUploading(false)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/compose/ -run 'TestAddAttachment|TestAttachments|TestRemoveLastAttachment|TestClearAttachments|TestSetUploading' -v`
Expected: FAIL — `PendingAttachment`, `AddAttachment`, etc. undefined.

- [ ] **Step 3: Add the type and methods**

In `internal/ui/compose/model.go`:

(a) Near the top of the file (above `type Model struct`), add the type:

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
```

(b) Add fields to the `Model` struct (around lines 18–47), placed after the existing fields and before `version int64`:

```go
	// pending lists attachments queued for the next send. Cleared on
	// successful submit; preserved on failure for retry.
	pending []PendingAttachment

	// uploading is true while attachments are mid-upload. Causes the
	// chip row to render in muted style and the Update() to refuse
	// Esc / Backspace-clear.
	uploading bool
```

(c) Add methods. Place them next to the existing simple accessors (e.g., near `Value()`, `SetValue()`):

```go
// AddAttachment appends a pending attachment. Newest is last.
func (m *Model) AddAttachment(a PendingAttachment) {
	m.pending = append(m.pending, a)
	m.dirty()
}

// RemoveLastAttachment removes the most-recently-added pending
// attachment and returns it. Returns ok=false if pending is empty.
func (m *Model) RemoveLastAttachment() (PendingAttachment, bool) {
	if len(m.pending) == 0 {
		return PendingAttachment{}, false
	}
	last := m.pending[len(m.pending)-1]
	m.pending = m.pending[:len(m.pending)-1]
	m.dirty()
	return last, true
}

// Attachments returns a copy of the current pending attachments.
func (m *Model) Attachments() []PendingAttachment {
	if len(m.pending) == 0 {
		return nil
	}
	out := make([]PendingAttachment, len(m.pending))
	copy(out, m.pending)
	return out
}

// ClearAttachments removes all pending attachments.
func (m *Model) ClearAttachments() {
	if len(m.pending) == 0 {
		return
	}
	m.pending = nil
	m.dirty()
}

// SetUploading sets the uploading flag, which causes the chip row to
// render in muted style and certain inputs to be ignored.
func (m *Model) SetUploading(on bool) {
	if m.uploading == on {
		return
	}
	m.uploading = on
	m.dirty()
}

// Uploading reports whether an upload is currently in flight.
func (m *Model) Uploading() bool { return m.uploading }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/compose/ -v`
Expected: PASS for all tests including the new ones.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/compose/model.go internal/ui/compose/model_test.go
git commit -m "feat(compose): add PendingAttachment state on Model"
```

---

## Task 3: Render attachment chips above the textarea in compose.View

**Files:**
- Modify: `internal/ui/compose/model.go` (the `View(width, focused)` method, currently around line 657–687).
- Test: `internal/ui/compose/model_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/ui/compose/model_test.go`:

```go
func TestComposeView_NoAttachments_NoChipRow(t *testing.T) {
	m := New("general")
	view := m.View(60, false)
	// No attachments → no 📎 glyph anywhere.
	if strings.Contains(view, "📎") {
		t.Errorf("did not expect chip glyph in view without attachments: %q", view)
	}
}

func TestComposeView_WithAttachment_RendersChip(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "screenshot.png", Size: 12345})
	view := m.View(60, false)
	if !strings.Contains(view, "📎") {
		t.Errorf("expected chip glyph in view: %q", view)
	}
	if !strings.Contains(view, "screenshot.png") {
		t.Errorf("expected filename in chip: %q", view)
	}
}

func TestComposeView_MultipleAttachments_AllChipsRender(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "a.png", Size: 1024})
	m.AddAttachment(PendingAttachment{Filename: "b.pdf", Size: 2048})
	view := m.View(80, false)
	if !strings.Contains(view, "a.png") {
		t.Errorf("expected a.png in view")
	}
	if !strings.Contains(view, "b.pdf") {
		t.Errorf("expected b.pdf in view")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/compose/ -run 'TestComposeView_(With|Multiple|No)' -v`
Expected: FAIL — current `View` doesn't render chips.

- [ ] **Step 3: Add a chip-row helper**

Add this private helper to `internal/ui/compose/model.go` (near the bottom, before the existing `View` method):

```go
// formatChipSize converts a byte count to a human-readable string
// like "12 KB" or "3.4 MB". For chips we round to one decimal place
// for MB and integer KB; bytes < 1 KB show as "<1 KB".
func formatChipSize(size int64) string {
	const kb = 1024
	const mb = 1024 * kb
	switch {
	case size >= mb:
		return fmt.Sprintf("%.1f MB", float64(size)/float64(mb))
	case size >= kb:
		return fmt.Sprintf("%d KB", size/kb)
	default:
		return "<1 KB"
	}
}

// renderChips returns the rendered chip row for current pending
// attachments, or "" if there are none. Width is the available
// horizontal space (the same width passed to View); chips wrap
// onto multiple rows if needed via lipgloss.
func (m Model) renderChips(width int) string {
	if len(m.pending) == 0 {
		return ""
	}
	bg := styles.SurfaceDark
	fg := styles.TextPrimary
	if m.uploading {
		fg = styles.TextMuted
	}

	chipStyle := lipgloss.NewStyle().
		Background(bg).
		Foreground(fg).
		Padding(0, 1).
		MarginRight(1)

	const maxNameLen = 32
	var rendered []string
	for _, p := range m.pending {
		name := p.Filename
		if len([]rune(name)) > maxNameLen {
			name = string([]rune(name)[:maxNameLen-1]) + "…"
		}
		label := fmt.Sprintf("📎 %s %s", name, formatChipSize(p.Size))
		rendered = append(rendered, chipStyle.Render(label))
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	// Constrain to width so very long chip rows wrap rather than
	// extending past the compose box.
	return lipgloss.NewStyle().MaxWidth(width).Render(row)
}
```

If `fmt` and `lipgloss` aren't imported yet, add them. `styles.SurfaceDark`, `styles.TextPrimary`, and `styles.TextMuted` already exist.

- [ ] **Step 4: Modify View to include the chip row**

Find the existing `View` method (around line 657). It currently looks like:

```go
func (m Model) View(width int, focused bool) string {
	// ... existing rendering of textarea + border ...
}
```

Modify it to compose chip row + existing rendering:

```go
func (m Model) View(width int, focused bool) string {
	chips := m.renderChips(width)
	// ... whatever existing rendering produces, captured into a local string `body` ...
	if chips == "" {
		return body
	}
	return lipgloss.JoinVertical(lipgloss.Left, chips, body)
}
```

The existing function body produces the full bordered compose. Wrap your modification carefully: keep the existing rendering exactly as-is, only prepend the chip row when present. Don't touch the textarea width/height math — chips occupy their own row above.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/compose/ -v`
Expected: PASS, including the three new view tests and all existing tests.

If any pre-existing view test fails because the output now contains an extra row of (empty) text, double-check that `renderChips` returns `""` when `len(m.pending) == 0`.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/compose/model.go internal/ui/compose/model_test.go
git commit -m "feat(compose): render attachment chips above the textarea"
```

---

## Task 4: Backspace-removes-chip in compose.Update

**Files:**
- Modify: `internal/ui/compose/model.go` (the `Update` method, currently around line 181).
- Test: `internal/ui/compose/model_test.go`

The textarea exposes `Line() int` and `Column() int` accessors. We use these plus `Value() == ""` to detect the "empty + at column 0" state.

- [ ] **Step 1: Write failing test**

Append to `internal/ui/compose/model_test.go`:

```go
func TestUpdate_BackspaceAtColZeroEmpty_RemovesLastAttachment(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "a.png", Size: 1})
	m.AddAttachment(PendingAttachment{Filename: "b.png", Size: 2})

	// Cursor starts at (0, 0) and value is empty.
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	got := m2.Attachments()
	if len(got) != 1 {
		t.Fatalf("expected 1 attachment after backspace, got %d", len(got))
	}
	if got[0].Filename != "a.png" {
		t.Errorf("expected a.png to remain, got %q", got[0].Filename)
	}
}

func TestUpdate_BackspaceWithText_DoesNotRemoveAttachment(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "a.png", Size: 1})
	m.SetValue("hello")

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if len(m2.Attachments()) != 1 {
		t.Errorf("expected attachment to remain when text present, got %d", len(m2.Attachments()))
	}
}

func TestUpdate_BackspaceNoAttachments_PassesThrough(t *testing.T) {
	m := New("general")
	m.SetValue("hello")
	// Move cursor to start.
	m.input.SetCursor(0)

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	// Value unchanged because cursor at column 0 with no attachments
	// is just a no-op for textarea.
	if m2.Value() != "hello" {
		t.Errorf("expected value unchanged, got %q", m2.Value())
	}
}

func TestUpdate_BackspaceWhileUploading_DoesNotRemove(t *testing.T) {
	m := New("general")
	m.AddAttachment(PendingAttachment{Filename: "a.png", Size: 1})
	m.SetUploading(true)

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if len(m2.Attachments()) != 1 {
		t.Errorf("expected attachment to remain while uploading, got %d", len(m2.Attachments()))
	}
}
```

If `m.input.SetCursor(0)` doesn't compile, look at the textarea API for an equivalent (e.g., `m.input.CursorStart()` exists per the textarea source we surveyed).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/compose/ -run TestUpdate_Backspace -v`
Expected: FAIL — Backspace currently passes through to textarea.

- [ ] **Step 3: Modify Update to intercept Backspace**

Find the existing `Update` method (around line 181). At the top of the function, before the existing emoji/mention picker checks, add:

```go
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	keyMsg, isKey := msg.(tea.KeyMsg)

	// Backspace at column 0 of an empty textarea removes the last
	// pending attachment instead of forwarding to the textarea.
	// Skipped when uploading.
	if isKey && !m.uploading && len(m.pending) > 0 &&
		keyMsg.Key().Code == tea.KeyBackspace &&
		m.input.Value() == "" &&
		m.input.Line() == 0 &&
		m.input.Column() == 0 {
		m.RemoveLastAttachment()
		return m, nil
	}

	// ... existing body unchanged from this point ...
}
```

Place this BEFORE the existing `if m.emojiActive && isKey { ... }` block. The `keyMsg, isKey := msg.(tea.KeyMsg)` line is already present in the existing function (it's the first line of Update); reuse that variable.

If the existing Update doesn't already declare `keyMsg, isKey`, declare them here once and use them in both places.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/compose/ -v`
Expected: PASS for all tests.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/compose/model.go internal/ui/compose/model_test.go
git commit -m "feat(compose): backspace at column 0 of empty textarea removes last attachment"
```

---

## Task 5: Add app.go upload tea.Msg types and helpers

**Files:**
- Modify: `internal/ui/app.go`

This task adds purely additive declarations. No behavior wired yet.

- [ ] **Step 1: Add the types**

Insert after `MessageDeletedMsg` (or near the other Msg/Func definitions added by the edit/delete feature):

```go
// UploadAttachmentsMsg is emitted when the user submits a compose
// with pending attachments. App.Update invokes the configured
// uploader and converts the result into UploadProgressMsg /
// UploadResultMsg.
type UploadAttachmentsMsg struct {
	ChannelID   string
	ThreadTS    string
	Caption     string
	Attachments []compose.PendingAttachment
}

// UploadProgressMsg is dispatched out-of-band by the uploader as
// each file completes. App updates the status-bar toast.
type UploadProgressMsg struct {
	Done  int
	Total int
}

// UploadResultMsg carries the final result of an upload batch.
type UploadResultMsg struct {
	Err error
}

// UploadFunc performs an upload of one or more files to a channel
// (with optional thread). It returns a tea.Cmd whose terminal
// message is UploadResultMsg; intermediate UploadProgressMsg events
// are dispatched out-of-band via program.Send.
type UploadFunc func(channelID, threadTS, caption string, attachments []compose.PendingAttachment) tea.Cmd
```

- [ ] **Step 2: Add fields and setters**

Add fields to the `App` struct (next to the `messageEditor`/`messageDeleter` fields from the edit/delete feature, around line 431–432):

```go
	uploader            UploadFunc

	// clipboardAvailable is set at startup based on the result of
	// clipboard.Init(). When false, Ctrl+V smart-paste is a no-op.
	clipboardAvailable  bool
```

Add setters near `SetMessageDeleter` (around line 2300):

```go
// SetUploader wires the upload callback used by Ctrl+V smart-paste
// when the user submits with attachments.
func (a *App) SetUploader(fn UploadFunc) {
	a.uploader = fn
}

// SetClipboardAvailable signals whether the OS clipboard library
// initialized successfully. When false, the smart-paste code path
// is short-circuited.
func (a *App) SetClipboardAvailable(ok bool) {
	a.clipboardAvailable = ok
}
```

- [ ] **Step 3: Add helper functions**

At the bottom of `internal/ui/app.go`, near `truncateReason` (added by the edit/delete feature), add:

```go
// humanSize formats a byte count as "12 KB", "3.4 MB", or "<1 KB".
func humanSize(size int64) string {
	const kb = 1024
	const mb = 1024 * kb
	switch {
	case size >= mb:
		return fmt.Sprintf("%.1f MB", float64(size)/float64(mb))
	case size >= kb:
		return fmt.Sprintf("%d KB", size/kb)
	default:
		return "<1 KB"
	}
}

// resolveFilePath inspects clipboard text and returns a cleaned,
// absolute file path if it looks like a single-line existing-file
// reference. Returns ok=false on multi-line input, oversized input,
// non-absolute and non-./-relative paths, or paths that don't
// expand. The caller is responsible for the os.Stat / IsRegular
// check and the size check.
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

// uploadToastCmd builds a tea.Cmd that sets the status bar to the
// given message and schedules a CopiedClearMsg after dur.
func (a *App) uploadToastCmd(text string, dur time.Duration) tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			a.statusbar.SetToast(text)
			return nil
		},
		tea.Tick(dur, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}),
	)
}
```

If `os`, `path/filepath`, or `mime` aren't already imported in `app.go`, add them.

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: success (everything is additive).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat(app): add upload tea.Msg types, setters, and helpers"
```

---

## Task 6: Implement smartPaste and Ctrl+V binding in handleInsertMode

**Files:**
- Modify: `internal/ui/app.go`
- Test: `internal/ui/app_test.go`

This is where Ctrl+V becomes functional. Smart-paste needs an actual clipboard read at runtime; tests use injected dependencies.

- [ ] **Step 1: Add clipboard read indirection for testability**

In `internal/ui/app.go`, add this near the top (with other small types/vars):

```go
// clipboardReader abstracts clipboard.Read so tests can inject fake
// clipboard contents. Production code initializes this with the real
// golang.design/x/clipboard.Read function.
type clipboardReader func(format clipboard.Format) []byte

// defaultClipboardReader is the package-level real clipboard reader.
// It's overridable for tests via App.SetClipboardReader.
var defaultClipboardReader clipboardReader = clipboard.Read
```

Add to the App struct (next to `clipboardAvailable`):

```go
	clipboardRead       clipboardReader
```

In `NewApp`, initialize:

```go
		clipboardRead: defaultClipboardReader,
```

Add a setter (for tests):

```go
// SetClipboardReader replaces the clipboard read function. Used by
// tests to inject canned clipboard contents. Pass nil to restore
// the default real clipboard reader.
func (a *App) SetClipboardReader(fn clipboardReader) {
	if fn == nil {
		a.clipboardRead = defaultClipboardReader
	} else {
		a.clipboardRead = fn
	}
}
```

Add the `golang.design/x/clipboard` import to `app.go` (it must be in `go.mod` from Task 0 — which we'll add now if it isn't already).

**Pre-step: ensure the dependency is in go.mod.** Run:

```bash
go get golang.design/x/clipboard@latest
go mod tidy
```

Verify `go.mod` now lists `golang.design/x/clipboard` as a direct require. The cgo build will require `libx11-dev` (or equivalent) on Linux — if the build machine doesn't have it, install it:

- Debian/Ubuntu: `sudo apt-get install -y libx11-dev`
- Fedora/RHEL: `sudo dnf install -y libX11-devel`
- macOS: no extra deps needed.

- [ ] **Step 2: Write failing tests**

Append to `internal/ui/app_test.go`:

```go
// fakeClipboard returns a clipboardReader that returns canned bytes
// for FmtImage and FmtText.
func fakeClipboard(image, text []byte) func(clipboard.Format) []byte {
	return func(f clipboard.Format) []byte {
		switch f {
		case clipboard.FmtImage:
			return image
		case clipboard.FmtText:
			return text
		}
		return nil
	}
}

func TestSmartPaste_ImagePresent_AttachesToCompose(t *testing.T) {
	app := NewApp()
	app.SetClipboardAvailable(true)
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	pngBytes := []byte("\x89PNG\r\n\x1a\nfake")
	app.SetClipboardReader(fakeClipboard(pngBytes, nil))

	app.smartPaste()

	atts := app.compose.Attachments()
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(atts))
	}
	if atts[0].Mime != "image/png" {
		t.Errorf("expected image/png, got %q", atts[0].Mime)
	}
	if !strings.HasPrefix(atts[0].Filename, "slk-paste-") || !strings.HasSuffix(atts[0].Filename, ".png") {
		t.Errorf("unexpected filename: %q", atts[0].Filename)
	}
	if atts[0].Size != int64(len(pngBytes)) {
		t.Errorf("expected size %d, got %d", len(pngBytes), atts[0].Size)
	}
}

func TestSmartPaste_ImageTooLarge_Refuses(t *testing.T) {
	app := NewApp()
	app.SetClipboardAvailable(true)
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	huge := make([]byte, 11*1024*1024)
	app.SetClipboardReader(fakeClipboard(huge, nil))

	app.smartPaste()

	if len(app.compose.Attachments()) != 0 {
		t.Errorf("expected no attachment for oversized image, got %d", len(app.compose.Attachments()))
	}
}

func TestSmartPaste_FilePathPresent_AttachesByPath(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "doc.pdf")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.SetClipboardAvailable(true)
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	app.SetClipboardReader(fakeClipboard(nil, []byte(path)))

	app.smartPaste()

	atts := app.compose.Attachments()
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(atts))
	}
	if atts[0].Path != path {
		t.Errorf("expected Path=%q, got %q", path, atts[0].Path)
	}
	if atts[0].Filename != "doc.pdf" {
		t.Errorf("expected filename doc.pdf, got %q", atts[0].Filename)
	}
}

func TestSmartPaste_NoImage_NoValidPath_FallsThroughToText(t *testing.T) {
	app := NewApp()
	app.SetClipboardAvailable(true)
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	app.SetClipboardReader(fakeClipboard(nil, []byte("just some text")))

	app.smartPaste()

	if len(app.compose.Attachments()) != 0 {
		t.Errorf("expected no attachment, got %d", len(app.compose.Attachments()))
	}
	// Text was inserted into compose.
	if !strings.Contains(app.compose.Value(), "just some text") {
		t.Errorf("expected text to be inserted, got %q", app.compose.Value())
	}
}

func TestSmartPaste_ClipboardUnavailable_NoOp(t *testing.T) {
	app := NewApp()
	app.SetClipboardAvailable(false)
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	pngBytes := []byte("\x89PNGfake")
	app.SetClipboardReader(fakeClipboard(pngBytes, nil))

	app.smartPaste()

	if len(app.compose.Attachments()) != 0 {
		t.Error("expected no-op when clipboard unavailable")
	}
}

func TestSmartPaste_ThreadPane_AttachesToThreadCompose(t *testing.T) {
	app := NewApp()
	app.SetClipboardAvailable(true)
	app.activeChannelID = "C1"
	app.threadPanel.SetThread(messages.MessageItem{TS: "P1"}, nil, "C1", "P1")
	app.threadVisible = true
	app.focusedPanel = PanelThread
	app.SetMode(ModeInsert)
	app.SetClipboardReader(fakeClipboard([]byte("\x89PNG"), nil))

	app.smartPaste()

	if len(app.threadCompose.Attachments()) != 1 {
		t.Errorf("expected attachment on threadCompose, got %d", len(app.threadCompose.Attachments()))
	}
	if len(app.compose.Attachments()) != 0 {
		t.Errorf("expected no attachment on channel compose, got %d", len(app.compose.Attachments()))
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run TestSmartPaste -v`
Expected: FAIL — `smartPaste` undefined.

- [ ] **Step 4: Implement smartPaste**

Add to `internal/ui/app.go` near the other action handlers (e.g., next to `beginEditOfSelected` or `copyPermalinkOfSelected`):

```go
const maxAttachmentSize = 10 * 1024 * 1024 // 10 MB cap

// smartPaste inspects the OS clipboard and dispatches:
//  1. PNG image bytes → attach as image with auto-generated filename.
//  2. Single-line file-path text → attach by path.
//  3. Anything else → insert text into the active compose.
//
// Returns a tea.Cmd that emits the appropriate status-bar toast.
func (a *App) smartPaste() tea.Cmd {
	if !a.clipboardAvailable {
		return nil
	}

	// Resolve the active compose pointer.
	target := &a.compose
	if a.focusedPanel == PanelThread && a.threadVisible {
		target = &a.threadCompose
	}

	// 1. Image bytes.
	if imgBytes := a.clipboardRead(clipboard.FmtImage); len(imgBytes) > 0 {
		if int64(len(imgBytes)) > maxAttachmentSize {
			return a.uploadToastCmd(
				fmt.Sprintf("Image too large (%s > 10 MB limit)", humanSize(int64(len(imgBytes)))),
				3*time.Second,
			)
		}
		filename := "slk-paste-" + time.Now().Format("2006-01-02-15-04-05") + ".png"
		target.AddAttachment(compose.PendingAttachment{
			Filename: filename,
			Bytes:    imgBytes,
			Mime:     "image/png",
			Size:     int64(len(imgBytes)),
		})
		return a.uploadToastCmd(
			fmt.Sprintf("Attached: %s (%s)", filename, humanSize(int64(len(imgBytes)))),
			2*time.Second,
		)
	}

	// 2. File-path text.
	textBytes := a.clipboardRead(clipboard.FmtText)
	if path, ok := resolveFilePath(string(textBytes)); ok {
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() {
			if info.Size() > maxAttachmentSize {
				return a.uploadToastCmd("File too large (>10 MB limit)", 3*time.Second)
			}
			if info.Size() == 0 {
				return a.uploadToastCmd("Empty file", 2*time.Second)
			}
			filename := filepath.Base(path)
			target.AddAttachment(compose.PendingAttachment{
				Filename: filename,
				Path:     path,
				Mime:     mime.TypeByExtension(filepath.Ext(path)),
				Size:     info.Size(),
			})
			return a.uploadToastCmd(
				fmt.Sprintf("Attached: %s (%s)", filename, humanSize(info.Size())),
				2*time.Second,
			)
		}
	}

	// 3. Text fallback — paste verbatim into the active compose.
	if len(textBytes) > 0 {
		target.SetValue(target.Value() + string(textBytes))
	}
	return nil
}
```

- [ ] **Step 5: Wire Ctrl+V into handleInsertMode**

Find `handleInsertMode` (around line 1404). The current `code` and `mod` lines are around 1432:

```go
code := msg.Key().Code
mod := msg.Key().Mod
```

Right after those lines (and before the `isSend` / `isNewline` calculations), add:

```go
isPaste := code == 'v' && mod == tea.ModCtrl
if isPaste {
	return a.smartPaste()
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS, including the 6 new smartPaste tests and all existing tests.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/ui/app.go internal/ui/app_test.go
git commit -m "feat(app): implement Ctrl+V smart-paste (image / file-path / text)"
```

---

## Task 7: Implement submitWithAttachments and result handlers

**Files:**
- Modify: `internal/ui/app.go`
- Test: `internal/ui/app_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/ui/app_test.go`:

```go
func TestSubmitWithAttachments_EmitsUploadAttachmentsMsg(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.SetMode(ModeInsert)
	app.compose.AddAttachment(compose.PendingAttachment{
		Filename: "a.png", Bytes: []byte("png"), Size: 3,
	})
	app.compose.SetValue("look")

	cmd := app.submitWithAttachments(&app.compose)
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	// The cmd is a tea.Batch; collect messages until we find UploadAttachmentsMsg.
	// Easiest path: directly inspect that uploading is set on the compose.
	if !app.compose.Uploading() {
		t.Error("expected compose.Uploading() == true after submit")
	}
}

func TestSubmitWithAttachments_RefusesDuringEdit(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.compose.AddAttachment(compose.PendingAttachment{Filename: "a.png", Size: 1})
	app.editing.active = true
	app.editing.channelID = "C1"
	app.editing.ts = "1.0"
	app.editing.panel = PanelMessages

	_ = app.submitWithAttachments(&app.compose)

	if app.compose.Uploading() {
		t.Error("expected no upload kicked off during edit mode")
	}
	// Attachment should still be there for the user to remove.
	if len(app.compose.Attachments()) != 1 {
		t.Errorf("expected attachments preserved, got %d", len(app.compose.Attachments()))
	}
}

func TestUploadResultMsg_SuccessClearsAttachmentsAndCompose(t *testing.T) {
	app := NewApp()
	app.compose.AddAttachment(compose.PendingAttachment{Filename: "a.png", Size: 1})
	app.compose.SetValue("caption")
	app.compose.SetUploading(true)

	app.Update(UploadResultMsg{Err: nil})

	if app.compose.Uploading() {
		t.Error("expected Uploading=false after success")
	}
	if len(app.compose.Attachments()) != 0 {
		t.Errorf("expected attachments cleared, got %d", len(app.compose.Attachments()))
	}
	if app.compose.Value() != "" {
		t.Errorf("expected text reset, got %q", app.compose.Value())
	}
}

func TestUploadResultMsg_FailureKeepsAttachments(t *testing.T) {
	app := NewApp()
	app.compose.AddAttachment(compose.PendingAttachment{Filename: "a.png", Size: 1})
	app.compose.SetValue("caption")
	app.compose.SetUploading(true)

	app.Update(UploadResultMsg{Err: errors.New("network failure")})

	if app.compose.Uploading() {
		t.Error("expected Uploading=false after failure")
	}
	if len(app.compose.Attachments()) != 1 {
		t.Errorf("expected attachments preserved on failure, got %d", len(app.compose.Attachments()))
	}
	if app.compose.Value() != "caption" {
		t.Errorf("expected caption preserved, got %q", app.compose.Value())
	}
}
```

If `errors` isn't imported in `app_test.go`, add it.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestSubmitWithAttachments|TestUploadResultMsg' -v`
Expected: FAIL — `submitWithAttachments` undefined; result-msg arms not handled.

- [ ] **Step 3: Implement submitWithAttachments**

Add to `internal/ui/app.go` near `smartPaste`:

```go
// submitWithAttachments dispatches the pending attachments + caption
// to the configured uploader. Refused if an edit is in progress
// (chat.update doesn't support file attachments).
func (a *App) submitWithAttachments(c *compose.Model) tea.Cmd {
	if a.editing.active {
		return a.uploadToastCmd("Cannot attach files to an edit (send a new message)", 3*time.Second)
	}
	attachments := c.Attachments()
	if len(attachments) == 0 {
		return nil
	}
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
		return a.uploadToastCmd("Cannot upload: no active channel", 2*time.Second)
	}

	c.SetUploading(true)
	cmds := []tea.Cmd{
		a.uploader(channelID, threadTS, caption, attachments),
		a.uploadToastCmd(fmt.Sprintf("Uploading 0/%d…", len(attachments)), 30*time.Second),
	}
	return tea.Batch(cmds...)
}
```

- [ ] **Step 4: Add Update arms for the result messages**

In `internal/ui/app.go`'s `Update` switch, place these arms next to the other Upload* and edit/delete result handlers:

```go
	case UploadAttachmentsMsg:
		// Already dispatched via submitWithAttachments → uploader cmd.
		// This case exists only if some other code path emits it
		// directly; treat as a re-dispatch. For now, no-op.

	case UploadProgressMsg:
		a.statusbar.SetToast(fmt.Sprintf("Uploading %d/%d…", msg.Done, msg.Total))

	case UploadResultMsg:
		a.compose.SetUploading(false)
		a.threadCompose.SetUploading(false)
		if msg.Err != nil {
			cmds = append(cmds, a.uploadToastCmd(
				"Upload failed: "+truncateReason(msg.Err.Error(), 40),
				3*time.Second,
			))
			break
		}
		a.compose.ClearAttachments()
		a.threadCompose.ClearAttachments()
		a.compose.Reset()
		a.threadCompose.Reset()
		cmds = append(cmds, a.uploadToastCmd("Sent", 2*time.Second))
```

- [ ] **Step 5: Branch the existing isSend logic in handleInsertMode to route attachments through submitWithAttachments**

Find the existing thread-compose `isSend` block (around line 1455, modified by Task 14 of the edit/delete plan to add the `editing.active` check). Modify it to check attachments first:

```go
		if isSend {
			if len(a.threadCompose.Attachments()) > 0 {
				return a.submitWithAttachments(&a.threadCompose)
			}
			if a.editing.active && a.editing.panel == PanelThread {
				return a.submitEdit(a.threadCompose.Value(), a.threadCompose.TranslateMentionsForSend(a.threadCompose.Value()))
			}
			// ... existing send-thread-reply logic unchanged ...
		}
```

Find the existing channel-compose `isSend` block (around line 1491). Same modification:

```go
	if isSend {
		if len(a.compose.Attachments()) > 0 {
			return a.submitWithAttachments(&a.compose)
		}
		if a.editing.active && a.editing.panel == PanelMessages {
			return a.submitEdit(a.compose.Value(), a.compose.TranslateMentionsForSend(a.compose.Value()))
		}
		// ... existing send logic unchanged ...
	}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS for all tests including the 4 new ones.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "feat(app): implement send-with-attachments and upload result handling"
```

---

## Task 8: Esc/channel-switch/workspace-switch guards during upload

**Files:**
- Modify: `internal/ui/app.go`
- Test: `internal/ui/app_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/ui/app_test.go`:

```go
func TestEscDuringUpload_RefusedWithToast(t *testing.T) {
	app := NewApp()
	app.SetMode(ModeInsert)
	app.compose.SetUploading(true)
	app.focusedPanel = PanelMessages

	cmd := app.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected non-nil cmd (toast)")
	}
	// Mode should still be Insert (Esc was refused).
	if app.mode != ModeInsert {
		t.Errorf("expected ModeInsert preserved during upload, got %v", app.mode)
	}
}

func TestChannelSwitchDuringUpload_Refused(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.compose.SetUploading(true)

	app.Update(ChannelSelectedMsg{ID: "C2"})

	if app.activeChannelID != "C1" {
		t.Errorf("expected activeChannelID preserved during upload, got %q", app.activeChannelID)
	}
	if !app.compose.Uploading() {
		t.Error("expected upload still in flight")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestEscDuringUpload|TestChannelSwitchDuringUpload' -v`
Expected: FAIL.

- [ ] **Step 3: Add the guard at the top of handleInsertMode**

Find `handleInsertMode` (line 1404). Right after the existing edit-cancel-on-esc check (the block added by Task 14 of the edit/delete feature), add:

```go
	if (a.compose.Uploading() || a.threadCompose.Uploading()) && key.Matches(msg, a.keys.Escape) {
		return a.uploadToastCmd("Upload in progress", 2*time.Second)
	}
```

- [ ] **Step 4: Add the guard in ChannelSelectedMsg and WorkspaceSwitchedMsg**

Find `case ChannelSelectedMsg:` (around line 841). At the top of the case body, before the existing `a.cancelEdit()` call (added by edit/delete Task 14), add:

```go
		if a.compose.Uploading() || a.threadCompose.Uploading() {
			cmds = append(cmds, a.uploadToastCmd("Upload in progress", 2*time.Second))
			break
		}
```

Find `case WorkspaceSwitchedMsg:` (around line 1052). Same guard at the top:

```go
		if a.compose.Uploading() || a.threadCompose.Uploading() {
			cmds = append(cmds, a.uploadToastCmd("Upload in progress", 2*time.Second))
			break
		}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "feat(app): refuse Esc / channel switch / workspace switch during upload"
```

---

## Task 9: Wire clipboard.Init and SetUploader in cmd/slk/main.go

**Files:**
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Add clipboard.Init at startup**

Find a sensible place in `cmd/slk/main.go` near the start of the program — somewhere after the workspace setup but before `app.SetMessageSender(...)` (around line 349). Add:

```go
	clipboardOK := true
	if err := clipboard.Init(); err != nil {
		log.Printf("Warning: clipboard init failed (%v); Ctrl+V image paste disabled", err)
		clipboardOK = false
	}
	app.SetClipboardAvailable(clipboardOK)
```

Add the import `"golang.design/x/clipboard"` to `main.go`.

- [ ] **Step 2: Wire SetUploader**

Immediately after the existing `app.SetMessageEditor(...)` and `app.SetMessageDeleter(...)` blocks (added by edit/delete Task 12), add:

```go
	app.SetUploader(func(channelID, threadTS, caption string, attachments []compose.PendingAttachment) tea.Cmd {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

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

				if _, err := client.UploadFile(ctx, channelID, threadTS, att.Filename, reader, att.Size, currentCaption); err != nil {
					return ui.UploadResultMsg{Err: fmt.Errorf("uploading %s (%d/%d): %w", att.Filename, i+1, len(attachments), err)}
				}
			}
			program.Send(ui.UploadProgressMsg{Done: len(attachments), Total: len(attachments)})
			return ui.UploadResultMsg{Err: nil}
		}
	})
```

Imports to verify present in `main.go`: `bytes`, `context`, `fmt`, `io`, `os`, `time`, `log` (most are already there from existing wiring).

Also add the `compose` package import: `"github.com/gammons/slk/internal/ui/compose"` — needed for the `compose.PendingAttachment` type signature.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Run full test suite**

Run: `go test ./...`
Expected: all packages pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat(main): wire clipboard.Init and uploader callback"
```

---

## Task 10: Manual smoke test

**Files:** none modified

- [ ] **Step 1: Build and run**

```bash
go build -o bin/slk ./cmd/slk
./bin/slk
```

(Or `make build` if the project's Makefile is preferred.)

- [ ] **Step 2: Smoke checklist**

Test these flows in a real workspace:

1. **Image paste (channel):** Take a screenshot (Cmd+Shift+Ctrl+4 on macOS, Shift+PrintScreen on Linux), then in slk press `i` to enter insert mode, press Ctrl+V. Verify a chip appears with `slk-paste-...png` and a size; press Enter. Verify the file appears in Slack web within ~2s.

2. **Image paste (thread):** Open a thread with `Enter` on a message, focus the thread compose with `Tab` or `l`, press `i` then Ctrl+V with an image on clipboard. Verify the file lands in the thread, not the channel.

3. **File-path paste:** Copy a file path to clipboard (e.g., `cat ~/Downloads/test.pdf | clip` or copy from a file manager). Press `i`, Ctrl+V. Verify the chip shows the basename. Send.

4. **Text fallback:** Copy plain text. Press `i`, Ctrl+V. Verify text is inserted into the textarea (no chip, no upload).

5. **Multiple attachments:** Image-paste, then immediately file-path paste, then send. Verify both files arrive in Slack with the caption attached to the last one.

6. **Backspace remove chip:** Add an attachment, with empty textarea press Backspace. Verify chip disappears.

7. **Refusal — too large:** Try to paste an image > 10 MB. Verify toast `Image too large` and no chip appears.

8. **Refusal — non-existent path:** Copy a string that looks like a path but doesn't exist (`/tmp/does-not-exist`). Press Ctrl+V. Verify text is inserted as text (fallback) rather than refused.

9. **Edit + paste refusal:** Press `E` on an own message, Ctrl+V to attach an image, press Enter. Verify `Cannot attach files to an edit (send a new message)` toast.

10. **Esc during upload:** Paste a large-ish file, send. While upload is in flight, press Esc. Verify `Upload in progress` toast and Insert mode preserved.

11. **Headless Linux (optional):** If available, run with `DISPLAY=` unset on Linux. Verify startup logs `clipboard init failed` and Ctrl+V is a no-op.

If anything misbehaves, fix and re-run the suite + smoke tests.

- [ ] **Step 3: Commit any fixes**

```bash
git add -A
git commit -m "fix: <describe issue> from manual smoke"   # only if needed
```

---

## Task 11: Update README and STATUS docs

**Files:**
- Modify: `README.md`
- Modify: `docs/STATUS.md`

- [ ] **Step 1: Add Ctrl+V keybinding row to README**

In `README.md`'s keybinding table, locate the section near the message-pane keys (`E` / `D` rows added by the edit/delete feature). Add:

```
| `Ctrl+V` | Insert | Smart paste — image / file path / text |
```

- [ ] **Step 2: Add a Messaging feature line**

In the `### Messaging` features list (where edit/delete were added), append:

```
- Smart-paste image/file uploads — `Ctrl+V` in insert mode attaches a clipboard image, a copied file path, or falls through to text paste; multiple attachments + caption send together via Slack's V2 file-upload flow
```

- [ ] **Step 3: Add Linux build dependency note**

In the `### Build from source` section, after `Requires Go 1.22+.`, add:

```markdown
On Linux, the clipboard library used for `Ctrl+V` paste-to-upload
requires X11 development headers at build time:

- Debian/Ubuntu: `sudo apt-get install -y libx11-dev`
- Fedora/RHEL: `sudo dnf install -y libX11-devel`
- Arch: included in `xorg-server`

At runtime, an X11 display is required (XWayland is fine on
Wayland-only sessions and is the default on most Wayland desktops).
On headless Linux, slk runs but `Ctrl+V` smart-paste is disabled.
```

- [ ] **Step 4: Update docs/STATUS.md**

In the Messaging section (where edit/delete were marked done), append:

```
- [x] Paste-to-upload via `Ctrl+V` (image, file path, or text fallback) using Slack's V2 file-upload API
```

In `## Not Yet Implemented`, find the line about file uploads:
```
- [ ] File uploads and downloads
```
and split it (uploads via paste are now done; downloads still pending):
```
- [ ] File downloads (browser-style "save attachment" command; uploads via Ctrl+V paste are implemented)
```

- [ ] **Step 5: Commit**

```bash
git add README.md docs/STATUS.md
git commit -m "docs: document Ctrl+V smart-paste and Linux build deps"
```

---

## Self-Review Checklist (run before declaring done)

- [ ] Spec coverage: every spec section maps to at least one task. Task 1 = Slack client. Tasks 2–4 = compose state + chip render + backspace. Task 5 = app.go declarations. Task 6 = smartPaste + Ctrl+V binding. Task 7 = submitWithAttachments + result handlers + isSend branching. Task 8 = upload guards. Task 9 = wiring in main.go. Task 10 = manual smoke. Task 11 = docs.
- [ ] No placeholders: every step contains the actual code or command needed. No "TBD" / "implement later" / "add error handling" without showing how.
- [ ] Type consistency: `PendingAttachment` (compose), `UploadAttachmentsMsg` / `UploadProgressMsg` / `UploadResultMsg` / `UploadFunc` (app). Method names: `AddAttachment`, `RemoveLastAttachment`, `Attachments`, `ClearAttachments`, `SetUploading`, `Uploading`. Helper names: `humanSize`, `resolveFilePath`, `uploadToastCmd`, `smartPaste`, `submitWithAttachments`. Constant names: `maxAttachmentSize`. All consistent across tasks.
- [ ] Linux dep note appears in both README and the spec.
- [ ] Test coverage: smartPaste (6 cases), submit (2 cases), result-msg (2 cases), upload guards (2 cases), compose data layer (6 cases), compose chip render (3 cases), compose backspace (4 cases), Slack client wrapper (3 cases). Total ~28 new tests.
- [ ] Edit-mode interaction is explicit: `submitWithAttachments` refuses with toast.
- [ ] Multi-file caption-on-last semantics is documented in both the wrapper comment and the uploader closure.
