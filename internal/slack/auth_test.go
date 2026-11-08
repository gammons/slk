package slackclient

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/slack-go/slack"
)

func TestSaveAndLoadToken(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	token := Token{
		AccessToken: "xoxc-test-token",
		Cookie:      "xoxd-test-cookie",
		TeamID:      "T123",
		TeamName:    "Acme",
	}

	if err := store.Save(token); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load("T123")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "xoxc-test-token" {
		t.Errorf("expected access token 'xoxc-test-token', got %q", got.AccessToken)
	}
	if got.Cookie != "xoxd-test-cookie" {
		t.Errorf("expected cookie 'xoxd-test-cookie', got %q", got.Cookie)
	}
	if got.TeamName != "Acme" {
		t.Errorf("expected team name 'Acme', got %q", got.TeamName)
	}
}

func TestLoadTokenNotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	_, err := store.Load("nonexistent")
	if err == nil {
		t.Error("expected error for missing token")
	}
}

func TestListTokens(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	store.Save(Token{AccessToken: "t1", Cookie: "c1", TeamID: "T1", TeamName: "Team 1"})
	store.Save(Token{AccessToken: "t2", Cookie: "c2", TeamID: "T2", TeamName: "Team 2"})

	tokens, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(tokens))
	}
}

func TestTokenRoundTripIncludesDomain(t *testing.T) {
	dir := t.TempDir()
	s := NewTokenStore(dir)
	in := Token{AccessToken: "xoxc-1", Cookie: "xoxd-1", Domain: "acme", TeamID: "T1", TeamName: "Acme"}
	if err := s.Save(in); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("T1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Domain != "acme" {
		t.Errorf("Domain = %q, want acme", got.Domain)
	}
}

// TestTokenStore_ListNeverSeesPartialSave is the regression guard for
// non-atomic token saves. List silently skips a token file it cannot
// parse, so a TUI that starts while another slk process (a CLI command
// that refreshed a login, or slk export) rewrites a token would come
// up without that workspace. The reader below stands in for that TUI:
// every List must return both workspaces, whole.
func TestTokenStore_ListNeverSeesPartialSave(t *testing.T) {
	store := NewTokenStore(t.TempDir())
	// A long name widens the truncate-then-write window enough that a
	// reader lands in it reliably; a real token file is a few hundred
	// bytes, which makes the race real but rare.
	long := strings.Repeat("padding ", 64*1024)
	if err := store.Save(Token{AccessToken: "xoxc-1", TeamID: "T1", TeamName: long}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Token{AccessToken: "xoxc-2", TeamID: "T2", TeamName: "Other"}); err != nil {
		t.Fatal(err)
	}

	var stop atomic.Bool
	saveErr := make(chan error, 1)
	go func() {
		defer close(saveErr)
		for i := 0; !stop.Load(); i++ {
			if err := store.Save(Token{AccessToken: fmt.Sprintf("xoxc-%d", i), TeamID: "T1", TeamName: long}); err != nil {
				saveErr <- err
				return
			}
		}
	}()

	for i := 0; i < 300; i++ {
		tokens, err := store.List()
		if err != nil || len(tokens) != 2 {
			stop.Store(true)
			<-saveErr
			t.Fatalf("read %d: List() returned %d tokens, err %v; want both workspaces", i, len(tokens), err)
		}
	}
	stop.Store(true)
	if err := <-saveErr; err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// TestIsAuthError separates Slack refusing the login (which a fresh
// token can cure, and which the CLI reports as exit 3) from Slack
// refusing one request.
func TestIsAuthError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{slack.SlackErrorResponse{Err: "invalid_auth"}, true},
		{fmt.Errorf("auth test failed: %w", slack.SlackErrorResponse{Err: "not_authed"}), true},
		{slack.SlackErrorResponse{Err: "token_expired"}, true},
		{slack.SlackErrorResponse{Err: "token_revoked"}, true},
		{slack.SlackErrorResponse{Err: "account_inactive"}, true},
		{slack.SlackErrorResponse{Err: "channel_not_found"}, false},
		{&slack.RateLimitedError{}, false},
		{errors.New("invalid_auth"), false}, // only Slack's own answer counts
		{nil, false},
	}
	for _, tc := range cases {
		if got := IsAuthError(tc.err); got != tc.want {
			t.Errorf("IsAuthError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
