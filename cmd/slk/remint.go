package main

import (
	"context"
	"log"

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

// remintTokenWith is remintToken with the desktop reads, the mint and
// the save injected, for tests.
//
// A browser sign-in returns at once. remintTokens never replaces one,
// but it reads the desktop app's cookie and tokens before it gets to
// that check, and those reads go through the OS keychain, which costs
// time and can show an authorization dialog. For a single token that
// is known to be skipped, that is all cost and no use.
func remintTokenWith(
	ctx context.Context,
	tok slackclient.Token,
	cookieFn func() (string, error),
	tokensFn func() (map[string]string, error),
	mintFn func(ctx context.Context, domain, cookie string) (string, error),
	saveFn func(slackclient.Token) error,
) slackclient.Token {
	if tok.Source == slackclient.TokenSourceBrowser {
		return tok
	}
	return remintTokens(ctx, []slackclient.Token{tok}, cookieFn, tokensFn, mintFn, saveFn)[0]
}

// remintTokens refreshes every token's xoxc from the live desktop cookie. On
// any failure for a given token it keeps the cached token (offline-friendly).
// cookieFn is read once up front; mintFn/saveFn are injected for testing.
func remintTokens(
	ctx context.Context,
	tokens []slackclient.Token,
	cookieFn func() (string, error),
	tokensFn func() (map[string]string, error),
	mintFn func(ctx context.Context, domain, cookie string) (string, error),
	saveFn func(slackclient.Token) error,
) []slackclient.Token {
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
		if out[i].Source == slackclient.TokenSourceBrowser {
			// Signed in from a browser session on purpose. The desktop
			// app can hold the same workspace under another account, and
			// its credentials would silently replace the chosen ones.
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
		if err := saveFn(out[i]); err != nil {
			log.Printf("remint: %s: save failed: %v", out[i].TeamName, err)
		}
	}
	return out
}
