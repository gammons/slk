package mrkdwn

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gammons/slk/internal/linkscheme"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// Typed URLs in the places Linkify does not reach. Linkify (convert.go,
// typedURLRegexp) links a bare URL inside a paragraph or list item, but:
//
//   - it starts a URL only at a line head or after a space, '*', '_', '~'
//     or '(', so "https://x" in quotes or after a colon stayed text
//     (afterPunctURL);
//   - the tokenizer runs first and read "see:https://x" as the emoji
//     :https: (tokens.go);
//   - headings and blockquotes were not parsed inline at all, so their
//     URLs were text, and a blockquote's were dropped (walk.go).
//
// The one rule for what a typed URL is stays typedURLRegexp; typedURLLen
// only applies it, with Linkify's closing-paren balancing, outside
// goldmark.

// typedURLLen returns the length of the typed URL at the start of s, as
// Linkify would link it, or 0 when s does not start with one.
func typedURLLen(s string) int {
	if !linkscheme.HasPrefix(s) {
		return 0
	}
	m := typedURLRegexp.FindString(s)
	if m == "" {
		return 0
	}
	// Linkify drops closing parens the URL did not open.
	for strings.HasSuffix(m, ")") && strings.Count(m, ")") > strings.Count(m, "(") {
		m = m[:len(m)-1]
	}
	return len(m)
}

// urlMayStartAfter reports whether a typed URL may start right after r:
// not in the middle of a word ("xhttps://" is not one).
func urlMayStartAfter(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// bareURLSpans returns the [start, end) byte ranges of the typed URLs in s,
// for text goldmark does not parse inline.
func bareURLSpans(s string) [][2]int {
	var spans [][2]int
	for i := 0; i < len(s); {
		if i == 0 || urlMayStartAfter(lastRuneOf(s[:i])) {
			if n := typedURLLen(s[i:]); n > 0 {
				spans = append(spans, [2]int{i, i + n})
				i += n
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return spans
}

func lastRuneOf(s string) rune {
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}

// afterPunctURL lets Linkify start a URL after a quote or a colon. goldmark
// offers inline parsers only punctuation and whitespace positions, and
// Linkify triggers on few of them; this one takes the punctuation mark as
// text when a typed URL follows it, and goldmark then offers Linkify the
// position right after, as the start of a run.
type afterPunctURL struct{}

func (afterPunctURL) Trigger() []byte { return []byte{'"', '\'', ':'} }

func (afterPunctURL) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, seg := block.PeekLine()
	if len(line) < 2 || typedURLLen(string(line[1:])) == 0 {
		return nil
	}
	block.Advance(1)
	return ast.NewTextSegment(seg.WithStop(seg.Start + 1))
}

// appendRawText is appendText for text goldmark does not parse inline (a
// heading, kept as typed): a typed URL in it still goes out as a link.
func (w *walker) appendRawText(s string) {
	start := 0
	for _, sp := range bareURLSpans(s) {
		w.appendText(s[start:sp[0]])
		u := s[sp[0]:sp[1]]
		w.appendLink(u, u)
		start = sp[1]
	}
	w.appendText(s[start:])
}
