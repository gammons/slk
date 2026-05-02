package mrkdwn

import (
	"strings"

	"github.com/slack-go/slack"
	"github.com/yuin/goldmark/ast"
	extensionAST "github.com/yuin/goldmark/extension/ast"
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
	case *ast.Emphasis:
		if n.Level == 2 {
			w.walkBold(n)
			return
		}
		// Level 1: distinguish _italic_ from *italic*. Goldmark's
		// emphasis node doesn't expose the delimiter byte directly,
		// so we look at the byte immediately before the first child
		// text segment.
		if w.emphasisDelimiter(n) == '_' {
			w.walkItalic(n)
		} else {
			w.walkAsteriskLiteral(n)
		}
	case *ast.CodeSpan:
		w.mrkdwn.WriteString("`")
		prev := w.inheritedStyle
		w.inheritedStyle.Code = true
		w.walkInlineChildren(n)
		w.inheritedStyle = prev
		w.mrkdwn.WriteString("`")
	case *ast.Link:
		w.handleLink(n)
	case *extensionAST.Strikethrough:
		w.mrkdwn.WriteString("~")
		prev := w.inheritedStyle
		w.inheritedStyle.Strike = true
		w.walkInlineChildren(n)
		w.inheritedStyle = prev
		w.mrkdwn.WriteString("~")
	default:
		// Other inline nodes are handled in later tasks. Walk
		// children to preserve text.
		w.walkInlineChildren(n)
	}
}

// walkBold emits *body* mrkdwn and sets the bold style flag for the
// duration of the inline-children walk. Save/restore inheritedStyle
// so nested formatting (e.g. **bold _italic_**) correctly composes
// the styles.
func (w *walker) walkBold(n *ast.Emphasis) {
	w.mrkdwn.WriteString("*")
	prev := w.inheritedStyle
	w.inheritedStyle.Bold = true
	w.walkInlineChildren(n)
	w.inheritedStyle = prev
	w.mrkdwn.WriteString("*")
}

// emphasisDelimiter returns the byte used to open the emphasis node n
// ('_' or '*'). The source contains a stack of opening delimiters
// before the first text descendant: outermost first, innermost last.
// To locate n's own opener we skip past the openers belonging to any
// emphasis nodes nested INSIDE n (each Level-1 emphasis consumes one
// byte; a Level-2 bold consumes two), then read the byte that n itself
// contributed. Returns '_' as a safe default for malformed input.
func (w *walker) emphasisDelimiter(n *ast.Emphasis) byte {
	first := findFirstTextDescendant(n)
	if first == nil {
		return '_'
	}
	// Sum of delimiter widths for emphasis nodes strictly between n
	// and the text descendant.
	innerWidth := 0
	for c := n.FirstChild(); c != nil; {
		em, ok := c.(*ast.Emphasis)
		if !ok {
			// Walk into non-emphasis container looking for the path
			// to first; if we find emphasis ancestors of first, they
			// add to innerWidth too. For our current grammar this
			// branch is unused (emphasis only nests directly), but
			// stay defensive.
			next := c.FirstChild()
			if next == nil {
				break
			}
			c = next
			continue
		}
		innerWidth += em.Level
		c = em.FirstChild()
	}
	pos := first.Segment.Start - innerWidth - 1
	if pos < 0 || pos >= len(w.source) {
		return '_'
	}
	b := w.source[pos]
	if b != '_' && b != '*' {
		return '_'
	}
	return b
}

// findFirstTextDescendant returns the leftmost *ast.Text node under n,
// or nil if there isn't one.
func findFirstTextDescendant(n ast.Node) *ast.Text {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			return t
		}
		if t := findFirstTextDescendant(c); t != nil {
			return t
		}
	}
	return nil
}

// walkItalic emits _x_ mrkdwn and sets Style.Italic for the duration
// of the inline-children walk.
func (w *walker) walkItalic(n *ast.Emphasis) {
	w.mrkdwn.WriteString("_")
	prev := w.inheritedStyle
	w.inheritedStyle.Italic = true
	w.walkInlineChildren(n)
	w.inheritedStyle = prev
	w.mrkdwn.WriteString("_")
}

// walkAsteriskLiteral preserves *x* as literal text in both outputs.
// The asterisks become text elements (no italic style of their own)
// in the rich text block; if an enclosing emphasis is in scope, they
// inherit ITS style via inheritedStyle so they visually flow with the
// surrounding text. Inline formatting inside the run (e.g. *hello
// **bold***) is still processed via walkInlineChildren — only the
// outer asterisk pair is treated as literal text.
func (w *walker) walkAsteriskLiteral(n *ast.Emphasis) {
	w.appendText("*")
	w.walkInlineChildren(n)
	w.appendText("*")
}

// handleLink emits a CommonMark [label](url) as Slack mrkdwn
// <url|label> and a RichTextSectionLinkElement in the block.
//
// If the URL contains '|', we emit the bare-URL form <url> on the
// mrkdwn side (Slack's wire format has no escape mechanism for pipes
// in URLs; with a label, the parser would split on the first pipe
// and produce a wrong URL). The block element still carries URL and
// label as separate fields, so block-rendering Slack clients see the
// labeled link correctly.
func (w *walker) handleLink(n *ast.Link) {
	url := string(n.Destination)
	label := w.collectInlineText(n)

	if strings.Contains(url, "|") {
		w.mrkdwn.WriteString("<")
		w.mrkdwn.WriteString(url)
		w.mrkdwn.WriteString(">")
	} else {
		w.mrkdwn.WriteString("<")
		w.mrkdwn.WriteString(url)
		w.mrkdwn.WriteString("|")
		w.mrkdwn.WriteString(label)
		w.mrkdwn.WriteString(">")
	}

	if w.curSection == nil {
		w.curSection = slack.NewRichTextSection()
	}
	link := slack.NewRichTextSectionLinkElement(url, label, w.copyStyle())
	w.curSection.Elements = append(w.curSection.Elements, link)
}

// collectInlineText concatenates the text content of n's children,
// stripping inline formatting markers. Used for link labels where
// Slack's wire form expects plain text after the '|'.
func (w *walker) collectInlineText(n ast.Node) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(c ast.Node) {
		if t, ok := c.(*ast.Text); ok {
			b.Write(w.source[t.Segment.Start:t.Segment.Stop])
			return
		}
		for cc := c.FirstChild(); cc != nil; cc = cc.NextSibling() {
			walk(cc)
		}
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		walk(c)
	}
	// Restore Slack wire-form tokens that may live inside the label.
	return detokenizeText(b.String(), w.table)
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
// style. Slack wire-form sentinels embedded in s are split out into
// typed rich_text elements (user / channel / broadcast / link) and
// restored to their original <...> form in the mrkdwn output.
func (w *walker) appendText(s string) {
	if s == "" {
		return
	}
	// Mrkdwn side: write as-is. The detokenize pass at the end of
	// Convert restores all sentinels in one go.
	w.mrkdwn.WriteString(s)

	// Block side: split on sentinels.
	if w.curSection == nil {
		w.curSection = slack.NewRichTextSection()
	}
	i := 0
	for i < len(s) {
		idx, end, ok := parseSentinel(s, i)
		if !ok {
			// Find the next sentinel boundary (or end of string)
			// and emit the run between [i, j) as a text element.
			j := nextSentinelStart(s, i)
			if j > i {
				te := slack.NewRichTextSectionTextElement(s[i:j], w.copyStyle())
				w.curSection.Elements = append(w.curSection.Elements, te)
			}
			i = j
			continue
		}
		if idx >= 0 && idx < len(w.table) {
			w.curSection.Elements = append(w.curSection.Elements, w.tokenElement(w.table[idx]))
		} else {
			// Out-of-range index, emit raw bytes.
			te := slack.NewRichTextSectionTextElement(s[i:end], w.copyStyle())
			w.curSection.Elements = append(w.curSection.Elements, te)
		}
		i = end
	}
}

// nextSentinelStart returns the byte index of the next sentinelStart
// rune in s at or after i, or len(s) if none.
func nextSentinelStart(s string, i int) int {
	idx := strings.IndexRune(s[i:], sentinelStart)
	if idx < 0 {
		return len(s)
	}
	return i + idx
}

// tokenElement converts a wire-form token into the corresponding
// rich_text element with the current inherited style applied where
// the schema supports a style.
func (w *walker) tokenElement(t token) slack.RichTextSectionElement {
	style := w.copyStyle()
	switch t.kind {
	case tokUser:
		return slack.NewRichTextSectionUserElement(t.id, style)
	case tokChannel:
		return slack.NewRichTextSectionChannelElement(t.id, style)
	case tokBroadcast:
		// Broadcasts don't carry a style on the wire (slack-go's
		// RichTextSectionBroadcastElement has no Style field).
		return slack.NewRichTextSectionBroadcastElement(t.id)
	case tokLink:
		text := t.label
		if text == "" {
			text = t.id
		}
		return slack.NewRichTextSectionLinkElement(t.id, text, style)
	}
	return nil
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
