# Reflow + Viewport Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace custom text wrapping, truncation, padding, and scroll logic with `muesli/reflow` and `bubbles/viewport`.

**Architecture:** Adopt `reflow/wordwrap` for message text wrapping, `reflow/truncate` for ANSI-safe string truncation, `reflow/padding` for selection highlight padding, and `bubbles/viewport` as the scroll container for messages, thread, and sidebar panels. Each panel keeps its item-level `selected` index; the viewport is used purely as a rendering container with manual `SetYOffset` to keep the selected item visible.

**Tech Stack:** Go, muesli/reflow, charmbracelet/bubbles/viewport, lipgloss

---

### Task 1: Add muesli/reflow Dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/muesli/reflow@latest
```

- [ ] **Step 2: Verify it resolves**

Run: `go mod tidy`
Expected: `go.sum` updated, no errors

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add muesli/reflow for text wrapping, padding, and truncation"
```

---

### Task 2: Replace Truncation with reflow/truncate

**Files:**
- Modify: `internal/ui/channelfinder/model.go:221-223`
- Modify: `internal/ui/sidebar/model.go:174-175`

- [ ] **Step 1: Fix channelfinder truncation (active bug)**

In `internal/ui/channelfinder/model.go`, add import:

```go
"github.com/muesli/reflow/truncate"
```

Replace line 221-223:

```go
		if len(line) > innerWidth {
			line = line[:innerWidth-1] + "…"
		}
```

With:

```go
		if lipgloss.Width(line) > innerWidth {
			line = truncate.StringWithTail(line, uint(innerWidth), "…")
		}
```

This fixes the bug where byte-level slicing corrupts ANSI escape codes from `channelPrefix()` styling.

- [ ] **Step 2: Fix sidebar truncation**

In `internal/ui/sidebar/model.go`, add import:

```go
"github.com/muesli/reflow/truncate"
```

Replace line 174-175:

```go
		if len(name) > maxNameLen {
			name = name[:maxNameLen-1] + "…"
		}
```

With:

```go
		if len(name) > maxNameLen {
			name = truncate.StringWithTail(name, uint(maxNameLen), "…")
		}
```

- [ ] **Step 3: Verify it compiles and tests pass**

Run: `go build ./internal/ui/... && go test ./internal/ui/... -v`
Expected: All pass

- [ ] **Step 4: Commit**

```bash
git add internal/ui/channelfinder/model.go internal/ui/sidebar/model.go
git commit -m "fix: use reflow/truncate for ANSI-safe string truncation"
```

---

### Task 3: Replace Text Wrapping with reflow/wordwrap

**Files:**
- Modify: `internal/ui/messages/model.go:230, 311`
- Modify: `internal/ui/thread/model.go:271`

- [ ] **Step 1: Update messages pane text wrapping**

In `internal/ui/messages/model.go`, add import:

```go
"github.com/muesli/reflow/wordwrap"
```

Replace line 230:

```go
	text := styles.MessageText.Width(contentWidth).Render(RenderSlackMarkdown(msg.Text, userNames))
```

With:

```go
	text := styles.MessageText.Render(wordwrap.String(RenderSlackMarkdown(msg.Text, userNames), contentWidth))
```

Replace line 311:

```go
		header += "\n" + styles.Timestamp.Width(width).Render(m.channelTopic)
```

With:

```go
		header += "\n" + styles.Timestamp.Render(wordwrap.String(m.channelTopic, width))
```

- [ ] **Step 2: Update thread panel text wrapping**

In `internal/ui/thread/model.go`, add import:

```go
"github.com/muesli/reflow/wordwrap"
```

Replace line 271 (inside `renderThreadMessage`):

```go
	text := styles.MessageText.Width(contentWidth).Render(messages.RenderSlackMarkdown(msg.Text, userNames))
```

With:

```go
	text := styles.MessageText.Render(wordwrap.String(messages.RenderSlackMarkdown(msg.Text, userNames), contentWidth))
```

- [ ] **Step 3: Verify it compiles and tests pass**

Run: `go build ./internal/ui/... && go test ./internal/ui/... -v`
Expected: All pass

- [ ] **Step 4: Commit**

```bash
git add internal/ui/messages/model.go internal/ui/thread/model.go
git commit -m "refactor: use reflow/wordwrap for ANSI-aware message text wrapping"
```

---

### Task 4: Replace Selection Highlight Padding with reflow/padding

**Files:**
- Modify: `internal/ui/thread/model.go:244-259` (the `applySelection` function)
- Modify: `internal/ui/messages/model.go:298-302` (the `applySelection` function)

- [ ] **Step 1: Update thread panel applySelection**

In `internal/ui/thread/model.go`, add import:

```go
"github.com/muesli/reflow/padding"
```

Replace the entire `applySelection` function:

```go
// applySelection highlights a message by padding each line to full width
// and applying the selection background color.
func applySelection(content string, width int) string {
	padded := padding.String(content, uint(width))
	lines := strings.Split(padded, "\n")
	bg := selectedBg
	for i, line := range lines {
		lines[i] = bg.Render(line)
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 2: Update messages pane applySelection**

In `internal/ui/messages/model.go`, add import:

```go
"github.com/muesli/reflow/padding"
```

Replace the `applySelection` function:

```go
// applySelection wraps a rendered message with selection highlight.
func applySelection(content string, width int) string {
	padded := padding.String(content, uint(width))
	lines := strings.Split(padded, "\n")
	bg := selectedBg
	for i, line := range lines {
		lines[i] = bg.Render(line)
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 3: Verify it compiles and tests pass**

Run: `go build ./internal/ui/... && go test ./internal/ui/... -v`
Expected: All pass

- [ ] **Step 4: Commit**

```bash
git add internal/ui/thread/model.go internal/ui/messages/model.go
git commit -m "refactor: use reflow/padding for selection highlight background"
```

---

### Task 5: Add viewport to Messages Pane

**Files:**
- Modify: `internal/ui/messages/model.go`

This is the largest task. It replaces ~120 lines of custom viewport logic (lines 340-457) with `bubbles/viewport`.

- [ ] **Step 1: Add viewport field to Model**

Add import:

```go
"github.com/charmbracelet/bubbles/viewport"
```

Remove the `offset` field from the `Model` struct (line 53). Remove `math` from imports (no longer needed for `math.MaxInt32`).

Add a viewport field:

```go
type Model struct {
	messages     []MessageItem
	selected     int
	channelName  string
	channelTopic string
	loading      bool
	avatarFn     AvatarFunc
	userNames    map[string]string

	// Render cache
	cache       []viewEntry
	cacheWidth  int
	cacheMsgLen int

	// Viewport for scrolling
	vp viewport.Model
}
```

- [ ] **Step 2: Remove all references to m.offset**

In `SetMessages()`, remove `m.offset = 0` and `m.offset = math.MaxInt32` lines.

In `AppendMessage()`, remove `m.offset = math.MaxInt32`.

In `GoToTop()`, remove `m.offset = 0`.

- [ ] **Step 3: Replace the View() method**

Replace the entire section from `// Find the entry index` (line 340) through the end of the `msgContent` render (line 457) with viewport-based rendering:

```go
	// Rebuild cache if messages or width changed
	if m.cache == nil || m.cacheWidth != width || m.cacheMsgLen != len(m.messages) {
		m.buildCache(width)
	}

	entries := m.cache

	// Build the full content string, tracking line offsets per entry
	var allRows []string
	selectedStartLine := 0
	selectedEndLine := 0
	currentLine := 0

	for _, e := range entries {
		content := e.content
		if e.msgIdx == m.selected {
			selectedStartLine = currentLine
			content = applySelection(content, width)
		}
		h := lipgloss.Height(content)
		if e.msgIdx == m.selected {
			selectedEndLine = currentLine + h
		}
		allRows = append(allRows, content)
		currentLine += h
	}

	fullContent := strings.Join(allRows, "\n")

	// Configure viewport
	m.vp.Width = width
	m.vp.Height = msgAreaHeight
	m.vp.SetContent(fullContent)

	// Scroll to keep selected item visible
	// Try to center the selected item, but at minimum ensure it's fully visible
	if selectedEndLine > m.vp.YOffset+m.vp.Height {
		// Selected is below viewport -- scroll down
		m.vp.SetYOffset(selectedEndLine - m.vp.Height)
	}
	if selectedStartLine < m.vp.YOffset {
		// Selected is above viewport -- scroll up
		m.vp.SetYOffset(selectedStartLine)
	}

	// Scroll indicators
	var scrollUp, scrollDown string
	if m.vp.YOffset > 0 {
		indicator := "  -- more above --"
		if m.loading {
			indicator = "  Loading older messages..."
		}
		scrollUp = lipgloss.NewStyle().Foreground(styles.TextMuted).Render(indicator)
	} else if m.loading {
		scrollUp = lipgloss.NewStyle().Foreground(styles.TextMuted).Render("  Loading older messages...")
	}

	if m.vp.YOffset+m.vp.Height < m.vp.TotalLineCount() {
		scrollDown = lipgloss.NewStyle().Foreground(styles.TextMuted).Render("  -- more below --")
	}

	vpView := m.vp.View()
	if scrollUp != "" {
		vpView = scrollUp + "\n" + vpView
	}
	if scrollDown != "" {
		vpView = vpView + "\n" + scrollDown
	}

	return header + "\n" + separator + "\n" + lipgloss.NewStyle().
		Width(width).
		Height(msgAreaHeight).
		MaxHeight(msgAreaHeight).
		Render(vpView)
```

- [ ] **Step 4: Disable viewport key handling**

In `New()` or at the top of `View()`, ensure the viewport doesn't handle keys by zeroing its keymap:

```go
	m.vp.KeyMap = viewport.KeyMap{} // disable all default keys
```

Do this when the viewport is first used (e.g., at the start of `View()` before setting content).

- [ ] **Step 5: Verify it compiles and tests pass**

Run: `go build ./internal/ui/... && go test ./internal/ui/... -v`
Expected: All pass

- [ ] **Step 6: Commit**

```bash
git add internal/ui/messages/model.go
git commit -m "refactor: use bubbles/viewport for messages pane scrolling"
```

---

### Task 6: Add viewport to Thread Panel

**Files:**
- Modify: `internal/ui/thread/model.go`

- [ ] **Step 1: Add viewport field**

Add import:

```go
"github.com/charmbracelet/bubbles/viewport"
```

Add to `Model` struct:

```go
	vp viewport.Model
```

- [ ] **Step 2: Replace the reply rendering section of View()**

Replace the custom scroll logic (from `// Render replies with viewport scrolling` through the double-height-constraint return at the end of `View()`) with viewport-based rendering:

```go
	// Pre-render all replies, tracking line offsets
	var allRows []string
	selectedStartLine := 0
	selectedEndLine := 0
	currentLine := 0

	for i, reply := range m.replies {
		content := renderThreadMessage(reply, width, m.userNames)
		if i == m.selected {
			selectedStartLine = currentLine
			content = applySelection(content, width)
		}
		h := lipgloss.Height(content)
		if i == m.selected {
			selectedEndLine = currentLine + h
		}
		allRows = append(allRows, content)
		currentLine += h
	}

	fullContent := strings.Join(allRows, "\n")

	// Configure viewport
	m.vp.Width = width
	m.vp.Height = replyAreaHeight
	m.vp.KeyMap = viewport.KeyMap{}
	m.vp.SetContent(fullContent)

	// Scroll to keep selected item visible
	if selectedEndLine > m.vp.YOffset+m.vp.Height {
		m.vp.SetYOffset(selectedEndLine - m.vp.Height)
	}
	if selectedStartLine < m.vp.YOffset {
		m.vp.SetYOffset(selectedStartLine)
	}

	result := chrome + "\n" + m.vp.View()
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(result)
```

Remove the now-unused `visibleRows`, `usedHeight`, `renderedEntry` type, `showIndices` variables.

- [ ] **Step 3: Verify it compiles and tests pass**

Run: `go build ./internal/ui/... && go test ./internal/ui/... -v`
Expected: All pass

- [ ] **Step 4: Commit**

```bash
git add internal/ui/thread/model.go
git commit -m "refactor: use bubbles/viewport for thread panel scrolling"
```

---

### Task 7: Add viewport to Sidebar

**Files:**
- Modify: `internal/ui/sidebar/model.go`

- [ ] **Step 1: Add viewport field, remove offset**

Add import:

```go
"github.com/charmbracelet/bubbles/viewport"
```

Remove `offset int` from the `Model` struct (line 24).

Add:

```go
	vp viewport.Model
```

Remove all assignments to `m.offset` throughout the file:
- Line 41: `m.offset = 0` in `SetItems()`
- Line 74: `m.offset = 0` in `GoToTop()`
- Line 91: `m.offset = 0` in `SetFilter()`

- [ ] **Step 2: Replace the scroll logic in View()**

Replace lines 238-287 (from `// Adjust offset to keep selected row visible` through the final `Render(content)`) with:

```go
	// Build full content from all rows
	var allLines []string
	selectedStartLine := 0
	selectedEndLine := 0
	currentLine := 0

	for _, r := range allRows {
		if r.filterIdx == m.selected {
			selectedStartLine = currentLine
		}
		h := lipgloss.Height(r.content)
		if r.filterIdx == m.selected {
			selectedEndLine = currentLine + h
		}
		allLines = append(allLines, r.content)
		currentLine += h
	}

	fullContent := strings.Join(allLines, "\n")

	// Configure viewport
	m.vp.Width = width
	m.vp.Height = height
	m.vp.KeyMap = viewport.KeyMap{}
	m.vp.SetContent(fullContent)

	// Scroll to keep selected row visible
	if selectedEndLine > m.vp.YOffset+m.vp.Height {
		m.vp.SetYOffset(selectedEndLine - m.vp.Height)
	}
	if selectedStartLine < m.vp.YOffset {
		m.vp.SetYOffset(selectedStartLine)
	}

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxHeight(height).
		Background(styles.Surface).
		Render(m.vp.View())
```

- [ ] **Step 3: Verify it compiles and tests pass**

Run: `go build ./internal/ui/... && go test ./internal/ui/... -v`
Expected: All pass

- [ ] **Step 4: Commit**

```bash
git add internal/ui/sidebar/model.go
git commit -m "refactor: use bubbles/viewport for sidebar scrolling"
```

---

### Task 8: Full Build and Test Verification

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v`
Expected: All pass

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 3: Build the binary**

Run: `make build`
Expected: Binary at `bin/slack-tui`

- [ ] **Step 4: Run go mod tidy to clean up**

Run: `go mod tidy`
Expected: No changes or only cleanup of unused indirect deps

- [ ] **Step 5: Commit if any cleanup was needed**

Only commit if `go mod tidy` or vet fixes required changes.
