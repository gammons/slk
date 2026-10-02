// internal/ui/userprofile_live_test.go
//
// Render-time liveness of the K-opened dialog: a status update or an
// avatar arriving while it's open shows up in the NEXT frame, since
// userProfileLive() (view_overlays.go) reads a.presence.peers,
// a.sidebar.PresenceByUser and a.avatarFn fresh at render time rather
// than the modal copying any of it. See docs/superpowers/specs/
// 2026-09-30-user-profile-dialog-design.md, "AvatarReadyMsg".
package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/ui/messages"
)

// TestUserProfile_LiveStatusUpdate: while the dialog is open, a
// UserStatusChangeMsg for its target user makes the new status text
// show up in the next frame without re-opening the dialog.
func TestUserProfile_LiveStatusUpdate(t *testing.T) {
	a := newTestApp(t, normalOpts()...)
	focusMessages(t, a)
	a.activeTeamID = "T1"
	_ = a.openUserProfile()
	if !a.userProfile.IsVisible() {
		t.Fatal("precondition: dialog did not open")
	}
	_, userID := a.userProfile.Target()

	before := stripANSI(a.View().Content)
	if strings.Contains(before, "On a call") {
		t.Fatal("precondition: status text already present before the update")
	}

	updateAndRender(t, a, UserStatusChangeMsg{
		TeamID: "T1", UserID: userID,
		Emoji: ":phone:", Text: "On a call", Expires: time.Now().Add(time.Hour),
	})

	after := stripANSI(a.View().Content)
	if !strings.Contains(after, "On a call") {
		t.Errorf("live status update did not show up in the next frame; frame:\n%s", after)
	}
}

// TestUserProfile_AvatarAppears: the avatar service returns "" at open
// time (no empty gutter, per the spec) and then "AV" once an
// AvatarReadyMsg lands for the displayed user; the next frame shows it.
func TestUserProfile_AvatarAppears(t *testing.T) {
	a := newTestApp(t, normalOpts()...)
	focusMessages(t, a)
	a.activeTeamID = "T1"

	var avatarReady bool
	a.avatarFn = func(userID string) string {
		if avatarReady {
			return "AV\nAV"
		}
		return ""
	}

	_ = a.openUserProfile()
	if !a.userProfile.IsVisible() {
		t.Fatal("precondition: dialog did not open")
	}
	_, userID := a.userProfile.Target()

	before := stripANSI(a.View().Content)
	if strings.Contains(before, "AV") {
		t.Fatal("precondition: avatar glyph already present before it was ready")
	}

	avatarReady = true
	updateAndRender(t, a, messages.AvatarReadyMsg{UserID: userID})

	after := stripANSI(a.View().Content)
	if !strings.Contains(after, "AV") {
		t.Errorf("avatar did not appear after AvatarReadyMsg; frame:\n%s", after)
	}
}
