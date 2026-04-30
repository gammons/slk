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
	}
	out.Height = len(out.Lines)
	return out
}

// appendLegacyAttachment draws a single legacy attachment onto out.
// Pretext is rendered above the stripe at full width; title, text,
// and footer are rendered to the right of the colored stripe at
// width - stripeCol.
func appendLegacyAttachment(out *RenderResult, a LegacyAttachment, ctx Context, width int) {
	// Pretext renders ABOVE the stripe, full width, no indent.
	if a.Pretext != "" {
		out.Lines = append(out.Lines, renderTextLines(a.Pretext, ctx, width)...)
	}

	stripeColor := ResolveAttachmentColor(a.Color)
	stripeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(stripeColor))
	contentW := width - stripeCol
	if contentW < 1 {
		contentW = 1
	}

	// Body lines — title, text, footer. Fields and image_url come in Task 13.
	var body []string
	if a.Title != "" {
		title := a.Title
		if a.TitleLink != "" {
			title = "\x1b]8;;" + a.TitleLink + "\x1b\\" + title + "\x1b]8;;\x1b\\"
		}
		titleStyle := lipgloss.NewStyle().Bold(true)
		if lipgloss.Width(title) > contentW {
			title = truncateToWidth(title, contentW)
		}
		body = append(body, titleStyle.Render(title))
	}
	if a.Text != "" {
		body = append(body, renderTextLines(a.Text, ctx, contentW)...)
	}
	// Fields grid (Task 13).
	if len(a.Fields) > 0 {
		body = append(body, renderLegacyFields(a.Fields, ctx, contentW)...)
	}
	// Inline image (Task 13). Uses the same fetcher path as image
	// blocks; falls back to a single OSC-8 link line when no fetcher
	// is configured or the protocol is off.
	// TODO(blockkit): render thumb_url alongside text via joinSideBySide.
	var imageHitInBody *HitRect
	if a.ImageURL != "" {
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
	}
	// Footer.
	if a.Footer != "" || a.TS != 0 {
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
	}

	// Prefix every body line with the colored stripe + 1 col space.
	stripe := stripeStyle.Render(stripeGlyph) + " "
	startRow := len(out.Lines)
	for _, line := range body {
		out.Lines = append(out.Lines, stripe+line)
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
