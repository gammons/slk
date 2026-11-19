package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/slackurl"
)

// pendingFinderPermalink is one pasted archive link waiting for its
// workspace switch. Nil on App means nothing is waiting. url, teamID
// and seq always travel together: seq is what the switch message
// carries back, so a late switch for an older paste does not consume
// the request that replaced this one.
type pendingFinderPermalink struct {
	url    string
	teamID string
	seq    uint64
}

// finderPermalinkDroppedMsg is what openFinderPermalink delivers when
// the workspace switch it started returns nothing. The pending link is
// cleared and the URL opens in the browser, unless a newer paste has
// already replaced this request.
type finderPermalinkDroppedMsg struct {
	URL string
	Seq uint64
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
	a.finderPermalinkSeq++
	seq := a.finderPermalinkSeq
	a.pendingFinderPermalink = &pendingFinderPermalink{url: rawURL, teamID: teamID, seq: seq}
	switchCmd := a.switchWorkspace(teamID)
	return func() tea.Msg {
		var msg tea.Msg
		if switchCmd != nil {
			msg = switchCmd()
		}
		sw, ok := msg.(WorkspaceSwitchedMsg)
		if !ok {
			return finderPermalinkDroppedMsg{URL: rawURL, Seq: seq}
		}
		sw.FinderPermalinkSeq = seq
		return sw
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

// takeFinderPermalink consumes a finder permalink aimed at teamID when
// seq is the request still waiting. A switch that finished for an
// older paste leaves the newer link alone. The bool is true when
// routeLink started an in-app jump, so the caller must not also
// restore the last channel. A non-nil command with false is the
// browser fallback.
func (a *App) takeFinderPermalink(teamID string, seq uint64) (tea.Cmd, bool) {
	p := a.pendingFinderPermalink
	if p == nil || seq == 0 || seq != p.seq {
		return nil, false
	}
	url, team := p.url, p.teamID
	a.clearFinderPermalink()
	if team != teamID {
		return nil, false
	}
	// resetWindowTree emptied the pane and the composer was detached,
	// but activeChannelID still names the channel we left. A shared
	// channel has that same ID here, and applyLocation would then skip
	// ChannelSelectedMsg: the thread would open on an empty pane and
	// the composer would stay detached. Forget the id so this jump
	// loads the channel.
	a.activeChannelID = ""
	cmd := a.routeLink(url)
	return cmd, a.pendingLinkNav != nil
}

func (a *App) clearFinderPermalink() {
	a.pendingFinderPermalink = nil
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
