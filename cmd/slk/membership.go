package main

import (
	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// OnPrefChange handles user-pref mutations from the WebSocket. Currently
// the only pref slk reacts to is muted_channels: the MuteStore is
// updated and (when the set actually changed) every wctx.Channels item's
// IsMuted flag is recomputed and the active sidebar is asked to
// re-render. Other prefs are ignored — add a case here when slk grows
// support for them.
func (h *rtmEventHandler) OnPrefChange(name, value string) {
	debuglog.WS("pref_change received: name=%q value-len=%d", name, len(value))
	// Both names are routes to mute state. all_notifications_prefs is
	// the live per-channel notification blob (current Slack); the flat
	// muted_channels pref is legacy back-compat.
	if name != "muted_channels" && name != "all_notifications_prefs" {
		return
	}
	if h.wsCtx == nil || h.wsCtx.MuteStore == nil {
		return
	}
	changed := h.wsCtx.MuteStore.ApplyPrefChange(name, value)
	debuglog.WS("pref_change %s for %s: changed=%v muted=%v", name, h.wsCtx.TeamName, changed, h.wsCtx.MuteStore.MutedChannels())
	if !changed {
		return
	}
	h.refreshMutedForActive()
}

func (h *rtmEventHandler) OnMemberJoined(channelID, userID string) {
	if h.wsCtx == nil || h.wsCtx.Membership == nil {
		return
	}
	h.wsCtx.Membership.ApplyJoin(channelID, userID)
}

func (h *rtmEventHandler) OnMemberLeft(channelID, userID string) {
	if h.wsCtx == nil || h.wsCtx.Membership == nil {
		return
	}
	h.wsCtx.Membership.ApplyLeave(channelID, userID)
}

// refreshMutedForActive walks wctx.Channels, refreshes each item's
// IsMuted flag from the current MuteStore, and posts the message
// muteRefreshMsg chooses so the UI re-derives whatever it showed from
// those flags. Mirrors refreshSectionsForActive but for the mute
// dimension.
func (h *rtmEventHandler) refreshMutedForActive() {
	if h.wsCtx == nil || h.wsCtx.MuteStore == nil {
		return
	}
	store := h.wsCtx.MuteStore
	for i := range h.wsCtx.Channels {
		chID := h.wsCtx.Channels[i].ID
		before := h.wsCtx.Channels[i].IsMuted
		after := store.IsMuted(chID)
		if before != after {
			debuglog.Cache("refreshMutedForActive: channel=%s name=%q muted_before=%v muted_after=%v",
				chID, h.wsCtx.Channels[i].Name, before, after)
		}
		h.wsCtx.Channels[i].IsMuted = after
	}
	if h.program == nil {
		return
	}
	active := h.isActive == nil || h.isActive()
	h.program.Send(muteRefreshMsg(active, h.workspaceID, h.wsCtx.Channels))
}

// muteRefreshMsg is the message refreshMutedForActive posts after the
// IsMuted flags change, chosen by whether the workspace is the active
// one.
//
// Active: SectionsRefreshedMsg with a copy of the rebuilt list -- the
// same "channel-list-attributes-changed" signal refreshSectionsForActive
// uses -- so the App swaps the sidebar's items.
//
// Inactive: ReadStateChangedMsg. There is no sidebar on screen for
// this workspace, but its rail dot, the title's "+N" and
// $SLK_OTHER_UNREAD are all derived from these flags by
// railUnreadWorkspaces, and nothing else re-reads them until the next
// read-state event. ReadStateChangedMsg is what notifyReadStateChanged
// already answers to, and the App ignores its fields, so no new
// message type is needed.
//
// Pure so the choice is testable without a *tea.Program.
func muteRefreshMsg(active bool, teamID string, channels []sidebar.ChannelItem) tea.Msg {
	if !active {
		return ui.ReadStateChangedMsg{WorkspaceID: teamID}
	}
	channelsCopy := make([]sidebar.ChannelItem, len(channels))
	copy(channelsCopy, channels)
	return ui.SectionsRefreshedMsg{TeamID: teamID, Channels: channelsCopy}
}
