package blockkit

import (
	"encoding/json"
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

func TestParseRichTextProducesRichTextBlock(t *testing.T) {
	// rich_text blocks are parsed into a typed RichTextBlock so the
	// host can reconstruct a newline-faithful mrkdwn body from
	// them. Slack's Message.Text fallback is a lossy projection of
	// rich_text (newlines collapse to spaces), so we cannot rely on
	// it for multi-line block-kit messages from bots like GitHub.
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewRichTextBlock("rt",
			slack.NewRichTextSection(
				slack.NewRichTextSectionTextElement("hello", nil),
			),
		),
	}}
	got := Parse(in)
	rt, ok := got[0].(RichTextBlock)
	if !ok {
		t.Fatalf("got %T, want RichTextBlock for rich_text", got[0])
	}
	if len(rt.Elements) != 1 {
		t.Fatalf("RichTextBlock.Elements len = %d, want 1", len(rt.Elements))
	}
}

func TestParseAttachmentsEmptyReturnsNil(t *testing.T) {
	got := ParseAttachments(nil)
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestParseAttachmentBasicFields(t *testing.T) {
	in := []slack.Attachment{{
		Color:     "danger",
		Pretext:   "Heads up:",
		Title:     "Service down",
		TitleLink: "https://status.example.com",
		Text:      "checkout-svc returning 5xx",
		Footer:    "Datadog",
	}}
	got := ParseAttachments(in)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	a := got[0]
	if a.Color != "danger" {
		t.Errorf("Color = %q", a.Color)
	}
	if a.Pretext != "Heads up:" {
		t.Errorf("Pretext = %q", a.Pretext)
	}
	if a.Title != "Service down" {
		t.Errorf("Title = %q", a.Title)
	}
	if a.TitleLink != "https://status.example.com" {
		t.Errorf("TitleLink = %q", a.TitleLink)
	}
	if a.Text != "checkout-svc returning 5xx" {
		t.Errorf("Text = %q", a.Text)
	}
	if a.Footer != "Datadog" {
		t.Errorf("Footer = %q", a.Footer)
	}
}

func TestParseAttachmentFields(t *testing.T) {
	in := []slack.Attachment{{
		Fields: []slack.AttachmentField{
			{Title: "Service", Value: "checkout-svc", Short: true},
			{Title: "Region", Value: "us-east-1", Short: true},
			{Title: "Notes", Value: "long form note", Short: false},
		},
	}}
	a := ParseAttachments(in)[0]
	if len(a.Fields) != 3 {
		t.Fatalf("Fields len = %d", len(a.Fields))
	}
	if a.Fields[0].Title != "Service" || a.Fields[0].Value != "checkout-svc" || !a.Fields[0].Short {
		t.Errorf("Fields[0] = %+v", a.Fields[0])
	}
	if a.Fields[2].Short {
		t.Errorf("Fields[2] should not be Short")
	}
}

func TestParseAttachmentTimestampParsesUnixSeconds(t *testing.T) {
	in := []slack.Attachment{{Ts: "1700000000"}}
	a := ParseAttachments(in)[0]
	if a.TS != 1700000000 {
		t.Errorf("TS = %d, want 1700000000", a.TS)
	}
}

// TestParseAttachmentSharedMessageSource covers Slack's "Share
// message" / forward shape: the shared message's permalink is only
// in the attachment's from_url (the outer text carries no link), and
// `o` needs it to jump to the original.
func TestParseAttachmentSharedMessageSource(t *testing.T) {
	in := []slack.Attachment{{
		FromURL:    "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139",
		AuthorName: "Jenny Leyva",
	}}
	a := ParseAttachments(in)[0]
	if a.FromURL != "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139" {
		t.Errorf("FromURL = %q", a.FromURL)
	}
	if a.AuthorName != "Jenny Leyva" {
		t.Errorf("AuthorName = %q", a.AuthorName)
	}
}

func TestParseAttachmentImageAndThumb(t *testing.T) {
	in := []slack.Attachment{{
		ImageURL: "https://example.com/img.png",
		ThumbURL: "https://example.com/thumb.png",
	}}
	a := ParseAttachments(in)[0]
	if a.ImageURL != "https://example.com/img.png" {
		t.Errorf("ImageURL = %q", a.ImageURL)
	}
	if a.ThumbURL != "https://example.com/thumb.png" {
		t.Errorf("ThumbURL = %q", a.ThumbURL)
	}
}

// TestParseAttachmentNestedBlocks covers Slack's newer attachment
// shape (used by Linear/Jira/etc. link unfurls) where the attachment
// carries no title/text/fields and instead nests Block Kit blocks in
// its `blocks` array. The parser must surface those so the renderer
// can display them.
func TestParseAttachmentNestedBlocks(t *testing.T) {
	in := []slack.Attachment{{
		Color: "#2d1c9c",
		Blocks: slack.Blocks{BlockSet: []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn", "TRU-111 Customer Facing Blacklist Monitoring", false, false),
				nil, nil,
			),
		}},
	}}
	a := ParseAttachments(in)[0]
	if len(a.Blocks) != 1 {
		t.Fatalf("Blocks len = %d, want 1", len(a.Blocks))
	}
	sec, ok := a.Blocks[0].(SectionBlock)
	if !ok {
		t.Fatalf("Blocks[0] type = %T, want SectionBlock", a.Blocks[0])
	}
	if sec.Text != "TRU-111 Customer Facing Blacklist Monitoring" {
		t.Errorf("section text = %q", sec.Text)
	}
}

// capturedMentionAttachments is the `attachments` array of Slackbot's
// "You mentioned @x, but they're not in this channel" ephemeral, as
// captured from the Slack web client (IDs replaced with placeholders).
// See docs/superpowers/specs/2026-10-09-interactive-ephemeral-messages-design.md.
const capturedMentionAttachments = `[{
  "callback_id":"consistentephemeralmentions_U0EXAMPLE01_1791541429559219_0",
  "fallback":"You may want to invite them.","id":1,
  "actions":[
    {"id":"1","name":"invite","text":"Add Them","type":"button","value":"invite","style":"",
     "confirm":{"text":"New members will be able to see all of the channel's history, including any files that have been shared in the channel.",
                "title":"Are you sure you want to add them?","ok_text":"Add","dismiss_text":"Cancel"}},
    {"id":"2","name":"ignore","text":"Dismiss","type":"button","value":"ignore","style":""},
    {"id":"3","name":"dont-show-again","text":"Don't Show Again","type":"button","value":"dont-show-again","style":""}]}]`

func decodeCapturedAttachments(t *testing.T) []slack.Attachment {
	t.Helper()
	var atts []slack.Attachment
	if err := json.Unmarshal([]byte(capturedMentionAttachments), &atts); err != nil {
		t.Fatalf("decoding captured attachments: %v", err)
	}
	return atts
}

// TestParseAttachmentLegacyActions: the buttons of a legacy attachment,
// with everything chat.attachmentAction echoes back, survive parsing.
func TestParseAttachmentLegacyActions(t *testing.T) {
	a := ParseAttachments(decodeCapturedAttachments(t))[0]
	if a.ID != 1 {
		t.Errorf("ID = %d, want 1", a.ID)
	}
	if a.CallbackID != "consistentephemeralmentions_U0EXAMPLE01_1791541429559219_0" {
		t.Errorf("CallbackID = %q", a.CallbackID)
	}
	if len(a.Actions) != 3 {
		t.Fatalf("len(Actions) = %d, want 3", len(a.Actions))
	}
	add := a.Actions[0]
	if add.Name != "invite" || add.Text != "Add Them" || add.Type != "button" || add.Value != "invite" {
		t.Errorf("Actions[0] = %+v", add)
	}
	if add.Confirm == nil {
		t.Fatal("Actions[0].Confirm = nil, want the captured confirm dialog")
	}
	if add.Confirm.Title != "Are you sure you want to add them?" || add.Confirm.OKText != "Add" || add.Confirm.DismissText != "Cancel" {
		t.Errorf("Actions[0].Confirm = %+v", *add.Confirm)
	}
	if a.Actions[1].Confirm != nil {
		t.Errorf("Actions[1].Confirm = %+v, want nil", *a.Actions[1].Confirm)
	}
	// slack-go's AttachmentAction has no id field: the parser cannot
	// see it. The WebSocket path fills it from the raw frame (Task 4).
	if a.Actions[1].ID != "" {
		t.Errorf("Actions[1].ID = %q, want empty from slack-go parsing", a.Actions[1].ID)
	}
}

func TestParseAttachmentWithoutActions(t *testing.T) {
	a := ParseAttachments([]slack.Attachment{{Title: "T"}})[0]
	if a.Actions != nil {
		t.Errorf("Actions = %+v, want nil", a.Actions)
	}
}
