package blockkit

import (
	"github.com/slack-go/slack"
)

// Parse converts a slack-go Blocks value into our typed Block slice.
// Every input block produces exactly one output entry; unhandled
// types become UnknownBlock so the renderer can show a placeholder.
// Returns nil for an empty input.
func Parse(in slack.Blocks) []Block {
	if len(in.BlockSet) == 0 {
		return nil
	}
	out := make([]Block, 0, len(in.BlockSet))
	for _, b := range in.BlockSet {
		out = append(out, parseOne(b))
	}
	return out
}

func parseOne(b slack.Block) Block {
	switch v := b.(type) {
	case *slack.HeaderBlock:
		return HeaderBlock{Text: textOf(v.Text)}
	case *slack.DividerBlock:
		return DividerBlock{}
	case *slack.SectionBlock:
		return parseSection(v)
	case *slack.ContextBlock:
		return parseContext(v)
	case *slack.ImageBlock:
		title := ""
		if v.Title != nil {
			title = v.Title.Text
		}
		// slack-go v0.23.0's ImageBlock does not expose pixel
		// dimensions, so Width/Height stay 0 and the renderer must
		// fall back to its own size heuristics.
		return ImageBlock{
			URL:   v.ImageURL,
			Title: title,
			Alt:   v.AltText,
		}
	case *slack.ActionBlock:
		return parseActions(v)
	default:
		return UnknownBlock{Type: string(b.BlockType())}
	}
}

func parseSection(s *slack.SectionBlock) SectionBlock {
	out := SectionBlock{Text: textOf(s.Text)}
	for _, f := range s.Fields {
		out.Fields = append(out.Fields, textOf(f))
	}
	if s.Accessory != nil {
		out.Accessory = parseAccessory(s.Accessory)
	}
	return out
}

func parseContext(c *slack.ContextBlock) ContextBlock {
	out := ContextBlock{}
	if c.ContextElements.Elements == nil {
		return out
	}
	for _, e := range c.ContextElements.Elements {
		switch v := e.(type) {
		case *slack.TextBlockObject:
			out.Elements = append(out.Elements, ContextElement{Text: v.Text})
		case *slack.ImageBlockElement:
			url := ""
			if v.ImageURL != nil {
				url = *v.ImageURL
			}
			out.Elements = append(out.Elements, ContextElement{ImageURL: url, AltText: v.AltText})
		}
	}
	return out
}

func parseActions(a *slack.ActionBlock) ActionsBlock {
	out := ActionsBlock{}
	if a.Elements == nil {
		return out
	}
	for _, e := range a.Elements.ElementSet {
		out.Elements = append(out.Elements, actionElementOf(e))
	}
	return out
}

func parseAccessory(a *slack.Accessory) AccessoryElement {
	switch {
	case a.ImageElement != nil:
		url := ""
		if a.ImageElement.ImageURL != nil {
			url = *a.ImageElement.ImageURL
		}
		return ImageAccessory{URL: url, AltText: a.ImageElement.AltText}
	case a.ButtonElement != nil:
		return LabelAccessory{Kind: "button", Label: textOf(a.ButtonElement.Text)}
	case a.OverflowElement != nil:
		return LabelAccessory{Kind: "overflow", Label: ""}
	case a.SelectElement != nil:
		return LabelAccessory{Kind: "static_select", Label: textOf(a.SelectElement.Placeholder)}
	case a.MultiSelectElement != nil:
		return LabelAccessory{Kind: "multi_select", Label: textOf(a.MultiSelectElement.Placeholder)}
	case a.DatePickerElement != nil:
		return LabelAccessory{Kind: "datepicker", Label: a.DatePickerElement.InitialDate}
	case a.TimePickerElement != nil:
		return LabelAccessory{Kind: "timepicker", Label: a.TimePickerElement.InitialTime}
	case a.RadioButtonsElement != nil:
		return LabelAccessory{Kind: "radio_buttons", Label: ""}
	case a.CheckboxGroupsBlockElement != nil:
		return LabelAccessory{Kind: "checkboxes", Label: ""}
	case a.WorkflowButtonElement != nil:
		return LabelAccessory{Kind: "workflow_button", Label: textOf(a.WorkflowButtonElement.Text)}
	default:
		return LabelAccessory{Kind: "unknown", Label: ""}
	}
}

func actionElementOf(e slack.BlockElement) ActionElement {
	switch v := e.(type) {
	case *slack.ButtonBlockElement:
		return ActionElement{Kind: "button", Label: textOf(v.Text)}
	case *slack.OverflowBlockElement:
		return ActionElement{Kind: "overflow"}
	case *slack.SelectBlockElement:
		return ActionElement{Kind: "static_select", Label: textOf(v.Placeholder)}
	case *slack.MultiSelectBlockElement:
		return ActionElement{Kind: "multi_select", Label: textOf(v.Placeholder)}
	case *slack.DatePickerBlockElement:
		return ActionElement{Kind: "datepicker", Label: v.InitialDate}
	case *slack.TimePickerBlockElement:
		return ActionElement{Kind: "timepicker", Label: v.InitialTime}
	case *slack.RadioButtonsBlockElement:
		return ActionElement{Kind: "radio_buttons"}
	case *slack.CheckboxGroupsBlockElement:
		return ActionElement{Kind: "checkboxes"}
	case *slack.WorkflowButtonBlockElement:
		return ActionElement{Kind: "workflow_button", Label: textOf(v.Text)}
	default:
		return ActionElement{Kind: "unknown"}
	}
}

// textOf returns the .Text of a TextBlockObject, or "" if nil.
func textOf(t *slack.TextBlockObject) string {
	if t == nil {
		return ""
	}
	return t.Text
}

// ParseAttachments converts slack-go Attachment slice to our
// LegacyAttachment slice. Returns nil for empty input.
func ParseAttachments(in []slack.Attachment) []LegacyAttachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]LegacyAttachment, 0, len(in))
	for _, a := range in {
		out = append(out, parseAttachment(a))
	}
	return out
}

func parseAttachment(a slack.Attachment) LegacyAttachment {
	la := LegacyAttachment{
		Color:      a.Color,
		Pretext:    a.Pretext,
		Title:      a.Title,
		TitleLink:  a.TitleLink,
		Text:       a.Text,
		ImageURL:   a.ImageURL,
		ThumbURL:   a.ThumbURL,
		Footer:     a.Footer,
		FooterIcon: a.FooterIcon,
	}
	for _, f := range a.Fields {
		la.Fields = append(la.Fields, LegacyField{
			Title: f.Title, Value: f.Value, Short: f.Short,
		})
	}
	if a.Ts != "" {
		// json.Number; safe to parse as int64.
		if n, err := a.Ts.Int64(); err == nil {
			la.TS = n
		}
	}
	return la
}
