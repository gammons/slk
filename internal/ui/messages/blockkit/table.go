package blockkit

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/slack-go/slack"
)

// RichTextToPlain is RichTextToMrkdwn without markup, for text shown
// verbatim (a table cell inside a code block): styles are dropped,
// links show their label (or URL), mentions their ID and emoji their
// shortcode. Newlines become spaces, so a cell stays on one row.
func RichTextToPlain(rt RichTextBlock) string {
	var b strings.Builder
	for _, e := range rt.Elements {
		var inline []slack.RichTextSectionElement
		switch v := e.(type) {
		case *slack.RichTextSection:
			inline = v.Elements
		case *slack.RichTextPreformatted:
			inline = v.Elements
		case *slack.RichTextQuote:
			inline = v.Elements
		case *slack.RichTextList:
			for _, item := range v.Elements {
				if s, ok := item.(*slack.RichTextSection); ok {
					inline = append(inline, s.Elements...)
				}
			}
		}
		if b.Len() > 0 && len(inline) > 0 {
			b.WriteByte(' ')
		}
		for _, el := range inline {
			b.WriteString(inlineToPlain(el))
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func inlineToPlain(e slack.RichTextSectionElement) string {
	switch v := e.(type) {
	case *slack.RichTextSectionTextElement:
		return v.Text
	case *slack.RichTextSectionLinkElement:
		if v.Text != "" {
			return v.Text
		}
		return v.URL
	case *slack.RichTextSectionUserElement:
		return "@" + v.UserID
	case *slack.RichTextSectionChannelElement:
		return "#" + v.ChannelID
	}
	// Everything else already has a plain wire form.
	return inlineToMrkdwn(e)
}

// TableToText lays t out as aligned monospace rows, columns separated
// by " │ " and the header row (row 0) underlined with ─┼─. Short rows
// are padded with empty cells. Returns "" for a table with no rows.
func TableToText(t TableBlock) string {
	cols := 0
	for _, r := range t.Rows {
		cols = max(cols, len(r))
	}
	if cols == 0 {
		return ""
	}
	widths := make([]int, cols)
	for _, r := range t.Rows {
		for i, c := range r {
			widths[i] = max(widths[i], lipgloss.Width(c))
		}
	}
	lines := make([]string, 0, len(t.Rows)+1)
	for ri, r := range t.Rows {
		cells := make([]string, cols)
		for i := range cols {
			var c string
			if i < len(r) {
				c = r[i]
			}
			align := ""
			if i < len(t.Align) {
				align = t.Align[i]
			}
			cells[i] = pad(c, widths[i], align)
		}
		lines = append(lines, strings.TrimRight(strings.Join(cells, " │ "), " "))
		if ri == 0 && len(t.Rows) > 1 {
			rule := make([]string, cols)
			for i, w := range widths {
				rule[i] = strings.Repeat("─", w)
			}
			lines = append(lines, strings.Join(rule, "─┼─"))
		}
	}
	return strings.Join(lines, "\n")
}

// pad aligns s within width display columns.
func pad(s string, width int, align string) string {
	gap := max(width-lipgloss.Width(s), 0)
	switch align {
	case "right":
		return strings.Repeat(" ", gap) + s
	case "center":
		left := gap / 2
		return strings.Repeat(" ", left) + s + strings.Repeat(" ", gap-left)
	default:
		return s + strings.Repeat(" ", gap)
	}
}
