package main

// Conversation stars, engine side. The /star and /unstar built-ins
// make the call and move the sidebar row optimistically; the
// star_added / star_removed WebSocket echo (Slack sends it to every
// client of the user, including the one that made the change) is what
// keeps the rest current: the section store's stars membership, the
// in-memory channel row, the cache flag, and - for the active
// workspace - the sidebar, via the same refresh the section events
// use. A star made in the web client arrives the same way, so the
// sidebar follows it live too.

import (
	"context"
	"time"

	"github.com/gammons/slk/internal/debuglog"
	slackclient "github.com/gammons/slk/internal/slack"
)

// OnStarChanged applies a star_added / star_removed echo. Runs on the
// serialized WebSocket event owner, so it may touch wsCtx.Channels.
// Only a conversation star re-sections a sidebar row; a message or
// file star belongs to Slack's saved-items list, which slk has no view
// for yet, so it is noted and ignored.
func (h *rtmEventHandler) OnStarChanged(change slackclient.StarChange, starred bool) {
	if h.wsCtx == nil {
		return
	}
	debuglog.WS("star %s for %s: item=%s channel=%s", starWord(starred), h.workspaceID, change.ItemType, change.Channel)
	switch change.ItemType {
	case "im", "channel", "group", "mpim":
	default:
		return
	}
	channelID := change.Channel
	if h.wsCtx.SectionStore != nil {
		h.wsCtx.SectionStore.SetChannelStarred(channelID, starred)
	}
	known := false
	for i := range h.wsCtx.Channels {
		if h.wsCtx.Channels[i].ID == channelID {
			h.wsCtx.Channels[i].IsStarred = starred
			known = true
			break
		}
	}
	if h.db != nil {
		if ok, err := h.db.SetChannelStarred(h.workspaceID, channelID, starred); err != nil || !ok {
			debuglog.General("star echo: caching %s/%s star=%v: matched=%v err=%v",
				h.workspaceID, channelID, starred, ok, err)
		}
	}
	if !known && starred {
		// A conversation this workspace has no row for (a closed DM,
		// starred in another client): hydrate it the way the reconnect
		// reconciliation does, so the star has something to show.
		h.recoverStarredConversation(channelID)
	}
	// The sidebar follows for the active workspace: with a section
	// store every row's Section is re-resolved off it (so the moved
	// row lands in or out of the Starred section); without one (config
	// sections, or a failed sections bootstrap) the updated rows are
	// still published, so the header glyph at least follows the star.
	if h.wsCtx.SectionStore != nil && h.wsCtx.SectionStore.Ready() {
		h.refreshSectionsForActive()
	} else {
		h.publishChannelsForActive()
	}
}

// recoverStarredConversation hydrates one conversation the workspace
// holds no row for, off the same path the reconnect reconciliation
// uses. Runs on the event owner like its caller.
func (h *rtmEventHandler) recoverStarredConversation(channelID string) {
	if h.resolveConversation == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	known := make([]string, 0, len(h.wsCtx.Channels))
	for _, item := range h.wsCtx.Channels {
		known = append(known, item.ID)
	}
	added := missingStarredConversations(ctx, known, []string{channelID}, nil, h.resolveConversation)
	for _, ch := range added {
		if item, finder, ok := h.addConversation(ch); ok {
			// The row is recovered BECAUSE it was starred; addConversation
			// builds and stores it without the flag, so set both the
			// stored row and the published copy, or the hollow glyph
			// would read as unstarred (and a click would re-star
			// instead of removing).
			item.IsStarred = true
			for i := range h.wsCtx.Channels {
				if h.wsCtx.Channels[i].ID == item.ID {
					h.wsCtx.Channels[i].IsStarred = true
					break
				}
			}
			h.publishConversation(item, finder)
		}
	}
}

// starApply is one queued star-state change: what a successful
// stars.add / stars.remove asked the event owner to apply to the
// workspace's rows. prev is the row's starred flag when the request
// was made (prevKnown false when there was no section store to read
// it from): the apply is skipped when a newer star event already
// moved the row off prev, so a delayed HTTP reply cannot overwrite
// what Slack's own later echo delivered.
type starApply struct {
	channelID string
	starred   bool
	prev      bool
	prevKnown bool
}

// queueStarApply hands a successful star change to the event owner:
// the rows it must update (wsCtx.Channels) are mutated only there, and
// the command goroutine the Web API call ran on may not touch them.
// prev is the starred flag the request was made against (prevKnown
// false when it could not be read), so a delayed reply whose outcome
// a newer star event already superseded is skipped at the drain. The
// star_added echo normally does this work; the queue covers the
// socket-down window where Slack accepted the change but cannot echo
// it back, so a workspace switch away and back does not rebuild the
// sidebar from rows that predate the star.
func (h *rtmEventHandler) queueStarApply(channelID string, starred, prev, prevKnown bool) {
	if h == nil || h.starWake == nil {
		return
	}
	h.starMu.Lock()
	h.starApplies = append(h.starApplies, starApply{
		channelID: channelID, starred: starred, prev: prev, prevKnown: prevKnown,
	})
	h.starMu.Unlock()
	select {
	case h.starWake <- struct{}{}:
	default:
	}
}

// drainStarApplies runs on the event owner (from OnPendingEvents) and
// applies the queued star changes to the workspace's rows, section
// store and cache, publishing the refreshed list for the active
// workspace. An apply whose row a newer star event already moved off
// the pre-request flag is skipped: a delayed HTTP reply must not
// overwrite what Slack's own later echo delivered.
func (h *rtmEventHandler) drainStarApplies() {
	h.starMu.Lock()
	queued := h.starApplies
	h.starApplies = nil
	h.starMu.Unlock()
	if len(queued) == 0 || h.wsCtx == nil {
		return
	}
	for _, apply := range queued {
		if apply.prevKnown {
			cur, known := h.rowStarred(apply.channelID)
			if known && cur != apply.prev {
				continue
			}
		}
		if h.wsCtx.SectionStore != nil {
			h.wsCtx.SectionStore.SetChannelStarred(apply.channelID, apply.starred)
		}
		for i := range h.wsCtx.Channels {
			if h.wsCtx.Channels[i].ID == apply.channelID {
				h.wsCtx.Channels[i].IsStarred = apply.starred
				break
			}
		}
		if h.db != nil {
			if ok, err := h.db.SetChannelStarred(h.workspaceID, apply.channelID, apply.starred); err != nil || !ok {
				debuglog.General("star apply: caching %s/%s star=%v: matched=%v err=%v",
					h.workspaceID, apply.channelID, apply.starred, ok, err)
			}
		}
	}
	if h.wsCtx.SectionStore != nil && h.wsCtx.SectionStore.Ready() {
		h.refreshSectionsForActive()
	} else {
		h.publishChannelsForActive()
	}
}

// rowStarred reads one row's starred flag, on the event owner.
func (h *rtmEventHandler) rowStarred(channelID string) (starred, known bool) {
	for i := range h.wsCtx.Channels {
		if h.wsCtx.Channels[i].ID == channelID {
			return h.wsCtx.Channels[i].IsStarred, true
		}
	}
	return false, false
}

func starWord(starred bool) string {
	if starred {
		return "added"
	}
	return "removed"
}
