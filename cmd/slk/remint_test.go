package main

import (
	"context"
	"testing"

	slackclient "github.com/gammons/slk/internal/slack"
)

func TestRemintTokens(t *testing.T) {
	in := []slackclient.Token{
		{AccessToken: "old1", Cookie: "c-old", Domain: "acme", TeamID: "T1", TeamName: "Acme"},
	}
	saved := map[string]slackclient.Token{}
	out := remintTokens(context.Background(), in,
		func() (string, error) { return "c-new", nil },
		func() (map[string]string, error) { return nil, nil }, // no desktop tokens → fall back to mint
		func(_ context.Context, domain, cookie string) (string, error) { return "xoxc-" + domain, nil },
		func(tok slackclient.Token) error { saved[tok.TeamID] = tok; return nil },
	)
	if out[0].AccessToken != "xoxc-acme" || out[0].Cookie != "c-new" {
		t.Fatalf("token not refreshed: %+v", out[0])
	}
	if saved["T1"].AccessToken != "xoxc-acme" {
		t.Fatalf("refreshed token not persisted: %+v", saved["T1"])
	}
}

// The desktop LevelDB token is preferred over minting, and mint is not called
// when it's present (client-v2 workspaces have no scrapeable token).
func TestRemintTokensPrefersDesktopToken(t *testing.T) {
	in := []slackclient.Token{
		{AccessToken: "old1", Cookie: "c-old", Domain: "acme", TeamID: "T1", TeamName: "Acme"},
	}
	saved := map[string]slackclient.Token{}
	mintCalled := false
	out := remintTokens(context.Background(), in,
		func() (string, error) { return "c-new", nil },
		func() (map[string]string, error) { return map[string]string{"T1": "xoxc-from-leveldb"}, nil },
		func(_ context.Context, _, _ string) (string, error) { mintCalled = true; return "xoxc-minted", nil },
		func(tok slackclient.Token) error { saved[tok.TeamID] = tok; return nil },
	)
	if out[0].AccessToken != "xoxc-from-leveldb" {
		t.Fatalf("expected desktop token, got %+v", out[0])
	}
	if mintCalled {
		t.Fatal("mint must not be called when a desktop token is present")
	}
	if saved["T1"].AccessToken != "xoxc-from-leveldb" {
		t.Fatalf("desktop token not persisted: %+v", saved["T1"])
	}
}

func TestRemintTokensKeepsOldOnMintFailure(t *testing.T) {
	in := []slackclient.Token{{AccessToken: "old1", Cookie: "c-old", Domain: "acme", TeamID: "T1"}}
	out := remintTokens(context.Background(), in,
		func() (string, error) { return "", context.Canceled }, // cookie read fails
		func() (map[string]string, error) { return nil, nil },
		func(_ context.Context, _, _ string) (string, error) { return "should-not-be-used", nil },
		func(slackclient.Token) error { return nil },
	)
	if out[0].AccessToken != "old1" {
		t.Fatalf("expected fallback to cached token, got %+v", out[0])
	}
}

// Cookie read succeeds but the per-token mint fails: keep that token's cached
// credentials untouched (the more common partial-failure case).
func TestRemintTokensKeepsOldOnPerTokenMintFailure(t *testing.T) {
	in := []slackclient.Token{{AccessToken: "old1", Cookie: "c-old", Domain: "acme", TeamID: "T1"}}
	out := remintTokens(context.Background(), in,
		func() (string, error) { return "c-new", nil }, // cookie OK
		func() (map[string]string, error) { return nil, nil },
		func(_ context.Context, _, _ string) (string, error) { return "", context.Canceled }, // mint fails
		func(slackclient.Token) error { return nil },
	)
	if out[0].AccessToken != "old1" {
		t.Fatalf("expected cached token kept on mint failure, got %+v", out[0])
	}
	if out[0].Cookie != "c-old" {
		t.Fatalf("cookie must not change when mint fails, got %+v", out[0])
	}
}
