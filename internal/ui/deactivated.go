package ui

import (
	"maps"

	"github.com/gammons/slk/internal/core"
)

// The deactivated-user set is global and immutable once published: every
// change builds a new map and hands it to the sidebar and channel finder,
// which keep it. That is the repo's "a map shared across goroutines is
// never written" rule applied inside the UI: nothing here writes a map a
// component already holds.

// hiddenUsers is the set the surfaces filter by: the deactivated users
// while general.hide_deactivated_users is on, nil (show everyone) when it
// is off. Reading a nil map is safe, so callers index it directly.
func (a *App) hiddenUsers() map[string]bool {
	if !a.hideDeactivatedUsers {
		return nil
	}
	return a.deactivatedUsers
}

// SetDeactivatedUsers merges a workspace snapshot into the App's global
// set. It only adds: a snapshot taken at connect time must not undo a
// deactivation another source already reported. A reactivation arrives as a
// UserDeactivatedMsg instead. seq is the snapshot's DeactivatedSeq: a user
// whose last live update is newer was read before that update and is
// skipped, so an old snapshot cannot hide a reactivated user again. Call it
// before SetChannels, the finder items and SetUserNames when applying a
// workspace, so those build filtered.
func (a *App) SetDeactivatedUsers(users map[string]bool, seq uint64) {
	merged := make(map[string]bool, len(a.deactivatedUsers)+len(users))
	maps.Copy(merged, a.deactivatedUsers)
	for id, deactivated := range users {
		if deactivated && a.deactivatedSeq[id] <= seq {
			merged[id] = true
		}
	}
	// Each workspace message carries the cache's whole set, almost always
	// already known: keep the published map and skip the refilter.
	if maps.Equal(merged, a.deactivatedUsers) {
		return
	}
	a.publishDeactivatedUsers(merged)
}

// setUserDeactivated records one user's live state, in either direction,
// and the update's seq for SetDeactivatedUsers. An update older than one
// already applied for the user is dropped: sends from different goroutines
// can arrive out of order.
func (a *App) setUserDeactivated(userID string, deactivated bool, seq uint64) {
	if seq < a.deactivatedSeq[userID] {
		return
	}
	if a.deactivatedSeq == nil {
		a.deactivatedSeq = map[string]uint64{}
	}
	a.deactivatedSeq[userID] = seq
	if a.deactivatedUsers[userID] == deactivated {
		return
	}
	next := maps.Clone(a.deactivatedUsers)
	if next == nil {
		next = map[string]bool{}
	}
	if deactivated {
		next[userID] = true
	} else {
		delete(next, userID)
	}
	a.publishDeactivatedUsers(next)
}

// SetHideDeactivatedUsers turns general.hide_deactivated_users on or off.
func (a *App) SetHideDeactivatedUsers(hide bool) {
	if a.hideDeactivatedUsers == hide {
		return
	}
	a.hideDeactivatedUsers = hide
	a.refilterDeactivated()
}

func (a *App) publishDeactivatedUsers(set map[string]bool) {
	a.deactivatedUsers = set
	a.refilterDeactivated()
	// Slack takes no messages for a deactivated account, whatever the
	// option says.
	a.syncComposeDisabled("deactivated_users")
}

// peerDeactivated reports whether channelID is a DM or app DM whose peer
// is deactivated. The peer comes from the finder's rows, which hold the
// conversation picked there, else from the sidebar's items (a DM just
// opened can reach the sidebar first). Both hold hidden rows too.
func (a *App) peerDeactivated(channelID string) bool {
	if channelID == "" || len(a.deactivatedUsers) == 0 {
		return false
	}
	if item, ok := a.findChannelByID(channelID); ok && item.DMUserID != "" {
		return core.IsHiddenPeerConversation(item.Type, item.DMUserID, a.deactivatedUsers)
	}
	for _, item := range a.sidebar.Items() {
		if item.ID == channelID {
			return core.IsHiddenPeerConversation(item.Type, item.DMUserID, a.deactivatedUsers)
		}
	}
	return false
}

// refilterDeactivated pushes the current filter to every surface that
// holds one. The new-message picker is not among them: it is seeded each
// time it opens.
func (a *App) refilterDeactivated() {
	hidden := a.hiddenUsers()
	a.sidebar.SetDeactivatedUsers(hidden)
	a.channelFinder.SetDeactivatedUsers(hidden)
	a.channelFinder.SetUnsendableUsers(a.deactivatedUsers)
	a.pushChannelPicks()
	// The roster is rebuilt from the full name map, as UserResolvedMsg does.
	if len(a.userNames) > 0 {
		a.SetUserNames(a.userNames)
	}
}
