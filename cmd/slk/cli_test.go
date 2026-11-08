package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gammons/slk/internal/cli"
	"github.com/gammons/slk/internal/config"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/slack-go/slack"
)

// fakeSignIn records the tokens connectWithRemint signs in with and
// answers each with the next error in errs (nil: success).
type fakeSignIn struct {
	errs    []error
	tokens  []string
	remints int
}

func (f *fakeSignIn) connect(_ context.Context, tok slackclient.Token) (*slackclient.Client, error) {
	f.tokens = append(f.tokens, tok.AccessToken)
	err := f.errs[0]
	f.errs = f.errs[1:]
	if err != nil {
		return nil, err
	}
	return &slackclient.Client{}, nil
}

// remintTo returns a remint that hands out tok as the refreshed token.
func (f *fakeSignIn) remintTo(access string) func(context.Context, slackclient.Token) slackclient.Token {
	return func(_ context.Context, tok slackclient.Token) slackclient.Token {
		f.remints++
		tok.AccessToken = access
		return tok
	}
}

var errInvalidAuth = fmt.Errorf("auth test failed: %w", slack.SlackErrorResponse{Err: "invalid_auth"})

func TestConnectWithRemint_StoredTokenWorks(t *testing.T) {
	f := &fakeSignIn{errs: []error{nil}}
	client, err := connectWithRemint(context.Background(), slackclient.Token{AccessToken: "stored"}, f.connect, f.remintTo("fresh"))
	if err != nil || client == nil {
		t.Fatalf("client %v, err %v", client, err)
	}
	if f.remints != 0 || len(f.tokens) != 1 {
		t.Errorf("reminted %d times, signed in %d times; want 0 and 1", f.remints, len(f.tokens))
	}
}

func TestConnectWithRemint_InvalidAuthRemintsOnce(t *testing.T) {
	f := &fakeSignIn{errs: []error{errInvalidAuth, nil}}
	client, err := connectWithRemint(context.Background(), slackclient.Token{AccessToken: "stored"}, f.connect, f.remintTo("fresh"))
	if err != nil || client == nil {
		t.Fatalf("client %v, err %v", client, err)
	}
	if f.remints != 1 || len(f.tokens) != 2 || f.tokens[1] != "fresh" {
		t.Errorf("reminted %d times, signed in with %q; want once, then with the fresh token", f.remints, f.tokens)
	}
}

func TestConnectWithRemint_StillRefusedIsAnAuthError(t *testing.T) {
	f := &fakeSignIn{errs: []error{errInvalidAuth, errInvalidAuth}}
	_, err := connectWithRemint(context.Background(), slackclient.Token{AccessToken: "stored"}, f.connect, f.remintTo("fresh"))
	if !slackclient.IsAuthError(err) {
		t.Errorf("err = %v, want the auth error (exit 3)", err)
	}
	if f.remints != 1 || len(f.tokens) != 2 {
		t.Errorf("reminted %d times, signed in %d times; want 1 and 2", f.remints, len(f.tokens))
	}
}

// TestConnectWithRemint_UnchangedTokenIsNotRetried covers a browser
// sign-in (remintTokens never touches one) and a failed refresh: both
// give the same token back, and signing in with it again cannot help.
func TestConnectWithRemint_UnchangedTokenIsNotRetried(t *testing.T) {
	f := &fakeSignIn{errs: []error{errInvalidAuth}}
	_, err := connectWithRemint(context.Background(), slackclient.Token{AccessToken: "stored"}, f.connect, f.remintTo("stored"))
	if !slackclient.IsAuthError(err) || len(f.tokens) != 1 {
		t.Errorf("err %v after %d sign-ins; want the auth error after one", err, len(f.tokens))
	}
}

func TestConnectWithRemint_OtherErrorsDoNotRemint(t *testing.T) {
	offline := errors.New("dial tcp: no route to host")
	f := &fakeSignIn{errs: []error{offline}}
	_, err := connectWithRemint(context.Background(), slackclient.Token{AccessToken: "stored"}, f.connect, f.remintTo("fresh"))
	if !errors.Is(err, offline) || f.remints != 0 {
		t.Errorf("err %v, %d remints; want the network error and no remint", err, f.remints)
	}
}

// TestCLIWorkspaces_RailOrderSlugsAndDefault: the CLI lists workspaces
// as the rail shows them, and marks the one default_workspace names.
func TestCLIWorkspaces_RailOrderSlugsAndDefault(t *testing.T) {
	tokens := []slackclient.Token{
		{TeamID: "T2", TeamName: "Two", Domain: "two"},
		{TeamID: "T1", TeamName: "One", Domain: "one"},
		{TeamID: "T3", TeamName: "Three", Domain: "three"},
	}
	cfg := config.Config{
		General: config.General{DefaultWorkspace: "second"},
		Workspaces: map[string]config.Workspace{
			"first":  {TeamID: "T1", Order: 1},
			"second": {TeamID: "T2", Order: 2},
		},
	}
	want := []cli.Workspace{
		{TeamID: "T1", Slug: "first", Name: "One", Domain: "one"},
		{TeamID: "T2", Slug: "second", Name: "Two", Domain: "two", Default: true},
		{TeamID: "T3", Name: "Three", Domain: "three"},
	}
	got := cliWorkspaces(tokens, cfg)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("cliWorkspaces =\n%v\nwant\n%v", got, want)
	}

	cfg.General.DefaultWorkspace = "nope"
	for _, ws := range cliWorkspaces(tokens, cfg) {
		if ws.Default {
			t.Errorf("an unknown default_workspace marked %s as the default", ws.TeamID)
		}
	}
}
