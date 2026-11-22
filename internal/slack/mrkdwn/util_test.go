package mrkdwn

import (
	"testing"

	"github.com/okneniz/parsec"
	pstrings "github.com/okneniz/parsec/strings"
	slk "github.com/slack-go/slack"
)

func TestMergeSectionElements(t *testing.T) {
	// Basic text merging
	text1 := &slk.RichTextSectionTextElement{
		Text:  "Hello",
		Style: nil,
	}
	text2 := &slk.RichTextSectionTextElement{
		Text:  " World",
		Style: nil,
	}

	result := mergeSectionElements(text1, text2)
	if len(result) != 1 {
		t.Errorf("Expected 1 element after merging, got %d", len(result))
	}

	mergedText, ok := result[0].(*slk.RichTextSectionTextElement)
	if !ok {
		t.Errorf("Expected text element, got %T", result[0])
	}
	if mergedText.Text != "Hello World" {
		t.Errorf("Expected 'Hello World', got '%s'", mergedText.Text)
	}

	// Text merging with same style
	style := &slk.RichTextSectionTextStyle{Bold: true}
	text3 := &slk.RichTextSectionTextElement{
		Text:  "Bold",
		Style: style,
	}
	text4 := &slk.RichTextSectionTextElement{
		Text:  " text",
		Style: style,
	}

	result2 := mergeSectionElements(text3, text4)
	if len(result2) != 1 {
		t.Errorf("Expected 1 element after merging with style, got %d", len(result2))
	}

	mergedText2, ok := result2[0].(*slk.RichTextSectionTextElement)
	if !ok {
		t.Errorf("Expected text element, got %T", result2[0])
	}
	if mergedText2.Text != "Bold text" {
		t.Errorf("Expected 'Bold text', got '%s'", mergedText2.Text)
	}

	// Skin tone merging
	emoji1 := &slk.RichTextSectionEmojiElement{
		Name: "grinning",
	}
	emoji2 := &slk.RichTextSectionEmojiElement{
		Name: "skin-tone-2",
	}

	result3 := mergeSectionElements(emoji1, emoji2)
	if len(result3) != 1 {
		t.Errorf("Expected 1 element after skin tone merging, got %d", len(result3))
	}

	mergedEmoji, ok := result3[0].(*slk.RichTextSectionEmojiElement)
	if !ok {
		t.Errorf("Expected emoji element, got %T", result3[0])
	}
	if mergedEmoji.Name != "grinning::skin-tone-2" {
		t.Errorf("Expected 'grinning::skin-tone-2', got '%s'", mergedEmoji.Name)
	}

	// Mixed elements (no merge)
	text5 := &slk.RichTextSectionTextElement{
		Text: "Hello",
	}
	emoji3 := &slk.RichTextSectionEmojiElement{
		Name: "wave",
	}

	result4 := mergeSectionElements(text5, emoji3)
	if len(result4) != 2 {
		t.Errorf("Expected 2 elements, got %d", len(result4))
	}

	// Two emoji (no merge)

	result5 := mergeSectionElements(emoji1, emoji3)
	if len(result5) != 2 {
		t.Errorf("Expected 2 elements, got %d", len(result4))
	}

	// Empty input
	result6 := mergeSectionElements()
	if len(result6) != 0 {
		t.Errorf("Expected 0 elements for empty input, got %d", len(result6))
	}
}

func TestMerge(t *testing.T) {
	// Empty slice
	result := Merge([]slk.RichTextElement{})
	if len(result) != 0 {
		t.Errorf("Expected empty slice, got %d elements", len(result))
	}

	// Single element
	singleElement := slk.NewRichTextSection(
		slk.NewRichTextSectionTextElement("test", nil),
	)
	result = Merge([]slk.RichTextElement{singleElement})
	if len(result) != 1 {
		t.Errorf("Expected 1 element, got %d", len(result))
	}

	// Mergeable sections
	section1 := slk.NewRichTextSection(
		slk.NewRichTextSectionTextElement("Hello", nil),
	)
	section2 := slk.NewRichTextSection(
		slk.NewRichTextSectionTextElement(" World", nil),
	)

	result = Merge([]slk.RichTextElement{section1, section2})
	if len(result) != 1 {
		t.Errorf("Expected 1 merged section, got %d", len(result))
	}

	// Quotes
	quote1 := &slk.RichTextQuote{
		Elements: []slk.RichTextSectionElement{
			slk.NewRichTextSectionTextElement("Quote 1", nil),
		},
	}
	quote2 := &slk.RichTextQuote{
		Elements: []slk.RichTextSectionElement{
			slk.NewRichTextSectionTextElement("Quote 2", nil),
		},
	}

	result = Merge([]slk.RichTextElement{quote1, quote2})
	if len(result) != 1 {
		t.Errorf("Expected 1 merged quote, got %d", len(result))
	}
	te := result[0].(*slk.RichTextQuote).Elements[0].(*slk.RichTextSectionTextElement).Text
	if te != "Quote 1\nQuote 2" {
		t.Errorf("text = %q, want %q", te, "Quote 1\nQuote 2")
	}

	// Lists
	list1 := slk.NewRichTextList(slk.RTEListBullet, 0, slk.NewRichTextSection(slk.NewRichTextSectionTextElement("foo", nil)))
	list2 := slk.NewRichTextList(slk.RTEListBullet, 0, slk.NewRichTextSection(slk.NewRichTextSectionTextElement("bar", nil)))

	result = Merge([]slk.RichTextElement{list1, list2})
	if len(result) != 1 {
		t.Errorf("Expected 1 merged list, got %d", len(result))
	}
	te = result[0].(*slk.RichTextList).Elements[0].(*slk.RichTextSection).Elements[0].(*slk.RichTextSectionTextElement).Text
	if te != "foo" {
		t.Errorf("text = %q, want %q", te, "foo")
	}

	// Different styles
	list3 := slk.NewRichTextList(slk.RTEListOrdered, 0, slk.NewRichTextSection(slk.NewRichTextSectionTextElement("goo", nil)))

	result = Merge([]slk.RichTextElement{list1, list3})
	if len(result) != 2 {
		t.Errorf("Expected 2 unmerged lists, got %d", len(result))
	}
	te = result[1].(*slk.RichTextList).Elements[0].(*slk.RichTextSection).Elements[0].(*slk.RichTextSectionTextElement).Text
	if te != "goo" {
		t.Errorf("text = %q, want %q", te, "foo")
	}
}

func TestMergeIdemp(t *testing.T) {
	block1 := Convert("foo bar *bold _italics_* goo doo\n* with list")
	block2 := Convert("* more list\n> quote")

	var elems []slk.RichTextElement
	elems = append(elems, block1.Elements...)
	elems = append(elems, block2.Elements...)

	res1 := Merge(elems)
	res2 := Merge(res1)

	for i := range res1 {
		if res1[i] != res2[i] {
			t.Errorf("[Merge] not idempotent: %#v != %#v", res1[i], res2[i])
		}
	}
}

func TestFallibleStateRollback(t *testing.T) {
	parserAlwaysFails := func(s *mdState, buf buffer) (string, mdErr) {
		s.style = &slk.RichTextSectionTextStyle{Bold: true}
		return "", parsec.NewParseError(buf.Position(), "always fails")
	}
	parserAlwaysSucceeds := func(s *mdState, buf buffer) (string, mdErr) {
		s.style = &slk.RichTextSectionTextStyle{Bold: true}
		return "", nil
	}

	// Check that [try] rolls back the state
	state := &mdState{}
	buf := pstrings.Buffer([]rune("*word"))
	_, err := try(parserAlwaysFails)(state, buf)
	if err == nil {
		t.Errorf("expected parser to fail")
	}
	if state.style != nil {
		t.Errorf("expected [try] to roll back state if parser fails")
	}
	_, err = try(parserAlwaysSucceeds)(state, buf)
	if err != nil {
		t.Errorf("expected parser to succeed")
	}
	if state.style == nil {
		t.Errorf("expected [try] to keep the state if parser succeeds")
	}

	// Check that [lookAhead] rolls back the state
	state = &mdState{}
	buf = pstrings.Buffer([]rune("*word"))
	_, err = lookAhead(parserAlwaysSucceeds)(state, buf)

	if err != nil {
		t.Errorf("expected parser to succeed")
	}
	if state.style != nil {
		t.Errorf("expected [lookAhead] to roll back state")
	}

	_, err = lookAhead(parserAlwaysFails)(state, buf)

	if err == nil {
		t.Errorf("expected parser to fail")
	}
	if state.style != nil {
		t.Errorf("expected [lookAhead] to roll back state")
	}

	// Check that [notFollowedBy] rolls back the state
	state = &mdState{}
	buf = pstrings.Buffer([]rune("*word"))
	_, err = notFollowedBy(parserAlwaysSucceeds, "unexpected")(state, buf)

	if err == nil {
		t.Errorf("expected parser to fail")
	}
	if state.style != nil {
		t.Errorf("expected [notFollowedBy] to roll back state")
	}

	_, err = notFollowedBy(parserAlwaysFails, "unexpected")(state, buf)

	if err != nil {
		t.Errorf("expected parser to succeed")
	}
	if state.style != nil {
		t.Errorf("expected [notFollowedBy] to roll back state")
	}
}
