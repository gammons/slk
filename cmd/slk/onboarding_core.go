package main

import (
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackdesktop"
)

// buildWorkspaceTokens builds a Token per selected workspace from the token the
// desktop app already holds (localConfig_v2) plus the d cookie. Workspaces
// whose TeamID is not in `selected` are skipped.
func buildWorkspaceTokens(cookie string, ws []slackdesktop.Workspace, selected map[string]bool) []slackclient.Token {
	var out []slackclient.Token
	for _, w := range ws {
		if !selected[w.TeamID] {
			continue
		}
		out = append(out, slackclient.Token{
			AccessToken: w.Token,
			Cookie:      cookie,
			Domain:      w.Domain,
			TeamID:      w.TeamID,
			TeamName:    w.Name,
		})
	}
	return out
}
