package main

import (
	"context"
	"log"

	slackclient "github.com/gammons/slk/internal/slack"
)

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
