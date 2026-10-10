package main

import slackclient "github.com/gammons/slk/internal/slack"

var _ slackclient.PendingEventHandler = (*rtmEventHandler)(nil)

// queueResolvedPeer runs after a profile and its name have been cached. It
// records only an ID and a wake-up, never reads or writes conversation slices.
func (r *userResolver) queueResolvedPeer(userID string) {
	if r == nil || r.resolvedWake == nil {
		return
	}
	r.resolvedPeers.Store(userID, struct{}{})
	select {
	case r.resolvedWake <- struct{}{}:
	default:
	}
}

func (h *rtmEventHandler) PendingEvents() <-chan struct{} {
	if h.wsCtx == nil || h.wsCtx.UserResolver == nil {
		return nil
	}
	return h.wsCtx.UserResolver.resolvedWake
}

// OnPendingEvents runs only on the WebSocket event owner. Cached names and
// profiles may have resolved in the background after OnConnect added a row;
// repair both snapshots now, rather than waiting for another reconnect.
func (h *rtmEventHandler) OnPendingEvents() {
	if h.wsCtx == nil || h.wsCtx.UserResolver == nil {
		return
	}
	// Most resolved authors/members have no DM row. Filter once per drain
	// before any SQLite reads or per-peer conversation scans.
	dmPeers := make(map[string]struct{})
	for _, item := range h.wsCtx.Channels {
		if item.DMUserID != "" && (item.Type == "dm" || item.Type == "app") {
			dmPeers[item.DMUserID] = struct{}{}
		}
	}
	r := h.wsCtx.UserResolver
	r.resolvedPeers.Range(func(key, _ any) bool {
		r.resolvedPeers.Delete(key)
		userID := key.(string)
		if _, hasDM := dmPeers[userID]; hasDM {
			h.repairDMPeer(userID)
		}
		return true
	})
}
