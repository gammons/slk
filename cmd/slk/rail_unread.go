package main

import "github.com/gammons/slk/internal/cache"

// railUnreadWorkspaces returns the workspace IDs whose rail dot should
// be lit. It is the reader wireCallbacks installs through
// App.SetWorkspaceUnreadReader, and OtherUnreadCount (the title's "+N"
// and $SLK_OTHER_UNREAD) reads through the same installed reader, so
// the three surfaces cannot disagree with each other.
//
// It answers the rail's question the way the sidebar answers its own:
// a workspace is lit when at least one channel in its wctx.Channels is
// ChannelItem.IsVisiblyUnread (internal/ui/sidebar/model.go) -- the one
// predicate the sidebar dot, UnreadChannelCount and therefore
// $SLK_UNREAD already share. Before this the reader lit every
// workspace with any has_unread=1 row, so a single muted firehose
// channel kept a workspace's dot on permanently while the sidebar,
// correctly, showed nothing unread in it. That is the case
// IsVisiblyUnread exists to prevent, applied one surface short.
//
// unread is db.UnreadChannels; byID resolves a workspace to its live
// context and is router.ByID in production. Both are parameters so
// the predicate is pure and testable with neither a router nor a DB,
// the same reason sidebar.IsStale takes its read state as arguments.
//
// Two "unknown, so light it" cases keep the conservative default that
// MuteStore.Ready documents (a dot we might have suppressed beats one
// the user wanted to see and lost):
//
//   - byID returns nil: the workspace is still connecting, or its
//     connect failed. There is no channel list to check against, so
//     any unread row lights it, exactly as before this change. This is
//     also what keeps last session's cached dots visible during boot,
//     before any workspace has connected.
//   - the row's channel is not in wctx.Channels: its mute state is
//     unknown, so it is treated as unmuted.
//
// wctx.Channels is read here on the UI goroutine without
// synchronization, against writes from the WebSocket handler
// (refreshMutedForActive, OnConversationOpened). That is how the
// Lookup callback in wireCallbacks already reads it; this adds a
// reader, not a convention. Likewise router.ByID: the
// EnsureSubscriptions callback reads it from the UI goroutine while
// connect goroutines are still populating it.
func railUnreadWorkspaces(unread []cache.UnreadChannel, byID func(teamID string) *WorkspaceContext) []string {
	var out []string
	lit := map[string]bool{}
	for _, u := range unread {
		if lit[u.WorkspaceID] {
			continue
		}
		if railRowLights(u, byID(u.WorkspaceID)) {
			lit[u.WorkspaceID] = true
			out = append(out, u.WorkspaceID)
		}
	}
	return out
}

// railRowLights reports whether one unread row lights its workspace's
// dot. The two true-by-default branches are the "unknown, so light
// it" cases railUnreadWorkspaces documents.
func railRowLights(u cache.UnreadChannel, wctx *WorkspaceContext) bool {
	if wctx == nil {
		return true
	}
	for _, item := range wctx.Channels {
		if item.ID == u.ChannelID {
			return item.IsVisiblyUnread(u.State)
		}
	}
	return true
}
