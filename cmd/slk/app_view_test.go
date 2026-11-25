package main

import (
	"encoding/json"
	"testing"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/core/blocks"
	slackclient "github.com/gammons/slk/internal/slack"
)

func TestConvertAppView(t *testing.T) {
	var bs slack.Blocks
	if err := json.Unmarshal([]byte(`[
	  {"type":"header","text":{"type":"plain_text","text":"Your AI Budget"}},
	  {"type":"section","text":{"type":"mrkdwn","text":"Budget: *$400*"}},
	  {"type":"actions","block_id":"b1","elements":[
	    {"type":"button","action_id":"req_open","text":{"type":"plain_text","text":"Request budget increase"},
	     "style":"primary","value":"req_open"},
	    {"type":"button","action_id":"docs","text":{"type":"plain_text","text":"Docs"},"url":"https://example.com"},
	    {"type":"static_select","action_id":"pick","placeholder":{"type":"plain_text","text":"Pick"}}
	  ]},
	  {"type":"section","block_id":"sec1","text":{"type":"mrkdwn","text":"Not the perfect gif?"},
	   "accessory":{"type":"button","action_id":"shuffle","text":{"type":"plain_text","text":"Shuffle"}}},
	  {"type":"rich_text","elements":[
	    {"type":"rich_text_section","elements":[{"type":"text","text":"a rich line"}]}
	  ]}
	]`), &bs); err != nil {
		t.Fatalf("decoding blocks: %v", err)
	}
	v := &slackclient.View{
		ID:          "V1",
		TeamID:      "T1",
		AppID:       "A1",
		BotID:       "B1",
		Title:       "AI Budget Support",
		CloseLabel:  "Close",
		SubmitLabel: "",
		Blocks:      bs,
	}

	out := convertAppView(v)
	if out.ViewID != "V1" || out.TeamID != "T1" || out.AppID != "A1" || out.ServiceID != "B1" {
		t.Errorf("ids = %+v, want V1/T1/A1/B1", out)
	}
	if out.Title != "AI Budget Support" || out.CloseLabel != "Close" || out.SubmitLabel != "" {
		t.Errorf("labels = %q/%q/%q, want AI Budget Support/Close/empty",
			out.Title, out.CloseLabel, out.SubmitLabel)
	}
	if len(out.Blocks) != 5 {
		t.Errorf("blocks = %d, want 5", len(out.Blocks))
	}
	if _, ok := out.Blocks[0].(blocks.HeaderBlock); !ok {
		t.Errorf("first block = %T, want blocks.HeaderBlock", out.Blocks[0])
	}
	// rich_text is converted in place: the dialog has no second render
	// pass that would draw it.
	if _, ok := out.Blocks[4].(blocks.RichTextBlock); ok {
		t.Errorf("last block is still a RichTextBlock")
	}
	if sec, ok := out.Blocks[4].(blocks.SectionBlock); !ok || sec.Text != "a rich line" {
		t.Errorf("last block = %+v, want a section holding the rich text", out.Blocks[4])
	}
	if len(out.Actions) != 3 {
		t.Fatalf("actions = %d, want the two buttons and the section's", len(out.Actions))
	}
	first := out.Actions[0]
	if first.ActionID != "req_open" || first.BlockID != "b1" || first.Label != "Request budget increase" ||
		first.Value != "req_open" || first.Style != "primary" {
		t.Errorf("first action = %+v", first)
	}
	if out.Actions[1].URL != "https://example.com" {
		t.Errorf("second action url = %q, want the button's link", out.Actions[1].URL)
	}
	// A section's accessory button is pressable too, with the section
	// block's id.
	if got := out.Actions[2]; got.ActionID != "shuffle" || got.BlockID != "sec1" || got.Label != "Shuffle" {
		t.Errorf("section action = %+v, want shuffle on sec1", got)
	}
}

func TestConvertAppView_ServiceIDFallsBackToTheApp(t *testing.T) {
	out := convertAppView(&slackclient.View{ID: "V1", AppID: "A1"})
	if out.ServiceID != "A1" {
		t.Errorf("service id = %q, want the app id when the view has no bot", out.ServiceID)
	}
}
