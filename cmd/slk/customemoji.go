package main

import (
	"context"
	"maps"
	"time"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/ui"
)

// customEmojiTTL is how long a fetched set stands in for emoji.list.
// emoji_changed keeps it current while the socket is up; the TTL bounds
// what it misses: changes made while slk was closed or disconnected.
const customEmojiTTL = 24 * time.Hour

// maxEmojiFetches caps the emoji.list retries when emoji_changed events
// keep landing mid-fetch.
const maxEmojiFetches = 3

// emojiRefetchTimeout bounds the emoji_changed refetch, which has no
// app context to inherit. Long enough for a catalogue of 64k emoji.
const emojiRefetchTimeout = 30 * time.Second

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

// bootCustomEmoji returns the map to publish at connect: the persisted
// set with conversations.view's subset on top, since the subset is
// fresher for those names. freshFor is how long emoji.list can wait, 0
// meaning now; waiting takes a complete read of a set filled by
// emoji.list within the TTL. Only ReplaceCustomEmoji records that, so
// the subset never counts, and a failed read falls back to fetching. A
// stale set is still returned: it beats `:name:` until the fetch lands.
func bootCustomEmoji(db *cache.DB, teamID string, subset map[string]string, now time.Time) (emojis map[string]string, freshFor time.Duration) {
	emojis, err := db.CustomEmoji(teamID)
	if err != nil {
		debuglog.General("custom emoji cache: %v", err)
		return subset, 0
	}
	maps.Copy(emojis, subset)

	fetchedAt, ok, err := db.CustomEmojiFetchedAt(teamID)
	if err != nil {
		debuglog.General("custom emoji cache: %v", err)
		return emojis, 0
	}
	age := now.Sub(time.Unix(fetchedAt, 0))
	if !ok || age < 0 || age >= customEmojiTTL {
		return emojis, 0
	}
	return emojis, customEmojiTTL - age
}

// refreshWorkspaceEmoji runs fetchWorkspaceEmoji after wait, then once
// per TTL until ctx ends, so a long-running session doesn't keep a
// catalogue that missed changes. Run it in a goroutine.
func refreshWorkspaceEmoji(ctx context.Context, wctx *WorkspaceContext, client customEmojiLister, db *cache.DB, sender teaSender, teamID string, wait time.Duration) {
	for {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		fetchWorkspaceEmoji(ctx, wctx, client, db, sender, teamID)
		wait = customEmojiTTL
	}
}

// fetchWorkspaceEmoji publishes the workspace's full custom emoji set,
// persists it and tells the UI to re-render with it.
//
// Nothing short of a fresh persisted set may skip it (see
// bootCustomEmoji). Bootstrap may already have published a map, but
// conversations.view returns only the emoji the restored conversation
// uses — a per-conversation subset, not the workspace's set. An earlier
// version skipped emoji.list whenever that subset was non-empty, which
// left every channel other than the restored one rendering custom emoji
// as literal `:name:`.
// TestFetchWorkspaceEmoji_RunsEvenWhenBootstrapPublishedASubset pins
// that so the short-circuit cannot come back.
//
// Best-effort: on error nothing is published and nothing is sent, so
// what's there (the persisted set, the bootstrap subset or the
// built-ins) stays in place rather than being cleared. A failed cache
// write costs only a refetch next launch.
//
// A list fetched while an emoji_changed delta was applied may predate
// that delta, so it is discarded and fetched again. After
// maxEmojiFetches such misses the deltas stand and the cache stays
// stale, so the next launch fetches.
//
// Intended to be run in a goroutine, after WorkspaceReadyMsg, so it
// never blocks first paint.
func fetchWorkspaceEmoji(ctx context.Context, wctx *WorkspaceContext, client customEmojiLister, db *cache.DB, sender teaSender, teamID string) {
	for range maxEmojiFetches {
		wctx.emojiMu.Lock()
		seq := wctx.emojiSeq
		wctx.emojiMu.Unlock()

		emojis, err := client.ListCustomEmoji(ctx)
		if err != nil {
			return
		}

		wctx.emojiMu.Lock()
		if wctx.emojiSeq != seq {
			wctx.emojiMu.Unlock()
			continue
		}
		wctx.SetCustomEmoji(emojis)
		if err := db.ReplaceCustomEmoji(teamID, emojis, time.Now().Unix()); err != nil {
			debuglog.General("custom emoji cache: %v", err)
		}
		if sender != nil {
			sender.Send(ui.CustomEmojisLoadedMsg{
				TeamID:      teamID,
				CustomEmoji: emojis,
			})
		}
		wctx.emojiMu.Unlock()
		return
	}
}

// OnEmojiChanged keeps the published map and the cache current. add and
// remove apply as deltas; anything else (Slack is reported to send a
// rename, which it doesn't document) marks the cache stale and refetches.
// Deltas hold emojiMu, as fetchWorkspaceEmoji does when it publishes, so
// neither can publish a map built from one the other has replaced.
// Refetches are coalesced, so a burst of events costs a few emoji.list
// calls rather than a few per event.
func (h *rtmEventHandler) OnEmojiChanged(subtype, name string, names []string, value string) {
	if h.wsCtx == nil {
		return
	}
	isAdd := subtype == "add" && name != "" && value != ""
	if !isAdd && !(subtype == "remove" && len(names) > 0) {
		// Bump emojiSeq so a fetch already in flight, whose list may
		// predate this change, is discarded rather than published.
		w := h.wsCtx
		w.emojiMu.Lock()
		w.emojiSeq++
		h.staleCustomEmoji()
		if w.emojiRefetching {
			w.emojiRefetchPending = true
			w.emojiMu.Unlock()
			return
		}
		w.emojiRefetching = true
		w.emojiMu.Unlock()
		go func() {
			for {
				ctx, cancel := context.WithTimeout(context.Background(), emojiRefetchTimeout)
				fetchWorkspaceEmoji(ctx, w, h.emojiLister, h.db, h.program, h.workspaceID)
				cancel()
				w.emojiMu.Lock()
				if !w.emojiRefetchPending {
					w.emojiRefetching = false
					w.emojiMu.Unlock()
					return
				}
				w.emojiRefetchPending = false
				w.emojiMu.Unlock()
			}
		}()
		return
	}

	h.wsCtx.emojiMu.Lock()
	defer h.wsCtx.emojiMu.Unlock()
	h.wsCtx.emojiSeq++
	emojis := maps.Clone(h.wsCtx.CustomEmoji()) // the published map is never mutated
	var err error
	if isAdd {
		emojis[name] = value
		err = h.db.UpsertCustomEmoji(h.workspaceID, name, value)
	} else {
		for _, n := range names {
			delete(emojis, n)
		}
		err = h.db.DeleteCustomEmoji(h.workspaceID, names)
	}
	if err != nil {
		// The rows now lack this delta; don't let them pass as fresh.
		debuglog.General("custom emoji cache: %v", err)
		h.staleCustomEmoji()
	}
	h.wsCtx.SetCustomEmoji(emojis)
	if h.program != nil {
		h.program.Send(ui.CustomEmojisLoadedMsg{TeamID: h.workspaceID, CustomEmoji: emojis})
	}
}

func (h *rtmEventHandler) staleCustomEmoji() {
	if err := h.db.ZeroCustomEmojiFetchedAt(h.workspaceID); err != nil {
		debuglog.General("custom emoji cache: %v", err)
	}
}
