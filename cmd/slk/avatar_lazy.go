package main

import "github.com/gammons/slk/internal/avatar"

// lazyAvatar is the AvatarFunc behind every author row the messages
// and thread panes render, on the bubbletea Update goroutine. A hit is
// a map lookup. On a miss it starts a background Preload from the URL
// the workspace recorded at connect time (or that resolveUser filled
// in), or, with no URL, a users.info backfill (RequestAvatar), and
// returns "" for now; AvatarReadyMsg invalidates the panes once the
// avatar lands. wctx is nil before a workspace is ready.
func lazyAvatar(wctx *WorkspaceContext, avatars *avatar.Cache, userID string) string {
	if rendered := avatars.Get(userID); rendered != "" {
		return rendered
	}
	if wctx == nil || wctx.AvatarURLs == nil {
		return ""
	}
	if v, ok := wctx.AvatarURLs.Load(userID); ok {
		if url, ok := v.(string); ok && url != "" {
			avatars.Preload(userID, url)
			return ""
		}
	}
	// No URL: a user who never uploaded an avatar, whom the edge and
	// boot payloads describe without one. users.info has it.
	wctx.UserResolver.RequestAvatar(userID)
	return ""
}
