package mrkdwn

import (
	"reflect"
	"testing"

	"github.com/slack-go/slack"
)

// linkURLs returns the URL of every link element in blk, in order.
func linkURLs(blk *slack.RichTextBlock) []string {
	var out []string
	walk := func(els []slack.RichTextSectionElement) {
		for _, el := range els {
			if l, ok := el.(*slack.RichTextSectionLinkElement); ok {
				out = append(out, l.URL)
			}
		}
	}
	if blk == nil {
		return nil
	}
	for _, e := range blk.Elements {
		switch e := e.(type) {
		case *slack.RichTextSection:
			walk(e.Elements)
		case *slack.RichTextQuote:
			walk(e.Elements)
		case *slack.RichTextList:
			for _, s := range e.Elements {
				if sec, ok := s.(*slack.RichTextSection); ok {
					walk(sec.Elements)
				}
			}
		}
	}
	return out
}

// Typed URLs in the places Linkify does not reach on its own: after a
// quote or a colon, in a heading and in a quote.
// Slack's clients render the block and do not linkify a text element, so
// each of these used to arrive as plain text (or, in a quote, not at all).
func TestConvert_TypedURLGaps(t *testing.T) {
	cases := []struct {
		in    string
		mr    string
		links []string
	}{
		{`"https://example.com"`, `"<https://example.com>"`, []string{"https://example.com"}},
		{`'https://example.com/x'`, `'<https://example.com/x>'`, []string{"https://example.com/x"}},
		{"see:https://example.com", "see:<https://example.com>", []string{"https://example.com"}},
		{"# Title https://example.com", "# Title <https://example.com>", []string{"https://example.com"}},
		{"# see (https://example.com/x)", "# see (<https://example.com/x>)", []string{"https://example.com/x"}},
		{"> quote https://example.com", "> quote <https://example.com>", []string{"https://example.com"}},
		{"> > nested https://example.com", "> nested <https://example.com>", []string{"https://example.com"}},
		// A scheme with no "//" after a colon is a link, not an emoji.
		{"call:tel:+15550100", "call:<tel:+15550100>", []string{"tel:+15550100"}},
		// Each line of a quote is its own text: URLs on adjacent lines
		// stay two links, at any depth.
		{"> https://one.example\n> https://two.example", "> <https://one.example>\n> <https://two.example>", []string{"https://one.example", "https://two.example"}},
		{"> > https://one.example\n> > https://two.example", "> <https://one.example>\n> <https://two.example>", []string{"https://one.example", "https://two.example"}},
		{"> > https://one.example\n> >\n> > https://two.example", "> <https://one.example>\n>\n> <https://two.example>", []string{"https://one.example", "https://two.example"}},
		// Still not links.
		{"foo:bar", "foo:bar", nil},
		{"say \"hi\"", "say \"hi\"", nil},
		{"# Title", "# Title", nil},
		{"#xhttps://example.com", "#xhttps://example.com", nil},
		// An emoji next to a URL is still an emoji.
		{":smile: https://example.com", ":smile: <https://example.com>", []string{"https://example.com"}},
	}
	for _, c := range cases {
		mr, blk := Convert(c.in)
		if mr != c.mr {
			t.Errorf("Convert(%q) mrkdwn = %q, want %q", c.in, mr, c.mr)
		}
		if got := linkURLs(blk); !reflect.DeepEqual(got, c.links) {
			t.Errorf("Convert(%q) links = %q, want %q; block=%s", c.in, got, c.links, blockJSON(blk))
		}
	}
}

func TestTypedURLLenMatchesLinkify(t *testing.T) {
	// typedURLLen applies typedURLRegexp the way Linkify does, closing
	// parens included, so a URL in a heading ends where one in a
	// paragraph would.
	for _, in := range []string{
		"https://example.com/a_(b) and more",
		"https://example.com/x). Next",
		"http://localhost:8080/path?q=1, then",
		"ftp://10.0.0.1/file.txt.",
	} {
		_, blk := Convert(in)
		want := linkURLs(blk)
		if len(want) != 1 {
			t.Fatalf("Convert(%q) links = %q, want one", in, want)
		}
		if got := in[:typedURLLen(in)]; got != want[0] {
			t.Errorf("typedURLLen(%q) gives %q, Linkify links %q", in, got, want[0])
		}
	}
}
