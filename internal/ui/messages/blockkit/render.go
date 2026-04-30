// Package blockkit render.go: top-level Render dispatch and the
// renderers for divider/header/unsupported blocks.
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
// content width. Width is the available content width AFTER the
// caller has subtracted avatar gutter and border columns.
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

// RenderLegacy produces a RenderResult for a slice of legacy
// attachments. Same width contract as Render. Phase 4 fills it in.
func RenderLegacy(atts []LegacyAttachment, ctx Context, width int) RenderResult {
	return RenderResult{}
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
	case SectionBlock:
		appendSection(out, v, ctx, width)
	case UnknownBlock:
		out.Lines = append(out.Lines, renderUnsupported(v.Type, width))
	default:
		// Other block types (Context, Image, Actions) are added by
		// later tasks; for now, render them as unsupported so the
		// package is total even mid-implementation.
		out.Lines = append(out.Lines, renderUnsupported(v.blockType(), width))
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
		out = append(out, padRight(l, colW)+gap+padRight(r, colW))
	}
	return out
}

// padRight returns s right-padded with spaces to display width w.
// If s already meets or exceeds w, returns s unchanged.
func padRight(s string, w int) string {
	cur := lipgloss.Width(s)
	if cur >= w {
		return s
	}
	return s + strings.Repeat(" ", w-cur)
}

func renderDivider(width int) string {
	return dividerStyle().Render(strings.Repeat("─", width))
}

func renderHeader(text string, width int) string {
	if text == "" {
		return ""
	}
	if lipgloss.Width(text) > width {
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
