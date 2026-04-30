// Package blockkit render.go: top-level Render dispatch and the
// renderers for divider/header/unsupported blocks.
package blockkit

import (
	"image"
	"strings"

	"charm.land/lipgloss/v2"

	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/styles"
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
	case ContextBlock:
		appendContext(out, v, ctx, width)
	case ActionsBlock:
		appendActions(out, v, width)
	case ImageBlock:
		appendImageBlock(out, v, ctx, width)
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
	bodyW := width
	var accessoryLines []string
	var accessoryW int
	var accessoryHit *HitRect // populated for image accessories

	switch acc := s.Accessory.(type) {
	case LabelAccessory:
		label := renderControlLabel(acc.Kind, acc.Label)
		accessoryW = lipgloss.Width(label)
		accessoryLines = []string{label}
		out.Interactive = true
	case ImageAccessory:
		const accRows, accCols = 4, 8
		if ctx.Fetcher == nil || ctx.Protocol == imgpkg.ProtoOff {
			label := mutedStyle().Render("[image: " + fallbackAlt(acc.AltText) + "]")
			accessoryW = lipgloss.Width(label)
			accessoryLines = []string{label}
		} else {
			target := image.Pt(accCols, accRows)
			lines, flushes, sxl, hit, ok := fetchOrPlaceholder(acc.URL, target, ctx, 0)
			if ok {
				accessoryLines = lines
				accessoryW = accCols
				out.Flushes = append(out.Flushes, flushes...)
				if sxl != nil {
					if out.SixelRows == nil {
						out.SixelRows = map[int]SixelEntry{}
					}
					for k, v := range sxl {
						out.SixelRows[k] = v
					}
				}
				h := hit // copy
				accessoryHit = &h
			} else {
				label := mutedStyle().Render("[image: " + fallbackAlt(acc.AltText) + "]")
				accessoryW = lipgloss.Width(label)
				accessoryLines = []string{label}
			}
		}
	}

	// Width-budget logic: at narrow widths or when accessory is too
	// wide for side-by-side, stack the accessory below the body.
	sideBySide := accessoryW > 0 && width >= narrowBreakpoint
	if sideBySide {
		candidate := width - accessoryW - 2
		if candidate < 10 {
			sideBySide = false
		} else {
			bodyW = candidate
		}
	}

	// Body text.
	var bodyLines []string
	if s.Text != "" {
		bodyLines = renderTextLines(s.Text, ctx, bodyW)
	}

	startRow := len(out.Lines)
	if sideBySide {
		out.Lines = append(out.Lines, joinSideBySide(bodyLines, accessoryLines, bodyW, 2)...)
	} else {
		out.Lines = append(out.Lines, bodyLines...)
		if len(accessoryLines) > 0 {
			out.Lines = append(out.Lines, accessoryLines...)
		}
	}

	// Adjust accessory hit rect to absolute coordinates within out.Lines.
	if accessoryHit != nil {
		if sideBySide {
			accessoryHit.RowStart = startRow
			accessoryHit.RowEnd = startRow + len(accessoryLines)
			accessoryHit.ColStart = bodyW + 2
			accessoryHit.ColEnd = bodyW + 2 + accessoryW
		} else {
			// Stacked: accessory rows live below the body rows.
			accessoryHit.RowStart = startRow + len(bodyLines)
			accessoryHit.RowEnd = accessoryHit.RowStart + len(accessoryLines)
			accessoryHit.ColStart = 0
			accessoryHit.ColEnd = accessoryW
		}
		out.Hits = append(out.Hits, *accessoryHit)
	}

	if len(s.Fields) > 0 {
		out.Lines = append(out.Lines, renderFieldsGrid(s.Fields, ctx, width)...)
	}
}

// fallbackAlt returns alt or "image" if alt is empty.
func fallbackAlt(alt string) string {
	if alt == "" {
		return "image"
	}
	return alt
}

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

func appendActions(out *RenderResult, a ActionsBlock, width int) {
	if len(a.Elements) == 0 {
		return
	}
	out.Interactive = true

	const gapW = 2
	gap := strings.Repeat(" ", gapW)

	var rows []string
	current := ""
	currentW := 0
	for _, el := range a.Elements {
		label := renderControlLabel(el.Kind, el.Label)
		labelW := lipgloss.Width(label)
		var candidateW int
		var candidate string
		if current == "" {
			candidate = label
			candidateW = labelW
		} else {
			candidate = current + gap + label
			candidateW = currentW + gapW + labelW
		}
		if candidateW > width && current != "" {
			rows = append(rows, current)
			current = label
			currentW = labelW
		} else {
			current = candidate
			currentW = candidateW
		}
	}
	if current != "" {
		rows = append(rows, current)
	}
	out.Lines = append(out.Lines, rows...)
}

// appendImageBlock renders a Slack image block: an optional bold
// title line followed by either an inline image (kitty/sixel/half-
// block) or, when image rendering is unavailable, a single OSC-8
// hyperlinked "[image] <url>" fallback line.
func appendImageBlock(out *RenderResult, b ImageBlock, ctx Context, width int) {
	// Title (if any) renders as a small bold line above the image.
	if b.Title != "" {
		titleStyle := lipgloss.NewStyle().Bold(true).
			Foreground(styles.Primary).
			Background(styles.Background)
		title := b.Title
		if lipgloss.Width(title) > width {
			title = truncateToWidth(title, width)
		}
		out.Lines = append(out.Lines, titleStyle.Render(title))
	}

	// If we can't render images at all, emit the fallback link.
	if ctx.Fetcher == nil || ctx.Protocol == imgpkg.ProtoOff {
		out.Lines = append(out.Lines, renderImageFallback(b.URL))
		return
	}

	// Compute target cell dims. Without a thumb ladder we use the
	// declared image_width/image_height when present, falling back
	// to a reasonable default aspect.
	target := computeBlockImageTarget(b, ctx, width)
	if target.X <= 0 || target.Y <= 0 {
		out.Lines = append(out.Lines, renderImageFallback(b.URL))
		return
	}

	rowStart := len(out.Lines)
	lines, flushes, sxlMap, hit, ok := fetchOrPlaceholder(b.URL, target, ctx, rowStart)
	if !ok {
		out.Lines = append(out.Lines, renderImageFallback(b.URL))
		return
	}
	out.Lines = append(out.Lines, lines...)
	out.Flushes = append(out.Flushes, flushes...)
	if sxlMap != nil {
		if out.SixelRows == nil {
			out.SixelRows = map[int]SixelEntry{}
		}
		for k, v := range sxlMap {
			out.SixelRows[k] = v
		}
	}
	if hit.RowEnd > hit.RowStart {
		out.Hits = append(out.Hits, hit)
	}
}

// renderControlLabel produces a muted, non-interactive label for a
// section accessory or actions element. The shape depends on the
// element kind. Used by both Task 7 (accessories) and Task 9
// (actions block elements).
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
