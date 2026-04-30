// Package blockkit attachments.go: renders Slack legacy
// `attachments` payloads. Each attachment is drawn as a card with
// optional pretext above, then a colored vertical stripe (█) on
// the left margin with title/text/footer to its right.
//
// Fields, image_url, and thumb_url are deferred to Task 13.
package blockkit

import (
	"time"

	"charm.land/lipgloss/v2"
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
	for _, line := range body {
		out.Lines = append(out.Lines, stripe+line)
	}
}
