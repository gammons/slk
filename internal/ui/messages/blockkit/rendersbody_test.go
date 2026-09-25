// internal/ui/messages/blockkit/rendersbody_test.go
//
// RendersBody decides whether msg.Text is redundant. It must agree with
// the appendBlock switch: a block kind that draws content counts, one
// that draws nothing (or only a placeholder) does not.
package blockkit

import "testing"

func TestRendersBody_ContentBearingBlocks(t *testing.T) {
	cases := map[string][]Block{
		"section text":      {SectionBlock{Text: "body"}},
		"section fields":    {SectionBlock{Fields: []string{"a", "b"}}},
		"section accessory": {SectionBlock{Accessory: LabelAccessory{Kind: "button", Label: "Go"}}},
		"header":            {HeaderBlock{Text: "Title"}},
		"context":           {ContextBlock{Elements: []ContextElement{{Text: "note"}}}},
		"image":             {ImageBlock{URL: "https://example.test/i.png"}},
		"actions":           {ActionsBlock{Elements: []ActionElement{{}}}},
		"mixed with filler": {DividerBlock{}, SectionBlock{Text: "body"}},
	}
	for name, blocks := range cases {
		t.Run(name, func(t *testing.T) {
			if !RendersBody(blocks) {
				t.Errorf("RendersBody = false, want true for %s", name)
			}
		})
	}
}

func TestRendersBody_NonBodyBlocks(t *testing.T) {
	cases := map[string][]Block{
		"nil":             nil,
		"empty":           {},
		"divider only":    {DividerBlock{}},
		"unknown only":    {UnknownBlock{Type: "video"}},
		"empty section":   {SectionBlock{}},
		"empty header":    {HeaderBlock{}},
		"empty context":   {ContextBlock{}},
		"image no url":    {ImageBlock{Title: "titled but no url"}},
		"empty actions":   {ActionsBlock{}},
		"divider+unknown": {DividerBlock{}, UnknownBlock{Type: "file"}},
	}
	for name, blocks := range cases {
		t.Run(name, func(t *testing.T) {
			if RendersBody(blocks) {
				t.Errorf("RendersBody = true, want false for %s", name)
			}
		})
	}
}

// rich_text is rendered THROUGH msg.Text by the host (appendBlock
// returns early for it), so it must never suppress the text.
func TestRendersBody_RichTextDoesNotCount(t *testing.T) {
	if RendersBody([]Block{RichTextBlock{}}) {
		t.Error("RendersBody = true for rich_text; the host renders it via msg.Text")
	}
}
