package main

import (
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui"
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
func (h *rtmEventHandler) addConversation(ch slack.Channel) (sidebar.ChannelItem, core.ChannelFinderItem, bool) {
	if h.wsCtx == nil {
		return sidebar.ChannelItem{}, core.ChannelFinderItem{}, false
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
	}
	// Refresh rather than merely dedupe: a duplicate open may carry a
	// resolved name, presence or app classification that the finder lacks.
	finderItem, _ = h.upsertFinderItem(finderItem)

	h.rememberConversation(item)
	return item, finderItem, true
}

// upsertFinderItem refreshes one workspace finder entry by conversation ID.
// It returns the stored entry and whether it changed. A deduplicated row keeps
// its live LastVisited value; only a new row uses the startup visit snapshot.
// Callers must be on the serialized event owner, like addConversation.
func (h *rtmEventHandler) upsertFinderItem(item core.ChannelFinderItem) (core.ChannelFinderItem, bool) {
	for i, existing := range h.wsCtx.FinderItems {
		if existing.ID == item.ID {
			item.LastVisited = existing.LastVisited
			if item == existing {
				return item, false
			}
			h.wsCtx.FinderItems[i] = item
			return item, true
		}
	}
	item.LastVisited = h.wsCtx.LastVisitedByChannel[item.ID]
	h.wsCtx.FinderItems = append(h.wsCtx.FinderItems, item)
	return item, true
}

// refreshDMPeerFromCache repairs existing DM rows after a deferred profile
// resolves. It preserves section/order/visit metadata, updates both workspace
// snapshots independently, and publishes through the normal active-workspace
// insertion port (an ID-based upsert). Missing finder entries are recovered only
// for existing DM conversations; no conversation or network request is created.
// Like addConversation, only the serialized event owner may call this method.
func (h *rtmEventHandler) refreshDMPeerFromCache(userID string) {
	name, ok := resolveUserCached(userID, h.wsCtx.UserNames, h.db)
	if !ok {
		return
	}
	for i, item := range h.wsCtx.Channels {
		if item.DMUserID != userID || (item.Type != "dm" && item.Type != "app") {
			continue
		}
		updated := item
		updated.Name = name
		if h.wsCtx.IsBotUser(userID) {
			updated.Type = "app"
		}
		finder := core.ChannelFinderItem{
			ID: item.ID, Name: updated.Name, Type: updated.Type,
			Presence: updated.Presence, Joined: true,
		}
		seedDMFromCache(h.db, userID, &updated, &finder)
		finder, finderChanged := h.upsertFinderItem(finder)
		// These are independent snapshots. An already-correct sidebar
		// must not suppress finder-only repairs (including a missing row),
		// nor status/presence changes after a successful profile retry.
		if updated == item && !finderChanged {
			continue
		}
		h.wsCtx.Channels[i] = updated
		if h.db != nil && updated.Type != item.Type {
			if cached, err := h.db.GetChannel(item.ID); err == nil {
				cached.Type = updated.Type
				_ = h.db.UpsertChannel(cached)
			}
		}
		h.rememberConversation(updated)
		h.publishConversation(updated, finder)
	}
}

// rememberConversation mirrors the latest name/type into notifier lookup maps.
// It shares addConversation's serialized-event-owner requirement.
func (h *rtmEventHandler) rememberConversation(item sidebar.ChannelItem) {
	if h.channelNames != nil {
		h.channelNames[item.ID] = item.Name
	}
	if h.channelTypes != nil {
		h.channelTypes[item.ID] = item.Type
	}
}

func (h *rtmEventHandler) publishConversation(item sidebar.ChannelItem, finderItem core.ChannelFinderItem) {
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
