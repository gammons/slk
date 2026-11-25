package main

import (
	"context"

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
	// Before the conversation is published, so the UI hides the row it is
	// about to receive. The set is global, so this goes out whether or not
	// the workspace is the active one.
	if recordDeactivatedPeer(ch, h.db) && h.program != nil {
		announceUserDeleted(h.program.Send, ch.User, true, true)
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
	h.wsCtx.MarkConversation(item.ID)
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
			if item.DMUserID == "" {
				item.DMUserID = existing.DMUserID
			}
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

// repairDMPeer imports cached bot classification and repairs both DM snapshots.
// Only the serialized event owner may call it, including before a newly
// recovered conversation is inserted so its initial type is correct.
func (h *rtmEventHandler) repairDMPeer(userID string) {
	if h.db != nil {
		if u, err := h.db.GetUser(userID); err == nil && u.IsBot {
			h.wsCtx.MarkBotUser(userID)
		}
	}
	h.refreshDMPeerFromCache(userID)
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
			Presence: updated.Presence, Joined: true, DMUserID: userID,
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

// rememberConversation mirrors the latest name/type into notifier lookup maps
// and the mention-name store. Only the serialized event owner may call it.
func (h *rtmEventHandler) rememberConversation(item sidebar.ChannelItem) {
	if h.wsCtx != nil && (item.Type == "channel" || item.Type == "private") && item.Name != "" {
		h.wsCtx.ChannelResolver.nameStore().Set(item.ID, item.Name)
	}
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

// orgFallbackConversationResolver wraps a grid member workspace's
// conversation lookup with the same-user org-level session's. A grid
// conversation can belong to the org workspace while its message
// frames fan out to every member session (a DM with an org-workspace
// peer is the case that motivated this): the member token cannot
// resolve the org's conversation, so without the fallback the sidebar
// row never appears in the workspace the user is looking at, even
// though the messages arrive and cache there. The org session answers
// as this user's own conversation view. The org resolver receives the
// own lookup's error and is expected to return it when it cannot do
// better (no org session, or the org lookup also failed): the own
// error is what the caller's retry backoff classifies, and swapping
// it for a wrapper error would silently disable the backoff.
// org nil keeps the workspace's own resolver alone.
func orgFallbackConversationResolver(own func(ctx context.Context, channelID string) (*slack.Channel, error), org func(ctx context.Context, channelID string, ownErr error) (*slack.Channel, error)) func(ctx context.Context, channelID string) (*slack.Channel, error) {
	if org == nil {
		return own
	}
	return func(ctx context.Context, channelID string) (*slack.Channel, error) {
		ch, err := own(ctx, channelID)
		if err == nil {
			return ch, nil
		}
		return org(ctx, channelID, err)
	}
}

// conversationResolverFor builds the rtmEventHandler's
// resolveConversation: the workspace's own conversations.info, wrapped
// with the same-user org session's when this workspace is a grid
// member (see orgFallbackConversationResolver). The org resolver
// resolves at CALL time, not build time: workspaces connect in
// parallel, and a member whose handler is built before the org session
// registers in the router would otherwise keep its own lookup forever
// - the org session connecting a moment later would never be used.
func conversationResolverFor(router *workspaceRouter, wctx *WorkspaceContext) func(ctx context.Context, channelID string) (*slack.Channel, error) {
	return orgFallbackConversationResolver(wctx.Client.GetConversationInfo, func(ctx context.Context, channelID string, ownErr error) (*slack.Channel, error) {
		client := sameUserOrgClient(router, wctx)
		if client == nil {
			// Keep the member's own error: it carries the refusal the
			// caller's backoff classifies.
			return nil, ownErr
		}
		ch, err := client.GetConversationInfo(ctx, channelID)
		if err != nil {
			// The org session could not resolve it either; the member's
			// own error keeps the caller's backoff semantics.
			return nil, ownErr
		}
		return ch, nil
	})
}
