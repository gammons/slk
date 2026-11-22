package mrkdwn

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// blockJSON serialises a *slack.RichTextBlock to JSON for readable
// test failure messages.
func blockJSON[A any](a A) string {
	out, _ := json.Marshal(a)
	return string(out)
}

func TestConvert_EmptyInput(t *testing.T) {
	blk := Convert("")
	if blk != nil {
		t.Errorf("block = %s, want nil", blockJSON(blk))
	}
}

func TestConvert_WhitespaceOnly(t *testing.T) {
	blk := Convert("   \n  ")
	if blk != nil {
		t.Errorf("block = %s, want nil", blockJSON(blk))
	}
}

func TestConvert_PlainText(t *testing.T) {
	blk := Convert("hello world")
	if blk == nil {
		t.Fatal("block is nil")
	}
	if len(blk.Elements) != 1 {
		t.Fatalf("got %d elements, want 1", len(blk.Elements))
	}
	sec, ok := blk.Elements[0].(*slack.RichTextSection)
	if !ok {
		t.Fatalf("element[0] is %T, want *RichTextSection", blk.Elements[0])
	}
	if len(sec.Elements) != 1 {
		t.Fatalf("section has %d elements, want 1", len(sec.Elements))
	}
	te, ok := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if !ok {
		t.Fatalf("section element is %T, want *RichTextSectionTextElement", sec.Elements[0])
	}
	if te.Text != "hello world" {
		t.Errorf("text = %q, want %q", te.Text, "hello world")
	}
	if te.Style != nil {
		t.Errorf("style = %+v, want nil", te.Style)
	}
}

func TestConvert_TwoParagraphs(t *testing.T) {
	blk := Convert("para one\n\npara two")
	if blk == nil {
		t.Fatal("block is nil")
	}
	if len(blk.Elements) != 1 {
		t.Fatalf("got %d elements, want 1 (one section for both paragraphs)", len(blk.Elements))
	}
	sec, ok := blk.Elements[0].(*slack.RichTextSection)
	if !ok {
		t.Fatalf("element[0] is %T, want *RichTextSection", blk.Elements[0])
	}
	if len(sec.Elements) != 1 {
		t.Fatalf("section has %d elements, want 1", len(sec.Elements))
	}
	te, ok := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if !ok {
		t.Fatalf("section element is %T, want *RichTextSectionTextElement", sec.Elements[0])
	}
	want := "para one\n\npara two"
	if te.Text != want {
		t.Fatalf("text = %q, want %q", te.Text, want)
	}
}

func TestConvert_HTMLBlockPreserved(t *testing.T) {
	blk := Convert("<p>raw html</p>")
	if blk == nil {
		t.Fatal("block is nil; expected one section with HTML as literal text")
	}
	sec := blk.Elements[0].(*slack.RichTextSection)
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "<p>raw html</p>" {
		t.Errorf("text = %q, want %q", te.Text, "<p>raw html</p>")
	}
	if te.Style != nil {
		t.Errorf("style = %+v, want nil for HTML passthrough", te.Style)
	}
}

func TestConvert_InlineHTMLPreserved(t *testing.T) {
	// Inline HTML inside a paragraph; verify it survives.
	blk := Convert(`hello <span class="x">world</span> foo`)
	want := `hello <span class="x">world</span> foo`
	sec := blk.Elements[0].(*slack.RichTextSection)
	var got string
	for _, el := range sec.Elements {
		te, ok := el.(*slack.RichTextSectionTextElement)
		if !ok {
			t.Fatalf("element is %T, want only text elements", el)
		}
		got += te.Text
	}
	if got != want {
		t.Errorf("concatenated text = %q, want %q", got, want)
	}
}

func TestConvert_BoldWithSurroundingText(t *testing.T) {
	blk := Convert("hi *there* friend")
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 3 {
		t.Fatalf("got %d elements, want 3 (plain, bold, plain)", len(sec.Elements))
	}
	left := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if left.Style != nil {
		t.Errorf("leading element style = %+v, want nil (bold leaked left)", left.Style)
	}
	mid := sec.Elements[1].(*slack.RichTextSectionTextElement)
	if mid.Text != "there" || mid.Style == nil || !mid.Style.Bold {
		t.Errorf("middle element = %+v / style %+v, want bold 'there'", mid, mid.Style)
	}
	right := sec.Elements[2].(*slack.RichTextSectionTextElement)
	if right.Style != nil {
		t.Errorf("trailing element style = %+v, want nil (bold leaked right)", right.Style)
	}
}

func TestConvert_ItalicUnderscore(t *testing.T) {
	blk := Convert("_hello_")
	sec := blk.Elements[0].(*slack.RichTextSection)
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "hello" {
		t.Errorf("text = %q, want %q", te.Text, "hello")
	}
	if te.Style == nil || !te.Style.Italic {
		t.Errorf("expected Style.Italic = true, got %+v", te.Style)
	}
}

func TestConvert_SingleAsteriskIsBold(t *testing.T) {
	// *x* in CommonMark is italic, but Slack's own mrkdwn treats a
	// single asterisk pair as bold. slk treats it as bold (same as
	// **x**) so a message that renders bold in slk's own compose/
	// message view also renders bold in real Slack.
	blk := Convert("*hello*")
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 1 {
		t.Fatalf("elements = %d, want 1: %s", len(sec.Elements), blockJSON(blk))
	}
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "hello" {
		t.Errorf("text = %q, want %q", te.Text, "hello")
	}
	if te.Style == nil || !te.Style.Bold {
		t.Errorf("expected Style.Bold = true, got %+v", te.Style)
	}
}

func TestConvert_NestedItalicAroundBold(t *testing.T) {
	// _*both*_ : outer italic (underscore), inner bold (single
	// asterisk pair, Slack's native bold syntax). Both styles apply
	// to "both".
	blk := Convert("_*both*_")
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 1 {
		t.Fatalf("elements = %d, want 1: %s", len(sec.Elements), blockJSON(blk))
	}
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "both" {
		t.Errorf("text = %q, want %q", te.Text, "both")
	}
	if te.Style == nil || !te.Style.Italic || !te.Style.Bold {
		t.Errorf("style = %+v, want italic+bold", te.Style)
	}
}

func TestConvert_NestedBoldAroundItalic(t *testing.T) {
	// *_both_* : outer bold (single asterisk pair), inner italic
	// (underscore). Both styles apply to "both".
	blk := Convert("*_both_*")
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 1 {
		t.Fatalf("elements = %d, want 1: %s", len(sec.Elements), blockJSON(blk))
	}
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "both" {
		t.Errorf("text = %q, want %q", te.Text, "both")
	}
	if te.Style == nil || !te.Style.Italic || !te.Style.Bold {
		t.Errorf("style = %+v, want italic+bold", te.Style)
	}
}

func TestConvert_TwoSeparateItalics(t *testing.T) {
	// _a_ and _b_ : two italics in one paragraph.
	blk := Convert("_a_ and _b_")

	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 3 {
		t.Fatalf("elements = %d, want 3: %s", len(sec.Elements), blockJSON(blk))
	}
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "a" {
		t.Errorf("text = %q, want %q", te.Text, "a")
	}
	if te.Style == nil || !te.Style.Italic {
		t.Errorf("style = %+v, want italic", te.Style)
	}
	te = sec.Elements[2].(*slack.RichTextSectionTextElement)
	if te.Text != "b" {
		t.Errorf("text = %q, want %q", te.Text, "b")
	}
	if te.Style == nil || !te.Style.Italic {
		t.Errorf("style = %+v, want italic", te.Style)
	}
}

func TestConvert_ItalicContainingBold(t *testing.T) {
	// _x **y** z_ : italic envelope, bold inside.
	// The "y" should carry BOTH italic and bold.
	blk := Convert("_x **y** z_")
	sec := blk.Elements[0].(*slack.RichTextSection)
	for _, el := range sec.Elements {
		te := el.(*slack.RichTextSectionTextElement)
		if te.Text == "y" {
			if te.Style == nil || !te.Style.Italic || !te.Style.Bold {
				t.Errorf("'y' element style = %+v, want italic+bold", te.Style)
			}
			return
		}
	}
	t.Error("did not find a 'y' text element")
}

func TestConvert_Strikethrough(t *testing.T) {
	blk := Convert("~done~")
	te := blk.Elements[0].(*slack.RichTextSection).Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "done" || te.Style == nil || !te.Style.Strike {
		t.Errorf("got %+v / %+v, want strike=true on 'done'", te, te.Style)
	}
}

func TestConvert_InlineCode(t *testing.T) {
	blk := Convert("type `make build` to compile")
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 3 {
		t.Fatalf("got %d elements, want 3 (text, code, text)", len(sec.Elements))
	}
	mid := sec.Elements[1].(*slack.RichTextSectionTextElement)
	if mid.Text != "make build" || mid.Style == nil || !mid.Style.Code {
		t.Errorf("middle = %+v / %+v, want code 'make build'", mid, mid.Style)
	}
}

func TestConvert_LinkLabeled(t *testing.T) {
	blk := Convert("see [Slack](https://slack.com) docs")
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 3 {
		t.Fatalf("got %d elements, want 3 (text, link, text)", len(sec.Elements))
	}
	link, ok := sec.Elements[1].(*slack.RichTextSectionLinkElement)
	if !ok {
		t.Fatalf("middle element is %T, want *RichTextSectionLinkElement", sec.Elements[1])
	}
	if link.URL != "https://slack.com" || link.Text != "Slack" {
		t.Errorf("link = %+v", link)
	}
}

func TestConvert_LinkLabelEqualsURL(t *testing.T) {
	blk := Convert("see [https://x.com](https://x.com)")
	link := blk.Elements[0].(*slack.RichTextSection).Elements[1].(*slack.RichTextSectionLinkElement)
	if link.URL != "https://x.com" {
		t.Errorf("URL = %q", link.URL)
	}
}

func TestConvert_LinkWithPipeInURL(t *testing.T) {
	// URLs with '|' (e.g., tracking parameters) cannot use the
	// <url|label> wire form because Slack's parser would split on
	// the first pipe and corrupt the URL. We emit <url> only on the
	// mrkdwn side; the block element still carries URL and label
	// as separate fields.
	blk := Convert("[click](https://x.com?a=1|b=2)")
	link := blk.Elements[0].(*slack.RichTextSection).Elements[0].(*slack.RichTextSectionLinkElement)
	if link.URL != "https://x.com?a=1|b=2" {
		t.Errorf("block URL = %q, want full URL preserved", link.URL)
	}
	if link.Text != "click" {
		t.Errorf("block label = %q, want %q", link.Text, "click")
	}
}

func TestConvert_LinkLabelNoFormatting(t *testing.T) {
	blk := Convert("[*bold*](https://x.com)")
	link := blk.Elements[0].(*slack.RichTextSection).Elements[0].(*slack.RichTextSectionLinkElement)
	if link.Text != "*bold*" {
		t.Errorf("block label = %q, want %q", link.Text, "*bold*")
	}
}

func TestConvert_LinkInsideBoldCarriesBoldStyle(t *testing.T) {
	// **[link](url)** : the link element should inherit Bold style
	// from the enclosing emphasis.
	blk := Convert("*[click](https://x.com)*")
	sec := blk.Elements[0].(*slack.RichTextSection)
	for _, el := range sec.Elements {
		if link, ok := el.(*slack.RichTextSectionLinkElement); ok {
			if link.Style == nil || !link.Style.Bold {
				t.Errorf("link style = %+v, want Bold=true", link.Style)
			}
			return
		}
	}
	t.Error("did not find a link element")
}

func TestConvert_StrikethroughInsideBold(t *testing.T) {
	// Verify ~~strike~~ nested in **bold** composes styles correctly.
	blk := Convert("**bold ~~strike~~ bold**")
	sec := blk.Elements[0].(*slack.RichTextSection)
	// Find the "strike" element and verify both Bold and Strike.
	for _, el := range sec.Elements {
		te, ok := el.(*slack.RichTextSectionTextElement)
		if !ok {
			continue
		}
		if te.Text == "strike" {
			if te.Style == nil || !te.Style.Bold || !te.Style.Strike {
				t.Errorf("'strike' style = %+v, want Bold+Strike", te.Style)
			}
			return
		}
	}
	t.Error("did not find a 'strike' text element")
}

func TestConvert_UserMention(t *testing.T) {
	blk := Convert("hi <@U12345>!")
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 3 {
		t.Fatalf("got %d elements, want 3 (text, user, text)", len(sec.Elements))
	}
	user, ok := sec.Elements[1].(*slack.RichTextSectionUserElement)
	if !ok {
		t.Fatalf("middle is %T, want *RichTextSectionUserElement", sec.Elements[1])
	}
	if user.UserID != "U12345" {
		t.Errorf("UserID = %q, want U12345", user.UserID)
	}
}

func TestConvert_ChannelMention(t *testing.T) {
	blk := Convert("see <#C123> please")
	ch := blk.Elements[0].(*slack.RichTextSection).Elements[1].(*slack.RichTextSectionChannelElement)
	if ch.ChannelID != "C123" {
		t.Errorf("ChannelID = %q, want C123", ch.ChannelID)
	}
}

func TestConvert_Broadcast(t *testing.T) {
	blk := Convert("<!here> deploy now")
	bc := blk.Elements[0].(*slack.RichTextSection).Elements[0].(*slack.RichTextSectionBroadcastElement)
	if bc.Range != "here" {
		t.Errorf("Range = %q, want here", bc.Range)
	}
}

// TestConvert_Usergroup: @team mentions are a usergroup element, not a
// broadcast range. Sending them as a broadcast makes Slack reject the
// whole message with invalid_blocks.
func TestConvert_Usergroup(t *testing.T) {
	blk := Convert("<!subteam^S01> please review")
	ug := blk.Elements[0].(*slack.RichTextSection).Elements[0].(*slack.RichTextSectionUserGroupElement)
	if ug.UsergroupID != "S01" {
		t.Errorf("UsergroupID = %q, want S01", ug.UsergroupID)
	}
}

func TestConvert_UsergroupPreservesStyle(t *testing.T) {
	blk := Convert("*<!subteam^S01>*")
	ug := blk.Elements[0].(*slack.RichTextSection).Elements[0].(*slack.RichTextSectionUserGroupElement)
	if ug.Style == nil || !ug.Style.Bold {
		t.Fatalf("usergroup style = %+v, want bold", ug.Style)
	}
}

// TestConvert_UnknownBroadcastStaysText: reBroadcast matches any
// <!word>, but Slack only accepts here/channel/everyone as ranges.
// Unknown ones must degrade to literal text, not fail the send.
func TestConvert_UnknownBroadcastStaysText(t *testing.T) {
	blk := Convert("<!bogus> hi")
	el := blk.Elements[0].(*slack.RichTextSection).Elements[0]
	te, ok := el.(*slack.RichTextSectionTextElement)
	if !ok {
		t.Fatalf("element = %T, want text element", el)
	}
	if te.Text != "<!bogus> hi" {
		t.Errorf("Text = %q, want %q", te.Text, "<!bogus> hi")
	}
}

func TestConvert_BoldContainingMention(t *testing.T) {
	blk := Convert("*Hi <@U1>*")
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 2 {
		t.Fatalf("got %d elements, want 2 (text, user)", len(sec.Elements))
	}
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "Hi " || te.Style == nil || !te.Style.Bold {
		t.Errorf("text = %+v / %+v, want bold 'Hi '", te, te.Style)
	}
	user := sec.Elements[1].(*slack.RichTextSectionUserElement)
	if user.UserID != "U1" {
		t.Errorf("UserID = %q", user.UserID)
	}
	if user.Style == nil || !user.Style.Bold {
		t.Errorf("user.Style = %+v, want bold inherited", user.Style)
	}
}

func TestConvert_UnorderedList(t *testing.T) {
	blk := Convert("- one\n- two")
	if len(blk.Elements) != 1 {
		t.Fatalf("got %d block elements, want 1 list", len(blk.Elements))
	}
	list, ok := blk.Elements[0].(*slack.RichTextList)
	if !ok {
		t.Fatalf("element[0] is %T, want *RichTextList", blk.Elements[0])
	}
	if list.Style != slack.RTEListBullet {
		t.Errorf("list style = %q, want bullet", list.Style)
	}
	if list.Indent != 0 {
		t.Errorf("Indent = %d, want 0", list.Indent)
	}
	if len(list.Elements) != 2 {
		t.Fatalf("got %d items, want 2", len(list.Elements))
	}
	first := list.Elements[0].(*slack.RichTextSection)
	te := first.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "one" {
		t.Errorf("first item text = %q", te.Text)
	}
}

func TestConvert_OrderedList(t *testing.T) {
	blk := Convert("1. one\n2. two")
	list := blk.Elements[0].(*slack.RichTextList)
	if list.Style != slack.RTEListOrdered {
		t.Errorf("list style = %q, want ordered", list.Style)
	}
}

func TestConvert_NestedList(t *testing.T) {
	blk := Convert("- a\n    - b")
	// Block side: two RichTextList elements at the top level
	// (slack's wire shape — nested lists are flat with Indent=N).
	if len(blk.Elements) != 2 {
		t.Fatalf("got %d block elements, want 2 lists (flattened nested)", len(blk.Elements))
	}
	outer := blk.Elements[0].(*slack.RichTextList)
	if outer.Indent != 0 {
		t.Errorf("outer.Indent = %d, want 0", outer.Indent)
	}
	inner := blk.Elements[1].(*slack.RichTextList)
	if inner.Indent != 1 {
		t.Errorf("inner.Indent = %d, want 1", inner.Indent)
	}
}

func TestConvert_FencedCodeBlock(t *testing.T) {
	in := "```\nfoo\nbar\n```"
	blk := Convert(in)
	if len(blk.Elements) != 1 {
		t.Fatalf("got %d elements, want 1", len(blk.Elements))
	}
	pre, ok := blk.Elements[0].(*slack.RichTextPreformatted)
	if !ok {
		t.Fatalf("element[0] is %T, want *RichTextPreformatted", blk.Elements[0])
	}
	if len(pre.Elements) != 1 {
		t.Fatalf("got %d preformatted children, want 1 text", len(pre.Elements))
	}
	te := pre.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "foo\nbar\n" {
		t.Errorf("text = %q, want \"foo\\nbar\\n\"", te.Text)
	}
}

func TestConvert_HeadingPlainText(t *testing.T) {
	blk := Convert("# My Title")
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 1 {
		t.Fatalf("got %d elements, want 1 plain text", len(sec.Elements))
	}
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "# My Title" {
		t.Errorf("text = %q, want %q", te.Text, "# My Title")
	}
	if te.Style != nil {
		t.Errorf("style = %+v, want nil", te.Style)
	}
}

func TestConvert_Blockquote(t *testing.T) {
	blk := Convert("> a quoted line")
	q, ok := blk.Elements[0].(*slack.RichTextQuote)
	if !ok {
		t.Fatalf("element is %T, want *RichTextQuote; block=%s", blk.Elements[0], blockJSON(blk))
	}
	te := q.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "a quoted line" {
		t.Errorf("text = %q", te.Text)
	}
}

// A quote keeps its line breaks and its inline content: formatting, code
// and links. It used to go out as flattened plain text, with a URL in it
// dropped altogether.
func TestConvert_BlockquoteKeepsInlineContent(t *testing.T) {
	blk := Convert("> first *bold*\n> see https://example.com/x `code`\n\nafter")
	q, ok := blk.Elements[0].(*slack.RichTextQuote)
	if !ok {
		t.Fatalf("element is %T, want *RichTextQuote; block=%s", blk.Elements[0], blockJSON(blk))
	}
	var link *slack.RichTextSectionLinkElement
	var bold, code bool
	for _, el := range q.Elements {
		switch e := el.(type) {
		case *slack.RichTextSectionLinkElement:
			link = e
		case *slack.RichTextSectionTextElement:
			bold = bold || (e.Style != nil && e.Style.Bold && e.Text == "bold")
			code = code || (e.Style != nil && e.Style.Code && e.Text == "code")
		}
	}
	if link == nil || link.URL != "https://example.com/x" {
		t.Errorf("link = %+v, want https://example.com/x; block=%s", link, blockJSON(blk))
	}
	if !bold || !code {
		t.Errorf("bold=%v code=%v, want both; block=%s", bold, code, blockJSON(blk))
	}
	if _, ok := blk.Elements[1].(*slack.RichTextSection); !ok || len(blk.Elements) != 2 {
		t.Errorf("want the quote then one section; block=%s", blockJSON(blk))
	}
}

// A blank quote line between paragraphs is kept on both sides, as it was
// when quotes went out verbatim.
func TestConvert_BlockquoteParagraphs(t *testing.T) {
	blk := Convert("> one\n>\n> two")
	q, ok := blk.Elements[0].(*slack.RichTextQuote)
	if !ok {
		t.Fatalf("element is %T, want *RichTextQuote; block=%s", blk.Elements[0], blockJSON(blk))
	}
	var text strings.Builder
	for _, e := range q.Elements {
		if te, ok := e.(*slack.RichTextSectionTextElement); ok {
			text.WriteString(te.Text)
		}
	}
	if want := "one\n\ntwo"; text.String() != want {
		t.Errorf("quote text = %q, want %q; block=%s", text.String(), want, blockJSON(blk))
	}
}

// ---------------------------------------------------------------------------
// Bare URL autolinking (Linkify)
//
// Slack renders the rich_text block when present, so a bare URL typed by
// the user must be emitted as a RichTextSectionLinkElement, not as a
// plain text element. The mrkdwn fallback must also wrap the URL in
// <...> so Slack's mrkdwn parser would render it as a link if the block
// were ever stripped.
// ---------------------------------------------------------------------------

func TestConvert_BareHTTPSAutolinked(t *testing.T) {
	blk := Convert("visit https://example.com today")
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 3 {
		t.Fatalf("got %d elements, want 3 (text, link, text); block=%s", len(sec.Elements), blockJSON(blk))
	}
	link, ok := sec.Elements[1].(*slack.RichTextSectionLinkElement)
	if !ok {
		t.Fatalf("middle element is %T, want *RichTextSectionLinkElement", sec.Elements[1])
	}
	if link.URL != "https://example.com" {
		t.Errorf("link URL = %q, want %q", link.URL, "https://example.com")
	}
}

func TestConvert_BareHTTPAutolinked(t *testing.T) {
	blk := Convert("see http://example.com")
	sec := blk.Elements[0].(*slack.RichTextSection)
	var link *slack.RichTextSectionLinkElement
	for _, el := range sec.Elements {
		if l, ok := el.(*slack.RichTextSectionLinkElement); ok {
			link = l
			break
		}
	}
	if link == nil {
		t.Fatalf("no link element found; block=%s", blockJSON(blk))
	}
	if link.URL != "http://example.com" {
		t.Errorf("link URL = %q, want %q", link.URL, "http://example.com")
	}
}

func TestConvert_TypedURLSchemes(t *testing.T) {
	cases := []struct {
		name, in string
		want     []string // link URLs, in order; empty means no link
	}{
		{"ip host with port", "see http://192.0.2.3:8080/browse/X-1 ok", []string{"http://192.0.2.3:8080/browse/X-1"}},
		{"path with dot", "see https://example.com/foo.txt, http://example.com/résumé.txt", []string{"https://example.com/foo.txt", "http://example.com/résumé.txt"}},
		{"https ip host", "https://192.0.2.3:8443/a", []string{"https://192.0.2.3:8443/a"}},
		{"localhost", "http://localhost:3000/x", []string{"http://localhost:3000/x"}},
		{"ftp", "ftp://example.com/f", []string{"ftp://example.com/f"}},
		{"ssh", "ssh://192.0.2.3", []string{"ssh://192.0.2.3"}},
		{"trailing period", "see http://192.0.2.3/a.", []string{"http://192.0.2.3/a"}},
		{"formatting", "see *_http://192.0.2.3/a_*.", []string{"http://192.0.2.3/a"}},
		{"trailing comma", "http://192.0.2.3/a, then", []string{"http://192.0.2.3/a"}},
		{"trailing paren", "(http://192.0.2.3/a)", []string{"http://192.0.2.3/a"}},
		{"balanced trailing paren", "https://en.wikipedia.org/wiki/Mercury_(planet)", []string{"https://en.wikipedia.org/wiki/Mercury_(planet)"}},
		{"balanced paren inside parens", "(see https://en.wikipedia.org/wiki/Mercury_(planet))", []string{"https://en.wikipedia.org/wiki/Mercury_(planet)"}},
		{"query and fragment", "https://example.com/a?x=1&y=2#frag", []string{"https://example.com/a?x=1&y=2#frag"}},
		{"trailing semicolon bang question", "http://192.0.2.3/a; http://192.0.2.4/b! http://192.0.2.5/c?",
			[]string{"http://192.0.2.3/a", "http://192.0.2.4/b", "http://192.0.2.5/c"}},
		{"code span", "`http://192.0.2.3/a`", nil},
		{"code block", "```\nhttp://192.0.2.3/a\n```", nil},
		{"markdown link", "[x](http://192.0.2.3/a)", []string{"http://192.0.2.3/a"}},
		{"javascript", "javascript:alert(1)", nil},
		{"data", "data:text/html,hi", nil},
		{"vbscript", "vbscript:msgbox", nil},
		{"followed by a mention", "http://localhost:3000/a,<@UFAKE0001>", []string{"http://localhost:3000/a"}},
		{"followed by a channel", "http://localhost:3000/a<#CFAKE0001|general>", []string{"http://localhost:3000/a"}},
		{"followed by an emoji", "http://example.com/x:tada:", []string{"http://example.com/x:tada"}},
		{"followed by CJK text", "https://example.com/aを確認してください", []string{"https://example.com/aを確認してください"}},
		// {"followed by full-width paren", "https://example.com/a）", []string{"https://example.com/a"}},
		{"pipe ends the url", "http://a.example|other", []string{"http://a.example"}},
		// Host-less targets: nothing to open, so no link (http:///, file:foo
		// do link, like any scheme followed by a path).
		{"http scheme only", "http://", nil},
		{"http scheme then space", "http:// x", nil},
		{"bare file", "file:", nil},
		{"bare tel", "tel:", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			blk := Convert(c.in)
			var got []string
			for _, e := range blk.Elements {
				s, ok := e.(*slack.RichTextSection)
				if !ok {
					continue
				}
				for _, el := range s.Elements {
					if l, ok := el.(*slack.RichTextSectionLinkElement); ok {
						got = append(got, l.URL)
						if strings.ContainsAny(l.URL, "") {
							t.Errorf("link URL %q holds a tokenizer sentinel", l.URL)
						}
					}
				}
			}
			if strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Errorf("links = %q, want %q; block=%s", got, c.want, blockJSON(blk))
			}
		})
	}
}

func TestConvert_BareWWWAutolinked(t *testing.T) {
	// goldmark's Linkify prepends "http://" to www-only matches.
	blk := Convert("visit www.example.com")
	sec := blk.Elements[0].(*slack.RichTextSection)
	var link *slack.RichTextSectionLinkElement
	for _, el := range sec.Elements {
		if l, ok := el.(*slack.RichTextSectionLinkElement); ok {
			link = l
			break
		}
	}
	if link == nil {
		t.Fatalf("no link element found; block=%s", blockJSON(blk))
	}
	if link.URL != "www.example.com" {
		t.Errorf("link URL = %q, want %q", link.URL, "www.example.com")
	}
}

func TestConvert_BareEmailAutolinked(t *testing.T) {
	// goldmark's Linkify emits an AutoLink with Protocol="mailto" for
	// email addresses; URL() therefore returns "mailto:foo@bar.com".
	blk := Convert("email foo@bar.com please")
	sec := blk.Elements[0].(*slack.RichTextSection)
	var link *slack.RichTextSectionLinkElement
	for _, el := range sec.Elements {
		if l, ok := el.(*slack.RichTextSectionLinkElement); ok {
			link = l
			break
		}
	}
	if link == nil {
		t.Fatalf("no link element found; block=%s", blockJSON(blk))
	}
	if link.URL != "mailto:foo@bar.com" {
		t.Errorf("link URL = %q, want %q", link.URL, "mailto:foo@bar.com")
	}
	if link.Text != "foo@bar.com" {
		t.Errorf("link Text = %q, want %q (label is the typed address, no mailto: prefix)", link.Text, "foo@bar.com")
	}
}

func TestConvert_BareURLInsideBoldCarriesBoldStyle(t *testing.T) {
	// Autolinks under an emphasis node should inherit Bold the same
	// way explicit [text](url) links already do.
	blk := Convert("*see https://x.com*")
	sec := blk.Elements[0].(*slack.RichTextSection)
	for _, el := range sec.Elements {
		if link, ok := el.(*slack.RichTextSectionLinkElement); ok {
			if link.Style == nil || !link.Style.Bold {
				t.Errorf("link style = %+v, want Bold=true", link.Style)
			}
			return
		}
	}
	t.Errorf("did not find a link element; block=%s", blockJSON(blk))
}

// ---------------------------------------------------------------------------
// Emoji shortcodes
//
// Slack's rich_text renderer does NOT convert :name: inside text
// elements; it expects typed RichTextSectionEmojiElement entries. The
// mrkdwn fallback keeps :name: as-is because Slack mrkdwn renders it.
// ---------------------------------------------------------------------------

func TestConvert_EmojiShortcode(t *testing.T) {
	blk := Convert("hello :smile: world")
	sec := blk.Elements[0].(*slack.RichTextSection)
	if len(sec.Elements) != 3 {
		t.Fatalf("got %d elements, want 3 (text, emoji, text); block=%s", len(sec.Elements), blockJSON(blk))
	}
	emoji, ok := sec.Elements[1].(*slack.RichTextSectionEmojiElement)
	if !ok {
		t.Fatalf("middle element is %T, want *RichTextSectionEmojiElement", sec.Elements[1])
	}
	if emoji.Name != "smile" {
		t.Errorf("emoji name = %q, want %q", emoji.Name, "smile")
	}
}

func TestConvert_EmojiShortcodeAllowedChars(t *testing.T) {
	// Slack shortcodes can contain lowercase letters, digits, +, -, _.
	// :+1: is the canonical example.
	blk := Convert(":+1: :man-tipping-hand: :slightly_smiling_face:")
	sec := blk.Elements[0].(*slack.RichTextSection)
	var names []string
	for _, el := range sec.Elements {
		if e, ok := el.(*slack.RichTextSectionEmojiElement); ok {
			names = append(names, e.Name)
		}
	}
	want := []string{"+1", "man-tipping-hand", "slightly_smiling_face"}
	if len(names) != len(want) {
		t.Fatalf("emoji names = %v, want %v; block=%s", names, want, blockJSON(blk))
	}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("emoji[%d] = %q, want %q", i, n, want[i])
		}
	}
}

func TestConvert_EmojiInsideCodeSpanIsLiteral(t *testing.T) {
	// `:smile:` inside backticks must NOT be converted to an emoji;
	// users embed colon-delimited tokens (yaml keys, irc nicks, etc.)
	// in code spans and expect them to render literally.
	blk := Convert("x `:smile:` y")
	sec := blk.Elements[0].(*slack.RichTextSection)
	for _, el := range sec.Elements {
		if _, ok := el.(*slack.RichTextSectionEmojiElement); ok {
			t.Fatalf("emoji element emitted inside code span; block=%s", blockJSON(blk))
		}
	}
	// Verify the literal ":smile:" survives as text with Code style.
	found := false
	for _, el := range sec.Elements {
		te, ok := el.(*slack.RichTextSectionTextElement)
		if !ok {
			continue
		}
		if te.Text == ":smile:" {
			if te.Style == nil || !te.Style.Code {
				t.Errorf("text element %q has style %+v, want Code=true", te.Text, te.Style)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("literal ':smile:' text element not found; block=%s", blockJSON(blk))
	}
}

func TestConvert_EmojiInsideFencedCodeIsLiteral(t *testing.T) {
	// Fenced code blocks go through walkCodeBlock which constructs
	// the text element directly; emoji detection in appendText must
	// not affect them. Verify the shortcode survives literally.
	blk := Convert("```\nhi :smile:\n```")
	// Find the preformatted element.
	var pre *slack.RichTextPreformatted
	for _, el := range blk.Elements {
		if p, ok := el.(*slack.RichTextPreformatted); ok {
			pre = p
			break
		}
	}
	if pre == nil {
		t.Fatalf("no preformatted element; block=%s", blockJSON(blk))
	}
	for _, el := range pre.Elements {
		if _, ok := el.(*slack.RichTextSectionEmojiElement); ok {
			t.Fatalf("emoji element inside fenced code; block=%s", blockJSON(blk))
		}
	}
	// First (and only) element should be a text element containing the shortcode.
	te, ok := pre.Elements[0].(*slack.RichTextSectionTextElement)
	if !ok {
		t.Fatalf("first pre element is %T, want *RichTextSectionTextElement", pre.Elements[0])
	}
	if !strings.Contains(te.Text, ":smile:") {
		t.Errorf("preformatted text = %q, want it to contain ':smile:'", te.Text)
	}
}

func TestConvert_EmojiInsideBoldCarriesBoldStyle(t *testing.T) {
	blk := Convert("*:fire:*")
	sec := blk.Elements[0].(*slack.RichTextSection)
	for _, el := range sec.Elements {
		if e, ok := el.(*slack.RichTextSectionEmojiElement); ok {
			if e.Style == nil || !e.Style.Bold {
				t.Errorf("emoji style = %+v, want Bold=true", e.Style)
			}
			return
		}
	}
	t.Errorf("did not find an emoji element; block=%s", blockJSON(blk))
}

func TestConvert_ColonWithoutClosingIsNotEmoji(t *testing.T) {
	// Plain "8:30" and "rsync: error" must NOT be misparsed as
	// emojis. The regex requires :name: (closing colon present).
	blk := Convert("meeting at 8:30 about rsync: error")
	sec := blk.Elements[0].(*slack.RichTextSection)
	for _, el := range sec.Elements {
		if e, ok := el.(*slack.RichTextSectionEmojiElement); ok {
			t.Errorf("unexpected emoji element %q; block=%s", e.Name, blockJSON(blk))
		}
	}
}
