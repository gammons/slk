// internal/ui/reducer_userprofile.go
//
// User-profile-dialog reducer: owns UserProfileLoadedMsg, the result
// of the K-opened modal's fetch (see App.openUserProfile). Free
// reducer, mirroring reducer_reactions.go's shape -- the modal itself
// (internal/ui/userprofile) already carries the staleness guard
// (SetProfile / SetError return false for a closed or re-targeted
// modal), so this reducer only needs to dispatch into it.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

var reduceUserProfile reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	m, ok := msg.(UserProfileLoadedMsg)
	if !ok {
		return nil, false
	}
	if m.Err != nil {
		a.userProfile.SetError(m.TeamID, m.UserID, m.Err)
	} else {
		a.userProfile.SetProfile(m.TeamID, m.UserID, m.Profile)
	}
	return nil, true
}
