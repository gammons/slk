package slackclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func appCommandsCapture(t *testing.T) (body []byte, reason string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "superpowers", "specs",
		"2026-10-12-slash-commands-captures", "client_app_commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Form     [][2]string     `json:"form"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(raw, &capture); err != nil {
		t.Fatal(err)
	}
	for _, kv := range capture.Form {
		if kv[0] == "_x_reason" {
			reason = kv[1]
		}
	}
	if reason == "" {
		t.Fatal("client_app_commands.json has no _x_reason")
	}
	return capture.Response, reason
}

func TestParseAppCommands_Capture(t *testing.T) {
	body, _ := appCommandsCapture(t)
	got, err := parseAppCommands(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 32 {
		t.Fatalf("len = %d, want 32 commands", len(got))
	}
	byName := map[string]struct {
		usage, desc, app, alias string
	}{}
	var aliases, apps int
	for _, c := range got {
		byName[c.Name] = struct {
			usage, desc, app, alias string
		}{c.Usage, c.Desc, c.AppName, c.AliasOf}
		if c.AliasOf != "" {
			aliases++
		}
		if c.AppName != "" {
			apps++
		}
	}
	if aliases != 5 {
		t.Errorf("aliases = %d, want 5", aliases)
	}
	if apps != 7 {
		t.Errorf("commands with an app name = %d, want 7 (the app type; /giphy is a service)", apps)
	}
	// 24 core (including the 5 aliases) + /giphy have no app_name.
	if coreish := len(got) - apps; coreish != 25 {
		t.Errorf("commands without an app name = %d, want 25 (24 core + /giphy)", coreish)
	}
	checks := []struct {
		name, usage, desc, app, alias string
	}{
		{"/shrug", "your text", `Appends ¯\_(ツ)_/¯ to your message`, "", ""},
		{"/away", "", `Toggle your "away" status`, "", ""},
		{"/dm", "@user [your message]", "", "", "/msg"},
		{"/join", "", "", "", "/open"},
		{"/close", "", "", "", "/leave"},
		{"/part", "", "", "", "/leave"},
		{"/kick", "", "", "", "/remove"},
		{"/linear", "issue title (optional) ", "Create new issue on Linear", "Linear", ""},
		{"/giphy", "[text]", "Post a random gif to channel", "", ""},
	}
	for _, c := range checks {
		got := byName[c.name]
		if got.usage != c.usage || got.desc != c.desc || got.app != c.app || got.alias != c.alias {
			t.Errorf("%s = usage %q desc %q app %q alias %q; want usage %q desc %q app %q alias %q",
				c.name, got.usage, got.desc, got.app, got.alias, c.usage, c.desc, c.app, c.alias)
		}
	}
}

func TestParseAppCommands_NotOK(t *testing.T) {
	_, err := parseAppCommands([]byte(`{"ok":false,"error":"invalid_auth"}`))
	if err == nil || !strings.Contains(err.Error(), "invalid_auth") {
		t.Fatalf("err = %v, want invalid_auth", err)
	}
}

func TestAppCommands_PostsAndParses(t *testing.T) {
	body, reason := appCommandsCapture(t)
	var gotPath, gotReason string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		gotReason = r.FormValue("_x_reason")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)

	got, err := c.AppCommands(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/client.appCommands" {
		t.Errorf("path = %q, want /api/client.appCommands", gotPath)
	}
	if gotReason != reason {
		t.Errorf("_x_reason = %q, want %q", gotReason, reason)
	}
	if len(got) != 32 {
		t.Errorf("len = %d, want 32", len(got))
	}
}
