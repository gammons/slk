package mrkdwn

import (
	"encoding/json"
	"testing"

	"github.com/slack-go/slack"
)

// blockJSON serialises a *slack.RichTextBlock to JSON for readable
// test failure messages.
func blockJSON(b *slack.RichTextBlock) string {
	if b == nil {
		return "nil"
	}
	out, _ := json.Marshal(b)
	return string(out)
}

func TestConvert_EmptyInput(t *testing.T) {
	mr, blk := Convert("")
	if mr != "" {
		t.Errorf("mrkdwn = %q, want empty", mr)
	}
	if blk != nil {
		t.Errorf("block = %s, want nil", blockJSON(blk))
	}
}

func TestConvert_WhitespaceOnly(t *testing.T) {
	mr, blk := Convert("   \n  ")
	if mr != "" {
		t.Errorf("mrkdwn = %q, want empty for whitespace-only input", mr)
	}
	if blk != nil {
		t.Errorf("block = %s, want nil", blockJSON(blk))
	}
}

func TestConvert_PlainText(t *testing.T) {
	mr, blk := Convert("hello world")
	if mr != "hello world" {
		t.Errorf("mrkdwn = %q, want %q", mr, "hello world")
	}
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
	mr, blk := Convert("para one\n\npara two")
	want := "para one\n\npara two"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
	if blk == nil {
		t.Fatal("block is nil")
	}
	if len(blk.Elements) != 2 {
		t.Fatalf("got %d elements, want 2 (one section per paragraph)", len(blk.Elements))
	}
}

func TestConvert_HTMLBlockPreserved(t *testing.T) {
	mr, blk := Convert("<p>raw html</p>")
	if mr != "<p>raw html</p>" {
		t.Errorf("mrkdwn = %q, want %q", mr, "<p>raw html</p>")
	}
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
	mr, blk := Convert(`hello <span class="x">world</span> foo`)
	want := `hello <span class="x">world</span> foo`
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
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

func TestConvert_BoldDoubleAsterisk(t *testing.T) {
	mr, blk := Convert("**hello**")
	if mr != "*hello*" {
		t.Errorf("mrkdwn = %q, want %q", mr, "*hello*")
	}
	sec := blk.Elements[0].(*slack.RichTextSection)
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "hello" {
		t.Errorf("text = %q, want %q", te.Text, "hello")
	}
	if te.Style == nil || !te.Style.Bold {
		t.Errorf("expected Style.Bold = true, got %+v", te.Style)
	}
}

func TestConvert_BoldDoubleUnderscore(t *testing.T) {
	mr, blk := Convert("__hello__")
	if mr != "*hello*" {
		t.Errorf("mrkdwn = %q, want %q", mr, "*hello*")
	}
	sec := blk.Elements[0].(*slack.RichTextSection)
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Style == nil || !te.Style.Bold {
		t.Errorf("expected Style.Bold = true, got %+v", te.Style)
	}
}

func TestConvert_BoldWithSurroundingText(t *testing.T) {
	mr, blk := Convert("hi **there** friend")
	if mr != "hi *there* friend" {
		t.Errorf("mrkdwn = %q, want %q", mr, "hi *there* friend")
	}
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
	mr, blk := Convert("_hello_")
	if mr != "_hello_" {
		t.Errorf("mrkdwn = %q, want %q", mr, "_hello_")
	}
	sec := blk.Elements[0].(*slack.RichTextSection)
	te := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "hello" {
		t.Errorf("text = %q, want %q", te.Text, "hello")
	}
	if te.Style == nil || !te.Style.Italic {
		t.Errorf("expected Style.Italic = true, got %+v", te.Style)
	}
}

func TestConvert_AsteriskPreservedAsLiteral(t *testing.T) {
	// *x* in CommonMark is italic, but in Slack mrkdwn it's bold.
	// We preserve the asterisks as literal characters so users who
	// type Slack-style *bold* don't get it converted to _italic_.
	mr, blk := Convert("*hello*")
	if mr != "*hello*" {
		t.Errorf("mrkdwn = %q, want %q", mr, "*hello*")
	}
	// Block side: text elements concatenate to "*hello*", no italic.
	sec := blk.Elements[0].(*slack.RichTextSection)
	var got string
	for _, el := range sec.Elements {
		te, ok := el.(*slack.RichTextSectionTextElement)
		if !ok {
			t.Fatalf("element is %T, want only text elements", el)
		}
		if te.Style != nil && (te.Style.Italic || te.Style.Bold) {
			t.Errorf("element %+v has style %+v, want no italic/bold for literal asterisks", te, te.Style)
		}
		got += te.Text
	}
	if got != "*hello*" {
		t.Errorf("concatenated text = %q, want %q", got, "*hello*")
	}
}

func TestConvert_AsteriskRoundTripStable(t *testing.T) {
	// After one conversion, **bold** -> *bold*. Re-converting the
	// result must NOT change it (this is what happens on edit).
	mr1, _ := Convert("**bold**")
	if mr1 != "*bold*" {
		t.Fatalf("first pass: %q, want *bold*", mr1)
	}
	mr2, _ := Convert(mr1)
	if mr2 != "*bold*" {
		t.Errorf("second pass: %q, want *bold* (round-trip stable)", mr2)
	}
}

func TestConvert_NestedItalicAroundAsteriskLiteral(t *testing.T) {
	// _*both*_ : outer italic, inner literal asterisks. The outer
	// underscore-italic must apply, and the inner asterisks must be
	// preserved as literal text inside the italic run.
	mr, blk := Convert("_*both*_")
	want := "_*both*_"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
	// Block side: italic should be set on text elements inside the
	// outer wrapper. The literal asterisks inherit italic from the
	// surrounding inheritedStyle.
	sec := blk.Elements[0].(*slack.RichTextSection)
	for i, el := range sec.Elements {
		te, ok := el.(*slack.RichTextSectionTextElement)
		if !ok {
			t.Fatalf("element[%d] is %T, want text", i, el)
		}
		if te.Style == nil || !te.Style.Italic {
			t.Errorf("element[%d] %q style = %+v, want italic", i, te.Text, te.Style)
		}
		if te.Style != nil && te.Style.Bold {
			t.Errorf("element[%d] %q has bold style, want italic only", i, te.Text)
		}
	}
}

func TestConvert_NestedAsteriskLiteralAroundItalic(t *testing.T) {
	// *_both_* : outer literal asterisks, inner underscore-italic.
	// The outer asterisks must be preserved as literal text, and
	// the inner italic must apply to "both".
	mr, blk := Convert("*_both_*")
	want := "*_both_*"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
	// Block side: walk the elements and verify "both" carries italic
	// while the surrounding asterisks do not.
	sec := blk.Elements[0].(*slack.RichTextSection)
	var got string
	sawItalicBoth := false
	for _, el := range sec.Elements {
		te, ok := el.(*slack.RichTextSectionTextElement)
		if !ok {
			t.Fatalf("element is %T, want text", el)
		}
		got += te.Text
		if te.Text == "both" {
			if te.Style == nil || !te.Style.Italic {
				t.Errorf("'both' element style = %+v, want italic", te.Style)
			}
			sawItalicBoth = true
		}
	}
	if got != "*both*" {
		t.Errorf("concatenated visible text = %q, want %q", got, "*both*")
	}
	if !sawItalicBoth {
		t.Error("did not find a 'both' text element with italic style")
	}
}

func TestConvert_TwoSeparateItalics(t *testing.T) {
	// _a_ and _b_ : two italics in one paragraph.
	mr, _ := Convert("_a_ and _b_")
	want := "_a_ and _b_"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
}

func TestConvert_ItalicContainingBold(t *testing.T) {
	// _x **y** z_ : italic envelope, bold inside.
	// The "y" should carry BOTH italic and bold.
	mr, blk := Convert("_x **y** z_")
	want := "_x *y* z_"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
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
	mr, blk := Convert("~~done~~")
	if mr != "~done~" {
		t.Errorf("mrkdwn = %q, want ~done~", mr)
	}
	te := blk.Elements[0].(*slack.RichTextSection).Elements[0].(*slack.RichTextSectionTextElement)
	if te.Text != "done" || te.Style == nil || !te.Style.Strike {
		t.Errorf("got %+v / %+v, want strike=true on 'done'", te, te.Style)
	}
}

func TestConvert_InlineCode(t *testing.T) {
	mr, blk := Convert("type `make build` to compile")
	want := "type `make build` to compile"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
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
	mr, blk := Convert("see [Slack](https://slack.com) docs")
	want := "see <https://slack.com|Slack> docs"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
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
	mr, blk := Convert("see [https://x.com](https://x.com)")
	want := "see <https://x.com|https://x.com>"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
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
	mr, blk := Convert("[click](https://x.com?a=1|b=2)")
	wantMr := "<https://x.com?a=1|b=2>"
	if mr != wantMr {
		t.Errorf("mrkdwn = %q, want %q (label dropped to avoid pipe corruption)", mr, wantMr)
	}
	link := blk.Elements[0].(*slack.RichTextSection).Elements[0].(*slack.RichTextSectionLinkElement)
	if link.URL != "https://x.com?a=1|b=2" {
		t.Errorf("block URL = %q, want full URL preserved", link.URL)
	}
	if link.Text != "click" {
		t.Errorf("block label = %q, want %q", link.Text, "click")
	}
}

func TestConvert_LinkLabelStripsFormatting(t *testing.T) {
	// Slack's <url|label> wire form expects plain text after '|'.
	// CommonMark allows formatting inside a link label, so we strip
	// it: [**bold**](url) becomes <url|bold>, no asterisks.
	mr, blk := Convert("[**bold**](https://x.com)")
	wantMr := "<https://x.com|bold>"
	if mr != wantMr {
		t.Errorf("mrkdwn = %q, want %q", mr, wantMr)
	}
	link := blk.Elements[0].(*slack.RichTextSection).Elements[0].(*slack.RichTextSectionLinkElement)
	if link.Text != "bold" {
		t.Errorf("block label = %q, want %q", link.Text, "bold")
	}
}

func TestConvert_LinkInsideBoldCarriesBoldStyle(t *testing.T) {
	// **[link](url)** : the link element should inherit Bold style
	// from the enclosing emphasis.
	_, blk := Convert("**[click](https://x.com)**")
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
	mr, blk := Convert("**bold ~~strike~~ bold**")
	wantMr := "*bold ~strike~ bold*"
	if mr != wantMr {
		t.Errorf("mrkdwn = %q, want %q", mr, wantMr)
	}
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
	mr, blk := Convert("hi <@U12345>!")
	want := "hi <@U12345>!"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
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
	mr, blk := Convert("see <#C123|general> please")
	if mr != "see <#C123|general> please" {
		t.Errorf("mrkdwn = %q", mr)
	}
	ch := blk.Elements[0].(*slack.RichTextSection).Elements[1].(*slack.RichTextSectionChannelElement)
	if ch.ChannelID != "C123" {
		t.Errorf("ChannelID = %q, want C123", ch.ChannelID)
	}
}

func TestConvert_Broadcast(t *testing.T) {
	mr, blk := Convert("<!here> deploy now")
	if mr != "<!here> deploy now" {
		t.Errorf("mrkdwn = %q", mr)
	}
	bc := blk.Elements[0].(*slack.RichTextSection).Elements[0].(*slack.RichTextSectionBroadcastElement)
	if bc.Range != "here" {
		t.Errorf("Range = %q, want here", bc.Range)
	}
}

func TestConvert_BoldContainingMention(t *testing.T) {
	mr, blk := Convert("**Hi <@U1>**")
	if mr != "*Hi <@U1>*" {
		t.Errorf("mrkdwn = %q, want *Hi <@U1>*", mr)
	}
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
	mr, blk := Convert("- one\n- two")
	want := "• one\n• two"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
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
	mr, blk := Convert("1. one\n2. two")
	want := "1. one\n2. two"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
	list := blk.Elements[0].(*slack.RichTextList)
	if list.Style != slack.RTEListOrdered {
		t.Errorf("list style = %q, want ordered", list.Style)
	}
}

func TestConvert_NestedList(t *testing.T) {
	mr, blk := Convert("- a\n    - b")
	wantMr := "• a\n  • b"
	if mr != wantMr {
		t.Errorf("mrkdwn = %q, want %q", mr, wantMr)
	}
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
