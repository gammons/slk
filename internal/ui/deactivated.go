package ui

import "maps"

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

// SetDeactivatedUsers merges users into the App's global set. It only
// adds: a snapshot taken at connect time must not undo a deactivation
// another source already reported. A reactivation arrives as a
// UserDeactivatedMsg instead. Call it before SetChannels, the finder items
// and SetUserNames when applying a workspace, so those build filtered.
func (a *App) SetDeactivatedUsers(users map[string]bool) {
	merged := make(map[string]bool, len(a.deactivatedUsers)+len(users))
	maps.Copy(merged, a.deactivatedUsers)
	for id, deactivated := range users {
		if deactivated {
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

// setUserDeactivated records one user's state, in either direction.
func (a *App) setUserDeactivated(userID string, deactivated bool) {
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
}

// refilterDeactivated pushes the current filter to every surface that
// holds one. The new-message picker is not among them: it is seeded each
// time it opens.
func (a *App) refilterDeactivated() {
	hidden := a.hiddenUsers()
	a.sidebar.SetDeactivatedUsers(hidden)
	a.channelFinder.SetDeactivatedUsers(hidden)
	a.pushChannelPicks()
	// The roster is rebuilt from the full name map, as UserResolvedMsg does.
	if len(a.userNames) > 0 {
		a.SetUserNames(a.userNames)
	}
}
