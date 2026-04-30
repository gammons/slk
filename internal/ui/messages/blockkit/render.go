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
	case UnknownBlock:
		out.Lines = append(out.Lines, renderUnsupported(v.Type, width))
	default:
		// Other block types (Section, Context, Image, Actions) are
		// added by later tasks; for now, render them as unsupported
		// so the package is total even mid-implementation.
		out.Lines = append(out.Lines, renderUnsupported(v.blockType(), width))
	}
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
