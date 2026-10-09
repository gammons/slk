package ui

import "testing"

func TestExternalUserNotificationsAreWorkspaceScoped(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	a.SetUserNames(map[string]string{"U1": "Alice"})
	// The same user is internal in T1 and external relative to inactive T2.
	a.Update(UserExternalMsg{TeamID: "T2", UserID: "U1", IsExternal: true})
	if a.externalUsers["U1"] {
		t.Fatal("inactive resolution altered active external flags")
	}
	a.Update(UserExternalMsg{TeamID: "T1", UserID: "U1", IsExternal: true})
	if !a.externalUsers["U1"] {
		t.Fatal("active resolution did not flag external user")
	}
	a.Update(UserExternalMsg{TeamID: "T2", UserID: "U1", IsExternal: false})
	if !a.externalUsers["U1"] {
		t.Fatal("inactive resolution cleared active external flag")
	}
	a.Update(UserExternalMsg{TeamID: "T1", UserID: "U1", IsExternal: false})
	if a.externalUsers["U1"] {
		t.Fatal("active resolution did not clear external flag")
	}
}
