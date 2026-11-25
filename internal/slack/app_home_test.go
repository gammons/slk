package slackclient

import (
	"strings"
	"testing"
)

// appHomeResponse is a trimmed conversations.info answer for an app DM
// with a Home tab: the app_home metadata, one section and one actions
// block with a plain and a confirming button.
const appHomeResponse = `{
  "ok": true,
  "app_home": {
    "app_id": "A0EXAMPLE1",
    "home_tab_enabled": true,
    "home_view_id": "V0EXAMPLE1"
  },
  "home_view": {
    "id": "V0EXAMPLE1",
    "app_id": "A0EXAMPLE1",
    "bot_id": "B0EXAMPLE1",
    "title": {"type": "plain_text", "text": "View Title"},
    "blocks": [
      {"type": "section", "text": {"type": "mrkdwn", "text": "Spent so far: *$335*"}},
      {"type": "actions", "block_id": "actions1", "elements": [
        {"type": "button", "action_id": "home_refresh", "text": {"type": "plain_text", "text": "Refresh"}, "value": "home_refresh"},
        {"type": "button", "action_id": "email_creds", "text": {"type": "plain_text", "text": "Email my project credentials"}, "value": "email_creds",
         "confirm": {"title": {"type": "plain_text", "text": "Email credentials?"},
                     "text": {"type": "mrkdwn", "text": "A fresh link will be emailed."},
                     "confirm": {"type": "plain_text", "text": "Send email"},
                     "deny": {"type": "plain_text", "text": "Cancel"}}}
      ]}
    ]
  }
}`

func TestParseAppHome(t *testing.T) {
	home, err := parseAppHome([]byte(appHomeResponse))
	if err != nil {
		t.Fatalf("parseAppHome: %v", err)
	}
	if home == nil {
		t.Fatal("parseAppHome returned nil for a home tab")
	}
	if home.AppID != "A0EXAMPLE1" || home.ViewID != "V0EXAMPLE1" || home.BotID != "B0EXAMPLE1" {
		t.Errorf("ids = app %q view %q bot %q, want A0EXAMPLE1 V0EXAMPLE1 B0EXAMPLE1",
			home.AppID, home.ViewID, home.BotID)
	}
	if home.Title != "View Title" {
		t.Errorf("title = %q, want View Title", home.Title)
	}
	if len(home.Blocks.BlockSet) != 2 {
		t.Fatalf("block count = %d, want 2", len(home.Blocks.BlockSet))
	}
}

func TestParseAppHome_NoHomeTab(t *testing.T) {
	cases := map[string]string{
		"no app_home":       `{"ok":true}`,
		"home tab disabled": `{"ok":true,"app_home":{"app_id":"A1","home_tab_enabled":false}}`,
		"no home_view":      `{"ok":true,"app_home":{"app_id":"A1","home_tab_enabled":true}}`,
		"home_view is null": `{"ok":true,"app_home":{"app_id":"A1","home_tab_enabled":true},"home_view":null}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			home, err := parseAppHome([]byte(body))
			if err != nil {
				t.Fatalf("parseAppHome: %v", err)
			}
			if home != nil {
				t.Errorf("home = %+v, want nil", home)
			}
		})
	}
}

func TestParseAppHome_APIError(t *testing.T) {
	_, err := parseAppHome([]byte(`{"ok":false,"error":"channel_not_found"}`))
	if err == nil || !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("err = %v, want one carrying channel_not_found", err)
	}
}

func TestAppHomeForm(t *testing.T) {
	form := appHomeForm("D01")
	if got := form.Get("channel"); got != "D01" {
		t.Errorf("channel = %q, want D01", got)
	}
	if got := form.Get("return_app_home"); got != "true" {
		t.Errorf("return_app_home = %q, want true", got)
	}
}
