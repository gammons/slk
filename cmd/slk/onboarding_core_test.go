package main

import (
	"testing"

	"github.com/gammons/slk/internal/slackdesktop"
)

func TestBuildWorkspaceTokens(t *testing.T) {
	ws := []slackdesktop.Workspace{
		{Name: "Acme", Domain: "acme", TeamID: "T1", Token: "xoxc-acme"},
		{Name: "Beta", Domain: "beta", TeamID: "T2", Token: "xoxc-beta"},
	}
	selected := map[string]bool{"T1": true} // only Acme
	toks := buildWorkspaceTokens("xoxd-c", ws, selected)
	if len(toks) != 1 {
		t.Fatalf("got %d tokens, want 1", len(toks))
	}
	got := toks[0]
	if got.TeamID != "T1" || got.AccessToken != "xoxc-acme" || got.Domain != "acme" || got.Cookie != "xoxd-c" || got.TeamName != "Acme" {
		t.Fatalf("unexpected token: %+v", got)
	}
}
