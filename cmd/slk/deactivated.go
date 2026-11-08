package main

import (
	"sync"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui"
	"github.com/slack-go/slack"
)

// Deactivated users reach the UI from two kinds of source. A conversation
// object (users.conversations, conversations.info, im_created, im_open)
// carries is_user_deleted on an IM: it can only add a user, and it creates
// no cache row. A profile (users.info, edge users/info, bootstrap
// revalidation) is authoritative in both directions and is written to
// users.deleted by the full-profile writers.

// rememberedPeers holds users known deactivated only from this run: an IM
// peer Slack flagged is_user_deleted, or a profile that showed one
// deactivated, where the cache has no row to hold the flag. It is one
// process-wide set, not one per workspace, because an account is deactivated
// in every workspace at once, so a reactivation has to clear it everywhere.
// Positive entries only. An entry matters only while the cache has no row
// for the user: once a row exists the cache decides.
//
// Package-level rather than on a workspace context for the same reason:
// every workspace's handler reads and writes the one set. Tests empty it
// with isolateRememberedPeers.
var rememberedPeers sync.Map // user ID -> struct{}

// deactivatedSeq orders what the UI hears about deactivated users. Every
// UserDeactivatedMsg takes the next value, after the cache write it reports,
// and a workspace snapshot carries the value read before it reads the cache.
// A snapshot can then have an old number and fresh data, but never a fresh
// number and old data, so the UI drops a user from a snapshot whose number
// is lower than the last live update it applied for them. Without it, a
// snapshot read just before another workspace's reactivation lands after it
// and hides the user again until restart. Process-wide, like the set.
var deactivatedSeq atomic.Uint64

// knownDeleted reports whether userID is known deactivated, from the cache
// or from rememberedPeers. Read it before a profile writer overwrites the
// cached flag: a reactivation is reported only for a user known as deleted.
func knownDeleted(db *cache.DB, userID string) bool {
	if _, ok := rememberedPeers.Load(userID); ok {
		return true
	}
	if db == nil {
		return false
	}
	u, err := db.GetUser(userID)
	return err == nil && u.Deleted
}

// announceUserDeleted tells the UI about a fresh profile observation. A
// deactivated user is sent every time (idempotent, and the UI may not have
// seen the cache's earlier answer); an active one only when they were known
// as deleted, which is a reactivation, and then they are forgotten from
// rememberedPeers. The message names no workspace: the account is
// deactivated everywhere.
func announceUserDeleted(send func(tea.Msg), userID string, was, now bool) {
	if userID == "" {
		return
	}
	if !now {
		rememberedPeers.Delete(userID)
	}
	if send == nil || (!now && !was) {
		return
	}
	send(ui.UserDeactivatedMsg{UserID: userID, Deactivated: now, Seq: deactivatedSeq.Add(1)})
}

// announceRevalidated reports what bootstrap revalidation changed in the
// cache's deactivated set: before and after are db.DeactivatedUsers() read
// around bootstrap.Run, nil when a read failed (then nothing is sent).
// Revalidation writes users.deleted with no sender of its own, and a
// workspace message only ever adds to the UI's set, so without this a user
// reactivated while slk was off stays hidden when another workspace's
// message hid them first.
func announceRevalidated(send func(tea.Msg), before, after map[string]bool) {
	if before == nil || after == nil {
		return
	}
	for id := range before {
		if !after[id] {
			announceUserDeleted(send, id, true, false)
		}
	}
	for id := range after {
		if !before[id] {
			announceUserDeleted(send, id, false, true)
		}
	}
}

// recordUserDeleted persists and reports a profile observation that
// bypasses the full-profile writers (the peer-status users.info fallback,
// the avatar backfill, the profile dialog's fetch), which would otherwise
// drop the flag. It never creates a cache row, so a deactivated user with no
// row is remembered for this run instead.
func recordUserDeleted(db *cache.DB, send func(tea.Msg), userID string, deleted bool) {
	if userID == "" {
		return
	}
	was := knownDeleted(db, userID)
	if deleted {
		rememberedPeers.Store(userID, struct{}{})
		if db != nil {
			_ = db.MarkUserDeleted(userID)
		}
	} else if db != nil && cachedFlag(db, userID) {
		_ = db.ClearUserDeleted(userID)
	}
	announceUserDeleted(send, userID, was, deleted)
}

// cachedFlag is the users.deleted column alone.
func cachedFlag(db *cache.DB, userID string) bool {
	u, err := db.GetUser(userID)
	return err == nil && u.Deleted
}

// recordDeactivatedPeer notes the peer of an IM Slack flagged
// is_user_deleted and reports whether it was one. It remembers the peer,
// because a peer with no cache row cannot be marked there, and marks the row
// when there is one. It only ever adds: an IM without the flag says nothing,
// and only a profile can clear a deletion. buildChannelItem stays pure, so
// each place that builds conversations calls this.
func recordDeactivatedPeer(ch slack.Channel, db *cache.DB) bool {
	if !ch.IsIM || !ch.IsUserDeleted || ch.User == "" {
		return false
	}
	rememberedPeers.Store(ch.User, struct{}{})
	if db != nil {
		_ = db.MarkUserDeleted(ch.User)
	}
	return true
}

// deactivatedUsers is the set sent with WorkspaceReadyMsg and
// WorkspaceSwitchedMsg: every user the cache has as deactivated plus the
// remembered ones the cache has no row for, and the deactivatedSeq value
// read before either, which the message carries as DeactivatedSeq. The
// caller owns the map.
//
// A remembered user counts only while the cache has no row for them. Once a
// row exists the cache is the answer: it was marked when the row was there,
// and a later profile overwrites it, so a reactivation is not undone by the
// remembered flag on the next workspace switch.
func deactivatedUsers(db *cache.DB) (map[string]bool, uint64) {
	// First: a change written after this read has a higher number.
	seq := deactivatedSeq.Load()
	set := map[string]bool{}
	if db != nil {
		if cached, err := db.DeactivatedUsers(); err == nil {
			set = cached
		}
	}
	rememberedPeers.Range(func(key, _ any) bool {
		id := key.(string)
		if db != nil {
			if _, err := db.GetUser(id); err == nil {
				return true
			}
		}
		set[id] = true
		return true
	})
	return set, seq
}
