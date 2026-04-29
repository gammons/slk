# Edit & Delete Own Messages — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `E` (edit) and `D` (delete) keybindings that let users edit and delete their own messages in both the channel message pane and the thread reply pane, with edits/deletes from any client reflected live.

**Architecture:** Edit reuses the existing channel/thread `compose.Model` (seeded via `SetValue`, with a placeholder override and a draft stash/restore). Delete uses a new minimal `confirmprompt` overlay package that mirrors the reaction picker's shape (Open/Close/IsVisible/HandleKey/ViewOverlay + a new `ModeConfirm`). API calls go through the already-implemented `client.EditMessage` and `client.RemoveMessage`. The local UI is updated only by the WebSocket echo (`message_changed` → `IsEdited` field; `message_deleted` → new `WSMessageDeletedMsg`). Two pre-existing gaps are fixed as part of this work: the `NewMessageMsg` handler currently appends instead of updating in place when `IsEdited == true`, and `OnMessageDeleted` is a TODO.

**Tech Stack:** Go 1.22+, charm.land/bubbletea/v2, charm.land/bubbles/v2, charm.land/lipgloss/v2, slack-go/slack (already wrapped in `internal/slack`), SQLite via `internal/cache`.

---

## File Structure

**New files:**
- `internal/ui/confirmprompt/model.go` — generic yes/no overlay (Open, Close, IsVisible, HandleKey, View, ViewOverlay, RefreshStyles).
- `internal/ui/confirmprompt/model_test.go` — unit tests for the prompt.

**Modified files:**
- `internal/ui/keys.go` — change `Edit` binding from `"e"` to `"E"`; add `Delete` binding `"D"`.
- `internal/ui/mode.go` — add `ModeConfirm` enum entry + `String()` arm.
- `internal/ui/app.go` — new msg types, setters, `editing` state, dispatchers, handlers, view composition, NewMessageMsg edit-branch, WSMessageDeletedMsg handler.
- `internal/ui/messages/model.go` — `UpdateMessageInPlace`, `RemoveMessageByTS`.
- `internal/ui/messages/model_test.go` — tests for the two new methods.
- `internal/ui/thread/model.go` — `UpdateMessageInPlace`, `RemoveMessageByTS`, `UpdateParentInPlace`.
- `internal/ui/thread/model_test.go` — tests for the three new methods.
- `internal/ui/compose/model.go` — `SetPlaceholderOverride(string)` (empty clears the override).
- `cmd/slk/main.go` — wire `SetMessageEditor`, `SetMessageDeleter`; replace `OnMessageDeleted` TODO body.
- `README.md` — keybinding table: add `E` and `D`; remove edit/delete from roadmap.
- `docs/STATUS.md` — mark edit and delete implemented.

---

## Sequencing Rationale

We build bottom-up so each step is independently testable:
1. Model-level mutation methods (with tests).
2. Slack client wiring is already present — we only add tea.Msg plumbing and main.go setters.
3. Confirmation overlay package + ModeConfirm.
4. Wire the WS echo branch (this is invisible until edit/delete fire, but tests it independently).
5. Edit feature end-to-end.
6. Delete feature end-to-end.
7. Docs.

---

## Task 1: Add `UpdateMessageInPlace` and `RemoveMessageByTS` to `messages.Model`

**Files:**
- Modify: `internal/ui/messages/model.go` (add methods near line 421, next to `IncrementReplyCount`)
- Test: `internal/ui/messages/model_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/ui/messages/model_test.go`:

```go
func TestUpdateMessageInPlace_Found(t *testing.T) {
	msgs := []MessageItem{
		{TS: "1.0", UserName: "alice", Text: "old"},
		{TS: "2.0", UserName: "bob", Text: "hello"},
	}
	m := New(msgs, "general")
	got := m.UpdateMessageInPlace("2.0", "hello edited", true)
	if !got {
		t.Fatalf("expected UpdateMessageInPlace to return true for existing TS")
	}
	all := m.Messages()
	if all[1].Text != "hello edited" {
		t.Errorf("text not updated: %q", all[1].Text)
	}
	if !all[1].IsEdited {
		t.Error("IsEdited not set")
	}
	if all[0].Text != "old" {
		t.Error("other messages should be untouched")
	}
}

func TestUpdateMessageInPlace_NotFound(t *testing.T) {
	m := New([]MessageItem{{TS: "1.0", Text: "a"}}, "general")
	got := m.UpdateMessageInPlace("does-not-exist", "x", true)
	if got {
		t.Error("expected false when TS missing")
	}
}

func TestRemoveMessageByTS_Middle(t *testing.T) {
	m := New([]MessageItem{
		{TS: "1.0", Text: "a"},
		{TS: "2.0", Text: "b"},
		{TS: "3.0", Text: "c"},
	}, "general")
	// Selection starts at bottom (index 2 = "c").
	got := m.RemoveMessageByTS("2.0")
	if !got {
		t.Fatal("expected true")
	}
	all := m.Messages()
	if len(all) != 2 || all[0].TS != "1.0" || all[1].TS != "3.0" {
		t.Errorf("unexpected messages after remove: %+v", all)
	}
	// Removed index 1 was <= selected (2) → selected decrements to 1.
	if m.SelectedIndex() != 1 {
		t.Errorf("expected selected=1 after removing earlier message, got %d", m.SelectedIndex())
	}
}

func TestRemoveMessageByTS_AfterSelected(t *testing.T) {
	m := New([]MessageItem{
		{TS: "1.0", Text: "a"},
		{TS: "2.0", Text: "b"},
		{TS: "3.0", Text: "c"},
	}, "general")
	// Selection starts at index 2; remove TS "3.0" (the selected one).
	got := m.RemoveMessageByTS("3.0")
	if !got {
		t.Fatal("expected true")
	}
	if m.SelectedIndex() != 1 {
		t.Errorf("expected selected clamped to 1, got %d", m.SelectedIndex())
	}
}

func TestRemoveMessageByTS_NotFound(t *testing.T) {
	m := New([]MessageItem{{TS: "1.0", Text: "a"}}, "general")
	if m.RemoveMessageByTS("nope") {
		t.Error("expected false when TS missing")
	}
	if len(m.Messages()) != 1 {
		t.Error("messages should be unchanged when TS missing")
	}
}

func TestRemoveMessageByTS_LastBecomesEmpty(t *testing.T) {
	m := New([]MessageItem{{TS: "1.0", Text: "a"}}, "general")
	if !m.RemoveMessageByTS("1.0") {
		t.Fatal("expected true")
	}
	if len(m.Messages()) != 0 {
		t.Error("expected empty after removing last")
	}
	if _, ok := m.SelectedMessage(); ok {
		t.Error("SelectedMessage should be (_, false) when empty")
	}
}
```

If `Messages()` doesn't exist yet on the model, search `internal/ui/messages/model.go` for an accessor; if there's no public getter use `m.SelectedMessage()` and direct field access via package-internal tests (they're in the same package so it's fine). Replace `m.Messages()` with `m.messages` if needed.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/messages/ -run 'TestUpdateMessageInPlace|TestRemoveMessageByTS' -v`
Expected: FAIL with "undefined: UpdateMessageInPlace" and "undefined: RemoveMessageByTS".

- [ ] **Step 3: Implement the two methods**

Add to `internal/ui/messages/model.go`, immediately after `IncrementReplyCount` (around line 431):

```go
// UpdateMessageInPlace finds a message by TS, replaces its text, and
// optionally marks it as edited. Returns true if the message was found.
// Invalidates the render cache.
func (m *Model) UpdateMessageInPlace(ts, newText string, isEdited bool) bool {
	for i, msg := range m.messages {
		if msg.TS == ts {
			m.messages[i].Text = newText
			if isEdited {
				m.messages[i].IsEdited = true
			}
			m.cache = nil
			m.dirty()
			return true
		}
	}
	return false
}

// RemoveMessageByTS removes a message with the given TS, adjusting the
// selected index so it remains valid. Returns true if the message was
// found and removed. Invalidates the render cache.
func (m *Model) RemoveMessageByTS(ts string) bool {
	for i, msg := range m.messages {
		if msg.TS == ts {
			m.messages = append(m.messages[:i], m.messages[i+1:]...)
			if i <= m.selected && m.selected > 0 {
				m.selected--
			}
			if m.selected >= len(m.messages) {
				if len(m.messages) == 0 {
					m.selected = 0
				} else {
					m.selected = len(m.messages) - 1
				}
			}
			m.cache = nil
			m.dirty()
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/messages/ -v`
Expected: PASS (all messages tests, including the new ones).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/messages/model.go internal/ui/messages/model_test.go
git commit -m "feat(messages): add UpdateMessageInPlace and RemoveMessageByTS"
```

---

## Task 2: Add `UpdateMessageInPlace`, `RemoveMessageByTS`, `UpdateParentInPlace` to `thread.Model`

**Files:**
- Modify: `internal/ui/thread/model.go` (near `MoveDown` / parent accessors)
- Test: `internal/ui/thread/model_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/ui/thread/model_test.go`. Inspect the file first to confirm the existing constructor pattern and parent-message helper. Use this template, adjusting names if needed:

```go
package thread

import (
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
)

func TestThread_UpdateMessageInPlace(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "P1", Text: "parent"}
	replies := []messages.MessageItem{
		{TS: "R1", Text: "old reply"},
		{TS: "R2", Text: "other"},
	}
	m.SetThread(parent, replies, "C1", "P1")

	if !m.UpdateMessageInPlace("R1", "new reply", true) {
		t.Fatal("expected true updating R1")
	}
	got := m.SelectedReply()
	// SelectedReply may not be R1 — fetch by index instead via the public API.
	// Check via iteration of m.Replies() if such a getter exists; otherwise
	// access through the package-internal field m.replies.
	_ = got
	if m.replies[0].Text != "new reply" || !m.replies[0].IsEdited {
		t.Errorf("R1 not updated: %+v", m.replies[0])
	}
	if m.UpdateMessageInPlace("nope", "x", false) {
		t.Error("expected false for missing TS")
	}
}

func TestThread_RemoveMessageByTS(t *testing.T) {
	m := New()
	replies := []messages.MessageItem{
		{TS: "R1", Text: "a"},
		{TS: "R2", Text: "b"},
		{TS: "R3", Text: "c"},
	}
	m.SetThread(messages.MessageItem{TS: "P1"}, replies, "C1", "P1")
	if !m.RemoveMessageByTS("R2") {
		t.Fatal("expected true")
	}
	if len(m.replies) != 2 || m.replies[0].TS != "R1" || m.replies[1].TS != "R3" {
		t.Errorf("unexpected replies: %+v", m.replies)
	}
	if m.RemoveMessageByTS("nope") {
		t.Error("expected false for missing TS")
	}
}

func TestThread_UpdateParentInPlace(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "P1", Text: "parent original"}
	m.SetThread(parent, nil, "C1", "P1")
	if !m.UpdateParentInPlace("P1", "parent edited") {
		t.Fatal("expected true")
	}
	if m.ParentMsg().Text != "parent edited" || !m.ParentMsg().IsEdited {
		t.Errorf("parent not updated: %+v", m.ParentMsg())
	}
	if m.UpdateParentInPlace("OTHER", "x") {
		t.Error("expected false when TS does not match parent")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/thread/ -run 'TestThread_(Update|Remove)' -v`
Expected: FAIL.

- [ ] **Step 3: Implement the methods**

Add to `internal/ui/thread/model.go` after the existing `ParentMsg()` method:

```go
// UpdateMessageInPlace finds a reply by TS and replaces its text,
// optionally marking it edited. Returns true if found.
func (m *Model) UpdateMessageInPlace(ts, newText string, isEdited bool) bool {
	for i, r := range m.replies {
		if r.TS == ts {
			m.replies[i].Text = newText
			if isEdited {
				m.replies[i].IsEdited = true
			}
			m.InvalidateCache()
			return true
		}
	}
	return false
}

// RemoveMessageByTS removes a reply by TS, adjusting selection. Returns
// true if found.
func (m *Model) RemoveMessageByTS(ts string) bool {
	for i, r := range m.replies {
		if r.TS == ts {
			m.replies = append(m.replies[:i], m.replies[i+1:]...)
			if i <= m.selected && m.selected > 0 {
				m.selected--
			}
			if m.selected >= len(m.replies) {
				if len(m.replies) == 0 {
					m.selected = 0
				} else {
					m.selected = len(m.replies) - 1
				}
			}
			m.InvalidateCache()
			return true
		}
	}
	return false
}

// UpdateParentInPlace updates the thread parent's text if its TS matches.
// Returns true if updated.
func (m *Model) UpdateParentInPlace(ts, newText string) bool {
	if m.parent.TS != ts {
		return false
	}
	m.parent.Text = newText
	m.parent.IsEdited = true
	m.InvalidateCache()
	return true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/thread/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/thread/model.go internal/ui/thread/model_test.go
git commit -m "feat(thread): add UpdateMessageInPlace, RemoveMessageByTS, UpdateParentInPlace"
```

---

## Task 3: Add `SetPlaceholderOverride` to `compose.Model`

**Files:**
- Modify: `internal/ui/compose/model.go`

The compose box needs a placeholder override so the edit experience can show "Editing message — Enter to save, Esc to cancel" while edit mode is active, without the existing channel/thread placeholder leaking through on `Blur()`.

- [ ] **Step 1: Write failing test**

Create or append to `internal/ui/compose/model_test.go`:

```go
package compose

import (
	"strings"
	"testing"
)

func TestSetPlaceholderOverride(t *testing.T) {
	m := New("general")
	m.SetPlaceholderOverride("Editing message")
	m.Blur() // Blur normally restores the default placeholder
	got := m.View()
	if !strings.Contains(got, "Editing message") {
		t.Errorf("expected override placeholder in view, got: %q", got)
	}
	if strings.Contains(got, "Message #general") {
		t.Errorf("default placeholder should not appear while override active: %q", got)
	}

	m.SetPlaceholderOverride("")
	m.Blur()
	got2 := m.View()
	if !strings.Contains(got2, "Message #general") {
		t.Errorf("expected default placeholder after clearing override, got: %q", got2)
	}
}
```

If a `compose/model_test.go` doesn't exist, this is the new file. If `m.View()` requires arguments check the existing signature and adapt.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/compose/ -run TestSetPlaceholderOverride -v`
Expected: FAIL with "undefined: SetPlaceholderOverride" or "View" signature error.

- [ ] **Step 3: Implement override**

In `internal/ui/compose/model.go`:

(a) Add a field to the `Model` struct (near the bottom of the existing fields, before `version int64`):

```go
	// placeholderOverride, when non-empty, replaces the default
	// "Message #channel..." placeholder. Used by edit mode to display
	// "Editing message — Enter to save, Esc to cancel".
	placeholderOverride string
```

(b) Add the setter (place it near `SetChannel`, ~line 103):

```go
// SetPlaceholderOverride sets a custom placeholder string. Pass "" to
// clear the override and restore the default channel-aware placeholder.
func (m *Model) SetPlaceholderOverride(text string) {
	if m.placeholderOverride == text {
		return
	}
	m.placeholderOverride = text
	if text != "" {
		m.input.Placeholder = text
	} else {
		m.input.Placeholder = "Message #" + m.channelName + "... (i to insert)"
	}
	m.dirty()
}
```

(c) Update `SetChannel` (line 103) and `Blur` (line 117) so they respect the override:

```go
func (m *Model) SetChannel(name string) {
	if m.channelName != name {
		m.channelName = name
		if m.placeholderOverride == "" {
			m.input.Placeholder = "Message #" + name + "... (i to insert)"
		}
		m.dirty()
	}
}

func (m *Model) Blur() {
	if m.placeholderOverride != "" {
		m.input.Placeholder = m.placeholderOverride
	} else {
		m.input.Placeholder = "Message #" + m.channelName + "... (i to insert)"
	}
	m.input.Blur()
	m.dirty()
}
```

(d) Update `Focus` (line 111) — leave as-is; it already sets placeholder to "" while focused. That's fine; the override only matters when blurred or empty-and-unfocused.

(e) The override is preserved through `Reset()` deliberately (so submit→reset doesn't drop it mid-flight). But edit mode is responsible for clearing it after submit/cancel.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/compose/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/compose/
git commit -m "feat(compose): add SetPlaceholderOverride for edit mode"
```

---

## Task 4: Create `confirmprompt` package

**Files:**
- Create: `internal/ui/confirmprompt/model.go`
- Create: `internal/ui/confirmprompt/model_test.go`

Generic centered yes/no overlay, reusable for any future confirmation. Mirrors the reaction picker's interface so it composites the same way.

- [ ] **Step 1: Write failing tests**

Create `internal/ui/confirmprompt/model_test.go`:

```go
package confirmprompt

import (
	"strings"
	"testing"
)

type sentinelMsg struct{}

func TestOpenAndClose(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Error("should not be visible before Open")
	}
	m.Open("Delete?", "preview text", func() interface{} { return sentinelMsg{} })
	if !m.IsVisible() {
		t.Error("should be visible after Open")
	}
	m.Close()
	if m.IsVisible() {
		t.Error("should not be visible after Close")
	}
}

func TestHandleKey_ConfirmReturnsCmd(t *testing.T) {
	m := New()
	called := false
	m.Open("Delete?", "preview", func() interface{} {
		called = true
		return sentinelMsg{}
	})

	for _, k := range []string{"y", "enter"} {
		called = false
		m.Open("Delete?", "preview", func() interface{} {
			called = true
			return sentinelMsg{}
		})
		res := m.HandleKey(k)
		if !res.Confirmed {
			t.Errorf("key %q: expected Confirmed=true", k)
		}
		if res.Cmd == nil {
			t.Fatalf("key %q: expected non-nil Cmd", k)
		}
		_ = res.Cmd()
		if !called {
			t.Errorf("key %q: expected onConfirm to be called", k)
		}
		if m.IsVisible() {
			t.Errorf("key %q: prompt should be closed after confirm", k)
		}
	}
}

func TestHandleKey_CancelKeys(t *testing.T) {
	for _, k := range []string{"n", "esc", "escape", "x"} {
		m := New()
		m.Open("Delete?", "preview", func() interface{} { return sentinelMsg{} })
		res := m.HandleKey(k)
		if res.Confirmed {
			t.Errorf("key %q: should not confirm", k)
		}
		if !res.Cancelled {
			t.Errorf("key %q: expected Cancelled=true", k)
		}
		if m.IsVisible() {
			t.Errorf("key %q: prompt should be closed after cancel", k)
		}
	}
}

func TestView_ContainsTitleAndBody(t *testing.T) {
	m := New()
	m.Open("Delete message?", "hello world", func() interface{} { return nil })
	out := m.View(80)
	if !strings.Contains(out, "Delete message?") {
		t.Errorf("expected title in view: %q", out)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected body in view: %q", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/confirmprompt/ -v`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement the package**

Create `internal/ui/confirmprompt/model.go`:

```go
// Package confirmprompt provides a small centered yes/no confirmation
// overlay used for destructive actions (e.g. deleting a message).
//
// The shape mirrors reactionpicker.Model: Open / Close / IsVisible /
// HandleKey / View / ViewOverlay so the App can composite it with the
// same overlay.DimmedOverlay path.
package confirmprompt

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/muesli/reflow/truncate"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
)

// Result is returned by HandleKey to describe the outcome of a single
// key press.
type Result struct {
	// Confirmed is true if the user pressed y/Enter.
	Confirmed bool
	// Cancelled is true if the user pressed n/Esc/any other key.
	Cancelled bool
	// Cmd, when non-nil, is the tea.Cmd produced by the registered
	// onConfirm callback. The caller should append it to its returned
	// command list.
	Cmd tea.Cmd
}

// Model is the confirmation overlay.
type Model struct {
	visible   bool
	title     string
	body      string
	onConfirm func() tea.Msg
}

// New creates an empty, hidden prompt.
func New() *Model {
	return &Model{}
}

// Open shows the prompt with the given title (e.g. "Delete message?")
// and body (typically a short preview of the affected content).
// onConfirm is invoked as a tea.Cmd when the user confirms.
func (m *Model) Open(title, body string, onConfirm func() tea.Msg) {
	m.title = title
	m.body = body
	m.onConfirm = onConfirm
	m.visible = true
}

// Close hides the prompt and clears state.
func (m *Model) Close() {
	m.visible = false
	m.title = ""
	m.body = ""
	m.onConfirm = nil
}

// IsVisible returns whether the prompt is showing.
func (m *Model) IsVisible() bool { return m.visible }

// HandleKey processes a single key event. y/Enter confirm; n/Esc and
// any other key cancel. The caller should restore the previous Mode
// after this returns.
func (m *Model) HandleKey(keyStr string) Result {
	switch keyStr {
	case "y", "Y", "enter":
		var cmd tea.Cmd
		if m.onConfirm != nil {
			fn := m.onConfirm
			cmd = func() tea.Msg { return fn() }
		}
		m.Close()
		return Result{Confirmed: true, Cmd: cmd}
	default:
		// "n", "N", "esc", "escape", or anything else cancels.
		m.Close()
		return Result{Cancelled: true}
	}
}

// View renders the box content.
func (m *Model) View(termWidth int) string {
	return m.renderBox(termWidth)
}

// ViewOverlay composites the prompt over the given background.
func (m *Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}
	box := m.renderBox(termWidth)
	if box == "" {
		return background
	}
	result := overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
	lines := strings.Split(result, "\n")
	if len(lines) > termHeight {
		lines = lines[:termHeight]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderBox(termWidth int) string {
	if !m.visible {
		return ""
	}

	overlayWidth := termWidth * 35 / 100
	if overlayWidth < 40 {
		overlayWidth = 40
	}
	if overlayWidth > 60 {
		overlayWidth = 60
	}
	innerWidth := overlayWidth - 4 // border + padding

	bg := styles.Background

	title := lipgloss.NewStyle().
		Background(bg).
		Foreground(styles.Primary).
		Bold(true).
		Render(m.title)

	bodyText := m.body
	if lipgloss.Width(bodyText) > innerWidth {
		bodyText = truncate.StringWithTail(bodyText, uint(innerWidth), "…")
	}
	body := lipgloss.NewStyle().
		Background(bg).
		Foreground(styles.TextPrimary).
		Render("> " + bodyText)

	footer := lipgloss.NewStyle().
		Background(bg).
		Foreground(styles.TextMuted).
		Render("[y] confirm   [n/Esc] cancel")

	content := title + "\n\n" + body + "\n\n" + footer
	content = messages.ReapplyBgAfterResets(content, messages.BgANSI()+messages.FgANSI())

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		Background(bg).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)
}
```

The test signatures use `func() interface{}` to keep the test file independent of `tea.Msg`. Adjust the test to use `tea.Msg` if you prefer:

```go
// Replace `func() interface{}` with `func() tea.Msg` and import tea.
```

If that's cleaner, do it now — change both the test file and the matching parameter type in `Open`. Either is fine; the test file as written using `interface{}` will not compile with `func() tea.Msg`. **Pick one.** Recommended: change the test to use `tea.Msg`:

```go
import (
    tea "charm.land/bubbletea/v2"
)

// then sentinelMsg can stay; replace `func() interface{} { return sentinelMsg{} }`
// with `func() tea.Msg { return sentinelMsg{} }`
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/confirmprompt/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/confirmprompt/
git commit -m "feat(confirmprompt): add reusable yes/no overlay"
```

---

## Task 5: Add `ModeConfirm` to mode enum

**Files:**
- Modify: `internal/ui/mode.go`

- [ ] **Step 1: Add the enum entry**

Edit `internal/ui/mode.go`:

```go
const (
	ModeNormal Mode = iota
	ModeInsert
	ModeCommand
	ModeSearch
	ModeChannelFinder
	ModeReactionPicker
	ModeWorkspaceFinder
	ModeThemeSwitcher
	ModeConfirm
)
```

And add to `String()`:

```go
case ModeConfirm:
    return "CONFIRM"
```

- [ ] **Step 2: Build to verify**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/mode.go
git commit -m "feat(ui): add ModeConfirm enum entry"
```

---

## Task 6: Update keymap — bind `E` and add `D`

**Files:**
- Modify: `internal/ui/keys.go`

- [ ] **Step 1: Update bindings**

Edit `internal/ui/keys.go`:

(a) In the `KeyMap` struct, add a `Delete` field after `Edit`:

```go
	Edit                key.Binding
	Delete              key.Binding
	CopyPermalink       key.Binding
```

(b) In `DefaultKeyMap()`, change `Edit` and add `Delete`:

```go
	Edit:                key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "edit message")),
	Delete:              key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete message")),
	CopyPermalink:       key.NewBinding(key.WithKeys("Y", "C"), key.WithHelp("Y/C", "copy permalink")),
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/keys.go
git commit -m "feat(keys): bind E to edit, add D for delete"
```

---

## Task 7: Add tea.Msg types and setters in `app.go`

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add type declarations**

Insert after `MessageSentMsg` (around line 288) in `internal/ui/app.go`:

```go
// EditMessageMsg is emitted when the user submits an edit. App.Update
// invokes the configured messageEditor and converts the result to
// MessageEditedMsg.
type EditMessageMsg struct {
	ChannelID string
	TS        string
	NewText   string
}

// DeleteMessageMsg is emitted when the user confirms a delete.
type DeleteMessageMsg struct {
	ChannelID string
	TS        string
}

// MessageEditedMsg carries the result of the edit API call.
type MessageEditedMsg struct {
	ChannelID string
	TS        string
	Err       error
}

// MessageDeletedMsg carries the result of the delete API call.
type MessageDeletedMsg struct {
	ChannelID string
	TS        string
	Err       error
}

// WSMessageDeletedMsg is dispatched by the RTM event handler when a
// message_deleted event arrives. App.Update handles it by removing the
// message from both panes and the cache.
type WSMessageDeletedMsg struct {
	ChannelID string
	TS        string
}

// MessageEditFunc performs the chat.update API call. Returns a tea.Msg
// (typically MessageEditedMsg) describing the result.
type MessageEditFunc func(channelID, ts, newText string) tea.Msg

// MessageDeleteFunc performs the chat.delete API call. Returns a tea.Msg
// (typically MessageDeletedMsg) describing the result.
type MessageDeleteFunc func(channelID, ts string) tea.Msg
```

(b) Add fields to the `App` struct (near `messageSender` at line 385):

```go
	messageSender        MessageSendFunc
	messageEditor        MessageEditFunc
	messageDeleter       MessageDeleteFunc
```

(c) Add setters near `SetMessageSender` (line 2243):

```go
// SetMessageEditor wires the chat.update callback used by edit submit.
func (a *App) SetMessageEditor(fn MessageEditFunc) {
	a.messageEditor = fn
}

// SetMessageDeleter wires the chat.delete callback used by delete confirm.
func (a *App) SetMessageDeleter(fn MessageDeleteFunc) {
	a.messageDeleter = fn
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success (the new types are unused for now; that's fine).

- [ ] **Step 3: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat(app): add edit/delete tea.Msg types and setters"
```

---

## Task 8: Wire `confirmPrompt` into App + add view composition

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add the import and field**

Add to imports in `internal/ui/app.go`:

```go
"github.com/gammons/slk/internal/ui/confirmprompt"
```

Add to `App` struct (near `reactionPicker` field at line 410):

```go
	confirmPrompt    *confirmprompt.Model
```

- [ ] **Step 2: Initialize in NewApp**

Find `NewApp()` (around line 447) and add to the struct literal initialization (where `reactionPicker` is initialized — search the file for `reactionpicker.New()`):

```go
		reactionPicker:  reactionpicker.New(),
		confirmPrompt:   confirmprompt.New(),
```

- [ ] **Step 3: Add view composition**

In the View function, find the existing block that overlays the reaction picker (around line 2773):

```go
	if a.reactionPicker.IsVisible() {
		screen = a.reactionPicker.ViewOverlay(a.width, a.height, screen)
	}
```

Add immediately after:

```go
	if a.confirmPrompt.IsVisible() {
		screen = a.confirmPrompt.ViewOverlay(a.width, a.height, screen)
	}
```

And update `overlayActive` (around line 2798) to include it:

```go
	overlayActive := a.channelFinder.IsVisible() ||
		a.reactionPicker.IsVisible() ||
		a.confirmPrompt.IsVisible() ||
		a.workspaceFinder.IsVisible() ||
		a.themeSwitcher.IsVisible() ||
		a.loading
```

- [ ] **Step 4: Add the mode dispatcher**

Find the `Update` mode switch (around line 1133) and add a `ModeConfirm` arm:

```go
	case ModeReactionPicker:
		return a.handleReactionPickerMode(msg)
	case ModeConfirm:
		return a.handleConfirmMode(msg)
```

Add the handler function (place it after `handleReactionPickerMode`, around line 1572):

```go
func (a *App) handleConfirmMode(msg tea.KeyMsg) tea.Cmd {
	keyStr := msg.String()
	switch msg.Key().Code {
	case tea.KeyEscape:
		keyStr = "esc"
	case tea.KeyEnter:
		keyStr = "enter"
	}

	res := a.confirmPrompt.HandleKey(keyStr)
	if !a.confirmPrompt.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return res.Cmd
}
```

- [ ] **Step 5: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat(app): wire confirmPrompt overlay and ModeConfirm dispatcher"
```

---

## Task 9: Wire WS edit echo (in-place update)

**Files:**
- Modify: `internal/ui/app.go`

The current `NewMessageMsg` handler appends every message regardless of `IsEdited`. We need to branch on `IsEdited`.

- [ ] **Step 1: Write a failing test**

Create or append to `internal/ui/app_test.go`. Look at the existing test file first for setup helpers; reuse them. A minimal test:

```go
func TestNewMessageMsg_EditedUpdatesInPlace(t *testing.T) {
	app := NewApp()
	// Seed an active channel with a message.
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", Text: "original"},
	})

	app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS:       "1.0",
			UserName: "alice",
			Text:     "edited",
			IsEdited: true,
		},
	})

	msgs := app.messagepane.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after edit, got %d", len(msgs))
	}
	if msgs[0].Text != "edited" {
		t.Errorf("expected text 'edited', got %q", msgs[0].Text)
	}
	if !msgs[0].IsEdited {
		t.Error("expected IsEdited=true")
	}
}
```

If `Messages()` doesn't exist on the pane (it might be an internal field), look for an alternative accessor or use selection-based assertions: select the message, call `SelectedMessage()` and assert its text. Adjust accordingly.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestNewMessageMsg_EditedUpdatesInPlace -v`
Expected: FAIL (the existing handler appends, leaving 2 messages).

- [ ] **Step 3: Implement the branch**

In `internal/ui/app.go`, find the `case NewMessageMsg:` handler (around line 810) and modify:

```go
	case NewMessageMsg:
		if msg.Message.IsEdited {
			// Edit echo: update existing message in place rather than appending.
			a.messagepane.UpdateMessageInPlace(msg.Message.TS, msg.Message.Text, true)
			a.threadPanel.UpdateMessageInPlace(msg.Message.TS, msg.Message.Text, true)
			a.threadPanel.UpdateParentInPlace(msg.Message.TS, msg.Message.Text)
			break
		}
		if msg.ChannelID == a.activeChannelID {
			// (existing append logic, unchanged from current implementation)
			if a.threadVisible && msg.Message.ThreadTS == a.threadPanel.ThreadTS() {
				a.threadPanel.AddReply(msg.Message)
			}
			if msg.Message.ThreadTS == "" || msg.Message.ThreadTS == msg.Message.TS {
				a.messagepane.AppendMessage(msg.Message)
			}
			if msg.Message.ThreadTS != "" && msg.Message.ThreadTS != msg.Message.TS {
				a.messagepane.IncrementReplyCount(msg.Message.ThreadTS)
			}
		}
		if msg.Message.ThreadTS != "" {
			if c := a.scheduleThreadsDirty(); c != nil {
				cmds = append(cmds, c)
			}
		}
```

The key change: `if msg.Message.IsEdited { ... break }` short-circuits before the append path.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS, including all existing tests.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "fix(app): update message in place on edit echo instead of appending"
```

---

## Task 10: Add `WSMessageDeletedMsg` handler

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Write failing test**

Append to `internal/ui/app_test.go`:

```go
func TestWSMessageDeletedMsg_RemovesFromBothPanes(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "a"},
		{TS: "2.0", Text: "b"},
	})

	app.Update(WSMessageDeletedMsg{ChannelID: "C1", TS: "2.0"})

	msgs := app.messagepane.Messages()
	if len(msgs) != 1 || msgs[0].TS != "1.0" {
		t.Errorf("expected only TS 1.0 to remain, got %+v", msgs)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run TestWSMessageDeletedMsg -v`
Expected: FAIL — no handler.

- [ ] **Step 3: Add handler**

In `internal/ui/app.go`, find the `Update` switch in App.Update — locate the existing `case ReactionAddedMsg:` block (around line 898) and add a new arm just before it:

```go
	case WSMessageDeletedMsg:
		a.messagepane.RemoveMessageByTS(msg.TS)
		a.threadPanel.RemoveMessageByTS(msg.TS)
		// If the deleted message was the thread parent, close the panel.
		if a.threadVisible && a.threadPanel.ThreadTS() == msg.TS {
			a.CloseThread()
		}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "feat(app): handle WSMessageDeletedMsg from RTM"
```

---

## Task 11: Wire `OnMessageDeleted` in main.go

**Files:**
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Replace the TODO body**

Find `OnMessageDeleted` at `cmd/slk/main.go:1218` and replace its body:

```go
func (h *rtmEventHandler) OnMessageDeleted(channelID, ts string) {
	if err := h.db.DeleteMessage(channelID, ts); err != nil {
		log.Printf("Warning: failed to soft-delete cached message %s/%s: %v", channelID, ts, err)
	}
	if h.isActive != nil && !h.isActive() {
		// Inactive workspace — nothing to update in the UI.
		return
	}
	h.program.Send(ui.WSMessageDeletedMsg{ChannelID: channelID, TS: ts})
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat(rtm): dispatch WSMessageDeletedMsg on message_deleted event"
```

---

## Task 12: Wire `SetMessageEditor` and `SetMessageDeleter` in main.go

**Files:**
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Add the wiring**

After the existing `app.SetMessageSender(...)` block at `cmd/slk/main.go:349-370`, add:

```go
		app.SetMessageEditor(func(channelID, ts, text string) tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := client.EditMessage(ctx, channelID, ts, text)
			if err != nil {
				log.Printf("Warning: failed to edit message: %v", err)
			}
			return ui.MessageEditedMsg{ChannelID: channelID, TS: ts, Err: err}
		})

		app.SetMessageDeleter(func(channelID, ts string) tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := client.RemoveMessage(ctx, channelID, ts)
			if err != nil {
				log.Printf("Warning: failed to delete message: %v", err)
			}
			return ui.MessageDeletedMsg{ChannelID: channelID, TS: ts, Err: err}
		})
```

Verify the imports in `cmd/slk/main.go` include `time` and `context` already (the existing send code uses both).

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat(main): wire message editor and deleter callbacks"
```

---

## Task 13: Add `EditMessageMsg` / `DeleteMessageMsg` / `MessageEditedMsg` / `MessageDeletedMsg` handlers in `App.Update`

**Files:**
- Modify: `internal/ui/app.go`

This wires the App side: emitted `EditMessageMsg` / `DeleteMessageMsg` invoke the registered functions and yield result messages; the result messages handle success vs. error toasts.

- [ ] **Step 1: Add toast types in statusbar (or reuse existing)**

We'll reuse the existing `statusbar.SetToast` directly. We need a fresh `tea.Msg` type for the edit/delete-failed toasts so the existing 2-second clear pattern works. Add to `internal/ui/statusbar/model.go` near the existing toast types:

```go
// EditFailedMsg is delivered when chat.update fails. App handles by
// showing the toast and scheduling a CopiedClearMsg.
type EditFailedMsg struct{ Reason string }

// DeleteFailedMsg is delivered when chat.delete fails.
type DeleteFailedMsg struct{ Reason string }

// EditNotOwnMsg / DeleteNotOwnMsg are delivered when E/D was pressed on
// a message the current user does not own.
type EditNotOwnMsg struct{}
type DeleteNotOwnMsg struct{}
```

- [ ] **Step 2: Add Update handlers**

In `internal/ui/app.go`'s main `Update` switch, place these arms next to the existing `SendMessageMsg` / `MessageSentMsg` cases (around line 834):

```go
	case EditMessageMsg:
		if a.messageEditor != nil {
			editor := a.messageEditor
			chID, ts, text := msg.ChannelID, msg.TS, msg.NewText
			cmds = append(cmds, func() tea.Msg {
				return editor(chID, ts, text)
			})
		}

	case MessageEditedMsg:
		// Always exit edit mode regardless of success/error.
		a.cancelEdit()
		if msg.Err != nil {
			cmds = append(cmds, func() tea.Msg {
				return statusbar.EditFailedMsg{Reason: msg.Err.Error()}
			})
		}

	case DeleteMessageMsg:
		if a.messageDeleter != nil {
			deleter := a.messageDeleter
			chID, ts := msg.ChannelID, msg.TS
			cmds = append(cmds, func() tea.Msg {
				return deleter(chID, ts)
			})
		}

	case MessageDeletedMsg:
		if msg.Err != nil {
			cmds = append(cmds, func() tea.Msg {
				return statusbar.DeleteFailedMsg{Reason: msg.Err.Error()}
			})
		}
```

And add toast handlers near the existing `case statusbar.PermalinkCopiedMsg:` at line 755:

```go
	case statusbar.EditFailedMsg:
		a.statusbar.SetToast("Edit failed: " + truncateReason(msg.Reason, 40))
		cmds = append(cmds, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))

	case statusbar.DeleteFailedMsg:
		a.statusbar.SetToast("Delete failed: " + truncateReason(msg.Reason, 40))
		cmds = append(cmds, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))

	case statusbar.EditNotOwnMsg:
		a.statusbar.SetToast("Can only edit your own messages")
		cmds = append(cmds, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))

	case statusbar.DeleteNotOwnMsg:
		a.statusbar.SetToast("Can only delete your own messages")
		cmds = append(cmds, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))
```

Add a helper to `app.go` (or anywhere convenient — bottom of the file is fine):

```go
func truncateReason(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
```

`a.cancelEdit()` is the helper we'll add in Task 14. To keep this task self-contained and compileable, add a stub now:

```go
// cancelEdit is the full implementation is added in the next task.
// Stub here so MessageEditedMsg compiles.
func (a *App) cancelEdit() {
	// Implementation completed in Task 14.
}
```

When Task 14 lands it overwrites this stub.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/app.go internal/ui/statusbar/model.go
git commit -m "feat(app): handle EditMessageMsg, DeleteMessageMsg, and result messages"
```

---

## Task 14: Add edit-mode state and `beginEditOfSelected` / submit / cancel

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add the state struct**

Near the App struct (around line 415, after `currentUserID`):

```go
	// editing tracks an in-progress message edit. When active, the
	// channel or thread compose box is repurposed: its existing draft
	// is stashed, the message text seeded, and Enter submits an
	// EditMessageMsg instead of sending. Cancellation (Esc, channel
	// switch, panel switch, etc.) restores the stashed draft.
	editing editState
```

Add the type definition (place it near the other type declarations at the top of `app.go`, after `dragState` declarations or just before NewApp):

```go
type editState struct {
	active       bool
	channelID    string
	ts           string
	panel        Panel // PanelMessages or PanelThread
	stashedDraft string
}
```

- [ ] **Step 2: Replace the cancelEdit stub with the full implementation**

Replace the stub from Task 13:

```go
// cancelEdit exits edit mode, restoring the stashed draft to its source
// compose. Safe to call when no edit is active (no-op).
func (a *App) cancelEdit() {
	if !a.editing.active {
		return
	}
	switch a.editing.panel {
	case PanelMessages:
		a.compose.SetValue(a.editing.stashedDraft)
		a.compose.SetPlaceholderOverride("")
	case PanelThread:
		a.threadCompose.SetValue(a.editing.stashedDraft)
		a.threadCompose.SetPlaceholderOverride("")
	}
	a.editing = editState{}
	a.SetMode(ModeNormal)
	a.compose.Blur()
	a.threadCompose.Blur()
}

// isOwnMessage returns whether the given message is owned by the
// current user. Bot/system messages and unauthenticated states fail.
func (a *App) isOwnMessage(m messages.MessageItem) bool {
	return a.currentUserID != "" && m.UserID == a.currentUserID
}

// beginEditOfSelected starts editing the currently-selected message in
// the focused pane. No-op (with toast) if no message is selected or if
// the message is not owned by the current user.
func (a *App) beginEditOfSelected() tea.Cmd {
	var (
		channelID string
		ts        string
		text      string
		userID    string
		panel     Panel
	)
	switch a.focusedPanel {
	case PanelMessages:
		msg, ok := a.messagepane.SelectedMessage()
		if !ok {
			return nil
		}
		channelID = a.activeChannelID
		ts = msg.TS
		text = msg.Text
		userID = msg.UserID
		panel = PanelMessages
	case PanelThread:
		reply := a.threadPanel.SelectedReply()
		if reply == nil {
			return nil
		}
		channelID = a.threadPanel.ChannelID()
		ts = reply.TS
		text = reply.Text
		userID = reply.UserID
		panel = PanelThread
	default:
		return nil
	}
	if a.currentUserID == "" || userID != a.currentUserID {
		return func() tea.Msg { return statusbar.EditNotOwnMsg{} }
	}
	if channelID == "" || ts == "" {
		return nil
	}

	var stashed string
	switch panel {
	case PanelMessages:
		stashed = a.compose.Value()
		a.compose.SetValue(text)
		a.compose.SetPlaceholderOverride("Editing message — Enter to save, Esc to cancel")
	case PanelThread:
		stashed = a.threadCompose.Value()
		a.threadCompose.SetValue(text)
		a.threadCompose.SetPlaceholderOverride("Editing message — Enter to save, Esc to cancel")
	}

	a.editing = editState{
		active:       true,
		channelID:    channelID,
		ts:           ts,
		panel:        panel,
		stashedDraft: stashed,
	}
	a.SetMode(ModeInsert)
	a.focusedPanel = panel
	if panel == PanelThread {
		return a.threadCompose.Focus()
	}
	return a.compose.Focus()
}
```

- [ ] **Step 3: Branch insert-mode submit on edit state**

Modify `handleInsertMode` in `internal/ui/app.go` (around line 1277). At the very top of the function, before the existing `key.Matches(msg, a.keys.Escape)` check, add an Esc-during-edit branch:

```go
func (a *App) handleInsertMode(msg tea.KeyMsg) tea.Cmd {
	if a.editing.active && key.Matches(msg, a.keys.Escape) {
		a.cancelEdit()
		return nil
	}
```

Then, inside the existing send/reply logic, replace the `isSend` blocks. Locate the thread compose `if isSend {` block (around line 1328) and the channel compose `if isSend {` block (around line 1364). For each, prepend a check for `a.editing.active`:

For the thread compose block (replace the existing thread `if isSend { ... }`):

```go
		if isSend {
			if a.editing.active && a.editing.panel == PanelThread {
				return a.submitEdit(a.threadCompose.Value(), a.threadCompose.TranslateMentionsForSend(a.threadCompose.Value()))
			}
			text := a.threadCompose.Value()
			if text != "" {
				text = a.threadCompose.TranslateMentionsForSend(text)
				a.threadCompose.Reset()
				threadTS := a.threadPanel.ThreadTS()
				channelID := a.threadPanel.ChannelID()
				return func() tea.Msg {
					return SendThreadReplyMsg{
						ChannelID: channelID,
						ThreadTS:  threadTS,
						Text:      text,
					}
				}
			}
			return nil
		}
```

For the channel compose `if isSend {` block:

```go
	if isSend {
		if a.editing.active && a.editing.panel == PanelMessages {
			return a.submitEdit(a.compose.Value(), a.compose.TranslateMentionsForSend(a.compose.Value()))
		}
		text := a.compose.Value()
		if text != "" {
			text = a.compose.TranslateMentionsForSend(text)
			a.compose.Reset()
			return func() tea.Msg {
				return SendMessageMsg{
					ChannelID: a.activeChannelID,
					Text:      text,
				}
			}
		}
		return nil
	}
```

Add the `submitEdit` helper next to `cancelEdit`:

```go
// submitEdit emits an EditMessageMsg if the edit text is non-empty.
// Empty text refuses with an inline toast and keeps edit mode open.
func (a *App) submitEdit(rawValue, translated string) tea.Cmd {
	if strings.TrimSpace(rawValue) == "" {
		return func() tea.Msg {
			return editEmptyToastMsg{}
		}
	}
	chID := a.editing.channelID
	ts := a.editing.ts
	return func() tea.Msg {
		return EditMessageMsg{
			ChannelID: chID,
			TS:        ts,
			NewText:   translated,
		}
	}
}

type editEmptyToastMsg struct{}
```

Add the toast handler in App.Update next to the EditFailedMsg case (Task 13):

```go
	case editEmptyToastMsg:
		a.statusbar.SetToast("Edit must have text (use D to delete)")
		cmds = append(cmds, tea.Tick(3*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))
```

If `strings` isn't already imported in `app.go`, add it.

- [ ] **Step 4: Cancel-on-context-switch hooks**

Find every place that switches channels, workspaces, or panels and call `a.cancelEdit()` first. The relevant call sites are:

- `case ChannelSelectedMsg:` (around line 767) — add `a.cancelEdit()` right at the top of the case body, before the existing logic.
- `case WorkspaceSwitchedMsg:` (around line 927) — same.
- `FocusNext()` and `FocusPrev()` methods — find their definitions (probably named `FocusNext` and `FocusPrev`; search the file). Inside each, before the existing logic, add `a.cancelEdit()`.
- `case key.Matches(msg, a.keys.Escape):` in `handleNormalMode` — already returns to ModeNormal; the editing state is irrelevant in normal mode, but for safety add `a.cancelEdit()` at the top of that arm.

(The redundant calls are no-ops because `cancelEdit` checks `a.editing.active`.)

- [ ] **Step 5: Add the `E` keybinding handler**

In `handleNormalMode` (around line 1255), next to `CopyPermalink`:

```go
	case key.Matches(msg, a.keys.CopyPermalink):
		return a.copyPermalinkOfSelected()

	case key.Matches(msg, a.keys.Edit):
		return a.beginEditOfSelected()
```

- [ ] **Step 6: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 7: Quick smoke test (manual, optional)**

Run: `go run ./cmd/slk` and try pressing `E` on your own message. Confirm: the compose populates with the message, placeholder shows "Editing message…", Enter submits, Esc cancels and restores any prior draft.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat(app): implement edit mode with stash/restore draft"
```

---

## Task 15: Add `D` keybinding + `beginDeleteOfSelected`

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add `beginDeleteOfSelected`**

Place it near `beginEditOfSelected`:

```go
// beginDeleteOfSelected opens the confirmation prompt for deleting the
// currently-selected message in the focused pane. No-op (with toast)
// if not owned.
func (a *App) beginDeleteOfSelected() tea.Cmd {
	var (
		channelID string
		ts        string
		text      string
		userID    string
	)
	switch a.focusedPanel {
	case PanelMessages:
		msg, ok := a.messagepane.SelectedMessage()
		if !ok {
			return nil
		}
		channelID = a.activeChannelID
		ts = msg.TS
		text = msg.Text
		userID = msg.UserID
	case PanelThread:
		reply := a.threadPanel.SelectedReply()
		if reply == nil {
			return nil
		}
		channelID = a.threadPanel.ChannelID()
		ts = reply.TS
		text = reply.Text
		userID = reply.UserID
	default:
		return nil
	}
	if a.currentUserID == "" || userID != a.currentUserID {
		return func() tea.Msg { return statusbar.DeleteNotOwnMsg{} }
	}
	if channelID == "" || ts == "" {
		return nil
	}

	preview := text
	preview = strings.ReplaceAll(preview, "\n", " ")
	const maxPreview = 80
	if len([]rune(preview)) > maxPreview {
		runes := []rune(preview)
		preview = string(runes[:maxPreview]) + "…"
	}

	a.confirmPrompt.Open(
		"Delete message?",
		preview,
		func() tea.Msg {
			return DeleteMessageMsg{ChannelID: channelID, TS: ts}
		},
	)
	a.SetMode(ModeConfirm)
	return nil
}
```

- [ ] **Step 2: Wire the keybinding**

In `handleNormalMode`, add right after the new Edit arm from Task 14:

```go
	case key.Matches(msg, a.keys.Delete):
		return a.beginDeleteOfSelected()
```

- [ ] **Step 3: Add an integration test**

Append to `internal/ui/app_test.go`:

```go
func TestBeginDeleteOfSelected_NotOwn(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.SetCurrentUserID("U_ME")
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U_OTHER", Text: "not mine"},
	})
	app.focusedPanel = PanelMessages
	cmd := app.beginDeleteOfSelected()
	if cmd == nil {
		t.Fatal("expected toast cmd")
	}
	res := cmd()
	if _, ok := res.(statusbar.DeleteNotOwnMsg); !ok {
		t.Errorf("expected DeleteNotOwnMsg, got %T", res)
	}
	if app.confirmPrompt.IsVisible() {
		t.Error("confirm prompt should not be visible for non-owned message")
	}
}

func TestBeginDeleteOfSelected_Own_OpensPrompt(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.SetCurrentUserID("U_ME")
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserID: "U_ME", Text: "hi"},
	})
	app.focusedPanel = PanelMessages
	cmd := app.beginDeleteOfSelected()
	if cmd != nil {
		t.Errorf("expected nil cmd (prompt opens directly), got non-nil")
	}
	if !app.confirmPrompt.IsVisible() {
		t.Error("expected confirm prompt to be visible")
	}
	if app.Mode() != ModeConfirm {
		t.Errorf("expected ModeConfirm, got %v", app.Mode())
	}
}
```

If `app.Mode()` doesn't exist as an accessor, look at the existing tests for the canonical way to read the mode (it's likely an unexported field; tests live in the same package so direct access is fine: `app.mode`).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "feat(app): implement delete with confirmation prompt"
```

---

## Task 16: Verify end-to-end with full test suite

**Files:** none modified

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`
Expected: all tests pass.

- [ ] **Step 2: Build the binary**

Run: `go build ./cmd/slk`
Expected: success, no warnings.

- [ ] **Step 3: Run vet**

Run: `go vet ./...`
Expected: clean.

- [ ] **Step 4: Run linter if configured**

Run: `golangci-lint run` (if `.golangci.yml` is present in the repo)
Expected: clean.

- [ ] **Step 5: Manual smoke (run the binary against a real workspace)**

Test these flows:
1. Edit own message in a channel — Enter saves; status bar silent; message updates with `(edited)` marker on next WS echo.
2. Edit own message in a thread — same.
3. Press `E` on someone else's message — `Can only edit your own messages` toast; no compose change.
4. Delete own message — `D` opens prompt with first 80 chars; `y` confirms; message disappears via WS echo.
5. Delete own message → press `n` — prompt closes; message remains.
6. Type a draft, press `E` on prior own message, then `Esc` — draft is restored.
7. Edit a message from Slack web; verify slk updates the row in place rather than appending a duplicate.
8. Delete a message from Slack web; verify slk removes the row.
9. Delete a thread parent from the main pane; verify the thread panel closes.

If any flow misbehaves, fix and re-run the suite.

- [ ] **Step 6: Commit any fixes**

```bash
git add -A
git commit -m "fix: <describe issue> from manual smoke"   # only if needed
```

---

## Task 17: Update README and STATUS docs

**Files:**
- Modify: `README.md`
- Modify: `docs/STATUS.md`

- [ ] **Step 1: Update the keybinding table in README**

In `README.md`, locate the keybinding table (starts at line 202). Insert two new rows after the `R` row:

```
| `E` | Normal (message) | Edit your own message |
| `D` | Normal (message) | Delete your own message |
```

- [ ] **Step 2: Remove edit/delete from roadmap**

In `README.md`, find the "On the roadmap:" list (line 86–94). Remove the line:

```
- Message editing (`e`) and deletion (`dd`)
```

- [ ] **Step 3: Update docs/STATUS.md**

Find lines 105–106 in `docs/STATUS.md`:

```
- [ ] Message editing (`e` on own message)
- [ ] Message deletion (`dd` on own message)
```

Replace with:

```
- [x] Message editing (`E` on own message)
- [x] Message deletion (`D` on own message)
```

Move them to the appropriate "Implemented" section if STATUS.md splits by status (open the file first to see the conventions).

- [ ] **Step 4: Commit**

```bash
git add README.md docs/STATUS.md
git commit -m "docs: document E/D edit/delete keybindings"
```

---

## Self-Review Checklist (run before declaring done)

- [ ] Every spec section has at least one task implementing it: edit UI (Task 14), delete UI (Task 15), confirmation overlay (Task 4), edit-echo in-place update (Task 9), delete-echo handler (Tasks 10 & 11), wiring through main.go (Task 12), keybindings (Task 6), tests (Tasks 1, 2, 4, 9, 10, 15), docs (Task 17).
- [ ] No placeholders, "TBD", "TODO", or "implement later" remain in the plan.
- [ ] Method names are consistent across tasks: `UpdateMessageInPlace`, `RemoveMessageByTS`, `UpdateParentInPlace`, `SetPlaceholderOverride`, `SetMessageEditor`, `SetMessageDeleter`, `cancelEdit`, `submitEdit`, `beginEditOfSelected`, `beginDeleteOfSelected`, `isOwnMessage`.
- [ ] Type names are consistent: `EditMessageMsg`, `DeleteMessageMsg`, `MessageEditedMsg`, `MessageDeletedMsg`, `WSMessageDeletedMsg`, `MessageEditFunc`, `MessageDeleteFunc`, `editState`.
- [ ] Edit-mode lifecycle is fully covered: enter (Task 14 step 2), submit (Task 14 step 3 + Task 13), cancel (Task 14 step 2), context-switch cancel (Task 14 step 4), error handling (Task 13).
- [ ] Delete lifecycle: open prompt (Task 15), confirm (Task 8 + Task 13), error toast (Task 13), WS echo removal (Task 10 + Task 11).
- [ ] Empty-text edit refusal handled (Task 14, `submitEdit`).
- [ ] Thread parent delete closes thread panel (Task 10).
- [ ] Thread parent edit handled (Task 9 calls `UpdateParentInPlace`).
