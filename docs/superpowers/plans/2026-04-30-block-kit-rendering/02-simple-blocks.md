# Phase 2: Simple Blocks

> See `00-overview.md` for goal, architecture, and conventions. Phase 1 must be complete before this phase begins.

This phase fills in `Render` for the non-image, non-interactive blocks: `divider`, `header`, `unsupported`, `section` (text + fields + non-image accessory), and `context`. The output is fully testable with `ansi.Strip` substring assertions. After this phase, calling `blockkit.Render(...)` on any of these blocks returns sensible lines, but nothing in the app calls it yet.

---

## Task 5: Render `divider`, `header`, and `unsupported` blocks

**Files:**
- Modify: `internal/ui/messages/blockkit/render.go`
- Create: `internal/ui/messages/blockkit/styles.go`
- Create: `internal/ui/messages/blockkit/render_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/ui/messages/blockkit/render_test.go
package blockkit

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderEmptyBlocksProducesNoLines(t *testing.T) {
	r := Render(nil, Context{}, 80)
	if r.Height != 0 || len(r.Lines) != 0 {
		t.Errorf("got Height=%d Lines=%d, want 0/0", r.Height, len(r.Lines))
	}
}

func TestRenderDividerProducesHorizontalRule(t *testing.T) {
	r := Render([]Block{DividerBlock{}}, Context{}, 20)
	if r.Height != 1 {
		t.Fatalf("Height = %d, want 1", r.Height)
	}
	plain := ansi.Strip(r.Lines[0])
	// Divider should be exactly the available width, all dash-equivalent
	// glyphs. We accept either "─" or "-" as the divider rune.
	if len([]rune(plain)) != 20 {
		t.Errorf("rune width = %d, want 20", len([]rune(plain)))
	}
	for _, r := range plain {
		if r != '─' && r != '-' {
			t.Errorf("unexpected rune %q in divider %q", r, plain)
			break
		}
	}
}

func TestRenderHeaderProducesBoldText(t *testing.T) {
	r := Render([]Block{HeaderBlock{Text: "Deploy successful"}}, Context{}, 80)
	if r.Height != 1 {
		t.Fatalf("Height = %d, want 1", r.Height)
	}
	plain := ansi.Strip(r.Lines[0])
	if !strings.Contains(plain, "Deploy successful") {
		t.Errorf("plain = %q, want it to contain header text", plain)
	}
}

func TestRenderHeaderTruncatesIfTooWide(t *testing.T) {
	long := strings.Repeat("X", 200)
	r := Render([]Block{HeaderBlock{Text: long}}, Context{}, 20)
	plain := ansi.Strip(r.Lines[0])
	// Width must not exceed cap. We allow ≤ 20.
	if len([]rune(plain)) > 20 {
		t.Errorf("rune width = %d, want <= 20", len([]rune(plain)))
	}
}

func TestRenderUnknownBlockShowsTypePlaceholder(t *testing.T) {
	r := Render([]Block{UnknownBlock{Type: "rich_text"}}, Context{}, 80)
	if r.Height != 1 {
		t.Fatalf("Height = %d", r.Height)
	}
	plain := ansi.Strip(r.Lines[0])
	if !strings.Contains(plain, "rich_text") {
		t.Errorf("plain = %q, want it to mention type", plain)
	}
	if !strings.Contains(plain, "[unsupported block:") {
		t.Errorf("plain = %q, want '[unsupported block:' marker", plain)
	}
}

func TestRenderMultipleBlocksConcatInOrder(t *testing.T) {
	r := Render([]Block{
		HeaderBlock{Text: "First"},
		DividerBlock{},
		HeaderBlock{Text: "Second"},
	}, Context{}, 40)
	if r.Height != 3 {
		t.Fatalf("Height = %d, want 3", r.Height)
	}
	if !strings.Contains(ansi.Strip(r.Lines[0]), "First") {
		t.Errorf("Lines[0] = %q", ansi.Strip(r.Lines[0]))
	}
	if !strings.Contains(ansi.Strip(r.Lines[2]), "Second") {
		t.Errorf("Lines[2] = %q", ansi.Strip(r.Lines[2]))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/ui/messages/blockkit/ -run "TestRender" -v
```

Expected: most tests fail because `Render` is a stub returning empty.

- [ ] **Step 3: Create styles.go**

```go
// internal/ui/messages/blockkit/styles.go
package blockkit

import (
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/ui/styles"
)

// Style accessors. We define them as functions (not vars) so they
// pick up theme changes — the styles package mutates its color
// vars on theme switch.

func headerStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Primary).
		Background(styles.Background)
}

func dividerStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(styles.Border).
		Background(styles.Background)
}

func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(styles.TextMuted).
		Background(styles.Background)
}

// controlStyle is the muted, non-interactive look for buttons,
// select menus, overflow, and other "you can see this exists but
// you can't drive it" elements.
func controlStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(styles.TextMuted).
		Background(styles.SurfaceDark)
}

// fieldLabelStyle is the bold-muted style used for field titles
// in section field grids and legacy attachment field grids.
func fieldLabelStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.TextMuted).
		Background(styles.Background)
}
```

- [ ] **Step 4: Implement Render() for divider/header/unknown**

Replace `render.go` with:

```go
// internal/ui/messages/blockkit/render.go
package blockkit

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// narrowBreakpoint is the width below which renderers collapse
// side-by-side layouts (section accessory, fields grid) to stacked
// single-column equivalents.
const narrowBreakpoint = 60

// Render produces a RenderResult for a slice of blocks at the given
// content width.
func Render(blocks []Block, ctx Context, width int) RenderResult {
	if len(blocks) == 0 || width <= 0 {
		return RenderResult{}
	}
	var out RenderResult
	for _, b := range blocks {
		appendBlock(&out, b, ctx, width)
	}
	out.Height = len(out.Lines)
	return out
}

// appendBlock dispatches one block to its renderer and appends the
// result onto out. Per-block renderers MUST produce lines that each
// consume <= width display columns.
func appendBlock(out *RenderResult, b Block, ctx Context, width int) {
	switch v := b.(type) {
	case DividerBlock:
		out.Lines = append(out.Lines, renderDivider(width))
	case HeaderBlock:
		out.Lines = append(out.Lines, renderHeader(v.Text, width))
	case UnknownBlock:
		out.Lines = append(out.Lines, renderUnsupported(v.Type, width))
	default:
		// Other block types (Section, Context, Image, Actions) are
		// added by later tasks; for now, render them as unsupported
		// so the package is total even mid-implementation.
		out.Lines = append(out.Lines, renderUnsupported(string(v.blockType()), width))
	}
}

func renderDivider(width int) string {
	return dividerStyle().Render(strings.Repeat("─", width))
}

func renderHeader(text string, width int) string {
	if text == "" {
		return ""
	}
	if w := lipgloss.Width(text); w > width {
		// Truncate by display width; leave 1 col for ellipsis when
		// space allows.
		text = truncateToWidth(text, width)
	}
	return headerStyle().Render(text)
}

func renderUnsupported(typeName string, width int) string {
	label := "[unsupported block: " + typeName + "]"
	if lipgloss.Width(label) > width {
		label = truncateToWidth(label, width)
	}
	return mutedStyle().Render(label)
}

// truncateToWidth returns s truncated by display columns to at most
// width. If truncation occurs and width >= 1, the last visible col
// is replaced with '…'.
func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	// Walk runes, accumulating display width, until we'd exceed
	// width-1 (leaving a column for the ellipsis).
	target := width - 1
	if target < 0 {
		target = 0
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if used+w > target {
			break
		}
		b.WriteRune(r)
		used += w
	}
	if width >= 1 {
		b.WriteRune('…')
	}
	return b.String()
}
```

All of the above goes into a single `render.go`. The unused-import check should pass since `lipgloss.Width` and `strings.Repeat`/`strings.Builder` are all used.

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/ui/messages/blockkit/ -run "TestRender" -v
```

Expected: all six tests PASS.

- [ ] **Step 6: Run full package tests**

```bash
go test ./internal/ui/messages/blockkit/ -v
```

Expected: PASS.

- [ ] **Step 7: Run `make build`**

- [ ] **Step 8: Commit**

```bash
git add internal/ui/messages/blockkit/
git commit -m "feat(blockkit): render divider, header, and unsupported blocks"
```

---

## Task 6: Render `section` block — body text and fields grid

**Files:**
- Modify: `internal/ui/messages/blockkit/render.go`
- Modify: `internal/ui/messages/blockkit/render_test.go`

This task ignores accessories — they come in Task 7.

The body text needs Slack-style markdown rendering. We don't want to depend on `internal/ui/messages` (would create an import cycle), so this task introduces a thin wrapper. Initial implementation: re-implement the markdown call at the import-cycle-free level by accepting a `RenderText` function on `Context`. The host wires it to `RenderSlackMarkdown`.

- [ ] **Step 1: Add a `RenderText` callback to `Context`**

In `types.go`, append to the `Context` struct:

```go
// RenderText converts Slack-flavored mrkdwn (with mentions/links/
// emoji shortcodes) to ANSI-styled text. The host wires this to
// internal/ui/messages.RenderSlackMarkdown. May be nil; when nil,
// raw text passes through unchanged.
RenderText func(s string, userNames map[string]string) string

// WrapText word-wraps an ANSI-styled string to the given display
// width. The host wires this to internal/ui/messages.WordWrap.
// When nil, text passes through unchanged.
WrapText func(s string, width int) string
```

- [ ] **Step 2: Add the failing tests for section text**

Append to `render_test.go`:

```go
func TestRenderSectionTextOnly(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, w int) string { return s },
	}
	r := Render([]Block{SectionBlock{Text: "Hello world"}}, ctx, 40)
	if r.Height < 1 {
		t.Fatalf("Height = %d, want >= 1", r.Height)
	}
	if !strings.Contains(ansi.Strip(strings.Join(r.Lines, "\n")), "Hello world") {
		t.Errorf("rendered = %q", ansi.Strip(strings.Join(r.Lines, "\n")))
	}
}

func TestRenderSectionUsesRenderTextCallback(t *testing.T) {
	called := false
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string {
			called = true
			return "[rendered]" + s
		},
		WrapText: func(s string, _ int) string { return s },
	}
	Render([]Block{SectionBlock{Text: "x"}}, ctx, 40)
	if !called {
		t.Error("RenderText callback was not invoked")
	}
}

func TestRenderSectionFieldsTwoColumnGrid(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{
		Fields: []string{
			"Service\nweb",
			"Region\nus-east-1",
			"Status\nfiring",
		},
	}}, ctx, 80)
	all := ansi.Strip(strings.Join(r.Lines, "\n"))
	for _, want := range []string{"Service", "web", "Region", "us-east-1", "Status", "firing"} {
		if !strings.Contains(all, want) {
			t.Errorf("rendered missing %q: %q", want, all)
		}
	}
	// 3 fields with 2-col grid → 2 rows: row 1 has Service+Region,
	// row 2 has Status (single field on a 2-col row).
	if r.Height < 2 {
		t.Errorf("Height = %d, want >= 2 (two grid rows)", r.Height)
	}
}

func TestRenderSectionFieldsCollapseAtNarrowWidth(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{
		Fields: []string{"A\n1", "B\n2"},
	}}, ctx, 30) // < narrowBreakpoint
	// Expected: stacked single-column. 2 fields, each typically 2 lines
	// (label + value). Asserting >= 2 rows as a loose bound; the strong
	// assertion is "no two field titles on the same row".
	all := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(all, "A") || !strings.Contains(all, "B") {
		t.Errorf("missing field titles: %q", all)
	}
	for _, line := range r.Lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "A") && strings.Contains(plain, "B") {
			t.Errorf("at narrow width, fields should be stacked but found both on one line: %q", plain)
		}
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test ./internal/ui/messages/blockkit/ -run "TestRenderSection" -v
```

Expected: FAIL — section is currently routed to `renderUnsupported`.

- [ ] **Step 4: Add section rendering**

In `render.go`, replace the `appendBlock` function and add helpers:

```go
func appendBlock(out *RenderResult, b Block, ctx Context, width int) {
	switch v := b.(type) {
	case DividerBlock:
		out.Lines = append(out.Lines, renderDivider(width))
	case HeaderBlock:
		out.Lines = append(out.Lines, renderHeader(v.Text, width))
	case SectionBlock:
		appendSection(out, v, ctx, width)
	case UnknownBlock:
		out.Lines = append(out.Lines, renderUnsupported(v.Type, width))
	default:
		out.Lines = append(out.Lines, renderUnsupported(string(v.blockType()), width))
	}
}

func appendSection(out *RenderResult, s SectionBlock, ctx Context, width int) {
	// Body text first (no accessory handling yet — Task 7 adds it).
	if s.Text != "" {
		out.Lines = append(out.Lines, renderTextLines(s.Text, ctx, width)...)
	}
	// Then fields grid.
	if len(s.Fields) > 0 {
		out.Lines = append(out.Lines, renderFieldsGrid(s.Fields, ctx, width)...)
	}
}

// renderTextLines runs text through Context.RenderText then wraps it
// to width via Context.WrapText, then splits on newline.
func renderTextLines(text string, ctx Context, width int) []string {
	rendered := text
	if ctx.RenderText != nil {
		rendered = ctx.RenderText(text, ctx.UserNames)
	}
	if ctx.WrapText != nil {
		rendered = ctx.WrapText(rendered, width)
	}
	return strings.Split(rendered, "\n")
}

// renderFieldsGrid lays fields out 2-up at width >= narrowBreakpoint,
// stacked otherwise. Each cell is wrapped to its column width.
func renderFieldsGrid(fields []string, ctx Context, width int) []string {
	if width < narrowBreakpoint {
		// Stacked: each field becomes its own block.
		var out []string
		for i, f := range fields {
			if i > 0 {
				out = append(out, "")
			}
			out = append(out, renderTextLines(f, ctx, width)...)
		}
		return out
	}
	// Two-column grid. Gutter is 2 cols.
	gutter := 2
	colW := (width - gutter) / 2
	if colW < 1 {
		colW = 1
	}
	var rows []string
	for i := 0; i < len(fields); i += 2 {
		left := renderTextLines(fields[i], ctx, colW)
		var right []string
		if i+1 < len(fields) {
			right = renderTextLines(fields[i+1], ctx, colW)
		}
		rows = append(rows, joinSideBySide(left, right, colW, gutter)...)
	}
	return rows
}

// joinSideBySide places `right` to the right of `left`, padding both
// to colW so the resulting rows are width-aligned. Shorter side is
// padded with blank lines to match the taller side's height.
func joinSideBySide(left, right []string, colW, gutter int) []string {
	height := len(left)
	if len(right) > height {
		height = len(right)
	}
	gap := strings.Repeat(" ", gutter)
	out := make([]string, 0, height)
	for i := 0; i < height; i++ {
		var l, r string
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		// Right-pad each side with bg spaces to colW. We measure
		// display width via lipgloss.Width to handle ANSI/wide runes.
		out = append(out, padRight(l, colW)+gap+padRight(r, colW))
	}
	return out
}

func padRight(s string, w int) string {
	cur := lipgloss.Width(s)
	if cur >= w {
		return s
	}
	bg := lipgloss.NewStyle().Background(lipgloss.NoColor{}).Render(strings.Repeat(" ", w-cur))
	return s + bg
}
```

(`lipgloss` is already imported.)

- [ ] **Step 5: Run tests**

```bash
go test ./internal/ui/messages/blockkit/ -v
```

Expected: all section tests PASS, prior tests still PASS.

- [ ] **Step 6: Run `make build`**

- [ ] **Step 7: Commit**

```bash
git add internal/ui/messages/blockkit/
git commit -m "feat(blockkit): render section blocks with text body and 2-column fields grid"
```

---

## Task 7: Render `section` block — non-image accessory (muted labels)

**Files:**
- Modify: `internal/ui/messages/blockkit/render.go`
- Modify: `internal/ui/messages/blockkit/render_test.go`

The accessory sits to the right of the body text at width >= 60, or below the body at narrower widths. This task only handles `LabelAccessory` (button/select/etc). Image accessories come in Phase 3.

- [ ] **Step 1: Add the failing tests**

Append to `render_test.go`:

```go
func TestRenderSectionWithButtonAccessory(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{
		Text:      "Ready to deploy?",
		Accessory: LabelAccessory{Kind: "button", Label: "Deploy"},
	}}, ctx, 80)
	all := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(all, "Ready to deploy?") {
		t.Errorf("missing body: %q", all)
	}
	if !strings.Contains(all, "[ Deploy ]") {
		t.Errorf("missing button label: %q", all)
	}
	// Side-by-side at width 80: body and button on at least one
	// shared row.
	foundShared := false
	for _, line := range r.Lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "Ready") && strings.Contains(plain, "Deploy") {
			foundShared = true
			break
		}
	}
	if !foundShared {
		t.Error("expected body and button on at least one shared row at width 80")
	}
	if !r.Interactive {
		t.Error("Interactive should be true after rendering a button accessory")
	}
}

func TestRenderSectionWithSelectAccessory(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{
		Text:      "Pick env:",
		Accessory: LabelAccessory{Kind: "static_select", Label: "production"},
	}}, ctx, 80)
	all := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(all, "production ▾") {
		t.Errorf("expected 'production ▾' in output, got %q", all)
	}
}

func TestRenderSectionWithOverflowAccessory(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{
		Text:      "Options",
		Accessory: LabelAccessory{Kind: "overflow"},
	}}, ctx, 80)
	all := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(all, "⋯") {
		t.Errorf("expected '⋯' for overflow, got %q", all)
	}
}

func TestRenderSectionWithDatepickerAccessory(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{
		Text:      "Pick date",
		Accessory: LabelAccessory{Kind: "datepicker", Label: "2026-04-30"},
	}}, ctx, 80)
	all := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(all, "📅") || !strings.Contains(all, "2026-04-30") {
		t.Errorf("expected date glyph and value, got %q", all)
	}
}

func TestRenderSectionAccessoryStacksAtNarrowWidth(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{
		Text:      "Body",
		Accessory: LabelAccessory{Kind: "button", Label: "X"},
	}}, ctx, 30) // < narrowBreakpoint
	// Body and button must NOT share a row.
	for _, line := range r.Lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "Body") && strings.Contains(plain, "X") {
			t.Errorf("at narrow width, body and accessory should stack: %q", plain)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/ui/messages/blockkit/ -run "TestRenderSection.*Accessory" -v
```

Expected: FAIL — accessories aren't handled yet.

- [ ] **Step 3: Add accessory rendering**

Modify `appendSection` to handle accessory:

```go
func appendSection(out *RenderResult, s SectionBlock, ctx Context, width int) {
	// Determine accessory layout: side-by-side at width >= breakpoint
	// IFF accessory is a label (image accessories come in Phase 3 and
	// are routed differently). At narrow width, accessory stacks
	// below the body.
	bodyW := width
	var accessoryLines []string
	var accessoryW int
	if la, ok := s.Accessory.(LabelAccessory); ok {
		label := renderControlLabel(la.Kind, la.Label)
		accessoryW = lipgloss.Width(label)
		if width >= narrowBreakpoint {
			bodyW = width - accessoryW - 2 // 2 col gutter
			if bodyW < 10 {
				// Accessory too wide; fall through to stacked.
				bodyW = width
				accessoryW = 0
			}
		}
		if accessoryW == 0 || width < narrowBreakpoint {
			accessoryLines = []string{label}
		} else {
			accessoryLines = []string{label}
		}
		out.Interactive = true
	}

	// Body text.
	var bodyLines []string
	if s.Text != "" {
		bodyLines = renderTextLines(s.Text, ctx, bodyW)
	}

	if accessoryW > 0 && width >= narrowBreakpoint {
		// Side-by-side. Pad accessory column to height of body (or 1).
		out.Lines = append(out.Lines, joinSideBySide(bodyLines, accessoryLines, bodyW, 2)...)
	} else {
		out.Lines = append(out.Lines, bodyLines...)
		if len(accessoryLines) > 0 {
			out.Lines = append(out.Lines, accessoryLines...)
		}
	}

	if len(s.Fields) > 0 {
		out.Lines = append(out.Lines, renderFieldsGrid(s.Fields, ctx, width)...)
	}
}

// renderControlLabel produces a muted, non-interactive label for a
// section accessory or actions element. The shape depends on the
// element kind.
func renderControlLabel(kind, label string) string {
	switch kind {
	case "button", "workflow_button":
		text := label
		if text == "" {
			text = "Button"
		}
		return controlStyle().Render("[ " + text + " ]")
	case "static_select", "multi_select":
		text := label
		if text == "" {
			text = "Select"
		}
		return controlStyle().Render(text + " ▾")
	case "overflow":
		return controlStyle().Render("⋯")
	case "datepicker":
		text := label
		if text == "" {
			text = "Pick date"
		}
		return controlStyle().Render("📅 " + text)
	case "timepicker":
		text := label
		if text == "" {
			text = "Pick time"
		}
		return controlStyle().Render("🕒 " + text)
	case "radio_buttons":
		return controlStyle().Render("◯ Options")
	case "checkboxes":
		return controlStyle().Render("☐ Options")
	default:
		return controlStyle().Render("[ control ]")
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/messages/blockkit/ -v
```

Expected: all PASS.

- [ ] **Step 5: Run `make build`**

- [ ] **Step 6: Commit**

```bash
git add internal/ui/messages/blockkit/
git commit -m "feat(blockkit): render section accessory as muted, non-interactive label"
```

---

## Task 8: Render `context` block

**Files:**
- Modify: `internal/ui/messages/blockkit/render.go`
- Modify: `internal/ui/messages/blockkit/render_test.go`

Context blocks are a single line of muted text mixing inline text and small inline images. For Phase 2 we render image elements as their alt text in brackets (e.g. `[icon]`) — Phase 3 swaps in actual inline images.

- [ ] **Step 1: Failing tests**

Append:

```go
func TestRenderContextBlockTextOnly(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{ContextBlock{
		Elements: []ContextElement{
			{Text: "Posted by"},
			{Text: "@gammons"},
			{Text: "·"},
			{Text: "2 min ago"},
		},
	}}, ctx, 80)
	if r.Height < 1 {
		t.Fatalf("Height = %d", r.Height)
	}
	plain := ansi.Strip(r.Lines[0])
	for _, want := range []string{"Posted by", "@gammons", "2 min ago"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in %q", want, plain)
		}
	}
}

func TestRenderContextBlockWithImageElementsRendersAltText(t *testing.T) {
	// Phase 2: image elements render as bracketed alt text. Phase 3
	// will swap in actual inline images.
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{ContextBlock{
		Elements: []ContextElement{
			{ImageURL: "https://example.com/icon.png", AltText: "icon"},
			{Text: "by gammons"},
		},
	}}, ctx, 80)
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(plain, "[icon]") {
		t.Errorf("expected '[icon]' (Phase 2 alt-text fallback), got %q", plain)
	}
	if !strings.Contains(plain, "by gammons") {
		t.Errorf("missing text element: %q", plain)
	}
}
```

- [ ] **Step 2: Run, see failure, then implement.**

Add the case to `appendBlock`:

```go
case ContextBlock:
    appendContext(out, v, ctx, width)
```

Add the function:

```go
func appendContext(out *RenderResult, c ContextBlock, ctx Context, width int) {
	if len(c.Elements) == 0 {
		return
	}
	var parts []string
	for _, e := range c.Elements {
		switch {
		case e.ImageURL != "":
			alt := e.AltText
			if alt == "" {
				alt = "image"
			}
			parts = append(parts, "["+alt+"]")
		case e.Text != "":
			rendered := e.Text
			if ctx.RenderText != nil {
				rendered = ctx.RenderText(e.Text, ctx.UserNames)
			}
			parts = append(parts, rendered)
		}
	}
	combined := strings.Join(parts, " ")
	if ctx.WrapText != nil {
		combined = ctx.WrapText(combined, width)
	}
	for _, line := range strings.Split(combined, "\n") {
		out.Lines = append(out.Lines, mutedStyle().Render(line))
	}
}
```

- [ ] **Step 3: Run tests, build, commit**

```bash
go test ./internal/ui/messages/blockkit/ -v
make build
git add internal/ui/messages/blockkit/
git commit -m "feat(blockkit): render context block with text and image-alt elements"
```

---

## Phase 2 self-check

- [ ] All 4 tasks committed (Tasks 5-8)
- [ ] `go test ./internal/ui/messages/blockkit/ -v` shows all tests PASS
- [ ] `make build` clean
- [ ] No regressions: `go test ./... -race` passes

If any fails, fix before starting Phase 3.
