// internal/ui/reducer_links.go
//
// Link-open routing (issue #62 + in-app permalink navigation).
//
// OpenLinkMsg is the single place every link open flows through:
//   - Slack archive permalinks whose subdomain matches the active
//     workspace AND whose channel resolves via ChannelService.Lookup
//     navigate in-app: dispatch ChannelSelectedMsg, then complete via
//     pendingLinkNav once the channel's messages are loaded (select
//     the target ts, or open the thread panel for thread_ts links).
//   - Everything else opens in the OS browser (a.browserOpener).
//
// Completion hooks live in reducer_channels.go (ChannelSelectedMsg
// and MessagesLoadedMsg arms call completePendingLinkNav).
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/slackurl"
	"github.com/gammons/slk/internal/ui/messages"
)

// pendingLinkNav is the not-yet-completed tail of an in-app permalink
// navigation. Thread targets survive until both cache and fetch complete.
type pendingLinkNav struct {
	channelID    string
	messageTS    string
	threadTS     string // non-empty: select within the thread panel
	teamID       string
	threadOpened bool
	loadsDone    int
	fetchApplied bool
}

// Only permalink loads carry this envelope; ordinary thread refreshes must
// not inherit a pending target. nav also identifies superseded requests.
type permalinkThreadResultMsg struct {
	nav           *pendingLinkNav
	result        tea.Msg
	authoritative bool
	loads         int
}

func (p *pendingLinkNav) wrapThreadLoad(cmd tea.Cmd, authoritative bool, loads int) tea.Cmd {
	return func() tea.Msg {
		var result tea.Msg
		if cmd != nil {
			result = cmd()
		}
		if batch, ok := result.(tea.BatchMsg); ok {
			// openThreadPanel batches an optional cache command followed by
			// the authoritative fetch. Keep them concurrent, but tag their
			// results so a late cache response cannot undo the fetch.
			wrapped := make([]tea.Cmd, len(batch))
			for i, child := range batch {
				wrapped[i] = p.wrapThreadLoad(child, authoritative && i == len(batch)-1, len(batch))
			}
			return tea.Batch(wrapped...)()
		}
		return permalinkThreadResultMsg{nav: p, result: result, authoritative: authoritative, loads: loads}
	}
}

var reduceLinks reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	m, ok := msg.(OpenLinkMsg)
	if !ok {
		return nil, false
	}
	return a.routeLink(m.URL), true
}

// routeLink decides between in-app navigation and the browser.
func (a *App) routeLink(rawURL string) tea.Cmd {
	pl, ok := slackurl.Parse(rawURL)
	if !ok {
		return a.browserOpener(rawURL)
	}
	domain := a.activeWorkspaceDomain()
	if domain == "" || pl.Subdomain != domain {
		return a.browserOpener(rawURL)
	}
	name, chType, found := a.channels.Lookup(pl.ChannelID)
	if !found {
		return a.browserOpener(rawURL)
	}
	a.pendingLinkNav = &pendingLinkNav{
		channelID: string(pl.ChannelID),
		messageTS: string(pl.MessageTS),
		threadTS:  string(pl.ThreadTS),
		teamID:    a.activeTeamID,
	}
	if string(pl.ChannelID) == a.activeChannelID {
		// Already viewing the channel; the loaded buffer is as good
		// as it gets, so complete authoritatively right now.
		return a.completePendingLinkNav(a.activeChannelID, true)
	}
	id, n, t := string(pl.ChannelID), name, chType
	return func() tea.Msg {
		return ChannelSelectedMsg{ID: id, Name: n, Type: t}
	}
}

// completePendingLinkNav finishes (or drops) the pending permalink
// navigation for channelID. authoritative=true means "no more message
// data is coming for this channel" — if the target ts still isn't in
// the buffer, dispatch ChannelService.FetchAround to load a history
// window centered on the target instead of waiting.
//
// Called from: routeLink (already-active channel, authoritative),
// reduceChannels' ChannelSelectedMsg arm (cache render, best-effort),
// and reduceChannels' MessagesLoadedMsg arm (authoritative).
func (a *App) completePendingLinkNav(channelID string, authoritative bool) tea.Cmd {
	p := a.pendingLinkNav
	if p == nil {
		return nil
	}
	if p.channelID != channelID || (p.teamID != "" && p.teamID != a.activeTeamID) {
		// The user navigated somewhere unrelated before the link
		// target finished loading; the pending nav is stale.
		a.pendingLinkNav = nil
		return nil
	}
	if p.threadOpened {
		// A channel history refresh must not reopen/reset the thread while
		// its own cache/fetch commands are still completing.
		return nil
	}
	a.view = ViewChannels
	a.sidebar.SetThreadsActive(false)
	a.lastOpenedChannelID = ""
	a.lastOpenedThreadTS = ""
	a.focusedPanel = PanelMessages
	if p.threadTS != "" {
		p.teamID = a.activeTeamID
		p.threadOpened = true
		return p.wrapThreadLoad(a.openThreadForPermalink(p.channelID, p.threadTS), true, 1)
	}
	if a.messagepane.SelectByTS(p.messageTS) {
		a.pendingLinkNav = nil
		return nil
	}
	if authoritative {
		a.pendingLinkNav = nil
		channels := a.channels
		chID, ts := p.channelID, p.messageTS
		return func() tea.Msg {
			return channels.FetchAround(ids.ChannelID(chID), ids.MessageTS(ts))
		}
	}
	return nil
}

// openThreadForPermalink opens the thread panel for a permalink that
// carried thread_ts. Unlike openThreadForSelectedMessage it does not
// require the parent message to be in the pane buffer (mirrors
// openSelectedThreadCmd, which builds the parent from a summary):
// the parent row is taken from the loaded buffer or the thread cache
// when available, else a minimal stub that the ThreadRepliesLoadedMsg
// handler backfills from cache once the fetch lands.
func (a *App) openThreadForPermalink(channelID, threadTS string) tea.Cmd {
	parent := messages.MessageItem{TS: threadTS, ThreadTS: threadTS}
	if channelID == a.activeChannelID {
		for _, m := range a.messagepane.Messages() {
			if m.TS == threadTS {
				parent = m
				break
			}
		}
	}
	if parent.Text == "" {
		if cached := a.threads.CacheRead(ids.ChannelID(channelID), ids.ThreadTS(threadTS)); len(cached) > 0 {
			parent = cached[0]
		}
	}

	return a.openThreadPanel(parent, channelID, threadTS)
}
