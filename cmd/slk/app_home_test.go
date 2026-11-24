package main

import (
	"encoding/json"
	"testing"

	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/slack-go/slack"
)

const convertAppHomeJSON = `[
  {"type": "section", "text": {"type": "mrkdwn", "text": "Spent so far: *$335*"}},
  {"type": "actions", "block_id": "actions1", "elements": [
    {"type": "button", "action_id": "home_refresh", "text": {"type": "plain_text", "text": "Refresh"}, "value": "home_refresh"},
    {"type": "button", "action_id": "home_support", "text": {"type": "plain_text", "text": "Support"}, "url": "https://example.com/support", "style": "primary"},
    {"type": "button", "action_id": "email_creds", "text": {"type": "plain_text", "text": "Email my project credentials"}, "value": "email_creds",
     "confirm": {"title": {"type": "plain_text", "text": "Email credentials?"},
                 "text": {"type": "mrkdwn", "text": "A fresh link will be emailed."},
                 "confirm": {"type": "plain_text", "text": "Send email"},
                 "deny": {"type": "plain_text", "text": "Cancel"}}}
  ]},
  {"type": "actions", "block_id": "actions2", "elements": [
    {"type": "static_select", "action_id": "pick", "placeholder": {"type": "plain_text", "text": "Pick one"}}
  ]}
]`

func convertTestHome(t *testing.T) *slackclient.AppHome {
	t.Helper()
	var blocks slack.Blocks
	if err := json.Unmarshal([]byte(convertAppHomeJSON), &blocks); err != nil {
		t.Fatalf("decoding blocks: %v", err)
	}
	return &slackclient.AppHome{
		AppID: "A1", ViewID: "V1", Title: "View Title", BotID: "B1",
		Blocks: blocks,
	}
}

func TestConvertAppHome(t *testing.T) {
	home := convertAppHome(convertTestHome(t))
	if home.AppID != "A1" || home.ViewID != "V1" || home.ServiceID != "B1" {
		t.Errorf("ids = app %q view %q service %q, want A1 V1 B1",
			home.AppID, home.ViewID, home.ServiceID)
	}
	if len(home.Blocks) != 3 {
		t.Errorf("blocks = %d, want 3", len(home.Blocks))
	}
	if len(home.Actions) != 3 {
		t.Fatalf("actions = %d, want 3 buttons (selects excluded)", len(home.Actions))
	}

	refresh := home.Actions[0]
	if refresh.ActionID != "home_refresh" || refresh.BlockID != "actions1" ||
		refresh.Label != "Refresh" || refresh.Value != "home_refresh" {
		t.Errorf("refresh = %+v", refresh)
	}
	if refresh.Confirm != nil {
		t.Error("refresh carries a confirm, want none")
	}

	link := home.Actions[1]
	if link.URL != "https://example.com/support" || link.Style != "primary" {
		t.Errorf("link = %+v", link)
	}

	confirm := home.Actions[2]
	if confirm.Confirm == nil {
		t.Fatal("email button lost its confirmation")
	}
	want := struct{ title, text, ok, dismiss string }{
		"Email credentials?", "A fresh link will be emailed.", "Send email", "Cancel",
	}
	if confirm.Confirm.Title != want.title || confirm.Confirm.Text != want.text ||
		confirm.Confirm.OKText != want.ok || confirm.Confirm.DismissText != want.dismiss {
		t.Errorf("confirm = %+v, want %+v", confirm.Confirm, want)
	}
}
