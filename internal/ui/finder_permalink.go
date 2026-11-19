package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/slackurl"
)

// finderPermalinkDroppedMsg is what openFinderPermalink delivers when
// the workspace switch it started returns nothing. The pending link is
// cleared and the URL opens in the browser.
type finderPermalinkDroppedMsg struct {
	URL string
}

// openFinderPermalink navigates to a Slack archive permalink chosen in
// the channel finder. A link for the workspace on screen goes through
// routeLink, the same path o uses. A link whose domain (or configured
// alias) is another connected workspace switches there first;
// takeFinderPermalink calls routeLink once that workspace is active.
// An unknown workspace, or a channel routeLink cannot resolve, opens
// in the browser.
func (a *App) openFinderPermalink(rawURL string) tea.Cmd {
	if a.compose.Uploading() || a.threadCompose.Uploading() {
		return toastWithClear(a, "Upload in progress", 2*time.Second)
	}
	rawURL = strings.TrimSpace(rawURL)
	pl, ok := slackurl.Parse(rawURL)
	if !ok {
		return a.browserOpener(rawURL)
	}
	teamID, ok := a.teamForPermalink(pl)
	if !ok {
		return a.browserOpener(rawURL)
	}
	if teamID == a.activeTeamID {
		return a.routeLink(rawURL)
	}
	if a.workspaceSvc == nil {
		return a.browserOpener(rawURL)
	}
	a.pendingFinderPermalink = rawURL
	a.pendingFinderPermalinkTeam = teamID
	switchCmd := a.switchWorkspace(teamID)
	return func() tea.Msg {
		var msg tea.Msg
		if switchCmd != nil {
			msg = switchCmd()
		}
		if msg == nil {
			return finderPermalinkDroppedMsg{URL: rawURL}
		}
		return msg
	}
}

// teamForPermalink is the configured workspace pl belongs to. A primary
// auth.test domain wins over an alias, and the workspace on screen wins
// when only aliases match, so a Grid alias of the active workspace is
// not stolen by another workspace that lists the same alias.
func (a *App) teamForPermalink(pl slackurl.Permalink) (string, bool) {
	if pl.Subdomain == "" {
		return "", false
	}
	for teamID, domain := range a.workspaceDomains {
		if domain != "" && domain == pl.Subdomain {
			return teamID, true
		}
	}
	if a.permalinkBelongsToActiveWorkspace(pl) {
		return a.activeTeamID, true
	}
	for teamID, domains := range a.permalinkDomains {
		for _, domain := range domains {
			if domain == pl.Subdomain {
				return teamID, true
			}
		}
	}
	return "", false
}

// takeFinderPermalink consumes a finder permalink aimed at teamID, which
// has just become active. The bool is true when routeLink started an
// in-app jump, so the caller must not also restore the last channel.
// A non-nil command with false is the browser fallback.
func (a *App) takeFinderPermalink(teamID string) (tea.Cmd, bool) {
	if a.pendingFinderPermalink == "" {
		return nil, false
	}
	url, team := a.pendingFinderPermalink, a.pendingFinderPermalinkTeam
	a.clearFinderPermalink()
	if team != teamID {
		return nil, false
	}
	cmd := a.routeLink(url)
	return cmd, a.pendingLinkNav != nil
}

func (a *App) clearFinderPermalink() {
	a.pendingFinderPermalink = ""
	a.pendingFinderPermalinkTeam = ""
}

// pasteChannelFinder inserts a bracketed paste into the open finder.
// Newlines are dropped so a copied link stays one query.
func (a *App) pasteChannelFinder(content string) tea.Cmd {
	before := a.channelFinder.Query()
	flat := strings.NewReplacer("\r\n", "", "\r", "", "\n", "").Replace(content)
	a.channelFinder.InsertText(flat)
	return a.scheduleFinderSearch(before)
}

// scheduleFinderSearch queues the finder's remote channel search after
// the query changed from before. A permalink is not a channel query, so
// it cancels a search the previous keystroke scheduled and issues none.
func (a *App) scheduleFinderSearch(before string) tea.Cmd {
	if a.pendingForward != nil {
		return nil
	}
	after := a.channelFinder.Query()
	if after == before {
		return nil
	}
	if a.channelFinder.PermalinkURL() != "" {
		return a.scheduleChannelSearch("")
	}
	return a.scheduleChannelSearch(after)
}
