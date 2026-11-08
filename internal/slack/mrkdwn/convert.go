package mrkdwn

import (
	"regexp"
	"strings"

	"github.com/gammons/slk/internal/linkscheme"

	"github.com/slack-go/slack"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// typedURLRegexp tells Linkify what a bare typed URL is: an allowed scheme
// from linkscheme followed by printable ASCII minus < > and |, minus
// trailing punctuation. goldmark's default needs a dotted host with a TLD,
// which drops IP hosts, localhost and file: URLs. ASCII only, like goldmark's
// default, so a match never swallows the tokenizer's private-use sentinels
// (a mention, channel or emoji next to the URL) or adjacent CJK text.
// www. matches stay with goldmark.
var typedURLRegexp = regexp.MustCompile(`^(?:` + linkscheme.Alternation + `)[!-;=?-{}~]*[#-&()+\-/-9=@-\[\\^` + "`" + `a-{}]`)

// TypesAsBareURL reports whether Convert turns url, typed bare, into one
// link whose target is all of url. A URL with a space or a non-ASCII
// character fails: the matcher stops there and the link would be cut.
func TypesAsBareURL(url string) bool {
	return typedURLRegexp.FindString(url) == url
}

// typedURLProtocols lists the "scheme:" names Linkify may start a match at,
// taken from linkscheme.Prefixes. mailto: is left to Linkify's email matcher.
var typedURLProtocols = func() []string {
	var out []string
	for _, p := range linkscheme.Prefixes {
		name, _, _ := strings.Cut(p, ":")
		if name != "mailto" {
			out = append(out, name+":")
		}
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
		goldmark.WithParserOptions(parser.WithInlineParsers(
			// After the default parsers, so code spans and links claim
			// their punctuation first (typedurl.go).
			util.Prioritized(afterPunctURL{}, 900),
		)),
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
