package main

import "github.com/gammons/slk/internal/cache"

// railUnreadWorkspaces returns the workspace IDs whose rail dot should
// be lit. It is the reader wireCallbacks installs through
// App.SetWorkspaceUnreadReader; OtherUnreadCount (the title's "+N" and
// $SLK_OTHER_UNREAD) reads through the same installed reader, so the
// three surfaces cannot disagree.
//
// It answers the rail's question the way the sidebar answers its own:
// a workspace is lit when at least one channel in its wctx.Channels is
// ChannelItem.IsVisiblyUnread (internal/ui/sidebar/model.go), the one
// predicate the sidebar dot, UnreadChannelCount and $SLK_UNREAD already
// share. That is what keeps a muted channel from lighting the rail
// while the sidebar shows nothing unread.
//
// unread is db.UnreadChannels; byID resolves a workspace to its live
// context and is router.ByID in production. Both are parameters so
// the predicate is pure and testable with neither a router nor a DB,
// the same reason sidebar.IsStale takes its read state as arguments.
//
// Two edge cases go opposite ways:
//
//   - byID returns nil (the workspace is still connecting, or its
//     connect failed): any unread row lights it. There is no channel
//     list to check against, so MuteStore.Ready's conservative default
//     applies -- a dot we might have suppressed beats one the user
//     wanted and lost. This also keeps last session's cached dots
//     visible during boot.
//   - the row's channel is not in wctx.Channels: it never lights. The
//     sidebar and the local channel finder are built from that list,
//     so such a channel has no row on screen to explain a rail dot and
//     no keystroke to clear it. (The field case was an archived
//     channel: userBoot lists it, bootConversations drops it,
//     hydrateFirstSight still caches it, client.counts still reports
//     it unread.)
//
// wctx.Channels is read here on the UI goroutine without
// synchronization, against writes from the WebSocket handler
// (refreshMutedForActive, OnConversationOpened). That is how the
// Lookup callback in wireCallbacks already reads it; this adds a
// reader, not a convention.
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
// dot. The nil-wctx branch is the "unknown, so light it" case
// railUnreadWorkspaces documents; a channel absent from wctx.Channels
// is the "cannot be shown, so never light it" case.
func railRowLights(u cache.UnreadChannel, wctx *WorkspaceContext) bool {
	if wctx == nil {
		return true
	}
	for _, item := range wctx.Channels {
		if item.ID == u.ChannelID {
			return item.IsVisiblyUnread(u.State)
		}
	}
	return false
}
