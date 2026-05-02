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

// TestConvert_BlockFallback_KnownLimitation_TasksFix810 documents
// that the scaffold's walkBlock default case glues block-level
// children together without separators. List/heading/blockquote
// handlers added in Tasks 8 and 10 replace this behavior; until
// then, this test pins the (intentionally degraded) interim state
// so a regression doesn't slip past unnoticed.
//
// When Tasks 8/10 land, this test will fail and must be removed
// (the new tests in those tasks will assert correct behavior).
func TestConvert_BlockFallback_KnownLimitation_TasksFix810(t *testing.T) {
	// List input — items glue together, no separator
	mr, _ := Convert("- one\n- two")
	if mr != "onetwo" {
		t.Errorf("interim list-fallback mrkdwn = %q, want %q (replace this test in Task 8)", mr, "onetwo")
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
