package blockkit

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/linkscheme"
	"github.com/gammons/slk/internal/slack/mrkdwn"
)

// RichTextToMrkdwnUser rebuilds the Slack mrkdwn a person would type
// into the composer from a parsed rich_text block: the inverse of
// internal/slack/mrkdwn.Convert, for seeding the composer when a sent
// message is edited (`E`). Sending the result through Convert again
// gives back the same lists, links and styles.
//
// Bulleted items become "- ", ordered items "N. ", and each indent
// level is four spaces so the item nests under the one above it.
// Links become [label](url), or the bare URL when the label is the
// URL. Styles become *bold*, _italic_, ~strike~ and `code`.
// Mentions stay in wire form (<@U…>, <#C…>, <!here>, <!subteam^S…>)
// for the composer to turn into names. Text is not escaped: a "*" the
// author typed as a literal comes back as typed.
func RichTextToMrkdwnUser(rt RichTextBlock) string {
	// Merge together elements, so that we can safely separate all of
	// them by newlines. Otherwise, we would need to handle text
	// sections specially.
	elems := mrkdwn.Merge(rt.Elements)
	parts := make([]string, 0, len(rt.Elements))
	for i, e := range elems {
		switch v := e.(type) {
		case *slack.RichTextSection:
			sec := sectionToMrkdwnUser(v.Elements)
			if i < len(elems)-1 {
				sec = strings.TrimSuffix(sec, "\n")
			}
			parts = append(parts, sec)
		case *slack.RichTextList:
			parts = append(parts, listToMrkdwnUser(v))
		case *slack.RichTextPreformatted:
			parts = append(parts, preformattedToMrkdwnUser(*v))
		case *slack.RichTextQuote:
			parts = append(parts, quoteToMrkdwnUser(*v))
		}
	}
	return strings.Join(parts, "\n")
}

// sectionToMrkdwnUser renders the inline elements of a rich_text_section.
func sectionToMrkdwnUser(elements []slack.RichTextSectionElement) string {
	var b strings.Builder
	for _, e := range elements {
		b.WriteString(inlineToMarkdown(e))
	}
	return b.String()
}

// inlineToMarkdown renders one inline element as composer markdown.
// Everything that is not a styled text run or a link reads the same in
// the composer as on the wire, so it falls through to inlineToMrkdwn.
func inlineToMarkdown(e slack.RichTextSectionElement) string {
	switch v := e.(type) {
	case *slack.RichTextSectionTextElement:
		return textToMarkdown(v.Text, v.Style)
	case *slack.RichTextSectionLinkElement:
		return emphasize(LinkToMrkdwnUser(v.URL, v.Text), v.Style)
	case *slack.RichTextSectionUnknownElement:
		if m, ok := linkMention(v); ok {
			return LinkToMrkdwnUser(m.URL, m.Text)
		}
		return ""
	}
	return inlineToMrkdwn(e)
}

// textToMarkdown wraps a styled text run in CommonMark markers, bold
// outermost. Markers go around each line's text with its surrounding
// spaces left outside, because CommonMark only reads "**" as bold when
// it touches the text, and emphasis cannot span a blank line.
func textToMarkdown(text string, style *slack.RichTextSectionTextStyle) string {
	text = EscapeLiteralMentions(text)
	if style == nil || !(style.Bold || style.Italic || style.Strike || style.Code) {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		core := strings.TrimSpace(line)
		start := strings.Index(line, core)
		lead, trail := line[:start], line[start+len(core):]
		if style.Code {
			core = codeSpan(core)
		}
		core = emphasize(core, style)
		lines[i] = lead + core + trail
	}
	return strings.Join(lines, "\n")
}

// literalMentionRe matches text shaped like a Slack mention or
// broadcast token: <@…>, <#…>, <!…>.
var literalMentionRe = regexp.MustCompile(`<([@#!][^<>]*)>`)

// EscapeLiteralMentions entity-escapes text that would read as a
// mention in the composer, where <!here> typed as text is a broadcast.
// Literal text that only looks like one, such as "<!here>" written in
// Slack's client, then stays text when an edit is saved, and reads as
// Slack's text field shows it ("&lt;!here&gt;"), as before
// MentionsForEdit. Link-shaped text is left alone: on save it becomes
// the link it looks like, which pings no one.
func EscapeLiteralMentions(s string) string {
	return literalMentionRe.ReplaceAllString(s, "&lt;$1&gt;")
}

// emphasize wraps s in the strike, italic and bold markers style asks
// for, bold outermost. Code is the caller's: a link can't be one.
func emphasize(s string, style *slack.RichTextSectionTextStyle) string {
	if style == nil {
		return s
	}
	if s == "" {
		return s
	}
	if style.Strike {
		s = "~" + s + "~"
	}
	if style.Italic {
		s = "_" + s + "_"
	}
	if style.Bold {
		s = "*" + s + "*"
	}
	return s
}

// codeSpan wraps s in single backticks.
func codeSpan(s string) string {
	fence := "`"
	return fence + s + fence
}

// LinkToMrkdwnUser writes a link the way a person would type it: the
// bare URL when the label adds nothing (the composer turns a typed URL
// into a link), else [label](url). A URL or label that markdown's link
// syntax cannot hold as typed (brackets or spaces in the URL; brackets
// or formatting characters in the label, which markdown would read as
// styles and drop) keeps Slack's <url|label> form, which the composer
// also sends as a link.
func LinkToMrkdwnUser(url, label string) string {
	if label == "" || label == url || url == "mailto:"+label || url == "tel:"+label || url == "http://"+label {
		if strings.HasPrefix(url, "mailto:") {
			return strings.TrimPrefix(url, "mailto:")
		}
		if linkscheme.HasPrefix(url) && mrkdwn.TypesAsBareUrl(url) {
			return url
		}
		label = url
	}
	if strings.ContainsAny(url, " ()<>") || strings.ContainsAny(label, "[]*_~`\\") {
		return linkToMrkdwn(url, label)
	}
	return "[" + label + "](" + url + ")"
}

// listToMrkdwnUser writes one line per item, four spaces per indent
// level. Ordered items are numbered from Offset+1.
func listToMrkdwnUser(l *slack.RichTextList) string {
	lines := make([]string, 0, len(l.Elements))
	pad := strings.Repeat("    ", l.Indent)
	for i, child := range l.Elements {
		body := ""
		if sec, ok := child.(*slack.RichTextSection); ok {
			// Remove trailing newlines
			body = strings.Trim(sectionToMrkdwnUser(sec.Elements), "\n")
			// Indent all non-first lines of body so they line up with the first one, e.g.
			// - foo
			//   bar
			//   baz
			body = strings.ReplaceAll(body, "\n", "\n  ")
		}
		var marker string
		if l.Style == slack.RTEListOrdered {
			marker = strconv.Itoa(l.Offset+i+1) + ". "
		} else {
			marker = "- "
		}
		quoteMarker := strings.Repeat("> ", l.Border)
		lines = append(lines, quoteMarker+pad+marker+body)
	}
	return strings.Join(lines, "\n")
}

// preformattedToMrkdwnUser writes a fenced code block. The fence is one
// backtick longer than any run of three or more inside the body.
func preformattedToMrkdwnUser(preformatted slack.RichTextPreformatted) string {
	body := preformattedElementsToMrkdwn(preformatted.Elements)
	fence := "```"
	quoteMarker := strings.Repeat("> ", preformatted.Border)
	return quoteMarker + fence + "\n" + body + "\n" + fence
}

// quoteToMrkdwn emits a rich_text_quote as one or more "> " lines.
// Each "\n" inside the section becomes a line boundary that gets
// its own "> " prefix.
func quoteToMrkdwnUser(quote slack.RichTextQuote) string {
	body := sectionToMrkdwnUser(quote.Elements)
	lines := strings.Split(body, "\n")
	marker := strings.Repeat("> ", quote.Border+1)
	for i, l := range lines {
		lines[i] = marker + l
	}
	return strings.Join(lines, "\n")
}
