package main

import (
	"context"
	"fmt"

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

// workspaceChoice is one row of the desktop flow's workspace picker.
type workspaceChoice struct {
	label, teamID string
}

// desktopChoices builds the picker's rows and its pre-selection. Every
// workspace is pre-selected, except one that saved already holds as a
// browser-session token: selecting it replaces that session with the desktop
// app's, which may be another account, so that must be a deliberate tick, not
// a side effect of adding some other workspace.
func desktopChoices(ws []slackdesktop.Workspace, saved []slackclient.Token) ([]workspaceChoice, []string) {
	browser := map[string]bool{}
	for _, tok := range saved {
		if tok.Source == slackclient.TokenSourceBrowser {
			browser[tok.TeamID] = true
		}
	}
	choices := make([]workspaceChoice, 0, len(ws))
	chosen := make([]string, 0, len(ws))
	for _, w := range ws {
		label := fmt.Sprintf("%s  (%s.slack.com)", w.Name, w.Domain)
		if browser[w.TeamID] {
			label += "  browser session, select to replace it"
		} else {
			chosen = append(chosen, w.TeamID)
		}
		choices = append(choices, workspaceChoice{label: label, teamID: w.TeamID})
	}
	return choices, chosen
}
