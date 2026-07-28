package main

import (
	"log"

	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackdesktop"
)

// refreshTokens re-reads each workspace's xoxc token from the desktop app's
// localConfig_v2 (and the live d cookie) on startup, so a rotated/expired
// stored token never causes a failed launch. On any read failure, or when a
// stored team is not present in localConfig_v2, the cached token is kept.
func refreshTokens(
	tokens []slackclient.Token,
	cookieFn func() (string, error),
	teamsFn func() ([]slackdesktop.Workspace, error),
	saveFn func(slackclient.Token) error,
) []slackclient.Token {
	cookie, cerr := cookieFn()
	teams, terr := teamsFn()
	if cerr != nil || terr != nil {
		log.Printf("refresh: could not read desktop session (cookie err=%v, teams err=%v); using cached tokens", cerr, terr)
		return tokens
	}
	byID := make(map[string]slackdesktop.Workspace, len(teams))
	for _, w := range teams {
		byID[w.TeamID] = w
	}
	out := make([]slackclient.Token, len(tokens))
	copy(out, tokens)
	for i := range out {
		w, ok := byID[out[i].TeamID]
		if !ok || w.Token == "" {
			log.Printf("refresh: %s not in localConfig_v2; keeping cached token", out[i].TeamName)
			continue
		}
		out[i].AccessToken = w.Token
		out[i].Cookie = cookie
		if err := saveFn(out[i]); err != nil {
			log.Printf("refresh: %s: save failed: %v", out[i].TeamName, err)
		}
	}
	return out
}
