// internal/ui/location.go
//
// Location is the shared address type for navigable positions in a
// workspace: the currency of the navigation history, permalink jumps,
// and vim-style marks. It is deliberately the same shape as
// slackurl.Permalink (internal/slackurl/slackurl.go) with the
// workspace subdomain replaced by the team ID, because the team ID is
// what workspaceContext and every cache table key on.
package ui

import "github.com/gammons/slk/internal/ids"

// Location identifies a workspace, a channel, and optionally a message
// within that channel and a thread the message belongs to. A location
// that names only a channel remains valid and means "the channel, at
// its default position".
type Location struct {
	// TeamID is the workspace the location belongs to. Every recorded
	// location carries it; jump paths assert it matches the active
	// workspace and refuse otherwise.
	TeamID ids.TeamID
	// ChannelID is the channel the location is in.
	ChannelID ids.ChannelID
	// MessageTS is the target message timestamp; empty when the
	// location names only a channel.
	MessageTS ids.MessageTS
	// ThreadTS is the thread parent ts when the location names a
	// reply inside a thread; empty for channel-level locations. A
	// non-empty ThreadTS means "open the thread panel instead of
	// selecting".
	ThreadTS ids.ThreadTS
}

// currentLocation snapshots the message the user is looking at right
// now: the thread panel's selected reply when the thread panel is
// focused; in the Threads view, the parent of the selected thread; and
// otherwise the messages pane's selection. In the Activity view nothing
// is named: the messages pane is hidden behind the list there. ok=false
// when there is nothing to name.
func (a *App) currentLocation() (Location, bool) {
	if a.focusedPanel != PanelThread {
		switch a.view {
		case ViewThreads:
			// The messages pane behind the list still holds the last
			// channel, so reading it would name a message the user
			// cannot see.
			s, ok := a.threadsView.SelectedSummary()
			if !ok {
				return Location{}, false
			}
			return Location{
				TeamID:    ids.TeamID(a.activeTeamID),
				ChannelID: ids.ChannelID(s.ChannelID),
				MessageTS: ids.MessageTS(s.ThreadTS),
				ThreadTS:  ids.ThreadTS(s.ThreadTS),
			}, true
		case ViewActivity:
			return Location{}, false
		}
		return a.channelPaneLocation()
	}
	reply := a.threadPanel.SelectedReply()
	if reply == nil {
		return Location{}, false
	}
	// The thread's own channel, NOT activeChannelID. In the Threads
	// view the panel shows a thread from any channel while
	// activeChannelID still names whichever channel was last
	// opened, so recording the active channel here stored a thread
	// ts against a channel that has no such thread — the jump then
	// opened an empty panel reading "0 replies".
	channelID := a.threadPanel.ChannelID()
	if channelID == "" {
		channelID = a.activeChannelID
	}
	return Location{
		TeamID:    ids.TeamID(a.activeTeamID),
		ChannelID: ids.ChannelID(channelID),
		MessageTS: ids.MessageTS(reply.TS),
		ThreadTS:  ids.ThreadTS(a.threadPanel.ThreadTS()),
	}, true
}

// channelPaneLocation names the messages pane's selection in the
// active channel.
func (a *App) channelPaneLocation() (Location, bool) {
	msg, ok := a.messagepane.SelectedMessage()
	if !ok {
		return Location{}, false
	}
	return Location{
		TeamID:    ids.TeamID(a.activeTeamID),
		ChannelID: ids.ChannelID(a.activeChannelID),
		MessageTS: ids.MessageTS(msg.TS),
	}, true
}

// currentPosition returns the user's current position as a Location,
// falling back to a channel-only location when no message is selected.
// The fallback carries just the workspace and channel so a
// position-less departure still degrades cleanly instead of being
// dropped. It is what history records for the active channel, so
// outside the thread panel it reads the messages pane even from the
// Threads and Activity views: that pane still holds the active
// channel's position, which is where walking back should return.
func (a *App) currentPosition() Location {
	var (
		loc Location
		ok  bool
	)
	if a.focusedPanel == PanelThread {
		loc, ok = a.currentLocation()
	} else {
		loc, ok = a.channelPaneLocation()
	}
	if ok {
		return loc
	}
	return Location{
		TeamID:    ids.TeamID(a.activeTeamID),
		ChannelID: ids.ChannelID(a.activeChannelID),
	}
}
