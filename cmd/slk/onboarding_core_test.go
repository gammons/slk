package main

import (
	"context"
	"testing"

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
