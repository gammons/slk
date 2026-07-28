package main

import (
	"context"
	"testing"

	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackdesktop"
)

func TestRefreshTokens(t *testing.T) {
	in := []slackclient.Token{{AccessToken: "old", Cookie: "c-old", Domain: "acme", TeamID: "T1", TeamName: "Acme"}}
	saved := map[string]slackclient.Token{}
	out := refreshTokens(in,
		func() (string, error) { return "c-new", nil },
		func() ([]slackdesktop.Workspace, error) {
			return []slackdesktop.Workspace{{Name: "Acme", Domain: "acme", TeamID: "T1", Token: "xoxc-new"}}, nil
		},
		func(tok slackclient.Token) error { saved[tok.TeamID] = tok; return nil },
	)
	if out[0].AccessToken != "xoxc-new" || out[0].Cookie != "c-new" {
		t.Fatalf("token not refreshed: %+v", out[0])
	}
	if saved["T1"].AccessToken != "xoxc-new" {
		t.Fatalf("refreshed token not persisted: %+v", saved["T1"])
	}
}

func TestRefreshTokensKeepsOldOnReadFailure(t *testing.T) {
	in := []slackclient.Token{{AccessToken: "old", Cookie: "c-old", Domain: "acme", TeamID: "T1"}}
	out := refreshTokens(in,
		func() (string, error) { return "", context.Canceled },
		func() ([]slackdesktop.Workspace, error) { return nil, context.Canceled },
		func(slackclient.Token) error { return nil },
	)
	if out[0].AccessToken != "old" {
		t.Fatalf("expected cached token, got %+v", out[0])
	}
}

func TestRefreshTokensKeepsOldWhenTeamMissing(t *testing.T) {
	in := []slackclient.Token{{AccessToken: "old", Cookie: "c-old", Domain: "acme", TeamID: "T1"}}
	out := refreshTokens(in,
		func() (string, error) { return "c-new", nil },
		func() ([]slackdesktop.Workspace, error) {
			return []slackdesktop.Workspace{{Name: "Other", Domain: "other", TeamID: "T9", Token: "xoxc-other"}}, nil
		},
		func(slackclient.Token) error { return nil },
	)
	if out[0].AccessToken != "old" {
		t.Fatalf("expected cached token when team absent, got %+v", out[0])
	}
}
