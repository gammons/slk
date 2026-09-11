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
// One "unknown, so light it" case keeps the conservative default that
// MuteStore.Ready documents (a dot we might have suppressed beats one
// the user wanted to see and lost): byID returning nil, meaning the
// workspace is still connecting or its connect failed. There is no
// channel list to check against, so any unread row lights it, exactly
// as before this change. This is also what keeps last session's cached
// dots visible during boot, before any workspace has connected.
//
// A row whose channel is NOT in wctx.Channels is the opposite case and
// never lights. The sidebar and the local channel finder are built
// from the same loop that fills wctx.Channels (connectWorkspace), so
// such a channel has no row on screen, no dot, and no keystroke that
// can mark it read: a rail dot it lights can be neither explained nor
// cleared from inside slk. The field case was an archived Slack
// Connect channel. client.userBoot lists archived conversations the
// user belongs to, with is_archived=true (verified 2026-09-11 against
// a live response); bootConversations and users.conversations
// (ExcludeArchived) keep them out of the sidebar, but hydrateFirstSight
// caches every conversation userBoot names, archived or not, and
// client.counts still reported the channel unread because its
// last_read was Slack's never-opened sentinel. So a row the sidebar
// could not show carried has_unread=1 from the first boot, and kept it
// -- nothing deletes channel rows -- until the channel was opened in
// the official client. Filtering archived conversations out of
// hydrateFirstSight instead was rejected: the cache is allowed to know
// more conversations than the sidebar shows (search results resolve
// <#C…> mentions through those rows; see resolveChannel in main.go),
// it would leave every existing cache lit, and it would close one way
// a row can be unshowable rather than the property itself.
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
