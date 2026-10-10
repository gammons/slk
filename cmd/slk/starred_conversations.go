package main

import (
	"context"
	"log"

	"github.com/gammons/slk/internal/slack/boot"
	"github.com/slack-go/slack"
)

// missingStarredConversations returns eligible starred conversations absent
// from knownIDs, in stars.list order. Boot IM metadata avoids a network request
// and admits closed 1:1 DMs; resolve is the read-only conversations.info path.
// Errors are best-effort and cancellation stops further work, retaining any
// additions already resolved. Nothing is opened remotely or fabricated.
// Missing MPIMs are deliberately excluded: stars must not undo the ordinary
// closed-group-DM filter. Regular channels still require joined membership.
func missingStarredConversations(ctx context.Context, knownIDs, starredIDs []string, ims []boot.IM, resolve func(context.Context, string) (*slack.Channel, error)) []slack.Channel {
	seen := make(map[string]bool, len(knownIDs)+len(starredIDs))
	for _, id := range knownIDs {
		seen[id] = true
	}
	bootIMs := make(map[string]boot.IM, len(ims))
	for _, im := range ims {
		bootIMs[im.ID] = im
	}
	var added []slack.Channel
	for _, id := range starredIDs {
		if ctx.Err() != nil {
			break
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true // including failures: at most one lookup per ID/pass
		var ch *slack.Channel
		if im, ok := bootIMs[id]; ok {
			if im.IsArchived {
				continue
			}
			if im.UserID != "" {
				mapped := bootIMConversation(im)
				ch = &mapped
			}
		}
		if ch == nil && resolve != nil {
			var err error
			ch, err = resolve(ctx, id)
			if err != nil {
				log.Printf("starred conversations: looking up %s: %v (skipping missing sidebar row)", id, err)
				continue
			}
		}
		if ctx.Err() != nil {
			break
		}
		if ch == nil || ch.ID != id || ch.IsArchived || ch.IsMpIM {
			continue
		}
		if ch.IsIM {
			if ch.User == "" {
				continue
			}
		} else if !ch.IsMember || ch.Name == "" || (!ch.IsChannel && !ch.IsGroup && !ch.IsPrivate) {
			continue
		}
		added = append(added, *ch)
	}
	return added
}

// reconcileStarredConversations runs on the serialized WS event loop after
// section rebootstrap. Row hydration and peer resolution are independent:
// an inserted row is not evidence that its name/classification resolved. Its
// DMUserID retains the retry target for later passes, including cache fills by
// the background sweep. All row/finder mutations stay on this event owner.
// ctx bounds metadata, one edge profile batch, and per-user fallback together;
// cache repairs still run when that HTTP budget is exhausted.
func (h *rtmEventHandler) reconcileStarredConversations(ctx context.Context) {
	if h.wsCtx == nil || h.wsCtx.SectionStore == nil {
		return
	}
	starredIDs := h.wsCtx.SectionStore.StarredConversationIDs()
	starred := make(map[string]bool, len(starredIDs))
	for _, id := range starredIDs {
		starred[id] = true
	}
	known := make([]string, 0, len(h.wsCtx.Channels))
	var peers []string
	seenPeers := make(map[string]bool)
	addPeer := func(id string) {
		if id != "" && !seenPeers[id] {
			seenPeers[id] = true
			peers = append(peers, id)
		}
	}
	for _, item := range h.wsCtx.Channels {
		known = append(known, item.ID)
		if starred[item.ID] {
			addPeer(item.DMUserID)
		}
	}
	added := missingStarredConversations(ctx, known, starredIDs, nil, h.resolveConversation)
	for _, ch := range added {
		if ch.IsIM {
			addPeer(ch.User)
		}
	}

	peerResolved := func(id string) bool {
		if _, named := lookupUserCached(id, h.wsCtx.UserNames, h.db); !named {
			return false
		}
		if h.db != nil {
			_, err := h.db.GetUser(id)
			return err == nil || h.wsCtx.IsBotUser(id) // cached human is classified too
		}
		return true
	}
	var unresolved []string
	for _, id := range peers {
		if !peerResolved(id) {
			unresolved = append(unresolved, id)
		}
	}
	if h.wsCtx.UserResolver != nil && len(unresolved) > 0 && ctx.Err() == nil {
		// Reuse the cold-boot batching path rather than serially issuing a
		// profile request per starred peer on the event owner.
		h.wsCtx.UserResolver.ResolveNowContext(ctx, unresolved)
		for _, id := range unresolved {
			if ctx.Err() != nil {
				break
			}
			if !peerResolved(id) {
				h.wsCtx.UserResolver.resolveOneContext(ctx, id)
			}
		}
	}
	// A quiet peer cannot rely on message/membership lookups to retry. Use
	// the background resolver's own context, including when this pass's
	// shared budget is exhausted; success wakes the event owner for repair.
	if h.wsCtx.UserResolver != nil {
		for _, id := range unresolved {
			if !peerResolved(id) {
				h.wsCtx.UserResolver.Request(id)
			}
		}
	}
	for _, id := range peers {
		h.repairDMPeer(id)
	}
	for _, ch := range added {
		if item, finder, ok := h.addConversation(ch); ok {
			h.publishConversation(item, finder)
		}
	}
}
