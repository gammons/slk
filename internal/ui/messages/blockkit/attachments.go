// Package blockkit attachments.go: renders Slack legacy
// `attachments` payloads. Each attachment is drawn as a card with
// optional pretext above, then a colored vertical stripe (█) on
// the left margin with title/text/fields/image_url/footer to its
// right.
//
// thumb_url is deferred to a future task: rendering a small image
// to the right of Text requires joinSideBySide against the text
// column with width-aware truncation, which has not been wired up.
package blockkit

import (
	"time"

	"charm.land/lipgloss/v2"

	imgpkg "github.com/gammons/slk/internal/image"
)

// stripeGlyph is the leading character on every line inside the
// attachment's colored region.
const stripeGlyph = "█"

// stripeCol is the column count consumed by the stripe + 1-col
// gutter to its right.
const stripeCol = 2

// RenderLegacy renders a slice of legacy attachments, each as its
// own colored card. Attachments are joined with a single blank line
// between them.
//
// Perf: when ctx.Perf is non-nil, per-sub-lane wall-clock is
// accumulated for the caller (see LegacyPerf in types.go). The
// instrumentation guards every measurement so the nil path is
// branch-predicted away in production.
func RenderLegacy(atts []LegacyAttachment, ctx Context, width int) RenderResult {
	if len(atts) == 0 || width <= 0 {
		return RenderResult{}
	}
	var out RenderResult
	for i, a := range atts {
		if i > 0 {
			out.Lines = append(out.Lines, "")
		}
		appendLegacyAttachment(&out, a, ctx, width)
		if ctx.Perf != nil {
			ctx.Perf.attachmentCount++
		}
	}
	out.Height = len(out.Lines)
	return out
}

// appendLegacyAttachment draws a single legacy attachment onto out.
// Pretext is rendered above the stripe, full width; title, text,
// and footer are rendered to the right of the colored stripe at
// width - stripeCol.
func appendLegacyAttachment(out *RenderResult, a LegacyAttachment, ctx Context, width int) {
	perf := ctx.Perf // local alias; nil disables timing
	// Pretext renders ABOVE the stripe, full width, no indent.
	if a.Pretext != "" {
		var t0 time.Time
		if perf != nil {
			t0 = time.Now()
		}
		out.Lines = append(out.Lines, renderTextLines(a.Pretext, ctx, width)...)
		if perf != nil {
			perf.textTotal += time.Since(t0)
		}
	}

	var otherT0 time.Time
	if perf != nil {
		otherT0 = time.Now()
	}
	stripeColor := ResolveAttachmentColor(a.Color)
	stripeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(stripeColor))
	contentW := width - stripeCol
	if contentW < 1 {
		contentW = 1
	}
	if perf != nil {
		perf.otherTotal += time.Since(otherT0)
	}

	// Body lines — title, text, footer. Fields and image_url come in Task 13.
	var body []string
	if a.Title != "" {
		var t0 time.Time
		if perf != nil {
			t0 = time.Now()
		}
		title := a.Title
		if a.TitleLink != "" {
			title = "\x1b]8;;" + a.TitleLink + "\x1b\\" + title + "\x1b]8;;\x1b\\"
		}
		titleStyle := lipgloss.NewStyle().Bold(true)
		if lipgloss.Width(title) > contentW {
			title = truncateToWidth(title, contentW)
		}
		body = append(body, titleStyle.Render(title))
		if perf != nil {
			perf.otherTotal += time.Since(t0)
		}
	}
	if a.Text != "" {
		var t0 time.Time
		if perf != nil {
			t0 = time.Now()
		}
		body = append(body, renderTextLines(a.Text, ctx, contentW)...)
		if perf != nil {
			perf.textTotal += time.Since(t0)
		}
	}
	// Fields grid (Task 13). renderLegacyFields itself fans out to
	// renderTextLines per field value; we count the whole call as
	// text-lane work since that's its dominant cost.
	if len(a.Fields) > 0 {
		var t0 time.Time
		if perf != nil {
			t0 = time.Now()
		}
		body = append(body, renderLegacyFields(a.Fields, ctx, contentW)...)
		if perf != nil {
			perf.textTotal += time.Since(t0)
		}
	}
	// Nested Block Kit blocks. Slack's newer link-unfurl shape
	// (Linear/Jira/GitHub issue cards) carries all content here while
	// Title/Text/Fields are empty. Render them via the standard block
	// renderer at the stripe-indented content width, then splice the
	// resulting lines into body so they pick up the colored stripe
	// prefix below. Image flushes/sixels/hits from the sub-render are
	// merged with row/col offsets after the stripe loop.
	var nested RenderResult
	var nestedRowStartInBody int
	if len(a.Blocks) > 0 {
		var t0 time.Time
		if perf != nil {
			t0 = time.Now()
		}
		nested = Render(a.Blocks, ctx, contentW)
		nestedRowStartInBody = len(body)
		body = append(body, nested.Lines...)
		if nested.Interactive {
			out.Interactive = true
		}
		if perf != nil {
			perf.otherTotal += time.Since(t0)
		}
	}

	// Inline image (Task 13). Uses the same fetcher path as image
	// blocks; falls back to a single OSC-8 link line when no fetcher
	// is configured or the protocol is off.
	// TODO(blockkit): render thumb_url alongside text via joinSideBySide.
	var imageHitInBody *HitRect
	if a.ImageURL != "" {
		var t0 time.Time
		if perf != nil {
			t0 = time.Now()
		}
		if ctx.Fetcher == nil || ctx.Protocol == imgpkg.ProtoOff {
			body = append(body, renderImageFallback(a.ImageURL))
		} else {
			target := computeBlockImageTarget(ImageBlock{URL: a.ImageURL}, ctx, contentW)
			if target.X > 0 && target.Y > 0 {
				rowStartInBody := len(body)
				lines, flushes, sxl, hit, ok := fetchOrPlaceholder(a.ImageURL, target, ctx, rowStartInBody)
				if ok {
					body = append(body, lines...)
					out.Flushes = append(out.Flushes, flushes...)
					if sxl != nil {
						if out.SixelRows == nil {
							out.SixelRows = map[int]SixelEntry{}
						}
						for k, v := range sxl {
							out.SixelRows[k] = v
						}
					}
					h := hit
					imageHitInBody = &h
				} else {
					body = append(body, renderImageFallback(a.ImageURL))
				}
			} else {
				body = append(body, renderImageFallback(a.ImageURL))
			}
		}
		if perf != nil {
			perf.imageTotal += time.Since(t0)
		}
	}
	// Buttons from the legacy `actions` array, after any image and
	// before the footer. Drawn by the Block Kit actions renderer so both
	// kinds of button look the same.
	if len(a.Actions) > 0 {
		var acts RenderResult
		appendActions(&acts, legacyActionsBlock(a.Actions), contentW)
		body = append(body, acts.Lines...)
		if acts.Interactive {
			out.Interactive = true
		}
	}
	// Footer.
	if a.Footer != "" || a.TS != 0 {
		var t0 time.Time
		if perf != nil {
			t0 = time.Now()
		}
		footer := a.Footer
		if a.TS != 0 {
			ts := time.Unix(a.TS, 0).UTC().Format("2006-01-02 3:04 PM")
			if footer != "" {
				footer += " · " + ts
			} else {
				footer = ts
			}
		}
		if lipgloss.Width(footer) > contentW {
			footer = truncateToWidth(footer, contentW)
		}
		body = append(body, mutedStyle().Render(footer))
		if perf != nil {
			perf.otherTotal += time.Since(t0)
		}
	}

	// Prefix every body line with the colored stripe + 1 col space.
	var stripeT0 time.Time
	if perf != nil {
		stripeT0 = time.Now()
	}
	stripe := stripeStyle.Render(stripeGlyph) + " "
	startRow := len(out.Lines)
	for _, line := range body {
		out.Lines = append(out.Lines, stripe+line)
	}
	if perf != nil {
		perf.otherTotal += time.Since(stripeT0)
	}

	// Adjust the image hit so its rows are absolute within out.Lines
	// and its cols account for the stripe-prefix offset.
	if imageHitInBody != nil {
		imageHitInBody.RowStart += startRow
		imageHitInBody.RowEnd += startRow
		imageHitInBody.ColStart += stripeCol
		imageHitInBody.ColEnd += stripeCol
		out.Hits = append(out.Hits, *imageHitInBody)
	}

	// Merge nested-block image artifacts. Sub-render coordinates are
	// relative to nested.Lines (0-based); translate to absolute
	// out.Lines rows by adding startRow + the block region's offset
	// within body, and shift columns by the stripe prefix.
	if len(a.Blocks) > 0 {
		rowOffset := startRow + nestedRowStartInBody
		out.Flushes = append(out.Flushes, nested.Flushes...)
		for k, v := range nested.SixelRows {
			if out.SixelRows == nil {
				out.SixelRows = map[int]SixelEntry{}
			}
			out.SixelRows[k+rowOffset] = v
		}
		for _, h := range nested.Hits {
			h.RowStart += rowOffset
			h.RowEnd += rowOffset
			h.ColStart += stripeCol
			h.ColEnd += stripeCol
			out.Hits = append(out.Hits, h)
		}
	}
}

// renderLegacyFields lays out attachment fields. Two consecutive
// Short==true fields share a row; non-short fields take their own.
func renderLegacyFields(fields []LegacyField, ctx Context, width int) []string {
	var out []string
	i := 0
	for i < len(fields) {
		f := fields[i]
		if f.Short && i+1 < len(fields) && fields[i+1].Short {
			gutter := 2
			colW := (width - gutter) / 2
			if colW < 1 {
				colW = 1
			}
			left := renderLegacyField(f, ctx, colW)
			right := renderLegacyField(fields[i+1], ctx, colW)
			out = append(out, joinSideBySide(left, right, colW, gutter)...)
			i += 2
			continue
		}
		out = append(out, renderLegacyField(f, ctx, width)...)
		i++
	}
	return out
}

// legacyActionsBlock adapts legacy attachment actions to the Block Kit
// actions block appendActions draws. A legacy "select" draws as a
// static select labelled with its text.
func legacyActionsBlock(actions []LegacyAction) ActionsBlock {
	b := ActionsBlock{Elements: make([]ActionElement, 0, len(actions))}
	for _, a := range actions {
		kind := "button"
		if a.Type == "select" {
			kind = "static_select"
		}
		b.Elements = append(b.Elements, ActionElement{Kind: kind, Label: a.Text})
	}
	return b
}

// renderLegacyField renders a single attachment field's title +
// value to a list of lines bounded by width.
func renderLegacyField(f LegacyField, ctx Context, width int) []string {
	var out []string
	if f.Title != "" {
		title := fieldLabelStyle().Render(f.Title)
		if lipgloss.Width(title) > width {
			title = truncateToWidth(title, width)
		}
		out = append(out, title)
	}
	if f.Value != "" {
		out = append(out, renderTextLines(f.Value, ctx, width)...)
	}
	return out
}
