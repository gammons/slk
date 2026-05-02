package mrkdwn

import (
	"strings"

	"github.com/slack-go/slack"
	"github.com/yuin/goldmark/ast"
)

// walker accumulates two parallel outputs as it walks a goldmark AST:
// the mrkdwn fallback string (in mrkdwn) and a rich_text block (in block).
//
// The current rich_text section being assembled lives in curSection;
// inline element appenders (text, mention, link) push into it. Block-
// level appenders (paragraph, list, code-block) flush curSection into
// block.Elements first, then create a new container.
type walker struct {
	source []byte
	table  []token

	mrkdwn     strings.Builder
	block      *slack.RichTextBlock
	curSection *slack.RichTextSection

	// inheritedStyle is applied to every text element appended via
	// appendText. Inline-formatting walk methods toggle the relevant
	// flag for the duration of their child walk.
	inheritedStyle slack.RichTextSectionTextStyle
}

func newWalker(source []byte, table []token) *walker {
	return &walker{
		source: source,
		table:  table,
		block:  slack.NewRichTextBlock(""),
	}
}

func (w *walker) walkDocument(n ast.Node) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		w.walkBlock(c)
	}
	w.flushSection()
}

// walkBlock dispatches block-level nodes. Every block-level node
// flushes any in-progress inline section first.
func (w *walker) walkBlock(n ast.Node) {
	switch n := n.(type) {
	case *ast.Paragraph:
		w.flushSection()
		w.curSection = slack.NewRichTextSection()
		w.walkInlineChildren(n)
		w.flushSection()
		// Paragraph separator in mrkdwn is a blank line.
		w.mrkdwn.WriteString("\n\n")
	case *ast.HTMLBlock:
		w.walkRawHTMLBlock(n)
	default:
		// Other block types (List, FencedCodeBlock, Heading, Blockquote)
		// will be handled in later tasks. For now, walk children as
		// inline so we don't lose plain-text fallback content.
		w.walkInlineChildren(n)
	}
}

// flushSection moves the current in-progress section into block.Elements.
func (w *walker) flushSection() {
	if w.curSection != nil && len(w.curSection.Elements) > 0 {
		w.block.Elements = append(w.block.Elements, w.curSection)
	}
	w.curSection = nil
}

// walkInlineChildren walks the children of n as inline content.
func (w *walker) walkInlineChildren(n ast.Node) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		w.walkInline(c)
	}
}

func (w *walker) walkInline(n ast.Node) {
	switch n := n.(type) {
	case *ast.Text:
		seg := n.Segment
		w.appendText(string(w.source[seg.Start:seg.Stop]))
		if n.HardLineBreak() || n.SoftLineBreak() {
			// Slack chat preserves line layout; treat both hard and
			// soft breaks as literal newlines.
			w.appendText("\n")
		}
	case *ast.RawHTML:
		// Inline HTML (e.g. <span class=...>) — copy source bytes via
		// the segments slice so it appears as literal text. Same rationale
		// as walkRawHTMLBlock above.
		var b strings.Builder
		for i := 0; i < n.Segments.Len(); i++ {
			seg := n.Segments.At(i)
			b.Write(w.source[seg.Start:seg.Stop])
		}
		if s := b.String(); s != "" {
			w.appendText(s)
		}
	default:
		// Other inline nodes (Emphasis, CodeSpan, Link, etc.) are
		// handled in later tasks. Walk children to preserve text.
		w.walkInlineChildren(n)
	}
}

// walkRawHTMLBlock preserves block-level HTML as literal text. Goldmark
// parses HTML by default and emits HTMLBlock for things like <p>foo</p>;
// without explicit handling these nodes have no Text children and the
// content silently vanishes. We keep the source bytes intact so user-
// typed HTML survives as readable text in Slack (Slack mrkdwn doesn't
// process HTML, so this round-trips as expected).
func (w *walker) walkRawHTMLBlock(n *ast.HTMLBlock) {
	w.flushSection()
	var b strings.Builder
	for i := 0; i < n.Lines().Len(); i++ {
		seg := n.Lines().At(i)
		b.Write(w.source[seg.Start:seg.Stop])
	}
	body := strings.TrimRight(b.String(), "\n")
	if body == "" {
		return
	}
	w.mrkdwn.WriteString(body)
	w.mrkdwn.WriteString("\n\n")

	sec := slack.NewRichTextSection()
	sec.Elements = append(sec.Elements, slack.NewRichTextSectionTextElement(body, nil))
	w.block.Elements = append(w.block.Elements, sec)
}

// appendText writes s to both outputs with the current inherited
// style. Empty s is a no-op.
func (w *walker) appendText(s string) {
	if s == "" {
		return
	}
	w.mrkdwn.WriteString(s)
	if w.curSection == nil {
		w.curSection = slack.NewRichTextSection()
	}
	te := slack.NewRichTextSectionTextElement(s, w.copyStyle())
	w.curSection.Elements = append(w.curSection.Elements, te)
}

// copyStyle returns a pointer to a copy of inheritedStyle, or nil if
// no flags are set (so we don't emit "style":{} on the wire).
func (w *walker) copyStyle() *slack.RichTextSectionTextStyle {
	s := w.inheritedStyle
	if s == (slack.RichTextSectionTextStyle{}) {
		return nil
	}
	return &s
}
