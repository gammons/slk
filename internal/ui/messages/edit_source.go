package messages

import (
	"regexp"
	"strings"

	"github.com/gammons/slk/internal/linkscheme"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// wireLinkRe matches a Slack link token, <url> or <url|label>.
var wireLinkRe = regexp.MustCompile(`<((?:` + linkscheme.Alternation + `)[^|>]+)(?:\|([^>]+))?>`)

// wireTokenRe matches any Slack token: a mention, broadcast or link.
var wireTokenRe = regexp.MustCompile(`<[^<>]*>`)

// EditSource returns msg's body as the markdown the composer takes,
// for seeding it when the message is edited (`E`): lists, links, line
// breaks and styles read as the author typed them, and sending it
// unchanged posts the same message. Mentions stay in wire form for the
// composer to turn into names (compose.MentionsForEdit).
//
// The body comes from the message's rich_text blocks when it has any,
// because Slack's text field loses its line breaks (see
// MessageTextSource); all of them, a blank line apart, so saving does
// not drop the later ones. Otherwise it is msg.Text with each link
// written as markdown and Slack's &lt; &gt; &amp; decoded, except where
// the decoded text would read as a mention (blockkit.EscapeLiteralMentions).
func EditSource(msg MessageItem) string {
	var parts []string
	for _, b := range msg.Blocks {
		if rt, ok := b.(blockkit.RichTextBlock); ok {
			if md := blockkit.RichTextToMarkdown(rt); md != "" {
				parts = append(parts, md)
			}
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}
	// Decode around the links, not across them, so an escaped "<" in
	// the text cannot turn into the start of one.
	var b strings.Builder
	last := 0
	for _, m := range wireLinkRe.FindAllStringSubmatchIndex(msg.Text, -1) {
		b.WriteString(decodeLiteral(msg.Text[last:m[0]]))
		url := slackEntityDecoder.Replace(msg.Text[m[2]:m[3]])
		label := ""
		if m[4] >= 0 {
			label = slackEntityDecoder.Replace(msg.Text[m[4]:m[5]])
		}
		b.WriteString(blockkit.LinkToMarkdown(url, label))
		last = m[1]
	}
	b.WriteString(decodeLiteral(msg.Text[last:]))
	return b.String()
}

// decodeLiteral decodes Slack's escapes in a stretch of msg.Text
// between its links. Mention tokens in it are real (Slack never leaves
// a literal "<" unescaped) and are kept; decoded text that would read
// as one is escaped again, so it cannot ping anyone when saved.
func decodeLiteral(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range wireTokenRe.FindAllStringIndex(s, -1) {
		b.WriteString(blockkit.EscapeLiteralMentions(slackEntityDecoder.Replace(s[last:m[0]])))
		b.WriteString(s[m[0]:m[1]])
		last = m[1]
	}
	b.WriteString(blockkit.EscapeLiteralMentions(slackEntityDecoder.Replace(s[last:])))
	return b.String()
}
