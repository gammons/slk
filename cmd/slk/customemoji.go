package main

import (
	"context"

	"github.com/gammons/slk/internal/ui"
)

// customEmojiLister is the one-method slice of *slackclient.Client that
// the emoji fetch needs. Mirrors reconnectClient in reconnect_sync.go:
// a narrow interface keeps the fetch testable without a live client,
// and fails compilation if the surface grows.
type customEmojiLister interface {
	// ListCustomEmoji is emoji.list: the workspace's entire custom
	// emoji set. Distinct from conversations.view's Emojis field,
	// which is only the emoji the fetched conversation happens to use.
	ListCustomEmoji(ctx context.Context) (map[string]string, error)
}

// fetchWorkspaceEmoji publishes the workspace's full custom emoji set
// and tells the UI to re-render with it.
//
// It runs UNCONDITIONALLY, and that is the whole point of it. Bootstrap
// may already have published a map, but conversations.view returns only
// the emoji the restored conversation uses — a per-conversation subset,
// not the workspace's set. An earlier version skipped emoji.list
// whenever that subset was non-empty, which left every channel other
// than the restored one rendering custom emoji as literal `:name:`.
// TestFetchWorkspaceEmoji_RunsEvenWhenBootstrapPublishedASubset pins
// that so the short-circuit cannot come back.
//
// Best-effort: on error nothing is published and nothing is sent, so
// the bootstrap subset (or the built-ins) stays in place rather than
// being cleared.
//
// Intended to be run in a goroutine, after WorkspaceReadyMsg, so it
// never blocks first paint.
func fetchWorkspaceEmoji(ctx context.Context, wctx *WorkspaceContext, client customEmojiLister, sender teaSender, teamID string) {
	emojis, err := client.ListCustomEmoji(ctx)
	if err != nil {
		return
	}
	wctx.SetCustomEmoji(emojis)
	if sender != nil {
		sender.Send(ui.CustomEmojisLoadedMsg{
			TeamID:      teamID,
			CustomEmoji: emojis,
		})
	}
}
