package mrkdwn

import (
	"regexp"
	"strings"

	"github.com/gammons/slk/internal/linkscheme"

	"github.com/slack-go/slack"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// typedURLRegexp tells Linkify what a bare typed URL is: an allowed scheme
// from linkscheme followed by anything up to whitespace, minus trailing
// punctuation. goldmark's default needs a dotted host with a TLD, which
// drops IP hosts, localhost and file: URLs. www. matches stay with goldmark.
var typedURLRegexp = regexp.MustCompile(`^(?:` + linkscheme.Alternation + `)[^\s<>]*[^\s<>.,;:!?)\]'"*_~]`)

// typedURLProtocols lists the "scheme:" prefixes Linkify may start a match
// at, expanded from linkscheme.Alternation so the list stays in one place.
// mailto: is left to Linkify's email matcher.
var typedURLProtocols = func() []string {
	var out []string
	for _, alt := range strings.Split(linkscheme.Alternation, "|") {
		name, _, _ := strings.Cut(alt, ":")
		if name == "mailto" {
			continue
		}
		if opt, ok := strings.CutSuffix(name, "?"); ok {
			out = append(out, opt[:len(opt)-1]+":")
			name = opt
		}
		out = append(out, name+":")
	}
	return out
}()

// Convert parses CommonMark in input and returns the Slack-compatible
// payload pair: a mrkdwn fallback string and a rich_text block.
//
// input may contain Slack wire-form tokens (<@U…>, <#C…|name>,
// <!here>, <https://…|label>); these are preserved as opaque tokens
// in the mrkdwn fallback and become typed elements (user, channel,
// broadcast, link) in the block.
//
// For empty / whitespace-only input, returns ("", nil).
func Convert(input string) (string, *slack.RichTextBlock) {
	if strings.TrimSpace(input) == "" {
		return "", nil
	}

	tokenized, table := tokenize(input)

	md := goldmark.New(
		goldmark.WithExtensions(
			extension.Strikethrough,
			extension.NewLinkify(extension.WithLinkifyURLRegexp(typedURLRegexp),
				extension.WithLinkifyAllowedProtocols(typedURLProtocols)),
		),
	)
	doc := md.Parser().Parse(text.NewReader([]byte(tokenized)))

	w := newWalker([]byte(tokenized), table)
	w.walkDocument(doc)

	mr := strings.TrimRight(w.mrkdwn.String(), "\n")
	mr = detokenizeText(mr, table)

	if len(w.block.Elements) == 0 {
		return mr, nil
	}
	return mr, w.block
}
