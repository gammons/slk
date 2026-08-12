// internal/ui/mode_channel_finder.go
//
// Channel-finder mode key handler (Phase 5h).
//
// Forwards normalised keys to the channel-finder overlay. On a
// result:
//   - Synthetic "threads" destination -> activate the threads
//     view (ThreadsViewActivatedMsg).
//   - Already-joined channel -> select it (ChannelSelectedMsg).
//   - Not yet joined -> fire a Join via the channels service;
//     ChannelJoinedMsg (reducer_channels) folds it into the
//     sidebar and dispatches the ChannelSelectedMsg.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
)

func handleChannelFinderMode(a *App, msg tea.KeyMsg) tea.Cmd {
	// Map tea.KeyMsg to string for the finder.
	before := a.channelFinder.Query()
	result := a.channelFinder.HandleKey(normalizeFinderKey(msg))
	if result != nil {
		a.channelFinder.Close()
		a.SetMode(ModeNormal)
		// Synthetic destinations (e.g. Threads view) live alongside
		// channels in the finder but route to a view activation
		// rather than a channel switch.
		if result.Type == "threads" {
			return func() tea.Msg { return ThreadsViewActivatedMsg{} }
		}
		// Already-joined: switch immediately. Not joined: kick off
		// a join command; ChannelJoinedMsg will fold the channel
		// into the sidebar and switch to it.
		if result.Joined {
			a.sidebar.SelectByID(result.ID)
			return func() tea.Msg {
				return ChannelSelectedMsg{ID: result.ID, Name: result.Name, Type: result.Type}
			}
		}
		channels := a.channels
		id, name := ids.ChannelID(result.ID), result.Name
		return func() tea.Msg {
			return channels.Join(id, name)
		}
	}

	// Check if finder closed itself (Esc).
	if !a.channelFinder.IsVisible() {
		a.SetMode(ModeNormal)
		return nil
	}

	// The local filter has already run inside HandleKey, so the list
	// on screen is up to date before anything touches the network.
	// Only the server query is deferred.
	if after := a.channelFinder.Query(); after != before {
		return a.scheduleChannelSearch(after)
	}
	return nil
}
