package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackdesktop"
)

func TestBuildWorkspaceTokens(t *testing.T) {
	ws := []slackdesktop.Workspace{
		{Name: "Acme", Domain: "acme", TeamID: "T1"},
		{Name: "Beta", Domain: "beta", TeamID: "T2"},
	}
	selected := map[string]bool{"T1": true} // only Acme
	mint := func(_ context.Context, domain, cookie string) (string, error) {
		return "xoxc-" + domain, nil
	}
	toks, err := buildWorkspaceTokens(context.Background(), "xoxd-c", nil, ws, selected, mint)
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != 1 || toks[0].TeamID != "T1" || toks[0].AccessToken != "xoxc-acme" || toks[0].Domain != "acme" {
		t.Fatalf("unexpected tokens: %+v", toks)
	}
	if toks[0].Cookie != "xoxd-c" || toks[0].TeamName != "Acme" {
		t.Fatalf("unexpected token fields: %+v", toks[0])
	}
}

func TestDesktopChoicesLeavesProtectedSessionsUnselected(t *testing.T) {
	for _, tc := range []struct{ source, label string }{
		{slackclient.TokenSourceBrowser, "browser session"},
		{slackclient.TokenSourceImported, "imported session"},
		{"future-source", "saved session"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			ws := []slackdesktop.Workspace{
				{TeamID: "T1", Name: "Protected", Domain: "protected"},
				{TeamID: "T2", Name: "Desktop", Domain: "desktop"},
				{TeamID: "T3", Name: "New", Domain: "new"},
			}
			saved := []slackclient.Token{{TeamID: "T1", Source: tc.source}, {TeamID: "T2"}}
			choices, chosen := desktopChoices(ws, saved)
			if !slices.Equal(chosen, []string{"T2", "T3"}) {
				t.Fatalf("preselected %v, want only desktop and new workspaces", chosen)
			}
			if len(choices) != 3 || choices[0].teamID != "T1" || !strings.Contains(choices[0].label, tc.label) || !strings.Contains(choices[0].label, "select to replace") {
				t.Fatal("protected row must remain available with a deliberate replacement label")
			}
			if strings.Contains(choices[1].label, "select to replace") || strings.Contains(choices[2].label, "select to replace") {
				t.Fatal("desktop/new rows should keep their ordinary labels")
			}
		})
	}
}
