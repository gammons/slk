// internal/ui/winmodels.go
//
// Per-window messages.Model store (window-management design §2,
// Phase 3). Each window in the tree owns a live model; a.messagepane
// always aliases the focused window's model so the ~100 existing
// focused-window call sites keep their semantics.
package ui

import (
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/wintree"
)

// newWindowModel constructs a messages model for a new window and
// applies the app-global config every pane needs — mirroring exactly
// the Set* calls made on the root model via App's Set* forwarders
// (SetAvatarFunc / SetUserNames / SetChannels / SetEmojiContext /
// SetCustomEmoji / SetImageContext / the bootstrap spinner tick).
// The values are retained on App fields by those forwarders so
// late-created windows get them too.
func (a *App) newWindowModel(chName string) *messages.Model {
	m := messages.New(nil, chName)
	m.SetAvatarFunc(a.avatarFn)
	m.SetUserNames(a.userNames)
	m.SetUserStatuses(a.presence.peers)
	m.SetChannelNames(a.channelNames)
	m.SetUserGroups(a.userGroups)
	m.SetEmojiContext(a.emojiCtx)
	if a.emojiCustoms != nil {
		// SetCustomEmoji ran after SetEmojiContext: its customs map
		// supersedes the one captured inside emojiCtx.
		m.SetEmojiCustoms(a.emojiCustoms)
	}
	m.SetImageContext(a.imageCtx)
	m.SetSpinnerFrame(a.spinnerFrame)
	return &m
}

// modelsForChannel returns the models of every window viewing chID,
// in tree order. Used by channel-scoped event fan-out (Task 2).
//
// The FOCUSED window routes by a.activeChannelID, not its tree
// record, for two reasons. (a) Bug-compatibility with the legacy
// gates: every pre-Phase-3 seam asked `m.ChannelID ==
// a.activeChannelID` — e.g. during a workspace switch,
// resetWindowTree leaves the focused window's record empty while
// activeChannelID still names the outgoing channel until the queued
// ChannelSelectedMsg lands, and routing by activeChannelID reproduces
// the legacy behavior exactly in that interval (self-correcting once
// the selection applies). (b) activeChannelID is the established
// contract for "the focused pane's channel" that the legacy seams and
// ~95 existing tests encode. Unfocused windows have no App-level
// context, so their tree records route them.
func (a *App) modelsForChannel(chID string) []*messages.Model {
	if chID == "" {
		return nil
	}
	var out []*messages.Model
	for _, id := range a.wins.Leaves() {
		winChID := ""
		if id == a.focusedWin {
			winChID = a.activeChannelID
		} else if ch, ok := a.wins.Channel(id); ok {
			winChID = ch.ID
		}
		if winChID == chID {
			if m := a.winModels[id]; m != nil {
				out = append(out, m)
			}
		}
	}
	return out
}

// cloneMessageItem returns a copy of item safe to hand to one model
// when the same value fans out to several: the struct copies by
// value, but the Reactions slice needs its own backing array —
// UpdateReaction writes elements in place and shifts on remove, so a
// shared array would let one window's reaction event corrupt another
// window's view. ReactionItem.UserIDs needs no copy: Append /
// RemoveUserID are copy-on-write. Attachments / Blocks /
// LegacyAttachments are never mutated in place by the model.
func cloneMessageItem(item messages.MessageItem) messages.MessageItem {
	if len(item.Reactions) > 0 {
		item.Reactions = append([]messages.ReactionItem(nil), item.Reactions...)
	}
	return item
}

// cloneMessageItems deep-copies a message slice for handing to a
// model: a fresh top-level slice (covers in-place item writes:
// SwapLocalSent, UpsertSelfSent, PatchUserName, UpdateMessageInPlace,
// IncrementReplyCount, RemoveMessageByTS) plus a per-item
// cloneMessageItem.
//
// Preserves nil-ness: nil in, nil out (the nil-vs-empty distinction
// matters to MessagesLoadedMsg's network-failure contract).
func cloneMessageItems(items []messages.MessageItem) []messages.MessageItem {
	if items == nil {
		return nil
	}
	out := make([]messages.MessageItem, len(items))
	for i := range items {
		out[i] = cloneMessageItem(items[i])
	}
	return out
}

// allWinModels returns every window's model in tree order. Used by
// workspace/global fan-out (Task 3).
func (a *App) allWinModels() []*messages.Model {
	out := make([]*messages.Model, 0, len(a.winModels))
	for _, id := range a.wins.Leaves() {
		if m := a.winModels[id]; m != nil {
			out = append(out, m)
		}
	}
	return out
}

// invalidateAllWinModelCaches drops every window model's render cache
// (theme change: cached rows bake in the old palette).
func (a *App) invalidateAllWinModelCaches() {
	for _, m := range a.allWinModels() {
		m.InvalidateCache()
	}
}

// anyWinModelLoading reports whether ANY window's model is showing
// its loading spinner. The spinner-tick gate must consider every
// window, not just the focused one: a backfill sets loading on the
// then-focused model, and if focus moves before completion a
// focused-only gate kills the tick chain, freezing the other
// window's glyph.
func (a *App) anyWinModelLoading() bool {
	for _, m := range a.allWinModels() {
		if m.IsLoading() {
			return true
		}
	}
	return false
}

// syncWinModels evicts models (and their render-cache slots) for
// windows no longer in the tree (after close/only). Additions happen
// explicitly in splitWindow.
func (a *App) syncWinModels() {
	live := make(map[wintree.LeafID]bool, a.wins.Len())
	for _, id := range a.wins.Leaves() {
		live[id] = true
	}
	for id := range a.winModels {
		if !live[id] {
			delete(a.winModels, id)
			a.renderCache.dropWinPane(id)
		}
	}
}

// resetWindowTree rebuilds the tree + model store to a single empty
// window (workspace switch).
func (a *App) resetWindowTree() {
	wins, rootWin := wintree.New(wintree.Channel{})
	a.wins = wins
	a.focusedWin = rootWin
	rootModel := a.newWindowModel("")
	a.winModels = map[wintree.LeafID]*messages.Model{rootWin: rootModel}
	a.messagepane = rootModel
	a.renderCache.dropAllWinPanes()
}
