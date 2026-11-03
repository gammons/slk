package main

import (
	"context"
	"slices"
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

// A token signed in from a browser session is the user's explicit choice: the
// desktop app can hold the same workspace under another account, so neither
// its stored token nor a mint from its cookie may replace it.
func TestRemintTokensLeavesBrowserSessionsAlone(t *testing.T) {
	browser := slackclient.Token{
		AccessToken: "xoxc-browser", Cookie: "c-browser", Domain: "acme",
		TeamID: "T1", TeamName: "Acme", Source: slackclient.TokenSourceBrowser,
	}
	desktop := slackclient.Token{AccessToken: "old2", Cookie: "c-old", Domain: "other", TeamID: "T2", TeamName: "Other"}
	saved := map[string]slackclient.Token{}
	mintedFor := []string{}
	out := remintTokens(context.Background(), []slackclient.Token{browser, desktop},
		func() (string, error) { return "c-desktop", nil },
		func() (map[string]string, error) { return map[string]string{"T1": "xoxc-desktop-user"}, nil },
		func(_ context.Context, domain, _ string) (string, error) {
			mintedFor = append(mintedFor, domain)
			return "xoxc-" + domain, nil
		},
		func(tok slackclient.Token) error { saved[tok.TeamID] = tok; return nil },
	)
	if out[0] != browser {
		t.Errorf("browser session changed: %+v", out[0])
	}
	if _, ok := saved["T1"]; ok {
		t.Errorf("browser session was saved over: %+v", saved["T1"])
	}
	if out[1].AccessToken != "xoxc-other" || out[1].Cookie != "c-desktop" {
		t.Errorf("desktop token not refreshed: %+v", out[1])
	}
	if len(mintedFor) != 1 || mintedFor[0] != "other" {
		t.Errorf("minted for %v, want only the desktop workspace", mintedFor)
	}
}

// A refused browser sign-in must not touch the desktop app's credentials
// at all: remint never replaces one, and the cookie and token reads go
// through the OS keychain, which can show an authorization dialog to a
// script. remintTokens alone reads both before it skips the token.
func TestRemintTokenWithSkipsBrowserSessionsBeforeAnyDesktopRead(t *testing.T) {
	browser := slackclient.Token{
		AccessToken: "xoxc-browser", Cookie: "c-browser", Domain: "acme",
		TeamID: "T1", TeamName: "Acme", Source: slackclient.TokenSourceBrowser,
	}
	got := remintTokenWith(context.Background(), browser,
		func() (string, error) { t.Error("read the desktop cookie"); return "c-desktop", nil },
		func() (map[string]string, error) { t.Error("read the desktop tokens"); return nil, nil },
		func(context.Context, string, string) (string, error) {
			t.Error("minted a token")
			return "xoxc-minted", nil
		},
		func(slackclient.Token) error { t.Error("saved a token"); return nil },
	)
	if got != browser {
		t.Errorf("browser session changed: %+v", got)
	}
}

// The early return is for browser sign-ins only: a desktop sign-in still
// gets the desktop app's token, saved.
func TestRemintTokenWithRefreshesDesktopSessions(t *testing.T) {
	in := slackclient.Token{AccessToken: "old1", Cookie: "c-old", Domain: "acme", TeamID: "T1", TeamName: "Acme"}
	saved := map[string]slackclient.Token{}
	got := remintTokenWith(context.Background(), in,
		func() (string, error) { return "c-new", nil },
		func() (map[string]string, error) { return map[string]string{"T1": "xoxc-desktop"}, nil },
		func(context.Context, string, string) (string, error) {
			t.Error("minted despite a desktop token")
			return "", nil
		},
		func(tok slackclient.Token) error { saved[tok.TeamID] = tok; return nil },
	)
	if got.AccessToken != "xoxc-desktop" || got.Cookie != "c-new" || saved["T1"] != got {
		t.Errorf("got %+v, saved %+v; want the desktop token, saved", got, saved["T1"])
	}
}

func TestRemintTokenWithSkipsProtectedSources(t *testing.T) {
	for _, source := range []string{slackclient.TokenSourceBrowser, slackclient.TokenSourceImported, "future-source"} {
		t.Run(source, func(t *testing.T) {
			in := testLogin("xoxc-protected", "d-protected")
			in.Source, in.UserID = source, "UPROTECTED"
			got := remintTokenWith(context.Background(), in,
				func() (string, error) { t.Error("read desktop cookie/keychain"); return "d-desktop", nil },
				func() (map[string]string, error) { t.Error("read desktop tokens"); return nil, nil },
				func(context.Context, string, string) (string, error) {
					t.Error("minted a token")
					return "xoxc-desktop", nil
				},
				func(slackclient.Token) error { t.Error("saved a protected token"); return nil },
			)
			if got != in {
				t.Fatal("protected login changed")
			}
		})
	}
}

func TestRemintTokensSkipsDesktopReadsForProtectedBatch(t *testing.T) {
	protected := []slackclient.Token{
		{TeamID: "T1", Source: slackclient.TokenSourceBrowser},
		{TeamID: "T2", Source: slackclient.TokenSourceImported, UserID: "UIMPORTED"},
		{TeamID: "T3", Source: "future-source"},
	}
	for _, in := range [][]slackclient.Token{nil, protected} {
		out := remintTokens(context.Background(), in,
			func() (string, error) { t.Error("read desktop cookie/keychain"); return "d-desktop", nil },
			func() (map[string]string, error) { t.Error("read desktop tokens"); return nil, nil },
			func(context.Context, string, string) (string, error) {
				t.Error("minted a token")
				return "xoxc-desktop", nil
			},
			func(slackclient.Token) error { t.Error("saved a protected token"); return nil },
		)
		if !slices.Equal(out, in) {
			t.Error("protected batch changed")
		}
	}
}

func TestRemintTokensMixedSourcesRefreshesOnlyDesktop(t *testing.T) {
	in := []slackclient.Token{
		{TeamID: "T1", Domain: "browser", Source: slackclient.TokenSourceBrowser},
		{TeamID: "T2", Domain: "imported", Source: slackclient.TokenSourceImported, UserID: "UIMPORTED"},
		{TeamID: "T3", Domain: "future", Source: "future-source"},
		{TeamID: "T4", Domain: "desktop"},
	}
	var cookies, reads, mints int
	var saved []slackclient.Token
	out := remintTokens(context.Background(), in,
		func() (string, error) { cookies++; return "d-desktop", nil },
		func() (map[string]string, error) {
			reads++
			return map[string]string{"T1": "xoxc-wrong", "T2": "xoxc-wrong", "T3": "xoxc-wrong"}, nil
		},
		func(_ context.Context, domain, _ string) (string, error) {
			mints++
			if domain != "desktop" {
				t.Error("minted for a protected source")
			}
			return "xoxc-desktop", nil
		},
		func(tok slackclient.Token) error { saved = append(saved, tok); return nil },
	)
	if !slices.Equal(out[:3], in[:3]) || !slices.Equal(in[3:], []slackclient.Token{{TeamID: "T4", Domain: "desktop"}}) {
		t.Fatal("protected tokens or caller's input changed")
	}
	if cookies != 1 || reads != 1 || mints != 1 || len(saved) != 1 || saved[0] != out[3] || out[3].AccessToken != "xoxc-desktop" {
		t.Fatal("mixed batch did not refresh and save only the desktop entry once")
	}
}

func TestRemintTokensClearsIdentityOfRefreshedPair(t *testing.T) {
	for _, levelDB := range []bool{false, true} {
		t.Run(map[bool]string{false: "mint", true: "desktop-token"}[levelDB], func(t *testing.T) {
			original := testLogin("xoxc-old", "d-old")
			original.Domain, original.UserID = "acme", "UOLD"
			var saved slackclient.Token
			out := remintTokens(context.Background(), []slackclient.Token{original},
				func() (string, error) { return "d-desktop", nil },
				func() (map[string]string, error) {
					if levelDB {
						return map[string]string{"T1": "xoxc-desktop"}, nil
					}
					return nil, nil
				},
				func(context.Context, string, string) (string, error) { return "xoxc-desktop", nil },
				func(tok slackclient.Token) error { saved = tok; return nil },
			)
			if len(out) != 1 || out[0].UserID != "" || saved.UserID != "" || saved != out[0] {
				t.Fatal("desktop refresh retained an identity belonging to the old pair")
			}
			if original.UserID != "UOLD" {
				t.Fatal("refresh mutated the original snapshot")
			}
		})
	}
}

func TestRemintTokensFailurePreservesIdentity(t *testing.T) {
	original := testLogin("xoxc-old", "d-old")
	original.Domain, original.UserID = "acme", "UOLD"
	out := remintTokens(context.Background(), []slackclient.Token{original},
		func() (string, error) { return "d-desktop", nil },
		func() (map[string]string, error) { return nil, nil },
		func(context.Context, string, string) (string, error) { return "", context.Canceled },
		func(slackclient.Token) error { t.Fatal("saved failed refresh"); return nil },
	)
	if len(out) != 1 || out[0] != original {
		t.Fatal("failed refresh changed the verified cached identity")
	}
}
