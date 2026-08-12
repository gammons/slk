package main

import (
	"context"

	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackdesktop"
)

// minter matches slackclient.MintToken; injected for testing.
type minter func(ctx context.Context, domain, cookie string) (string, error)

// buildWorkspaceTokens resolves a token for each selected workspace and returns
// the Token records to persist. Workspaces whose TeamID is not in `selected`
// are skipped.
//
// The token comes from the desktop app's Local Storage (desktopTokens, keyed by
// team ID) — modern Slack (client-v2) no longer embeds it in page HTML. Minting
// via page-scrape is kept only as a fallback for older workspaces that still do.
func buildWorkspaceTokens(ctx context.Context, cookie string, desktopTokens map[string]string, ws []slackdesktop.Workspace, selected map[string]bool, mint minter) ([]slackclient.Token, error) {
	var out []slackclient.Token
	for _, w := range ws {
		if !selected[w.TeamID] {
			continue
		}
		tok := desktopTokens[w.TeamID]
		if tok == "" {
			var err error
			tok, err = mint(ctx, w.Domain, cookie)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, slackclient.Token{
			AccessToken: tok,
			Cookie:      cookie,
			Domain:      w.Domain,
			TeamID:      w.TeamID,
			TeamName:    w.Name,
		})
	}
	return out, nil
}
