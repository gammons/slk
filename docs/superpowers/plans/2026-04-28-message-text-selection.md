# Message Text Selection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add mouse drag-select with auto-copy to OSC 52 clipboard in the messages and thread panes, mirroring opencode's UX.

**Architecture:** New `internal/ui/selection` package (anchor types). Both `messages.Model` and `thread.Model` gain a parallel `linesPlain` cache (column-aligned, ANSI-stripped) plus a `Selectable` API. `App` adds a small drag FSM that translates `MouseClickMsg` / `MouseMotionMsg` / `MouseReleaseMsg` into Begin/Extend/End calls and emits `tea.SetClipboard` + a status-bar toast. Theme gains `SelectionBackground` / `SelectionForeground` (fall back to `Reverse`).

**Tech Stack:** Go, charm.land/bubbletea v2.0.6 (mouse + clipboard), charm.land/lipgloss/v2, github.com/charmbracelet/x/ansi (cell-accurate truncation/strip).

**Reference spec:** [`docs/superpowers/specs/2026-04-28-message-text-selection-design.md`](../specs/2026-04-28-message-text-selection-design.md).

**Worktree:** `/home/grant/local_code/slack-tui/.worktrees/message-selection` on branch `feat/message-selection`.

---

## Conventions used in this plan

- All file paths are relative to the repo root unless otherwise noted.
- Run commands from the worktree directory.
- After every task: `go build ./... && go test ./internal/ui/... -count=1` must pass before committing.
- Commit message style follows the repo (`feat:`, `chore:`, `test:` prefixes; short imperative).
- The wide-char sentinel (Task 4) is the rune `\u0000` — never emitted by Slack content, easy to grep, dropped during text extraction.

---

## Task 1: Create the `selection` package

**Files:**
- Create: `internal/ui/selection/doc.go`
- Create: `internal/ui/selection/selection.go`
- Create: `internal/ui/selection/selection_test.go`

- [ ] **Step 1: Write the failing tests**

Write `internal/ui/selection/selection_test.go`:

```go
package selection

import "testing"

func TestRange_NormalizeOrdersEndpoints(t *testing.T) {
	a := Anchor{MessageID: "1.0", Line: 0, Col: 5}
	b := Anchor{MessageID: "1.0", Line: 0, Col: 10}
	r := Range{Start: b, End: a}
	lo, hi := r.Normalize()
	if lo != a || hi != b {
		t.Fatalf("Normalize did not order endpoints: lo=%+v hi=%+v", lo, hi)
	}
}

func TestRange_NormalizeAcrossLines(t *testing.T) {
	a := Anchor{MessageID: "1.0", Line: 2, Col: 0}
	b := Anchor{MessageID: "1.0", Line: 0, Col: 99}
	lo, hi := Range{Start: a, End: b}.Normalize()
	if lo != b || hi != a {
		t.Fatalf("Line precedence wrong: lo=%+v hi=%+v", lo, hi)
	}
}

func TestRange_NormalizeAcrossMessages(t *testing.T) {
	// Earlier MessageID (Slack TS sorts lexicographically) wins regardless
	// of Line/Col.
	a := Anchor{MessageID: "1700000001.000100", Line: 5, Col: 5}
	b := Anchor{MessageID: "1700000000.000200", Line: 0, Col: 0}
	lo, hi := Range{Start: a, End: b}.Normalize()
	if lo != b || hi != a {
		t.Fatalf("MessageID precedence wrong: lo=%+v hi=%+v", lo, hi)
	}
}

func TestRange_IsEmpty(t *testing.T) {
	a := Anchor{MessageID: "1.0", Line: 1, Col: 4}
	if !(Range{Start: a, End: a}).IsEmpty() {
		t.Fatal("equal endpoints should be empty")
	}
	b := Anchor{MessageID: "1.0", Line: 1, Col: 5}
	if (Range{Start: a, End: b}).IsEmpty() {
		t.Fatal("differing endpoints should not be empty")
	}
}

func TestRange_ContainsHalfOpen(t *testing.T) {
	lo := Anchor{MessageID: "1.0", Line: 0, Col: 2}
	hi := Anchor{MessageID: "1.0", Line: 0, Col: 5}
	r := Range{Start: lo, End: hi}
	if !r.Contains(Anchor{MessageID: "1.0", Line: 0, Col: 2}) {
		t.Fatal("should contain lo (inclusive)")
	}
	if !r.Contains(Anchor{MessageID: "1.0", Line: 0, Col: 4}) {
		t.Fatal("should contain interior")
	}
	if r.Contains(Anchor{MessageID: "1.0", Line: 0, Col: 5}) {
		t.Fatal("should not contain hi (exclusive)")
	}
	if r.Contains(Anchor{MessageID: "1.0", Line: 0, Col: 1}) {
		t.Fatal("should not contain before lo")
	}
}

func TestAnchor_LessOrEqual(t *testing.T) {
	a := Anchor{MessageID: "1.0", Line: 0, Col: 5}
	b := Anchor{MessageID: "1.0", Line: 0, Col: 5}
	if !LessOrEqual(a, b) || !LessOrEqual(b, a) {
		t.Fatal("equal anchors must be <= each other")
	}
	c := Anchor{MessageID: "1.0", Line: 0, Col: 6}
	if !LessOrEqual(a, c) || LessOrEqual(c, a) {
		t.Fatal("col ordering wrong")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/selection/... -count=1`
Expected: FAIL with `package selection: no Go files` or undefined symbols.

- [ ] **Step 3: Write the implementation**

Write `internal/ui/selection/doc.go`:

```go
// Package selection provides anchor / range types for representing a
// user-driven text selection inside the messages and thread panes.
//
// The package is pure data: it has no knowledge of lipgloss, bubbletea,
// message structs, or rendered caches. UI code resolves Anchor.MessageID
// to absolute cache coordinates at render time. This keeps the selection
// stable across cache rebuilds (new messages, width changes, theme
// switches).
package selection
```

Write `internal/ui/selection/selection.go`:

```go
package selection

// Anchor identifies one endpoint of a selection.
//
//   MessageID is the Slack TS of the anchored message, or "" when the
//     anchor sits on a non-message row (e.g. a date separator). An anchor
//     on a separator is treated as a line boundary, not a character
//     position.
//   Line is the 0-indexed line within that message's rendered block
//     (after wrapping).
//   Col is the display column inside that line; columns are 0-indexed
//     and measured in display cells (wide chars occupy 2).
type Anchor struct {
	MessageID string
	Line      int
	Col       int
}

// Range is a half-open [Start, End) selection. Endpoints may be in any
// order; consumers must call Normalize before iterating.
//
// Active is true while the user is still dragging. Renderers use this
// to decide whether to draw the live highlight.
type Range struct {
	Start  Anchor
	End    Anchor
	Active bool
}

// LessOrEqual returns true when a precedes-or-equals b in document order.
// Order is (MessageID, Line, Col). MessageID is compared as a string —
// Slack timestamps sort correctly under string comparison because they
// are zero-padded fixed-width decimals.
func LessOrEqual(a, b Anchor) bool {
	if a.MessageID != b.MessageID {
		return a.MessageID < b.MessageID
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Col <= b.Col
}

// Normalize returns the endpoints in document order: lo <= hi.
func (r Range) Normalize() (lo, hi Anchor) {
	if LessOrEqual(r.Start, r.End) {
		return r.Start, r.End
	}
	return r.End, r.Start
}

// IsEmpty reports whether the selection covers zero characters.
func (r Range) IsEmpty() bool {
	return r.Start == r.End
}

// Contains reports whether a falls within the half-open [lo, hi) interval.
func (r Range) Contains(a Anchor) bool {
	lo, hi := r.Normalize()
	return LessOrEqual(lo, a) && !LessOrEqual(hi, a)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/selection/... -count=1 -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/selection/
git commit -m "feat(selection): add Anchor / Range types for message text selection"
```

---

## Task 2: Add `SelectionBackground` / `SelectionForeground` theme entries

**Files:**
- Modify: `internal/ui/styles/styles.go`
- Modify: `internal/ui/styles/themes.go`
- Modify: `internal/ui/styles/styles_test.go` (or create a new test if needed)

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/styles/styles_test.go` (read the file first to know its package import block):

```go
func TestApply_DefaultsSelectionColors(t *testing.T) {
	Apply("dracula", config.Theme{})
	if SelectionBackground == nil {
		t.Fatal("SelectionBackground must be non-nil after Apply")
	}
	if SelectionForeground == nil {
		t.Fatal("SelectionForeground must be non-nil after Apply")
	}
	// Selection style helper must produce a non-empty render result for
	// arbitrary text.
	rendered := SelectionStyle().Render("hello")
	if rendered == "" {
		t.Fatal("SelectionStyle().Render returned empty string")
	}
}

func TestApply_CustomSelectionFromTheme(t *testing.T) {
	RegisterCustomTheme("seltest", ThemeColors{
		Primary: "#000000", Accent: "#000000", Warning: "#000000",
		Error: "#000000", Background: "#000000", Surface: "#000000",
		SurfaceDark: "#000000", Text: "#FFFFFF", TextMuted: "#888888",
		Border: "#222222",
		SelectionBackground: "#FF00FF",
		SelectionForeground: "#00FF00",
	})
	Apply("seltest", config.Theme{})
	r, g, b, _ := SelectionBackground.RGBA()
	if r>>8 != 0xFF || g>>8 != 0x00 || b>>8 != 0xFF {
		t.Fatalf("custom SelectionBackground not applied: got %02x%02x%02x", r>>8, g>>8, b>>8)
	}
}
```

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/ui/styles/... -count=1 -run Selection`
Expected: FAIL — undefined `SelectionBackground`, `SelectionForeground`, `SelectionStyle`, `ThemeColors.SelectionBackground`, `ThemeColors.SelectionForeground`.

- [ ] **Step 3: Add the fields to `ThemeColors` and update `Apply`**

In `internal/ui/styles/themes.go`, append to the `ThemeColors` struct (after `RailBackground`):

```go
	SelectionBackground string `toml:"selection_background"`
	SelectionForeground string `toml:"selection_foreground"`
```

- [ ] **Step 4: Add the package-level color variables and helper to styles.go**

In `internal/ui/styles/styles.go`, in the `var ( ... )` block alongside the other colors, add:

```go
	// Selection highlight. Default to nil; Apply() either copies the theme's
	// values or leaves them nil so SelectionStyle() falls back to Reverse(true).
	SelectionBackground color.Color
	SelectionForeground color.Color
```

At the bottom of the same file (or in `buildStyles`), add:

```go
// SelectionStyle returns the lipgloss style used to highlight selected text.
// When the active theme defines SelectionBackground/SelectionForeground we
// honor them; otherwise we fall back to Reverse(true), which lets the
// terminal swap whatever fg/bg is in effect.
func SelectionStyle() lipgloss.Style {
	if SelectionBackground != nil && SelectionForeground != nil {
		return lipgloss.NewStyle().
			Background(SelectionBackground).
			Foreground(SelectionForeground)
	}
	return lipgloss.NewStyle().Reverse(true)
}
```

- [ ] **Step 5: Wire into `Apply`**

In `internal/ui/styles/themes.go`'s `Apply` function, after the rail/sidebar fallback block, add:

```go
	if colors.SelectionBackground != "" {
		SelectionBackground = lipgloss.Color(colors.SelectionBackground)
	} else {
		SelectionBackground = nil
	}
	if colors.SelectionForeground != "" {
		SelectionForeground = lipgloss.Color(colors.SelectionForeground)
	} else {
		SelectionForeground = nil
	}
```

(Reset to `nil` when the theme doesn't define them so a previously-themed
selection color doesn't leak into the next theme.)

The default `Reverse(true)` path means we satisfy `SelectionStyle().Render("hello") != ""` even when the theme leaves the colors blank — but the test asserts `SelectionBackground != nil`. To make `TestApply_DefaultsSelectionColors` pass on `dracula` (which has no selection colors), pick sensible derived defaults instead of nil. Replace the block above with:

```go
	if colors.SelectionBackground != "" {
		SelectionBackground = lipgloss.Color(colors.SelectionBackground)
	} else {
		// Default: use Primary as the highlight bg. Combined with the
		// derived foreground below this guarantees readable contrast on
		// every built-in theme.
		SelectionBackground = Primary
	}
	if colors.SelectionForeground != "" {
		SelectionForeground = lipgloss.Color(colors.SelectionForeground)
	} else {
		// Default: theme background — when the bg is Primary, drawing
		// text in the original Background gives high contrast on every
		// built-in palette.
		SelectionForeground = Background
	}
```

(`SelectionStyle()` no longer needs the nil-check fallback — keep it for
robustness but it'll always take the colored branch in practice.)

- [ ] **Step 6: Run tests**

Run: `go test ./internal/ui/styles/... -count=1`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/styles/
git commit -m "feat(styles): add SelectionBackground/SelectionForeground theme entries"
```

---

## Task 3: Status-bar `CopiedMsg` toast

**Files:**
- Modify: `internal/ui/statusbar/model.go`
- Modify: `internal/ui/statusbar/model_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/ui/statusbar/model_test.go`:

```go
func TestModel_CopiedToastShowsAndExpires(t *testing.T) {
	m := New()
	m.SetChannel("general")
	m.ShowCopied(42)
	out := m.View(80)
	if !strings.Contains(out, "Copied 42 chars") {
		t.Fatalf("expected toast in status bar; got %q", out)
	}
	// After ClearCopied the toast must disappear.
	m.ClearCopied()
	out = m.View(80)
	if strings.Contains(out, "Copied") {
		t.Fatalf("expected toast cleared; got %q", out)
	}
}

func TestModel_CopiedBumpsVersion(t *testing.T) {
	m := New()
	v0 := m.Version()
	m.ShowCopied(1)
	if m.Version() == v0 {
		t.Fatal("ShowCopied must bump Version()")
	}
}
```

(Add `import "strings"` if not already present.)

- [ ] **Step 2: Run tests, expect failure**

Run: `go test ./internal/ui/statusbar/... -count=1`
Expected: FAIL — `ShowCopied`, `ClearCopied` undefined.

- [ ] **Step 3: Add the toast field and methods**

In `internal/ui/statusbar/model.go`:

Add a field on `Model`:

```go
	copiedChars int // 0 == no toast; >0 == "Copied N chars"
```

Add methods (above or below the existing setters):

```go
// ShowCopied displays a "Copied N chars" toast on the right side of the
// status bar. Callers are responsible for clearing it (typically via a
// tea.Tick).
func (m *Model) ShowCopied(n int) {
	if n <= 0 {
		return
	}
	if m.copiedChars != n {
		m.copiedChars = n
		m.dirty()
	}
}

// ClearCopied removes the copy toast.
func (m *Model) ClearCopied() {
	if m.copiedChars != 0 {
		m.copiedChars = 0
		m.dirty()
	}
}
```

In `View(width int)`, just before the `switch m.connState` block (where right-side parts are appended), insert:

```go
	if m.copiedChars > 0 {
		rightParts = append(rightParts,
			lipgloss.NewStyle().
				Foreground(styles.Accent).
				Background(styles.SurfaceDark).
				Bold(true).
				Render(fmt.Sprintf("Copied %d chars", m.copiedChars)))
	}
```

- [ ] **Step 4: Define `CopiedMsg` (used later by App)**

In the same `internal/ui/statusbar/model.go`, append:

```go
// CopiedMsg is delivered when the messages or thread pane copies a
// selection to the clipboard. App handles it by calling ShowCopied and
// scheduling a ClearCopied after a short delay.
type CopiedMsg struct {
	N int
}

// CopiedClearMsg is the follow-up tick that clears the toast.
type CopiedClearMsg struct{}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/ui/statusbar/... -count=1 -v`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/statusbar/
git commit -m "feat(statusbar): add CopiedMsg toast for clipboard feedback"
```

---

## Task 4: messages — emit column-aligned plain lines in `buildCache`

**Files:**
- Modify: `internal/ui/messages/model.go` (the `viewEntry` struct + `buildCache`)
- Modify: `internal/ui/messages/render.go` (new `plainLines` helper)
- Create: `internal/ui/messages/plain_test.go`

The plain-text mirror is the foundation everything else depends on. Each
`viewEntry` will carry a `linesPlain []string` whose byte cluster N
corresponds to display column N of `linesNormal[i]`. Wide runes occupy
two columns; column N+1 holds the sentinel rune `\u0000`, dropped during
extraction.

- [ ] **Step 1: Write failing test for the plain-line helper**

Create `internal/ui/messages/plain_test.go`:

```go
package messages

import (
	"strings"
	"testing"
)

func TestPlainLines_StripsANSI(t *testing.T) {
	in := "\x1b[31mhello\x1b[0m world"
	got := plainLines(in)
	if len(got) != 1 || got[0] != "hello world" {
		t.Fatalf("plainLines unexpected: %#v", got)
	}
}

func TestPlainLines_PreservesNewlines(t *testing.T) {
	in := "a\nbb\nccc"
	got := plainLines(in)
	if len(got) != 3 || got[0] != "a" || got[1] != "bb" || got[2] != "ccc" {
		t.Fatalf("plainLines: %#v", got)
	}
}

func TestPlainLines_WideCharSentinel(t *testing.T) {
	// 🚀 occupies 2 cells; emit a sentinel after it so byte index == col.
	in := "x🚀y"
	got := plainLines(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 line, got %d", len(got))
	}
	line := got[0]
	// Display columns: 'x' (1) + 🚀 (2) + 'y' (1) = 4.
	if displayWidthOfPlain(line) != 4 {
		t.Fatalf("expected width 4, got %d for %q", displayWidthOfPlain(line), line)
	}
	// Sentinel must be present immediately after the rocket.
	if !strings.ContainsRune(line, plainWideSentinel) {
		t.Fatalf("expected sentinel rune in %q", line)
	}
}

func TestPlainLines_ColumnAlignment(t *testing.T) {
	// Slice plain[i][a:b] must correspond to display columns [a:b) of the
	// rendered text.
	in := "ab\x1b[1mcd\x1b[0mef"
	got := plainLines(in)
	if got[0][0:2] != "ab" || got[0][2:4] != "cd" || got[0][4:6] != "ef" {
		t.Fatalf("column alignment broken: %q", got[0])
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/ui/messages/... -run Plain -count=1`
Expected: FAIL — `plainLines`, `plainWideSentinel`, `displayWidthOfPlain` undefined.

- [ ] **Step 3: Implement the helper**

Append to `internal/ui/messages/render.go`:

```go
import (
	// ... existing imports ...
	uniseg "github.com/rivo/uniseg" // already an indirect dep via lipgloss
)

// plainWideSentinel sits in the byte position that corresponds to the
// trailing display column of a wide rune. It is dropped during selection
// text extraction. The rune is U+0000 — never emitted by Slack content.
const plainWideSentinel = '\x00'

// plainLines returns ANSI-stripped, column-aligned mirrors of each line
// in s. For each line, byte cluster index N corresponds to display
// column N. Wide runes occupy two columns: the rune itself for column N,
// and plainWideSentinel for column N+1.
//
// Returned slice has the same number of entries as strings.Split(s, "\n").
// Empty lines yield "".
func plainLines(s string) []string {
	stripped := ansi.Strip(s)
	rawLines := strings.Split(stripped, "\n")
	out := make([]string, len(rawLines))
	for i, line := range rawLines {
		out[i] = padPlainLine(line)
	}
	return out
}

// padPlainLine inserts plainWideSentinel after every wide grapheme cluster
// so byte clusters align to display columns.
func padPlainLine(line string) string {
	if line == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(line) + 4)
	g := uniseg.NewGraphemes(line)
	for g.Next() {
		cluster := g.Str()
		w := g.Width()
		b.WriteString(cluster)
		for j := 1; j < w; j++ {
			b.WriteRune(plainWideSentinel)
		}
	}
	return b.String()
}

// displayWidthOfPlain returns the display column count of a plain line
// produced by plainLines. It's just len([]rune(line)) because each
// rune (including the sentinel) corresponds to exactly one column.
func displayWidthOfPlain(line string) int {
	w := 0
	for range line {
		w++
	}
	return w
}
```

(If `uniseg` isn't already imported, add `"github.com/rivo/uniseg"` to
imports and run `go mod tidy` at end.)

- [ ] **Step 4: Run helper tests**

Run: `go test ./internal/ui/messages/... -run Plain -count=1 -v`
Expected: all PASS.

- [ ] **Step 5: Add `linesPlain` to `viewEntry` and populate during `buildCache`**

In `internal/ui/messages/model.go`, modify the `viewEntry` struct:

```go
type viewEntry struct {
	linesNormal   []string
	linesSelected []string
	linesPlain    []string // ANSI-stripped, column-aligned mirror of linesNormal
	height        int
	msgIdx        int
}
```

Modify `appendSeparator` inside `buildCache` to set `linesPlain`:

```go
	appendSeparator := func(rendered string) {
		lines := strings.Split(rendered, "\n")
		m.cache = append(m.cache, viewEntry{
			linesNormal:   lines,
			linesSelected: lines,
			linesPlain:    plainLines(rendered),
			height:        len(lines),
			msgIdx:        -1,
		})
	}
```

Modify the message-append path inside `buildCache` (replace the existing
`m.cache = append(m.cache, viewEntry{...})` for messages) to compute
`linesPlain` from the bordered (`normal`) string:

```go
		linesP := plainLines(normal)
		// Append a trailing-spacer plain line to match the normal/selected
		// slices when a spacer was added.
		if i < len(m.messages)-1 {
			linesP = append(linesP, "") // spacer is whitespace, plain == empty
		}
		m.cache = append(m.cache, viewEntry{
			linesNormal:   linesN,
			linesSelected: linesS,
			linesPlain:    linesP,
			height:        len(linesN),
			msgIdx:        i,
		})
```

(Spacer lines are themed-background blanks; their plain mirror is "" so
extraction never emits trailing whitespace.)

- [ ] **Step 6: Add a regression test that asserts `linesPlain` length matches `linesNormal`**

Append to `internal/ui/messages/plain_test.go`:

```go
func TestBuildCache_LinesPlainAlignsWithLinesNormal(t *testing.T) {
	m := New([]MessageItem{
		{TS: "1.0", UserName: "alice", UserID: "U1", Text: "hello world", Timestamp: "1:00 PM"},
		{TS: "2.0", UserName: "bob", UserID: "U2", Text: "x🚀y", Timestamp: "1:01 PM"},
	}, "general")
	m.buildCache(60)
	for _, e := range m.cache {
		if len(e.linesNormal) != len(e.linesPlain) {
			t.Fatalf("linesNormal/linesPlain length mismatch: %d vs %d", len(e.linesNormal), len(e.linesPlain))
		}
	}
}
```

- [ ] **Step 7: Run all messages tests**

Run: `go test ./internal/ui/messages/... -count=1`
Expected: all PASS. Run `go mod tidy` if you added uniseg.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/messages/ go.mod go.sum
git commit -m "feat(messages): emit column-aligned linesPlain in buildCache"
```

---

## Task 5: messages — `Selectable` API + `messageIDToEntryIdx`

**Files:**
- Modify: `internal/ui/messages/model.go`
- Create: `internal/ui/messages/selection_test.go`

This task adds the `BeginSelectionAt`, `ExtendSelectionAt`, `EndSelection`,
`ClearSelection`, `HasSelection`, `SelectionText`, and `ScrollHintForDrag`
methods plus the supporting state. Highlight rendering is Task 6.

- [ ] **Step 1: Write failing tests**

Create `internal/ui/messages/selection_test.go`:

```go
package messages

import (
	"strings"
	"testing"
)

func newTestModel(width int) *Model {
	m := New([]MessageItem{
		{TS: "1.0", UserName: "alice", UserID: "U1", Text: "hello world", Timestamp: "1:00 PM"},
		{TS: "2.0", UserName: "bob", UserID: "U2", Text: "second message", Timestamp: "1:01 PM"},
	}, "general")
	// Force cache build via View().
	_ = m.View(40, width)
	return &m
}

func TestSelection_BeginExtendEndCopiesText(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(0, 0)
	// Drag to end of last visible line.
	m.ExtendSelectionAt(20, 60)
	text, ok := m.EndSelection()
	if !ok {
		t.Fatalf("EndSelection returned ok=false")
	}
	if !strings.Contains(text, "hello world") {
		t.Fatalf("expected selected text to contain 'hello world'; got %q", text)
	}
	if !m.HasSelection() {
		t.Fatal("selection should persist after EndSelection (until cleared)")
	}
}

func TestSelection_ClickWithoutDragReturnsEmpty(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(0, 5)
	// No ExtendSelectionAt call — endpoints equal.
	_, ok := m.EndSelection()
	if ok {
		t.Fatal("zero-length selection must return ok=false")
	}
}

func TestSelection_ClearRemovesSelection(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(0, 10)
	m.EndSelection()
	m.ClearSelection()
	if m.HasSelection() {
		t.Fatal("ClearSelection must remove selection")
	}
}

func TestSelection_ScrollHintForDrag(t *testing.T) {
	m := newTestModel(60)
	if got := m.ScrollHintForDrag(0); got != -1 {
		t.Errorf("top edge: want -1 got %d", got)
	}
	if got := m.ScrollHintForDrag(40 /* viewport height passed to View */ - 1); got != +1 {
		t.Errorf("bottom edge: want +1 got %d", got)
	}
	if got := m.ScrollHintForDrag(20); got != 0 {
		t.Errorf("middle: want 0 got %d", got)
	}
}

func TestSelection_SurvivesAppendMessage(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(2, 5)
	textBefore, _ := m.EndSelection()

	m.AppendMessage(MessageItem{TS: "3.0", UserName: "carol", UserID: "U3", Text: "later", Timestamp: "1:02 PM"})
	_ = m.View(40, 60) // rebuild cache

	textAfter := m.SelectionText()
	if textBefore != textAfter {
		t.Fatalf("selection drifted after AppendMessage:\nbefore=%q\nafter =%q", textBefore, textAfter)
	}
}

func TestSelection_ChannelSwitchClears(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(0, 5)
	m.SetMessages([]MessageItem{{TS: "9.0", UserName: "x", UserID: "U9", Text: "z"}})
	if m.HasSelection() {
		t.Fatal("SetMessages (channel switch) must clear selection")
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/ui/messages/... -run Selection -count=1`
Expected: undefined `BeginSelectionAt`, etc.

- [ ] **Step 3: Add fields to `Model`**

In `internal/ui/messages/model.go`'s `Model` struct, after `version int64`, add:

```go
	// Mouse selection state. selRange is the user's drag selection;
	// messageIDToEntryIdx maps Slack TS -> entry index in m.cache for
	// O(1) anchor resolution. lastViewHeight is captured during View()
	// so ScrollHintForDrag knows the pane bounds.
	selRange             selection.Range
	hasSelection         bool
	messageIDToEntryIdx  map[string]int
	lastViewHeight       int
```

Add the import: `"github.com/gammons/slk/internal/ui/selection"`.

- [ ] **Step 4: Populate the index in `buildCache`**

At the top of `buildCache`, before the loop:

```go
	if m.messageIDToEntryIdx == nil {
		m.messageIDToEntryIdx = make(map[string]int, len(m.messages))
	} else {
		for k := range m.messageIDToEntryIdx {
			delete(m.messageIDToEntryIdx, k)
		}
	}
```

Inside the loop, after building each message's entry, before
`m.cache = append(...)`, capture the index:

```go
		m.messageIDToEntryIdx[msg.TS] = len(m.cache)
```

(The map records the slot the entry will occupy; `len(m.cache)` before
the append is the next index.)

- [ ] **Step 5: Add coordinate translation helpers**

Append to `internal/ui/messages/model.go`:

```go
// absoluteLineAt returns the global line index in the flattened cache
// for a viewport-local y coordinate (0 == top of message area). Clamps
// to [0, totalLines-1] for out-of-range inputs.
func (m *Model) absoluteLineAt(viewportY int) int {
	abs := viewportY + m.yOffset
	if abs < 0 {
		abs = 0
	}
	if m.totalLines > 0 && abs >= m.totalLines {
		abs = m.totalLines - 1
	}
	return abs
}

// anchorAt converts an absolute line index + display column into an
// Anchor. Returns ok=false when no entry covers the line (empty cache).
func (m *Model) anchorAt(absLine, col int) (selection.Anchor, bool) {
	for i, e := range m.cache {
		start := m.entryOffsets[i]
		end := start + e.height
		if absLine < start || absLine >= end {
			continue
		}
		lineIdx := absLine - start
		if col < 0 {
			col = 0
		}
		// Clamp col to plain-line width so we never anchor past the end.
		if lineIdx < len(e.linesPlain) {
			if w := displayWidthOfPlain(e.linesPlain[lineIdx]); col > w {
				col = w
			}
		}
		var msgID string
		if e.msgIdx >= 0 && e.msgIdx < len(m.messages) {
			msgID = m.messages[e.msgIdx].TS
		}
		return selection.Anchor{MessageID: msgID, Line: lineIdx, Col: col}, true
	}
	return selection.Anchor{}, false
}

// resolveAnchor returns the absolute line + col for an Anchor, using the
// current cache. Returns ok=false when the message is no longer present.
func (m *Model) resolveAnchor(a selection.Anchor) (absLine, col int, ok bool) {
	if a.MessageID == "" {
		return 0, 0, false
	}
	idx, found := m.messageIDToEntryIdx[a.MessageID]
	if !found || idx >= len(m.cache) {
		return 0, 0, false
	}
	e := m.cache[idx]
	if a.Line < 0 || a.Line >= e.height {
		return 0, 0, false
	}
	return m.entryOffsets[idx] + a.Line, a.Col, true
}
```

- [ ] **Step 6: Add the public API**

Append:

```go
// BeginSelectionAt anchors a new selection at the given pane-local
// coordinates. The selection becomes Active. Coordinates are clamped to
// the rendered area.
func (m *Model) BeginSelectionAt(viewportY, x int) {
	abs := m.absoluteLineAt(viewportY)
	a, ok := m.anchorAt(abs, x)
	if !ok {
		return
	}
	m.selRange = selection.Range{Start: a, End: a, Active: true}
	m.hasSelection = true
	m.dirty()
}

// ExtendSelectionAt updates the End anchor of the active selection.
// No-op if BeginSelectionAt was never called.
func (m *Model) ExtendSelectionAt(viewportY, x int) {
	if !m.hasSelection {
		return
	}
	abs := m.absoluteLineAt(viewportY)
	a, ok := m.anchorAt(abs, x)
	if !ok {
		return
	}
	m.selRange.End = a
	m.dirty()
}

// EndSelection finalizes the drag, returning the plain-text contents of
// the selection. Returns ok=false when the selection is empty (a click
// without drag). The selection itself remains visible until ClearSelection
// is called or a new drag begins.
func (m *Model) EndSelection() (string, bool) {
	if !m.hasSelection {
		return "", false
	}
	m.selRange.Active = false
	if m.selRange.IsEmpty() {
		m.hasSelection = false
		m.selRange = selection.Range{}
		m.dirty()
		return "", false
	}
	text := m.SelectionText()
	m.dirty()
	if text == "" {
		return "", false
	}
	return text, true
}

// ClearSelection removes the current selection, if any.
func (m *Model) ClearSelection() {
	if !m.hasSelection {
		return
	}
	m.hasSelection = false
	m.selRange = selection.Range{}
	m.dirty()
}

// HasSelection reports whether a selection is currently active or
// pinned-on-screen post-drag.
func (m *Model) HasSelection() bool {
	return m.hasSelection
}

// ScrollHintForDrag returns -1 if the cursor is within 1 row of the top
// edge of the message pane, +1 if within 1 row of the bottom, else 0.
// Used by App to schedule auto-scroll ticks during a drag.
func (m *Model) ScrollHintForDrag(viewportY int) int {
	h := m.lastViewHeight
	if h <= 0 {
		return 0
	}
	if viewportY <= 0 {
		return -1
	}
	if viewportY >= h-1 {
		return +1
	}
	return 0
}

// SelectionText extracts the plain-text contents of the current
// selection. Sentinel runes are stripped; trailing whitespace is
// trimmed per line; a final trailing newline is removed.
func (m *Model) SelectionText() string {
	if !m.hasSelection || m.selRange.IsEmpty() {
		return ""
	}
	loA, hiA := m.selRange.Normalize()
	loLine, loCol, ok1 := m.resolveAnchor(loA)
	hiLine, hiCol, ok2 := m.resolveAnchor(hiA)
	if !ok1 || !ok2 {
		return ""
	}
	if loLine > hiLine || (loLine == hiLine && loCol >= hiCol) {
		return ""
	}

	var b strings.Builder
	cur := 0
	for i, e := range m.cache {
		entryStart := m.entryOffsets[i]
		entryEnd := entryStart + e.height
		if entryEnd <= loLine {
			cur += e.height
			continue
		}
		if entryStart > hiLine {
			break
		}
		for j, plain := range e.linesPlain {
			absLine := entryStart + j
			if absLine < loLine {
				continue
			}
			if absLine > hiLine {
				break
			}
			from := 0
			to := displayWidthOfPlain(plain)
			if absLine == loLine {
				from = loCol
			}
			if absLine == hiLine {
				to = hiCol
			}
			if from < 0 {
				from = 0
			}
			if to > displayWidthOfPlain(plain) {
				to = displayWidthOfPlain(plain)
			}
			if from >= to {
				if absLine != hiLine {
					b.WriteByte('\n')
				}
				continue
			}
			seg := sliceColumns(plain, from, to)
			seg = strings.TrimRightFunc(seg, func(r rune) bool {
				return r == ' ' || r == plainWideSentinel
			})
			seg = strings.ReplaceAll(seg, string(plainWideSentinel), "")
			b.WriteString(seg)
			if absLine != hiLine {
				b.WriteByte('\n')
			}
		}
		cur += e.height
	}
	return strings.TrimRight(b.String(), "\n")
}

// sliceColumns returns the substring covering display columns [from, to).
// Because plainLines pads wide runes with sentinels, byte clusters and
// columns are 1:1 — we walk runes.
func sliceColumns(plain string, from, to int) string {
	if from <= 0 && to >= displayWidthOfPlain(plain) {
		return plain
	}
	col := 0
	startByte := -1
	endByte := len(plain)
	for byteIdx, r := range plain {
		if col == from && startByte < 0 {
			startByte = byteIdx
		}
		if col == to {
			endByte = byteIdx
			break
		}
		col++
		_ = r
	}
	if startByte < 0 {
		return ""
	}
	if endByte < startByte {
		endByte = startByte
	}
	return plain[startByte:endByte]
}
```

- [ ] **Step 7: Capture `lastViewHeight` in `View()`**

In `(m *Model) View(height, width int)` near the top, after computing
`msgAreaHeight`:

```go
	m.lastViewHeight = msgAreaHeight
```

- [ ] **Step 8: Clear selection on channel switch**

Modify `SetMessages` and `PrependMessages`:

In `SetMessages`, immediately after `m.messages = msgs`, add:

```go
	m.ClearSelection()
```

In `PrependMessages` we want to PRESERVE selection (anchors are
ID-based), so don't clear there.

`AppendMessage` also preserves; no change.

`InvalidateCache` (theme switch) also preserves.

- [ ] **Step 9: Run all selection tests**

Run: `go test ./internal/ui/messages/... -run Selection -count=1 -v`
Expected: all PASS.

- [ ] **Step 10: Run full messages package**

Run: `go test ./internal/ui/messages/... -count=1`
Expected: all PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/ui/messages/
git commit -m "feat(messages): add Selectable API for mouse text selection"
```

---

## Task 6: messages — selection highlight overlay in `View()`

**Files:**
- Modify: `internal/ui/messages/model.go` (the visible-window assembly inside `View()`)

The render-cache stays untouched. We compose selection only into the
visible window.

- [ ] **Step 1: Write a failing test (golden-ish, comparing presence of selection style)**

Append to `internal/ui/messages/selection_test.go`:

```go
func TestSelection_ViewIncludesHighlight(t *testing.T) {
	m := newTestModel(60)
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(0, 5)
	out := m.View(40, 60)
	// styles.SelectionStyle().Render produces an ANSI sequence; on the
	// default theme this includes the Reverse SGR or 38;2/48;2 codes.
	// We just assert that the rendered output is materially different
	// from the no-selection rendering.
	m.ClearSelection()
	out2 := m.View(40, 60)
	if out == out2 {
		t.Fatal("View output unchanged with active selection — highlight not applied")
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/ui/messages/... -run TestSelection_ViewIncludesHighlight -count=1`
Expected: FAIL (output identical because we haven't applied highlight yet).

- [ ] **Step 3: Implement the overlay**

In `internal/ui/messages/model.go`, inside `View()` after `visible` is
fully assembled and AFTER the scroll-indicator overrides (just before
the final `return chrome + "\n" + strings.Join(visible, "\n")`), insert:

```go
	if m.hasSelection {
		visible = m.applySelectionOverlay(visible, msgAreaHeight)
	}
```

Add the helper method:

```go
// applySelectionOverlay re-composes lines that intersect the active
// selection range. linesNormal supplies the original styled prefix and
// suffix; the selected interior is rendered through styles.SelectionStyle
// over the plain (ANSI-stripped) text so the highlight is uniform.
//
// visible is mutated in place when possible.
func (m *Model) applySelectionOverlay(visible []string, paneHeight int) []string {
	loA, hiA := m.selRange.Normalize()
	loLine, loCol, ok1 := m.resolveAnchor(loA)
	hiLine, hiCol, ok2 := m.resolveAnchor(hiA)
	if !ok1 || !ok2 {
		return visible
	}
	if loLine > hiLine || (loLine == hiLine && loCol >= hiCol) {
		return visible
	}

	selStyle := styles.SelectionStyle()

	for row := 0; row < len(visible); row++ {
		absLine := m.yOffset + row
		if absLine < loLine || absLine > hiLine {
			continue
		}
		// Find the entry covering this absolute line.
		entryIdx := -1
		for i := range m.cache {
			start := m.entryOffsets[i]
			if absLine >= start && absLine < start+m.cache[i].height {
				entryIdx = i
				break
			}
		}
		if entryIdx < 0 {
			continue
		}
		e := m.cache[entryIdx]
		j := absLine - m.entryOffsets[entryIdx]
		if j < 0 || j >= len(e.linesPlain) {
			continue
		}
		plain := e.linesPlain[j]
		styled := visible[row]

		from := 0
		to := displayWidthOfPlain(plain)
		if absLine == loLine {
			from = loCol
		}
		if absLine == hiLine {
			to = hiCol
		}
		if from < 0 {
			from = 0
		}
		if to > displayWidthOfPlain(plain) {
			to = displayWidthOfPlain(plain)
		}
		if from >= to {
			continue
		}

		// Slice the visible (styled) line by display columns. ansi.Cut
		// preserves the styling of the prefix/suffix.
		prefix := ansi.Cut(styled, 0, from)
		suffix := ansi.Cut(styled, to, ansi.StringWidth(styled))
		seg := sliceColumns(plain, from, to)
		seg = strings.ReplaceAll(seg, string(plainWideSentinel), " ")
		visible[row] = prefix + selStyle.Render(seg) + suffix
	}
	_ = paneHeight
	return visible
}
```

(Add `"github.com/charmbracelet/x/ansi"` import if not already present.)

- [ ] **Step 4: Run highlight test**

Run: `go test ./internal/ui/messages/... -run TestSelection_ViewIncludesHighlight -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Run full package + benchmarks (sanity)**

Run: `go test ./internal/ui/messages/... -count=1`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/messages/
git commit -m "feat(messages): render selection highlight in viewport overlay"
```

---

## Task 7: thread — same `Selectable` API (and refactor cache to viewEntry shape)

**Files:**
- Modify: `internal/ui/thread/model.go`
- Create: `internal/ui/thread/selection_test.go`

The thread panel currently caches `[]string` (one bordered string per
reply, joined by separators) and uses bubbles/viewport for scrolling.
We introduce a per-reply `entry` with `linesNormal` / `linesPlain` and
keep the existing viewport for vertical scrolling. The selection API
mirrors messages.Model.

- [ ] **Step 1: Write failing tests**

Create `internal/ui/thread/selection_test.go`:

```go
package thread

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
)

func newTestThread() *Model {
	m := New()
	parent := messages.MessageItem{TS: "1.0", UserName: "alice", UserID: "U1", Text: "parent", Timestamp: "1:00 PM"}
	replies := []messages.MessageItem{
		{TS: "2.0", UserName: "bob", UserID: "U2", Text: "first reply", Timestamp: "1:01 PM"},
		{TS: "3.0", UserName: "carol", UserID: "U3", Text: "second reply", Timestamp: "1:02 PM"},
	}
	m.SetThread(parent, replies, "C1", "1.0")
	_ = m.View(40, 60)
	return m
}

func TestThreadSelection_BeginExtendEnd(t *testing.T) {
	m := newTestThread()
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(20, 60)
	text, ok := m.EndSelection()
	if !ok || text == "" {
		t.Fatalf("EndSelection: ok=%v text=%q", ok, text)
	}
	if !strings.Contains(text, "reply") {
		t.Fatalf("expected text to contain 'reply'; got %q", text)
	}
}

func TestThreadSelection_ClearOnSetThread(t *testing.T) {
	m := newTestThread()
	m.BeginSelectionAt(0, 0)
	m.ExtendSelectionAt(0, 5)
	m.SetThread(messages.MessageItem{TS: "9.0", Text: "x"}, nil, "C2", "9.0")
	if m.HasSelection() {
		t.Fatal("SetThread must clear selection")
	}
}

func TestThreadSelection_ScrollHintForDrag(t *testing.T) {
	m := newTestThread()
	if got := m.ScrollHintForDrag(0); got != -1 {
		t.Errorf("top: want -1 got %d", got)
	}
	if got := m.ScrollHintForDrag(40 - 1); got != +1 {
		t.Errorf("bottom: want +1 got %d", got)
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/ui/thread/... -run Selection -count=1`
Expected: undefined methods.

- [ ] **Step 3: Refactor thread cache to track per-reply entries**

In `internal/ui/thread/model.go`, add an entry type and replace the
`cache []string` field:

```go
type viewEntry struct {
	linesNormal []string
	linesPlain  []string
	height      int
	replyIdx    int // index into m.replies
}

// (replace `cache []string`)
type Model struct {
	// ... existing fields ...
	cache        []viewEntry
	entryOffsets []int
	totalLines   int

	selRange      selection.Range
	hasSelection  bool
	replyIDToIdx  map[string]int
	lastViewHeight int
	// ... existing fields ...
}
```

Add the import `"github.com/gammons/slk/internal/ui/selection"`.

Replace the cache rebuild in `View()` (the `if m.cache == nil || ...`
block that fills `m.cache[i]`) with:

```go
	if m.cache == nil || m.cacheWidth != width || m.cacheReplyLen != len(m.replies) {
		m.cache = make([]viewEntry, 0, len(m.replies))
		m.replyIDToIdx = make(map[string]int, len(m.replies))
		for i, reply := range m.replies {
			rendered := m.renderThreadMessage(reply, width, m.userNames, i == m.selected)
			m.cache = append(m.cache, viewEntry{
				linesNormal: strings.Split(rendered, "\n"),
				linesPlain:  messages.PlainLines(rendered),
				height:      lipgloss.Height(rendered),
				replyIdx:    i,
			})
			m.replyIDToIdx[reply.TS] = i
		}
		m.cacheWidth = width
		m.cacheReplyLen = len(m.replies)
		m.viewCacheValid = false
		// Recompute entryOffsets / totalLines.
		off := 0
		m.entryOffsets = m.entryOffsets[:0]
		for _, e := range m.cache {
			m.entryOffsets = append(m.entryOffsets, off)
			off += e.height
		}
		m.totalLines = off
	}
```

This requires `messages.PlainLines` to be exported. In
`internal/ui/messages/render.go`, export the helper by renaming
`plainLines` (lowercase) to **also** expose it as `PlainLines`. The
simplest path is adding a thin wrapper:

```go
// PlainLines is the exported form of plainLines, used by sibling UI
// packages (thread) that maintain their own render caches.
func PlainLines(s string) []string { return plainLines(s) }
```

Also export `DisplayWidthOfPlain` and `SliceColumns` and the sentinel:

```go
const PlainWideSentinel = plainWideSentinel
func DisplayWidthOfPlain(s string) int { return displayWidthOfPlain(s) }
func SliceColumns(plain string, from, to int) string { return sliceColumns(plain, from, to) }
```

(These are needed in Task 7's selection extraction code.)

The existing `cached := m.cache[i]` and `lipgloss.Height(cached)` calls
inside `m.viewCacheValid` and `ClickAt` must be updated to use
`e.linesNormal` / `e.height`. Walk through each:

In `ClickAt`:

```go
func (m *Model) ClickAt(y int) {
	if len(m.replies) == 0 || len(m.cache) == 0 {
		return
	}
	absoluteY := y + m.vp.YOffset()
	currentLine := 0
	for _, e := range m.cache {
		h := e.height
		if h == 0 {
			h = 1
		}
		if absoluteY >= currentLine && absoluteY < currentLine+h {
			if m.selected != e.replyIdx {
				m.selected = e.replyIdx
				m.viewCacheValid = false
				m.dirty()
			}
			return
		}
		currentLine += h
	}
}
```

Inside the `viewCacheValid` rebuild block (the one that produces
`m.viewContent`), replace iterations over `m.cache` (which used to be
strings) with `e.linesNormal` joined back with `"\n"`:

```go
		for i, e := range m.cache {
			content := strings.Join(e.linesNormal, "\n")
			if i == m.selected {
				startLine = currentLine
				filled := borderFill.Width(width - 1).Render(content)
				content = borderSelect.Render(filled)
			} else {
				filled := borderFill.Width(width - 1).Render(content)
				content = borderInvis.Render(filled)
			}
			h := lipgloss.Height(content)
			if i == m.selected {
				endLine = currentLine + h
			}
			allRows = append(allRows, content)
			currentLine += h
			if i < len(m.cache)-1 {
				allRows = append(allRows, replySeparator)
				currentLine++
			}
		}
```

- [ ] **Step 4: Add the Selectable API to thread.Model**

Append (mirroring messages.Model exactly, adapted to the viewport):

```go
// SetThread / Clear / AddReply already call InvalidateCache. We add
// ClearSelection on SetThread/Clear so a new thread doesn't inherit a
// stale selection.

// (modify SetThread and Clear to call m.ClearSelection() at the top)

func (m *Model) BeginSelectionAt(viewportY, x int) {
	abs := m.absoluteLineAt(viewportY)
	a, ok := m.anchorAt(abs, x)
	if !ok {
		return
	}
	m.selRange = selection.Range{Start: a, End: a, Active: true}
	m.hasSelection = true
	m.dirty()
}

func (m *Model) ExtendSelectionAt(viewportY, x int) {
	if !m.hasSelection {
		return
	}
	abs := m.absoluteLineAt(viewportY)
	a, ok := m.anchorAt(abs, x)
	if !ok {
		return
	}
	m.selRange.End = a
	m.dirty()
}

func (m *Model) EndSelection() (string, bool) {
	if !m.hasSelection {
		return "", false
	}
	m.selRange.Active = false
	if m.selRange.IsEmpty() {
		m.hasSelection = false
		m.selRange = selection.Range{}
		m.dirty()
		return "", false
	}
	text := m.SelectionText()
	m.dirty()
	if text == "" {
		return "", false
	}
	return text, true
}

func (m *Model) ClearSelection() {
	if !m.hasSelection {
		return
	}
	m.hasSelection = false
	m.selRange = selection.Range{}
	m.dirty()
}

func (m *Model) HasSelection() bool { return m.hasSelection }

func (m *Model) ScrollHintForDrag(viewportY int) int {
	h := m.lastViewHeight
	if h <= 0 {
		return 0
	}
	if viewportY <= 0 {
		return -1
	}
	if viewportY >= h-1 {
		return +1
	}
	return 0
}

func (m *Model) absoluteLineAt(viewportY int) int {
	abs := viewportY + m.vp.YOffset()
	if abs < 0 {
		abs = 0
	}
	if m.totalLines > 0 && abs >= m.totalLines {
		abs = m.totalLines - 1
	}
	return abs
}

func (m *Model) anchorAt(absLine, col int) (selection.Anchor, bool) {
	for i, e := range m.cache {
		start := m.entryOffsets[i]
		end := start + e.height
		if absLine < start || absLine >= end {
			continue
		}
		lineIdx := absLine - start
		if col < 0 {
			col = 0
		}
		if lineIdx < len(e.linesPlain) {
			if w := messages.DisplayWidthOfPlain(e.linesPlain[lineIdx]); col > w {
				col = w
			}
		}
		var msgID string
		if e.replyIdx >= 0 && e.replyIdx < len(m.replies) {
			msgID = m.replies[e.replyIdx].TS
		}
		return selection.Anchor{MessageID: msgID, Line: lineIdx, Col: col}, true
	}
	return selection.Anchor{}, false
}

func (m *Model) resolveAnchor(a selection.Anchor) (absLine, col int, ok bool) {
	if a.MessageID == "" {
		return 0, 0, false
	}
	idx, found := m.replyIDToIdx[a.MessageID]
	if !found || idx >= len(m.cache) {
		return 0, 0, false
	}
	e := m.cache[idx]
	if a.Line < 0 || a.Line >= e.height {
		return 0, 0, false
	}
	return m.entryOffsets[idx] + a.Line, a.Col, true
}

func (m *Model) SelectionText() string {
	if !m.hasSelection || m.selRange.IsEmpty() {
		return ""
	}
	loA, hiA := m.selRange.Normalize()
	loLine, loCol, ok1 := m.resolveAnchor(loA)
	hiLine, hiCol, ok2 := m.resolveAnchor(hiA)
	if !ok1 || !ok2 {
		return ""
	}
	if loLine > hiLine || (loLine == hiLine && loCol >= hiCol) {
		return ""
	}
	var b strings.Builder
	for i, e := range m.cache {
		entryStart := m.entryOffsets[i]
		entryEnd := entryStart + e.height
		if entryEnd <= loLine {
			continue
		}
		if entryStart > hiLine {
			break
		}
		for j, plain := range e.linesPlain {
			absLine := entryStart + j
			if absLine < loLine || absLine > hiLine {
				if absLine > hiLine {
					break
				}
				continue
			}
			from := 0
			to := messages.DisplayWidthOfPlain(plain)
			if absLine == loLine {
				from = loCol
			}
			if absLine == hiLine {
				to = hiCol
			}
			if from < 0 { from = 0 }
			if to > messages.DisplayWidthOfPlain(plain) { to = messages.DisplayWidthOfPlain(plain) }
			if from < to {
				seg := messages.SliceColumns(plain, from, to)
				seg = strings.TrimRightFunc(seg, func(r rune) bool { return r == ' ' || r == messages.PlainWideSentinel })
				seg = strings.ReplaceAll(seg, string(messages.PlainWideSentinel), "")
				b.WriteString(seg)
			}
			if absLine != hiLine {
				b.WriteByte('\n')
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
```

In `SetThread` and `Clear`, prepend `m.ClearSelection()`.

In `View()` after computing `replyAreaHeight`:

```go
	m.lastViewHeight = replyAreaHeight
```

- [ ] **Step 5: Apply selection overlay in thread `View()`**

Because thread renders through bubbles/viewport (`m.vp.SetContent` then
`m.vp.View()`), we cannot easily intercept at the line level after
viewport renders. Easiest approach: build a parallel highlighted
content string when `m.hasSelection`, and pass that to the viewport.

After `m.vp.SetContent(m.viewContent)`, add:

```go
	if m.hasSelection {
		m.vp.SetContent(m.applySelectionOverlay(m.viewContent))
	}
```

Add `applySelectionOverlay` method:

```go
// applySelectionOverlay returns viewContent with selection-style applied
// to the visible columns of the active selection range. We work on the
// fully-built (bordered) content because the viewport will slice it for
// us; the overlay walks the same line indices the viewport does.
func (m *Model) applySelectionOverlay(content string) string {
	loA, hiA := m.selRange.Normalize()
	loLine, loCol, ok1 := m.resolveAnchor(loA)
	hiLine, hiCol, ok2 := m.resolveAnchor(hiA)
	if !ok1 || !ok2 || loLine > hiLine || (loLine == hiLine && loCol >= hiCol) {
		return content
	}
	selStyle := styles.SelectionStyle()
	lines := strings.Split(content, "\n")
	for absLine := loLine; absLine <= hiLine && absLine < len(lines); absLine++ {
		// The viewport's content line indices match m.entryOffsets math
		// because we fed it the same join order. But the bordered content
		// adds 1 column at the left for the thick border. Account for it
		// when slicing by columns.
		entryIdx := -1
		for i := range m.cache {
			start := m.entryOffsets[i]
			if absLine >= start && absLine < start+m.cache[i].height {
				entryIdx = i
				break
			}
		}
		if entryIdx < 0 {
			continue
		}
		e := m.cache[entryIdx]
		j := absLine - m.entryOffsets[entryIdx]
		if j < 0 || j >= len(e.linesPlain) {
			continue
		}
		plain := e.linesPlain[j]
		styled := lines[absLine]

		from := 0
		to := messages.DisplayWidthOfPlain(plain)
		if absLine == loLine { from = loCol }
		if absLine == hiLine { to = hiCol }
		if from < 0 { from = 0 }
		if to > messages.DisplayWidthOfPlain(plain) { to = messages.DisplayWidthOfPlain(plain) }
		if from >= to {
			continue
		}
		const borderOffset = 1 // thickLeftBorder is one column wide
		prefix := ansi.Cut(styled, 0, from+borderOffset)
		suffix := ansi.Cut(styled, to+borderOffset, ansi.StringWidth(styled))
		seg := messages.SliceColumns(plain, from, to)
		seg = strings.ReplaceAll(seg, string(messages.PlainWideSentinel), " ")
		lines[absLine] = prefix + selStyle.Render(seg) + suffix
	}
	return strings.Join(lines, "\n")
}
```

Add the import `"github.com/charmbracelet/x/ansi"` if not present.

- [ ] **Step 6: Run thread tests**

Run: `go test ./internal/ui/thread/... -count=1 -v`
Expected: all PASS (existing + new selection tests).

- [ ] **Step 7: Run messages tests too** (the new exports must still build)

Run: `go test ./internal/ui/messages/... -count=1`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/messages/ internal/ui/thread/
git commit -m "feat(thread): add Selectable API mirroring messages pane"
```

---

## Task 8: app — drag FSM + click/motion/release wiring

**Files:**
- Modify: `internal/ui/app.go`
- Create: `internal/ui/app_selection_test.go`

The drag state lives on `App`. We translate pane-relative coordinates
using the existing `layoutSidebarEnd` / `layoutMsgEnd` / `layoutThreadEnd`
boundaries (the same hit-testing as `MouseClickMsg`).

- [ ] **Step 1: Write failing test**

Create `internal/ui/app_selection_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/statusbar"
)

func newTestAppWithMessages(t *testing.T) *App {
	t.Helper()
	a := NewApp()
	a.width = 120
	a.height = 30
	a.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", UserID: "U1", Text: "hello world", Timestamp: "1:00 PM"},
		{TS: "2.0", UserName: "bob", UserID: "U2", Text: "second message", Timestamp: "1:01 PM"},
	})
	// Force a render so layout offsets and caches are populated.
	_ = a.View()
	return a
}

// SetMessages helper for the test (only used here).
func (a *App) SetMessages(msgs []messages.MessageItem) {
	a.messagepane.SetMessages(msgs)
}

func TestApp_DragInMessagesCopiesToClipboard(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layoutSidebarEnd + 2
	pressY := 2
	// Press
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	// Motion
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX + 10, Y: pressY + 1, Button: tea.MouseLeft})
	// Release
	_, cmd := a.Update(tea.MouseReleaseMsg{X: pressX + 10, Y: pressY + 1, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("expected a command on release")
	}
	msgs := drainBatch(cmd)
	var sawClipboard, sawCopiedToast bool
	for _, m := range msgs {
		switch v := m.(type) {
		case tea.SetClipboardMsg:
			if !strings.Contains(string(v), "hello") && !strings.Contains(string(v), "alice") {
				t.Errorf("clipboard payload looks empty: %q", string(v))
			}
			sawClipboard = true
		case statusbar.CopiedMsg:
			if v.N <= 0 {
				t.Errorf("CopiedMsg.N = %d", v.N)
			}
			sawCopiedToast = true
		}
	}
	if !sawClipboard {
		t.Error("expected tea.SetClipboardMsg in batched output")
	}
	if !sawCopiedToast {
		t.Error("expected statusbar.CopiedMsg in batched output")
	}
}

func TestApp_PlainClickDoesNotCopy(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layoutSidebarEnd + 2
	pressY := 2
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	_, cmd := a.Update(tea.MouseReleaseMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	for _, m := range drainBatch(cmd) {
		if _, ok := m.(tea.SetClipboardMsg); ok {
			t.Fatal("plain click must not write to clipboard")
		}
	}
}

// drainBatch collects messages from a tea.Batch result by invoking the
// underlying Cmds. It is best-effort — sufficient for asserting the
// types we care about.
func drainBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch v := msg.(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range v {
			if c == nil { continue }
			out = append(out, drainBatch(c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}
```

- [ ] **Step 2: Run, expect failure (or compile error)**

Run: `go test ./internal/ui/... -run TestApp_DragInMessagesCopiesToClipboard -count=1`
Expected: FAIL.

- [ ] **Step 3: Add `dragState` and the panel hit-test helper**

In `internal/ui/app.go`, near the other types, add:

```go
type dragState struct {
	panel            Panel  // PanelMessages or PanelThread; PanelWorkspace == idle
	pressX, pressY   int
	lastX, lastY     int
	moved            bool
	autoScrollActive bool
}
```

Add a field on `App`:

```go
	drag dragState
```

Add a helper to identify the pane at an absolute (x,y):

```go
func (a *App) panelAt(x, y int) (Panel, int, int, bool) {
	if y >= a.height-1 {
		return PanelWorkspace, 0, 0, false // status bar; selection ignored
	}
	switch {
	case x < a.layoutRailWidth:
		return PanelWorkspace, 0, 0, false
	case a.sidebarVisible && x < a.layoutSidebarEnd:
		return PanelSidebar, 0, 0, false
	case x < a.layoutMsgEnd:
		// Messages pane content starts at layoutSidebarEnd+1 (left border)
		// and y starts at 1 (top border).
		return PanelMessages, x - a.layoutSidebarEnd - 1, y - 1, true
	case a.threadVisible && x < a.layoutThreadEnd:
		return PanelThread, x - a.layoutMsgEnd - 1, y - 1, true
	}
	return PanelWorkspace, 0, 0, false
}
```

- [ ] **Step 4: Hook MouseClickMsg, MouseMotionMsg, MouseReleaseMsg**

In the `tea.MouseClickMsg` arm of `App.Update`, after the existing
`a.focusedPanel = ...` / `ClickAt` logic but before the closing brace,
seed the drag state when the click lands on messages or thread:

Replace the existing messages/thread arms in `MouseClickMsg`:

```go
		} else if x < a.layoutMsgEnd {
			a.focusedPanel = PanelMessages
			panel, px, py, ok := a.panelAt(msg.X, msg.Y)
			if ok && panel == PanelMessages && py >= 0 {
				a.drag = dragState{panel: PanelMessages, pressX: px, pressY: py, lastX: px, lastY: py}
				a.messagepane.BeginSelectionAt(py, px)
				a.messagepane.ClickAt(py)
			}
		} else if a.threadVisible && x < a.layoutThreadEnd {
			a.focusedPanel = PanelThread
			panel, px, py, ok := a.panelAt(msg.X, msg.Y)
			if ok && panel == PanelThread && py >= 0 {
				a.drag = dragState{panel: PanelThread, pressX: px, pressY: py, lastX: px, lastY: py}
				a.threadPanel.BeginSelectionAt(py, px)
				a.threadPanel.ClickAt(py)
			}
		}
```

Add a `tea.MouseMotionMsg` case immediately below `tea.MouseClickMsg`:

```go
	case tea.MouseMotionMsg:
		if a.loading || msg.Button != tea.MouseLeft {
			break
		}
		if a.drag.panel == PanelMessages || a.drag.panel == PanelThread {
			panel, px, py, _ := a.panelAt(msg.X, msg.Y)
			// Clamp to the originating pane's bounding rect.
			if panel != a.drag.panel {
				py = a.drag.lastY
				px = a.drag.lastX
			}
			a.drag.lastX, a.drag.lastY = px, py
			a.drag.moved = true
			switch a.drag.panel {
			case PanelMessages:
				a.messagepane.ExtendSelectionAt(py, px)
			case PanelThread:
				a.threadPanel.ExtendSelectionAt(py, px)
			}
		}
```

(Auto-scroll tick is added in Task 9.)

Add a `tea.MouseReleaseMsg` case:

```go
	case tea.MouseReleaseMsg:
		if a.drag.panel != PanelMessages && a.drag.panel != PanelThread {
			break
		}
		moved := a.drag.moved
		panel := a.drag.panel
		a.drag = dragState{}
		if !moved {
			// Plain click — clear any pinned selection.
			switch panel {
			case PanelMessages:
				a.messagepane.ClearSelection()
			case PanelThread:
				a.threadPanel.ClearSelection()
			}
			break
		}
		var text string
		var ok bool
		switch panel {
		case PanelMessages:
			text, ok = a.messagepane.EndSelection()
		case PanelThread:
			text, ok = a.threadPanel.EndSelection()
		}
		if ok && text != "" {
			n := len([]rune(text))
			cmds = append(cmds, tea.SetClipboard(text))
			cmds = append(cmds, func() tea.Msg { return statusbar.CopiedMsg{N: n} })
		}
```

- [ ] **Step 5: Handle `statusbar.CopiedMsg` in App.Update**

Add a case:

```go
	case statusbar.CopiedMsg:
		a.statusbar.ShowCopied(msg.N)
		cmds = append(cmds, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}))

	case statusbar.CopiedClearMsg:
		a.statusbar.ClearCopied()
```

- [ ] **Step 6: Run app tests**

Run: `go test ./internal/ui/... -count=1`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/app.go internal/ui/app_selection_test.go
git commit -m "feat(app): wire mouse drag selection to clipboard + status toast"
```

---

## Task 9: app — auto-scroll while dragging near pane edges

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Write failing test**

Append to `internal/ui/app_selection_test.go`:

```go
func TestApp_DragNearTopEdgeSchedulesAutoScroll(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layoutSidebarEnd + 2
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: 5, Button: tea.MouseLeft})
	// Move to row 0 (top edge of messages pane content).
	_, cmd := a.Update(tea.MouseMotionMsg{X: pressX, Y: 1, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("expected an auto-scroll tick command on edge motion")
	}
	// The tick should produce an autoScrollTickMsg-typed message.
	msg := cmd()
	if _, ok := msg.(autoScrollTickMsg); !ok {
		t.Fatalf("expected autoScrollTickMsg, got %T", msg)
	}
}
```

- [ ] **Step 2: Run, expect failure**

Run: `go test ./internal/ui/... -run AutoScroll -count=1`
Expected: FAIL — undefined `autoScrollTickMsg`.

- [ ] **Step 3: Add the tick type and emit ticks from MouseMotion**

In `internal/ui/app.go`, near the other internal types, add:

```go
type autoScrollTickMsg struct{}
```

Inside the `tea.MouseMotionMsg` arm, after `a.drag.moved = true` and the
ExtendSelectionAt call, add:

```go
		var hint int
		switch a.drag.panel {
		case PanelMessages:
			hint = a.messagepane.ScrollHintForDrag(py)
		case PanelThread:
			hint = a.threadPanel.ScrollHintForDrag(py)
		}
		if hint != 0 && !a.drag.autoScrollActive {
			a.drag.autoScrollActive = true
			cmds = append(cmds, tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
				return autoScrollTickMsg{}
			}))
		}
```

Add a handler:

```go
	case autoScrollTickMsg:
		if a.drag.panel != PanelMessages && a.drag.panel != PanelThread {
			a.drag.autoScrollActive = false
			break
		}
		hint := 0
		switch a.drag.panel {
		case PanelMessages:
			hint = a.messagepane.ScrollHintForDrag(a.drag.lastY)
		case PanelThread:
			hint = a.threadPanel.ScrollHintForDrag(a.drag.lastY)
		}
		if hint == 0 {
			a.drag.autoScrollActive = false
			break
		}
		switch a.drag.panel {
		case PanelMessages:
			if hint < 0 {
				a.messagepane.ScrollUp(1)
			} else {
				a.messagepane.ScrollDown(1)
			}
			a.messagepane.ExtendSelectionAt(a.drag.lastY, a.drag.lastX)
		case PanelThread:
			if hint < 0 {
				a.threadPanel.ScrollUp(1)
			} else {
				a.threadPanel.ScrollDown(1)
			}
			a.threadPanel.ExtendSelectionAt(a.drag.lastY, a.drag.lastX)
		}
		cmds = append(cmds, tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
			return autoScrollTickMsg{}
		}))
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/... -count=1`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/app_selection_test.go
git commit -m "feat(app): auto-scroll panes while dragging near top/bottom edge"
```

---

## Task 10: app — clear selection on focus mutations

**Files:**
- Modify: `internal/ui/app.go`

The spec calls for selection to clear on channel switch, mode-change
keys, and any focus-mutating handler. The big ones live in
`ChannelSelectedMsg` (already calls `SetMessages(nil)` which clears
selection per Task 5), `CloseThread`, `WorkspaceSwitchedMsg`,
`SetMode(ModeInsert)`, `FocusNext`/`FocusPrev`, `ToggleSidebar`,
`ToggleThread`.

`SetMessages(nil)` already clears messages-pane selection. We need to
add explicit thread-side clears and one shared helper.

- [ ] **Step 1: Add helper**

Add to `internal/ui/app.go`:

```go
// clearSelections removes any active mouse selection from the messages
// and thread panes. Called from any handler that mutates focus, mode,
// or visible content in a way that makes the existing selection
// nonsensical (channel switch, thread close, mode change, etc.).
func (a *App) clearSelections() {
	a.messagepane.ClearSelection()
	a.threadPanel.ClearSelection()
}
```

- [ ] **Step 2: Call it in focus-mutating sites**

- `CloseThread()`: add `a.clearSelections()` at the top.
- `ToggleSidebar()`: add `a.clearSelections()`.
- `ToggleThread()`: add `a.clearSelections()`.
- `SetMode(mode Mode)`: only when `mode == ModeInsert`, call `a.clearSelections()`.
- `FocusNext` and `FocusPrev`: add `a.clearSelections()` at the top.
- `WorkspaceSwitchedMsg` arm: add `a.clearSelections()` near `a.CloseThread()`.

(Channel switch already clears via `SetMessages(nil)` from Task 5.)

- [ ] **Step 3: Test**

Append to `internal/ui/app_selection_test.go`:

```go
func TestApp_FocusNextClearsSelection(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layoutSidebarEnd + 2
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: 2, Button: tea.MouseLeft})
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX + 5, Y: 2, Button: tea.MouseLeft})
	if !a.messagepane.HasSelection() {
		t.Fatal("precondition: should have selection")
	}
	a.FocusNext()
	if a.messagepane.HasSelection() {
		t.Fatal("FocusNext must clear selection")
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/... -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/app_selection_test.go
git commit -m "feat(app): clear text selection on focus / mode / panel changes"
```

---

## Task 11: README documentation

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add a paragraph in the "Messaging" feature list and a short troubleshooting note.**

In the Messaging bullet list (around line 28, after "ANSI-aware wrapping..."),
insert:

```markdown
- Drag-to-copy: drag the mouse across messages to highlight them; release to copy plain text to the system clipboard via OSC 52
```

Append a new section after the "Connectivity" feature list (or wherever
fits the README's flow — see `## Tradeoffs & Non-Goals` for placement
reference). Add at the end of the file, before `## License`:

```markdown
## Clipboard / OSC 52 caveats

slk writes the system clipboard via the OSC 52 escape. Most modern
terminal emulators (alacritty, kitty, wezterm, foot, iterm2, recent
gnome-terminal) accept these writes by default. A few need explicit
opt-in:

- **tmux:** `set -g set-clipboard on` in your tmux config.
- **screen:** has no working OSC 52 path; consider switching to tmux.
- **kitty (older versions):** `clipboard_control write-clipboard` in
  `kitty.conf`.

If `Copied N chars` shows in the status bar but nothing lands in your
clipboard, your terminal is silently dropping the OSC 52 write. There
is no reliable round-trip to detect this from inside slk.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs(readme): document drag-to-copy and OSC 52 caveats"
```

---

## Task 12: Final verification

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -count=1`
Expected: all PASS.

- [ ] **Step 2: Build the binary**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Manual smoke test (interactive)**

Launch `./bin/slk` (after `make build`), open a channel with a few
messages, click-and-drag across some text, release, and confirm:

- The drag region is visibly highlighted.
- Releasing shows "Copied N chars" in the status bar for ~2 seconds.
- Pasting in another app yields the expected plain text (no ANSI, no
  Slack `<@U…>` syntax).
- A plain click still selects the message normally and does not write
  to the clipboard.
- Switching channels with `Ctrl+t` clears any visible selection.

- [ ] **Step 4: Verify no untracked files**

Run: `git status`
Expected: clean working tree.

- [ ] **Step 5: Push the branch**

Run: `git push -u origin feat/message-selection`
Expected: branch pushed; PR can be created with `gh pr create`.

---

## Self-Review Notes

- **Spec coverage:** All sections of the design spec are mapped:
  - "User-Visible Behavior" → Tasks 5/6/7/8/9/10
  - "Architecture / A. selection package" → Task 1
  - "Architecture / B. Selectable on messages.Model" → Tasks 4, 5, 6
  - "Architecture / B. Selectable on thread.Model" → Task 7
  - "Architecture / C. App-level glue" → Tasks 8, 9, 10
  - "Rendering Selection Highlight" → Tasks 6, 7
  - "Plain-Text Extraction & Clipboard" → Tasks 4, 5, 7, 8
  - "Anchoring Across Scroll & New Messages" → Task 5 (test) + Task 4 (cache map)
  - "Files Touched" — every entry has a task.
- **Placeholders:** None present (`TBD`, `TODO`, "implement later", etc. all absent).
- **Type consistency:** `BeginSelectionAt`, `ExtendSelectionAt`, `EndSelection`,
  `ClearSelection`, `HasSelection`, `SelectionText`, `ScrollHintForDrag`,
  `Anchor`, `Range`, `LessOrEqual`, `Normalize`, `IsEmpty`, `Contains`,
  `plainLines`/`PlainLines`, `displayWidthOfPlain`/`DisplayWidthOfPlain`,
  `sliceColumns`/`SliceColumns`, `plainWideSentinel`/`PlainWideSentinel`,
  `dragState`, `autoScrollTickMsg`, `statusbar.CopiedMsg`,
  `statusbar.CopiedClearMsg`, `SelectionStyle`, `SelectionBackground`,
  `SelectionForeground`, `SelectionBackground`/`SelectionForeground`
  ThemeColors fields are used consistently across all tasks.
