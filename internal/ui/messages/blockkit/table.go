package blockkit

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/emoji"
)

// RichTextToPlain is RichTextToMrkdwn without markup, for text shown
// verbatim (a table cell): styles are dropped, links show their label
// (or URL), mentions their ID, and emoji their glyph — or their
// :shortcode: when the glyph is a multi-codepoint sequence whose width
// terminals disagree on (emoji.ResolveShortcodesInText, the rule the
// message body uses). Newlines become spaces, and the items of a list
// are separated by a space, so a cell stays on one row.
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
					if len(inline) > 0 {
						inline = append(inline, slack.NewRichTextSectionTextElement(" ", nil))
					}
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

// inlineToPlain is inlineToMrkdwn without markup, for RichTextToPlain.
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
	case *slack.RichTextSectionEmojiElement:
		// Skin tones are dropped, as in the body: a lone tone modifier
		// would otherwise render as its own swatch.
		return emoji.ResolveShortcodesInText(":" + v.Name + ":")
	}
	// Everything else already has a plain wire form.
	return inlineToMrkdwn(e)
}

// TableToText lays t out as aligned monospace rows, columns separated
// by " │ " and the header row (row 0) underlined with ─┼─. Short rows
// are padded with empty cells. Widths are measured with emoji.Width.
// Returns "" for a table with no rows.
//
// The result is plain text meant to be drawn verbatim: never pass it
// through the mrkdwn renderer, which would restyle *x* / _x_ / ~x~,
// resolve :shortcodes: to glyphs of a different width, and end a code
// fence at a cell holding ```. Render draws it in place with
// Context.RenderCode; the message body does the same (see
// messages.RenderMessage).
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
			widths[i] = max(widths[i], emoji.Width(c))
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
	gap := max(width-emoji.Width(s), 0)
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

// appendTable draws t in place among the blocks Render draws, as the
// host's code block (Context.RenderCode) or, without one, as plain
// rows hard-wrapped to width.
func appendTable(out *RenderResult, t TableBlock, ctx Context, width int) {
	text := TableToText(t)
	if text == "" {
		return
	}
	if ctx.RenderCode != nil {
		text = ctx.RenderCode(text, width)
	} else {
		text = ansi.Hardwrap(text, width, false)
	}
	out.Lines = append(out.Lines, strings.Split(text, "\n")...)
}
