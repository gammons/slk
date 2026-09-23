package main

import (
	"github.com/gammons/slk/internal/service"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// sectionsProviderAdapter adapts *service.SectionStore to the
// sidebar.SectionsProvider interface. Translates SidebarSection into
// the sidebar's view-only SectionMeta shape. The store may be nil;
// the adapter reports Ready()==false in that case so the sidebar
// stays in config-glob mode.
type sectionsProviderAdapter struct {
	store *service.SectionStore
}

func (a sectionsProviderAdapter) Ready() bool {
	return a.store != nil && a.store.Ready()
}

func (a sectionsProviderAdapter) OrderedSlackSections() []sidebar.SectionMeta {
	if a.store == nil {
		return nil
	}
	secs := a.store.OrderedSections()
	out := make([]sidebar.SectionMeta, 0, len(secs))
	for _, s := range secs {
		out = append(out, sidebar.SectionMeta{
			ID:    s.ID,
			Name:  s.Name,
			Emoji: s.Emoji,
			Type:  s.Type,
		})
	}
	return out
}

// refreshSectionsForActive re-syncs every wctx.Channels item's Section
// field with the current SectionStore state, then (if this workspace
// is active) posts a SectionsRefreshedMsg so the App rebuckets the
// sidebar. Inactive workspaces still get their wctx.Channels mutated
// in place; the user sees the refresh on next workspace switch.
//
// Called from the four channel-section WS event handlers after they've
// already applied their delta to the store.
func (h *rtmEventHandler) refreshSectionsForActive() {
	if h.wsCtx == nil || h.wsCtx.SectionStore == nil {
		return
	}
	store := h.wsCtx.SectionStore
	if !store.Ready() {
		return
	}
	// Update Section field on every channel in the workspace context
	// based on current store state. Channels not claimed by any
	// section have Section reset to "" — letting the sidebar's Slack
	// mode bucket them via type-default fallback (Task 8) or the
	// config-glob path if Slack mode isn't active.
	for i := range h.wsCtx.Channels {
		item := &h.wsCtx.Channels[i]
		if id, ok := store.SectionForChannel(item.ID); ok {
			item.Section = id
		} else {
			item.Section = ""
		}
		// SectionOrder is unused in Slack mode (linked-list order
		// comes from the provider); reset to 0 for consistency.
		item.SectionOrder = 0
	}
	if h.program == nil {
		return
	}
	if h.isActive != nil && !h.isActive() {
		return
	}
	// Send a copy so the App can mutate without racing the workspace's
	// mutator path.
	channelsCopy := make([]sidebar.ChannelItem, len(h.wsCtx.Channels))
	copy(channelsCopy, h.wsCtx.Channels)
	h.program.Send(ui.SectionsRefreshedMsg{
		TeamID:   h.workspaceID,
		Channels: channelsCopy,
	})
}

// OnChannelSectionUpserted handles section create/rename/reorder/emoji-change.
// The store applies last-write-wins; the sidebar refresh is a no-op for
// channels (no membership change) but invalidates the cache so renames
// re-render section header labels.
func (h *rtmEventHandler) OnChannelSectionUpserted(ev slackclient.ChannelSectionUpserted) {
	if h.wsCtx == nil || h.wsCtx.SectionStore == nil {
		return
	}
	h.wsCtx.SectionStore.ApplyUpsert(ev)
	h.refreshSectionsForActive()
}

// OnChannelSectionDeleted handles section delete. Channels formerly in
// the section have their channel→section mapping dropped by the store;
// refreshSectionsForActive then resets Section="" on those items and
// the sidebar rebuckets them into the type-default bucket.
func (h *rtmEventHandler) OnChannelSectionDeleted(sectionID string) {
	if h.wsCtx == nil || h.wsCtx.SectionStore == nil {
		return
	}
	h.wsCtx.SectionStore.ApplyDelete(sectionID)
	h.refreshSectionsForActive()
}

// OnChannelSectionChannelsUpserted handles channels added (or moved
// between sections). The store overwrites prior section membership;
// refreshSectionsForActive picks up the new IDs.
func (h *rtmEventHandler) OnChannelSectionChannelsUpserted(sectionID string, channelIDs []string) {
	if h.wsCtx == nil || h.wsCtx.SectionStore == nil {
		return
	}
	h.wsCtx.SectionStore.ApplyChannelsAdded(sectionID, channelIDs)
	h.refreshSectionsForActive()
}

// OnChannelSectionChannelsRemoved handles channels removed from a section.
// The store drops them from channelToSection; refreshSectionsForActive
// resets their Section="" and the sidebar rebuckets via type-default.
func (h *rtmEventHandler) OnChannelSectionChannelsRemoved(sectionID string, channelIDs []string) {
	if h.wsCtx == nil || h.wsCtx.SectionStore == nil {
		return
	}
	h.wsCtx.SectionStore.ApplyChannelsRemoved(sectionID, channelIDs)
	h.refreshSectionsForActive()
}
