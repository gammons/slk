// internal/ui/reducer_userprofile_test.go
//
// reduceUserProfile: applying a matching UserProfileLoadedMsg, and
// dropping one that targets a stale team/user, a closed dialog, or a
// dialog closed by a workspace switch. See reducer_userprofile.go and
// reduceWorkspaceSwitched's Close call (reducer_workspace.go).
package ui

import (
	"testing"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/userprofile"
)

// openUserProfileWith opens the dialog for T1/U1 via the production
// path (focus + openUserProfile), exactly like openUserProfileFor in
// mode_user_profile_test.go, but returning the App rather than taking
// one, since these tests build their own.
func openUserProfileWith(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t, normalOpts()...)
	focusMessages(t, a)
	a.activeTeamID = "T1"
	_ = a.openUserProfile()
	if !a.userProfile.IsVisible() {
		t.Fatal("precondition: openUserProfile did not open the modal")
	}
	return a
}

// TestReduceUserProfile_Applied: a result whose TeamID/UserID match
// the dialog's current Target() is applied.
func TestReduceUserProfile_Applied(t *testing.T) {
	a := openUserProfileWith(t)
	teamID, userID := a.userProfile.Target()

	_, handled := a.Update(UserProfileLoadedMsg{
		TeamID: teamID, UserID: userID,
		Profile: core.UserProfile{DisplayName: "Priya Raman", Email: "priya@example.com"},
	})
	_ = handled

	frame := a.userProfile.ViewOverlay(80, 24, blankFrame(80, 24), userprofileLiveZero())
	if !containsToast(frame, "priya@example.com") {
		t.Errorf("loaded profile's email not rendered; frame:\n%s", frame)
	}
}

// TestReduceUserProfile_StaleUserDropped: a result for a DIFFERENT
// user than the dialog currently targets is dropped -- the dialog
// keeps showing loading state, not the stale user's data.
func TestReduceUserProfile_StaleUserDropped(t *testing.T) {
	a := openUserProfileWith(t)
	teamID, _ := a.userProfile.Target()

	a.Update(UserProfileLoadedMsg{
		TeamID: teamID, UserID: "SOMEONE-ELSE",
		Profile: core.UserProfile{DisplayName: "Not Priya", Email: "nope@example.com"},
	})

	frame := a.userProfile.ViewOverlay(80, 24, blankFrame(80, 24), userprofileLiveZero())
	if containsToast(frame, "nope@example.com") {
		t.Errorf("stale-user result was applied; frame:\n%s", frame)
	}
	if !containsToast(frame, "Loading profile") {
		t.Errorf("dialog should still be in the loading state; frame:\n%s", frame)
	}
}

// TestReduceUserProfile_DroppedAfterClose: the dialog is closed (K
// again, or esc/q) before the fetch lands; the late result changes
// nothing observable, and in particular does not reopen the dialog.
//
// Asserted two ways. IsVisible() alone is not enough: a closed modal
// renders nothing regardless of what SetProfile does to its internal
// state (ViewOverlay's own guard), so a broken staleness check
// wouldn't show up there. userProfile.SetProfile's own return value
// is the direct signal reduceUserProfile relies on to know whether it
// applied anything.
func TestReduceUserProfile_DroppedAfterClose(t *testing.T) {
	a := openUserProfileWith(t)
	teamID, userID := a.userProfile.Target()

	a.userProfile.Close()
	if a.userProfile.IsVisible() {
		t.Fatal("precondition: dialog still visible after Close")
	}

	if applied := a.userProfile.SetProfile(teamID, userID, core.UserProfile{DisplayName: "Late Arrival"}); applied {
		t.Error("SetProfile applied a result targeting a closed dialog")
	}

	a.Update(UserProfileLoadedMsg{
		TeamID: teamID, UserID: userID,
		Profile: core.UserProfile{DisplayName: "Late Arrival"},
	})

	if a.userProfile.IsVisible() {
		t.Error("a late result reopened the closed dialog")
	}
}

// TestReduceUserProfile_DroppedAfterWorkspaceSwitch: switching
// workspaces closes the dialog and returns to Normal mode (see
// reduceWorkspaceSwitched); a T1 result that lands afterward is
// dropped and does not reopen it or re-enter ModeUserProfile.
func TestReduceUserProfile_DroppedAfterWorkspaceSwitch(t *testing.T) {
	a := openUserProfileWith(t)
	teamID, userID := a.userProfile.Target()
	if a.mode != ModeUserProfile {
		t.Fatalf("precondition: mode = %v, want ModeUserProfile", a.mode)
	}

	a.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "Other", Channels: nil})

	if a.userProfile.IsVisible() {
		t.Fatal("precondition: workspace switch should have closed the dialog")
	}
	if a.mode != ModeNormal {
		t.Fatalf("precondition: mode after switch = %v, want ModeNormal", a.mode)
	}

	a.Update(UserProfileLoadedMsg{
		TeamID: teamID, UserID: userID,
		Profile: core.UserProfile{DisplayName: "Late T1 Arrival"},
	})

	if a.userProfile.IsVisible() {
		t.Error("a late T1 result reopened the dialog after a workspace switch")
	}
	if a.mode != ModeNormal {
		t.Errorf("mode = %v, want ModeNormal to stay put", a.mode)
	}
}

// blankFrame builds a w x h grid of spaces, the minimal "background"
// ViewOverlay needs to composite onto.
func blankFrame(w, h int) string {
	row := ""
	for i := 0; i < w; i++ {
		row += " "
	}
	out := row
	for i := 1; i < h; i++ {
		out += "\n" + row
	}
	return out
}

// userprofileLiveZero is the zero-value userprofile.Live: no avatar,
// no known presence, no status, and a fixed clock -- enough to render
// a frame without depending on App-level plumbing this reducer test
// doesn't otherwise set up.
func userprofileLiveZero() userprofile.Live {
	return userprofile.Live{Now: goldenClock()}
}
