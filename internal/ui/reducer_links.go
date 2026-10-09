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
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/slackurl"
	"github.com/gammons/slk/internal/ui/messages"
)

// pendingLinkNav is the not-yet-completed tail of an in-app permalink
// navigation. Thread targets survive until both cache and fetch complete.
// The embedded Location is the target (ThreadTS non-empty: select within
// the thread panel); the rest is load-tracking state.
type pendingLinkNav struct {
	Location
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
	cmd, ok := a.applyLocation(Location{
		TeamID:    ids.TeamID(a.activeTeamID),
		ChannelID: pl.ChannelID,
		MessageTS: pl.MessageTS,
		ThreadTS:  pl.ThreadTS,
	}, false)
	if ok {
		return cmd
	}
	return a.browserOpener(rawURL)
}

// applyLocation is the single applier every in-app jump flows through
// — a permalink, a mark, or a history walk. It records loc as the
// pending navigation and dispatches the channel switch, so
// completePendingLinkNav can finish once the target channel's messages
// land (or immediately when the channel is already active). ok=false
// means loc's channel could not be resolved and nothing was started;
// while an upload is in flight nothing is started either, and the
// returned cmd is the "Upload in progress" toast with ok=true;
// callers that need a fallback (routeLink's browser opener, a mark-jump
// toast) branch on ok. A nil cmd with ok=true is a completed navigation
// (e.g. SelectByTS selected the message in the already-active channel),
// not a failure.
//
// fromHistory marks the synthesized ChannelSelectedMsg so the reducer
// does not record the walk as a new visit — history walks must not
// grow the stack. It also forces the ChannelSelectedMsg path even when
// the target channel is already active: a walk is a navigation between
// recorded positions, not an in-place jump, so it always goes through
// the channel-switch pipeline (a same-channel walk is reachable in
// production when stale entries between two same-channel entries are
// skipped and dropped). Direct jumps (permalinks, marks) keep the
// in-place completion so re-selecting the current channel does not
// reload it.
func (a *App) applyLocation(loc Location, fromHistory bool) (tea.Cmd, bool) {
	if a.compose.Uploading() || a.threadCompose.Uploading() {
		return a.uploadToastCmd("Upload in progress", 2*time.Second), true
	}
	name, chType, found := a.channels.Lookup(loc.ChannelID)
	if !found {
		return nil, false
	}
	a.pendingLinkNav = &pendingLinkNav{Location: loc}
	if !fromHistory && string(loc.ChannelID) == a.activeChannelID {
		// Already viewing the channel; the loaded buffer is as good
		// as it gets, so complete authoritatively right now.
		//
		// This path skips ChannelSelectedMsg, and with it the view and
		// focus reset that arm performs (reducer_channels.go). Without
		// doing it here, a jump taken from the Threads list or with the
		// thread panel focused moves the selection in a pane the user
		// is not looking at, and reads as "nothing happened". A
		// thread-bearing location re-focuses the thread panel below,
		// via openThreadForPermalink.
		a.view = ViewChannels
		if a.threadVisible {
			// ChannelSelectedMsg closes the thread panel on every
			// channel switch, and the in-place path has to as well —
			// otherwise a channel-level jump lands the selection in
			// the messages pane while the thread the user was reading
			// stays open beside it. A thread-bearing location reopens
			// the correct thread below, via openThreadForPermalink.
			a.CloseThread()
		}
		a.focusedPanel = PanelMessages
		return a.completePendingLinkNav(a.activeChannelID, true), true
	}
	id, n, t, team := string(loc.ChannelID), name, chType, a.activeTeamID
	return func() tea.Msg {
		return ChannelSelectedMsg{ID: id, Name: n, Type: t, FromHistory: fromHistory, TeamID: team}
	}, true
}

// completePendingLinkNav finishes (or drops) the pending permalink
// navigation for channelID. authoritative=true means "no more message
// data is coming for this channel" — if the target ts still isn't in
// the buffer, dispatch ChannelService.FetchAround to load a history
// window centered on the target instead of waiting.
//
// Called from: applyLocation (already-active channel, authoritative),
// reduceChannels' ChannelSelectedMsg arm (cache render, best-effort),
// and reduceChannels' MessagesLoadedMsg arm (authoritative).
func (a *App) completePendingLinkNav(channelID string, authoritative bool) tea.Cmd {
	p := a.pendingLinkNav
	if p == nil {
		return nil
	}
	if string(p.ChannelID) != channelID || (p.TeamID != "" && string(p.TeamID) != a.activeTeamID) {
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
	if p.MessageTS == "" && p.ThreadTS == "" {
		// Channel-only location: the channel is already (being)
		// opened and there is nothing to select or open on top of it.
		// Permalinks and search hits always carry a message ts, so
		// only history walks produce this — and FetchAround with an
		// empty ts would be a bogus request.
		a.pendingLinkNav = nil
		return nil
	}
	a.view = ViewChannels
	a.sidebar.SetThreadsActive(false)
	a.lastOpenedChannelID = ""
	a.lastOpenedThreadTS = ""
	a.focusedPanel = PanelMessages
	if p.ThreadTS != "" {
		p.TeamID = ids.TeamID(a.activeTeamID)
		p.threadOpened = true
		return p.wrapThreadLoad(a.openThreadForPermalink(string(p.ChannelID), string(p.ThreadTS)), true, 1)
	}
	if a.messagepane.SelectByTS(string(p.MessageTS)) {
		// Only forget the target once this is the freshest data we
		// will get. On a best-effort pass (the cache render, with a
		// network fetch still in flight) the selection is real but
		// temporary: the MessagesLoadedMsg arm calls SetMessages,
		// which resets the selection to the newest message. Clearing
		// here left nothing to re-apply, so the first jump into a
		// channel visibly landed on the target and then snapped to the
		// bottom a moment later. Keeping the pending makes the
		// authoritative pass re-select; it is idempotent.
		if authoritative {
			a.pendingLinkNav = nil
		}
		return nil
	}
	if authoritative {
		a.pendingLinkNav = nil
		channels := a.channels
		chID, ts := p.ChannelID, p.MessageTS
		return func() tea.Msg {
			return channels.FetchAround(chID, ts)
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
