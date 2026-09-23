package main

import (
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

// OnConversationOpened handles WS events that surface a new or
// previously-closed conversation: mpim_open, im_created, group_joined,
// channel_joined. Builds a sidebar.ChannelItem via the shared helper,
// persists it in WorkspaceContext (de-duped by ID, preserving live
// unread/last-read state), upserts the SQLite cache row, mirrors
// channelNames/Types maps used by the notifier, and — if the
// workspace is active — forwards a ConversationOpenedMsg to the UI
// so the live sidebar and channel finder (Ctrl+P) both update.
func (h *rtmEventHandler) OnConversationOpened(ch slack.Channel) {
	if item, finderItem, ok := h.addConversation(ch); ok {
		h.publishConversation(item, finderItem)
	}
}

// addConversation is OnConversationOpened without the UI message.
func (h *rtmEventHandler) addConversation(ch slack.Channel) (sidebar.ChannelItem, channelfinder.Item, bool) {
	if h.wsCtx == nil {
		return sidebar.ChannelItem{}, channelfinder.Item{}, false
	}

	item, finderItem := buildChannelItem(ch, h.wsCtx, h.cfg, h.workspaceID)
	if ch.IsIM {
		seedDMFromCache(h.db, ch.User, &item, &finderItem)
	}
	if h.db != nil {
		upsertChannelInDB(h.db, ch, item.Type, h.workspaceID)
	}

	// Persist in the workspace context so a workspace switch later
	// shows the new conversation. De-dupe on ID — the same event can
	// arrive twice (e.g. im_open followed by im_created on first DM).
	// No read-state preservation is needed: those fields no longer
	// live on ChannelItem; the read-state DB (per workspace) is the
	// single source of truth and is unaffected by this in-memory upsert.
	replaced := false
	for i := range h.wsCtx.Channels {
		if h.wsCtx.Channels[i].ID == item.ID {
			h.wsCtx.Channels[i] = item
			replaced = true
			break
		}
	}
	if !replaced {
		h.wsCtx.Channels = append(h.wsCtx.Channels, item)
		// FinderItems is intentionally only appended on the new-channel
		// path. On dedupe, the existing finder entry was added at
		// bootstrap (or a prior open) and carries no unread state to
		// refresh, so re-appending would double-list the channel in
		// Ctrl+P.
		finderItem.LastVisited = h.wsCtx.LastVisitedByChannel[ch.ID]
		h.wsCtx.FinderItems = append(h.wsCtx.FinderItems, finderItem)
	}

	// Mirror channelTypes / channelNames maps used by the notifier so
	// follow-up messages on this channel get notified correctly.
	if h.channelNames != nil {
		h.channelNames[ch.ID] = item.Name
	}
	if h.channelTypes != nil {
		h.channelTypes[ch.ID] = item.Type
	}
	return item, finderItem, true
}

func (h *rtmEventHandler) publishConversation(item sidebar.ChannelItem, finderItem channelfinder.Item) {
	if h.program == nil {
		return
	}
	if h.isActive != nil && !h.isActive() {
		// addConversation already updated wctx.Channels; defer the
		// UI message until the user switches into this workspace.
		return
	}
	h.program.Send(ui.ConversationOpenedMsg{
		TeamID:     h.workspaceID,
		Item:       item,
		FinderItem: finderItem,
	})
}
