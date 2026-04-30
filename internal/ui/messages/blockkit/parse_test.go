package blockkit

import (
	"testing"

	"github.com/slack-go/slack"
)

func TestParseEmptyBlocksReturnsNil(t *testing.T) {
	got := Parse(slack.Blocks{})
	if got != nil {
		t.Errorf("Parse(empty) = %v, want nil", got)
	}
}

func TestParseHeaderBlock(t *testing.T) {
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewHeaderBlock(slack.NewTextBlockObject("plain_text", "Deploy successful", false, false)),
	}}
	got := Parse(in)
	if len(got) != 1 {
		t.Fatalf("got %d blocks, want 1", len(got))
	}
	hb, ok := got[0].(HeaderBlock)
	if !ok {
		t.Fatalf("got %T, want HeaderBlock", got[0])
	}
	if hb.Text != "Deploy successful" {
		t.Errorf("Text = %q, want %q", hb.Text, "Deploy successful")
	}
}

func TestParseDividerBlock(t *testing.T) {
	in := slack.Blocks{BlockSet: []slack.Block{slack.NewDividerBlock()}}
	got := Parse(in)
	if len(got) != 1 {
		t.Fatalf("got %d blocks, want 1", len(got))
	}
	if _, ok := got[0].(DividerBlock); !ok {
		t.Errorf("got %T, want DividerBlock", got[0])
	}
}

func TestParseSectionBlockWithText(t *testing.T) {
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "*hello* world", false, false),
			nil, nil,
		),
	}}
	got := Parse(in)
	sb, ok := got[0].(SectionBlock)
	if !ok {
		t.Fatalf("got %T, want SectionBlock", got[0])
	}
	if sb.Text != "*hello* world" {
		t.Errorf("Text = %q, want %q", sb.Text, "*hello* world")
	}
	if len(sb.Fields) != 0 {
		t.Errorf("Fields len = %d, want 0", len(sb.Fields))
	}
	if sb.Accessory != nil {
		t.Errorf("Accessory = %v, want nil", sb.Accessory)
	}
}

func TestParseSectionBlockWithFields(t *testing.T) {
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewSectionBlock(nil, []*slack.TextBlockObject{
			slack.NewTextBlockObject("mrkdwn", "*Service*\nweb", false, false),
			slack.NewTextBlockObject("mrkdwn", "*Region*\nus-east-1", false, false),
		}, nil),
	}}
	sb := Parse(in)[0].(SectionBlock)
	if len(sb.Fields) != 2 {
		t.Fatalf("Fields len = %d, want 2", len(sb.Fields))
	}
	if sb.Fields[0] != "*Service*\nweb" {
		t.Errorf("Fields[0] = %q", sb.Fields[0])
	}
}

func TestParseSectionBlockWithButtonAccessory(t *testing.T) {
	btn := slack.NewButtonBlockElement("approve", "approve_value",
		slack.NewTextBlockObject("plain_text", "Approve", false, false))
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "Ready?", false, false),
			nil,
			slack.NewAccessory(btn),
		),
	}}
	sb := Parse(in)[0].(SectionBlock)
	if sb.Accessory == nil {
		t.Fatal("Accessory is nil")
	}
	la, ok := sb.Accessory.(LabelAccessory)
	if !ok {
		t.Fatalf("Accessory = %T, want LabelAccessory", sb.Accessory)
	}
	if la.Kind != "button" || la.Label != "Approve" {
		t.Errorf("got %+v, want {button Approve}", la)
	}
}

func TestParseSectionBlockWithImageAccessory(t *testing.T) {
	img := slack.NewImageBlockElement("https://example.com/logo.png", "company logo")
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "Hello", false, false),
			nil,
			slack.NewAccessory(img),
		),
	}}
	sb := Parse(in)[0].(SectionBlock)
	ia, ok := sb.Accessory.(ImageAccessory)
	if !ok {
		t.Fatalf("Accessory = %T, want ImageAccessory", sb.Accessory)
	}
	if ia.URL != "https://example.com/logo.png" {
		t.Errorf("URL = %q", ia.URL)
	}
	if ia.AltText != "company logo" {
		t.Errorf("AltText = %q", ia.AltText)
	}
}

func TestParseImageBlock(t *testing.T) {
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewImageBlock("https://example.com/chart.png", "chart", "block1",
			slack.NewTextBlockObject("plain_text", "Q3 metrics", false, false)),
	}}
	ib := Parse(in)[0].(ImageBlock)
	if ib.URL != "https://example.com/chart.png" {
		t.Errorf("URL = %q", ib.URL)
	}
	if ib.Title != "Q3 metrics" {
		t.Errorf("Title = %q", ib.Title)
	}
	if ib.Alt != "chart" {
		t.Errorf("Alt = %q", ib.Alt)
	}
}

func TestParseContextBlockMixedElements(t *testing.T) {
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewContextBlock("ctx",
			slack.NewImageBlockElement("https://example.com/icon.png", "icon"),
			slack.NewTextBlockObject("mrkdwn", "*by* gammons", false, false),
		),
	}}
	cb := Parse(in)[0].(ContextBlock)
	if len(cb.Elements) != 2 {
		t.Fatalf("Elements len = %d, want 2", len(cb.Elements))
	}
	if cb.Elements[0].ImageURL != "https://example.com/icon.png" {
		t.Errorf("Elements[0].ImageURL = %q", cb.Elements[0].ImageURL)
	}
	if cb.Elements[1].Text != "*by* gammons" {
		t.Errorf("Elements[1].Text = %q", cb.Elements[1].Text)
	}
}

func TestParseActionsBlock(t *testing.T) {
	btn := slack.NewButtonBlockElement("a", "v",
		slack.NewTextBlockObject("plain_text", "Click", false, false))
	in := slack.Blocks{BlockSet: []slack.Block{slack.NewActionBlock("act", btn)}}
	ab := Parse(in)[0].(ActionsBlock)
	if len(ab.Elements) != 1 {
		t.Fatalf("Elements len = %d, want 1", len(ab.Elements))
	}
	if ab.Elements[0].Kind != "button" || ab.Elements[0].Label != "Click" {
		t.Errorf("got %+v", ab.Elements[0])
	}
}

func TestParseUnknownBlockTypePreservesType(t *testing.T) {
	in := slack.Blocks{BlockSet: []slack.Block{
		&slack.UnknownBlock{Type: slack.MessageBlockType("video")},
	}}
	got := Parse(in)
	ub, ok := got[0].(UnknownBlock)
	if !ok {
		t.Fatalf("got %T, want UnknownBlock", got[0])
	}
	if ub.Type != "video" {
		t.Errorf("Type = %q, want %q", ub.Type, "video")
	}
}

func TestParseRichTextBecomesUnknownToFallThroughToText(t *testing.T) {
	// rich_text is intentionally not walked; we want it to land in
	// UnknownBlock so the renderer can produce a placeholder. The
	// host's Message.Text fallback handles the actual content.
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewRichTextBlock("rt"),
	}}
	got := Parse(in)
	if _, ok := got[0].(UnknownBlock); !ok {
		t.Errorf("got %T, want UnknownBlock for rich_text", got[0])
	}
}
