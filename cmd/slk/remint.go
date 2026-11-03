package main

import (
	"context"
	"log"
	"slices"

	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackdesktop"
)

// remintToken refreshes one stored token from the live desktop session
// and saves it to store, as remintTokens does for every token at TUI
// launch. It returns the token unchanged on any failure, and always for
// a browser sign-in.
func remintToken(ctx context.Context, store *slackclient.TokenStore, tok slackclient.Token) slackclient.Token {
	return remintTokenWith(ctx, tok, slackdesktop.Cookie, slackdesktop.Tokens, slackclient.MintToken, store.Save)
}

// desktopLogin reads the login that the desktop app holds for tok's
// workspace, as remintToken does, but saves nothing. loginRefresher
// saves a login only after Slack accepted it as the account its caller
// wants, so a login it rejects never reaches the token store that the
// other slk processes read.
func desktopLogin(ctx context.Context, tok slackclient.Token) slackclient.Token {
	noSave := func(slackclient.Token) error { return nil }
	return remintTokenWith(ctx, tok, slackdesktop.Cookie, slackdesktop.Tokens, slackclient.MintToken, noSave)
}

// remintTokenWith refreshes one login with injected desktop reads, mint and
// save. Protected sources return before any desktop/keychain access.
func remintTokenWith(
	ctx context.Context,
	tok slackclient.Token,
	cookieFn func() (string, error),
	tokensFn func() (map[string]string, error),
	mintFn func(ctx context.Context, domain, cookie string) (string, error),
	saveFn func(slackclient.Token) error,
) slackclient.Token {
	if !tok.UsesDesktopLogin() {
		return tok
	}
	return remintTokens(ctx, []slackclient.Token{tok}, cookieFn, tokensFn, mintFn, saveFn)[0]
}

// remintTokens refreshes desktop-backed logins from the live desktop cookie.
// Protected sources never change; an all-protected batch skips desktop/keychain
// reads entirely. Read/mint failures keep cached logins (offline-friendly).
// cookieFn is read once per eligible batch; mintFn/saveFn are injected for tests.
func remintTokens(
	ctx context.Context,
	tokens []slackclient.Token,
	cookieFn func() (string, error),
	tokensFn func() (map[string]string, error),
	mintFn func(ctx context.Context, domain, cookie string) (string, error),
	saveFn func(slackclient.Token) error,
) []slackclient.Token {
	if !slices.ContainsFunc(tokens, slackclient.Token.UsesDesktopLogin) {
		return tokens
	}
	cookie, err := cookieFn()
	if err != nil {
		log.Printf("remint: could not read desktop cookie, using cached tokens: %v", err)
		return tokens
	}
	// Primary source: the desktop app's stored tokens (client-v2). A read
	// failure is non-fatal — we fall back to page-scrape minting per token.
	desktopTokens, err := tokensFn()
	if err != nil {
		log.Printf("remint: could not read desktop tokens, falling back to mint: %v", err)
	}
	out := make([]slackclient.Token, len(tokens))
	copy(out, tokens)
	for i := range out {
		if !out[i].UsesDesktopLogin() {
			continue
		}
		newTok := desktopTokens[out[i].TeamID]
		if newTok == "" {
			if out[i].Domain == "" {
				continue // legacy token without a domain; cannot re-mint
			}
			minted, err := mintFn(ctx, out[i].Domain, cookie)
			if err != nil {
				log.Printf("remint: %s: %v (keeping cached token)", out[i].TeamName, err)
				continue
			}
			newTok = minted
		}
		out[i].AccessToken = newTok
		out[i].Cookie = cookie
		out[i].UserID = ""
		if err := saveFn(out[i]); err != nil {
			log.Printf("remint: %s: save failed: %v", out[i].TeamName, err)
		}
	}
	return out
}
