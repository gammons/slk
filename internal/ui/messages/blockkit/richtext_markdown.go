package blockkit

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/slack-go/slack"
)

// RichTextToMarkdown rebuilds the CommonMark a person would type into
// the composer from a parsed rich_text block: the inverse of
// internal/slack/mrkdwn.Convert, for seeding the composer when a sent
// message is edited (`E`). Sending the result through Convert again
// gives back the same lists, links and styles.
//
// Bulleted items become "- ", ordered items "N. ", and each indent
// level is four spaces so the item nests under the one above it.
// Links become [label](url), or the bare URL when the label is the
// URL. Styles become **bold**, _italic_, ~~strike~~ and `code`.
// Mentions stay in wire form (<@U…>, <#C…>, <!here>, <!subteam^S…>)
// for the composer to turn into names. Text is not escaped: a "*" the
// author typed as a literal comes back as typed.
func RichTextToMarkdown(rt RichTextBlock) string {
	var b strings.Builder
	prevList := -1 // indent of the list element just written; -1 after anything else
	for _, e := range rt.Elements {
		var part string
		list, interrupts := -1, false
		switch v := e.(type) {
		case *slack.RichTextSection:
			part = strings.Trim(sectionToMarkdown(v.Elements), "\n")
		case *slack.RichTextList:
			// A nested level sits directly under its parent list; one
			// with nothing above it to nest under starts at the margin.
			list = min(v.Indent, prevList+1)
			interrupts = list == 0 && interruptsParagraph(v)
			part = listToMarkdown(v, list)
		case *slack.RichTextPreformatted:
			part = preformattedToMarkdown(v.Elements)
		case *slack.RichTextQuote:
			part = quoteToMarkdown(v.Elements)
		}
		if part == "" {
			continue
		}
		if b.Len() > 0 {
			// A list continues the list above it, or interrupts the
			// paragraph above it, on the next line. Anything else
			// needs a blank line, or it would be read as a lazy
			// continuation of the list item or quote above.
			if list >= 0 && (prevList >= 0 || interrupts) {
				b.WriteString("\n")
			} else {
				b.WriteString("\n\n")
			}
		}
		b.WriteString(part)
		prevList = list
	}
	return b.String()
}

// interruptsParagraph reports whether l's first item can start right
// under a paragraph line: CommonMark lets a bullet list do that, and an
// ordered list only when it starts at 1.
func interruptsParagraph(l *slack.RichTextList) bool {
	return l.Style != slack.RTEListOrdered || l.Offset == 0
}

// sectionToMarkdown renders the inline elements of a rich_text_section.
func sectionToMarkdown(elements []slack.RichTextSectionElement) string {
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
		return emphasize(LinkToMarkdown(v.URL, v.Text), v.Style)
	case *slack.RichTextSectionUnknownElement:
		if m, ok := linkMention(v); ok {
			return LinkToMarkdown(m.URL, m.Text)
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
		if core == "" {
			continue
		}
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
	if style.Strike {
		s = "~~" + s + "~~"
	}
	if style.Italic {
		s = "_" + s + "_"
	}
	if style.Bold {
		s = "**" + s + "**"
	}
	return s
}

// codeSpan wraps s in a run of backticks one longer than the longest
// run inside it, padding with spaces when s starts or ends with one,
// as CommonMark requires.
func codeSpan(s string) string {
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		s = " " + s + " "
	}
	return fence + s + fence
}

// LinkToMarkdown writes a link the way a person would type it: the
// bare URL when the label adds nothing (the composer turns a typed URL
// into a link), else [label](url). A URL or label that markdown's link
// syntax cannot hold as typed (brackets or spaces in the URL; brackets
// or formatting characters in the label, which markdown would read as
// styles and drop) keeps Slack's <url|label> form, which the composer
// also sends as a link.
func LinkToMarkdown(url, label string) string {
	if label == "" || label == url || url == "mailto:"+label {
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "mailto:") {
			return strings.TrimPrefix(url, "mailto:")
		}
		label = url
	}
	if strings.ContainsAny(url, " ()<>") || strings.ContainsAny(label, "[]*_~`\\") {
		return linkToMrkdwn(url, label)
	}
	return "[" + label + "](" + url + ")"
}

// listToMarkdown writes one line per item, four spaces per indent
// level. Ordered items are numbered from Offset+1.
func listToMarkdown(l *slack.RichTextList, indent int) string {
	pad := strings.Repeat("    ", indent)
	lines := make([]string, 0, len(l.Elements))
	for i, child := range l.Elements {
		body := ""
		if sec, ok := child.(*slack.RichTextSection); ok {
			body = sectionToMarkdown(sec.Elements)
		}
		marker := "- "
		if l.Style == slack.RTEListOrdered {
			marker = strconv.Itoa(l.Offset+i+1) + ". "
		}
		lines = append(lines, pad+marker+body)
	}
	return strings.Join(lines, "\n")
}

// preformattedToMarkdown writes a fenced code block. The fence is one
// backtick longer than any run of three or more inside the body.
func preformattedToMarkdown(elements []slack.RichTextSectionElement) string {
	body := strings.TrimSuffix(strings.TrimPrefix(preformattedToMrkdwn(elements), "```\n"), "\n```")
	body = strings.TrimSuffix(body, "\n")
	fence := "```"
	for strings.Contains(body, fence) {
		fence += "`"
	}
	return fence + "\n" + body + "\n" + fence
}

// quoteToMarkdown writes a rich_text_quote as "> " lines.
func quoteToMarkdown(elements []slack.RichTextSectionElement) string {
	lines := strings.Split(strings.Trim(sectionToMarkdown(elements), "\n"), "\n")
	for i, l := range lines {
		lines[i] = "> " + l
	}
	return strings.Join(lines, "\n")
}
