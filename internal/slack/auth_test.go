package slackclient

import (
	"testing"
)

func TestSaveAndLoadToken(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	token := Token{
		AccessToken:  "xoxp-test-token",
		RefreshToken: "xoxr-refresh-token",
		TeamID:       "T123",
		TeamName:     "Acme",
	}

	if err := store.Save(token); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load("T123")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "xoxp-test-token" {
		t.Errorf("expected access token 'xoxp-test-token', got %q", got.AccessToken)
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

	store.Save(Token{AccessToken: "t1", TeamID: "T1", TeamName: "Team 1"})
	store.Save(Token{AccessToken: "t2", TeamID: "T2", TeamName: "Team 2"})

	tokens, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(tokens))
	}
}
