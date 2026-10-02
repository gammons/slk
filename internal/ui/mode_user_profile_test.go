// internal/ui/mode_user_profile_test.go
//
// K-opened user-profile dialog: opening from either pane, the
// no-target/bot-ID/empty-UserID toast paths, and ModeUserProfile's
// key handler (K/esc/q close). See mode_user_profile.go and
// App.openUserProfile (app.go).
package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/statusbar"
)

// TestUserProfileKey_OpensFromMessages drives K through the real
// Update chain (updateAndRender), from the messages pane.
func TestUserProfileKey_OpensFromMessages(t *testing.T) {
	a := newTestApp(t, normalOpts()...)
	a.activeTeamID = "T1"
	focusMessages(t, a)

	_, cmd := a.Update(keyPress('K'))
	if a.mode != ModeUserProfile {
		t.Fatalf("mode = %v, want ModeUserProfile", a.mode)
	}
	if !a.userProfile.IsVisible() {
		t.Fatal("userProfile modal not visible after K")
	}
	wantTeam, wantUser := "T1", "U1"
	gotTeam, gotUser := a.userProfile.Target()
	if gotTeam != wantTeam || gotUser != wantUser {
		t.Errorf("Target() = (%q, %q), want (%q, %q)", gotTeam, gotUser, wantTeam, wantUser)
	}
	if cmd == nil {
		t.Error("cmd = nil, want the profile-fetch command")
	}
}

// TestUserProfileKey_OpensFromThread is the thread-panel twin.
func TestUserProfileKey_OpensFromThread(t *testing.T) {
	a := newTestApp(t, normalOpts()...)
	a.activeTeamID = "T1"
	focusThreadPanel(t, a)

	reply := a.threadPanel.SelectedReply()
	if reply == nil {
		t.Fatal("precondition: no selected reply")
	}
	wantUser := reply.UserID

	_, cmd := a.Update(keyPress('K'))
	if a.mode != ModeUserProfile {
		t.Fatalf("mode = %v, want ModeUserProfile", a.mode)
	}
	if !a.userProfile.IsVisible() {
		t.Fatal("userProfile modal not visible after K")
	}
	gotTeam, gotUser := a.userProfile.Target()
	if gotTeam != "T1" || gotUser != wantUser {
		t.Errorf("Target() = (%q, %q), want (\"T1\", %q)", gotTeam, gotUser, wantUser)
	}
	if cmd == nil {
		t.Error("cmd = nil, want the profile-fetch command")
	}
}

// TestUserProfileKey_RejectionToast: K with no person behind the
// selection opens nothing and shows noProfileToast, which its own clear
// tick then removes. The rejected selections are none (an empty pane),
// a UserID starting with "B" (cmd/slk/history.go's bot-ID substitute),
// and an empty UserID (a message carrying only a bot_id upstream).
//
// The cmd is fed back as the message its real 2s tick delivers, since
// a non-nil check would also accept the profile fetch. The three ticks
// run together, so the test waits 2s, not 6s.
func TestUserProfileKey_RejectionToast(t *testing.T) {
	cases := []struct {
		name string
		msgs []messages.MessageItem
	}{
		{"no selection", nil},
		{"bot ID", []messages.MessageItem{{TS: "1.0", UserID: "B123", Text: "build ok"}}},
		{"empty UserID", []messages.MessageItem{{TS: "1.0", UserID: "", Text: "hello"}}},
	}
	apps := make([]*App, len(cases))
	cmds := make([]tea.Cmd, len(cases))
	for i, tc := range cases {
		a := newTestApp(t, withChannels(), withMessages(tc.msgs...), withActiveChannel(""))
		a.focusedPanel = PanelMessages

		_, cmds[i] = a.Update(keyPress('K'))
		if cmds[i] == nil {
			t.Fatalf("%s: cmd = nil, want the toast's clear tick", tc.name)
		}
		if a.mode != ModeNormal {
			t.Fatalf("%s: mode = %v, want ModeNormal", tc.name, a.mode)
		}
		if a.userProfile.IsVisible() {
			t.Fatalf("%s: userProfile modal opened", tc.name)
		}
		if got := statusbarText(a); !containsToast(got, noProfileToast) {
			t.Errorf("%s: statusbar = %q, want it to contain %q", tc.name, got, noProfileToast)
		}
		apps[i] = a
	}

	ticks := deliveredMsgs[statusbar.CopiedClearMsg](t, cmds...)
	for i, tc := range cases {
		apps[i].Update(ticks[i])
		if got := statusbarText(apps[i]); containsToast(got, noProfileToast) {
			t.Errorf("%s: statusbar = %q after the toast's clear tick, want it cleared", tc.name, got)
		}
	}
}

// containsToast is a thin strings.Contains wrapper so the toast
// assertions read as intent.
func containsToast(haystack, want string) bool {
	return strings.Contains(haystack, want)
}

// openUserProfileFor drives the production open path (focus the
// messages pane, K) and fails the case if the modal did not open --
// the same pattern mode_reactions_view_test.go uses for its own
// openReactionsViewFor.
func openUserProfileFor(t *testing.T, a *App) {
	t.Helper()
	focusMessages(t, a)
	a.activeTeamID = "T1"
	_ = a.openUserProfile()
	if !a.userProfile.IsVisible() {
		t.Fatal("precondition: openUserProfile did not open the modal")
	}
	if a.mode != ModeUserProfile {
		t.Fatalf("precondition: mode = %v, want ModeUserProfile", a.mode)
	}
}

// TestUserProfileModeKeys: K, esc and q each close the dialog and
// return to Normal mode; ModeUserProfile has no scroll state of its
// own so nothing else is asserted.
func TestUserProfileModeKeys(t *testing.T) {
	runKeyCases(t, ModeUserProfile, []keyCase{
		{
			name:     "K closes",
			opts:     normalOpts(),
			setup:    func(t *testing.T, a *App) { openUserProfileFor(t, a) },
			key:      keyPress('K'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.userProfile.IsVisible() {
					t.Error("userProfile still visible after K")
				}
			},
		},
		{
			name:     "esc closes",
			opts:     normalOpts(),
			setup:    func(t *testing.T, a *App) { openUserProfileFor(t, a) },
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.userProfile.IsVisible() {
					t.Error("userProfile still visible after esc")
				}
			},
		},
		{
			name:     "q closes",
			opts:     normalOpts(),
			setup:    func(t *testing.T, a *App) { openUserProfileFor(t, a) },
			key:      keyPress('q'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.userProfile.IsVisible() {
					t.Error("userProfile still visible after q")
				}
			},
		},
	})
}

// TestModeUserProfile_IsModalOverlay pins ModeUserProfile's membership
// in IsModalOverlay, which gates #265's sixel suppression (see
// TestCollectSixelPlacements_WithheldWhileAModalIsOpen).
func TestModeUserProfile_IsModalOverlay(t *testing.T) {
	if !ModeUserProfile.IsModalOverlay() {
		t.Error("ModeUserProfile.IsModalOverlay() = false, want true")
	}
}

// TestUserProfileCmd_TenSecondDeadline pins the fetch's context bound:
// exactly 10s from "now", and the returned message carries the seeded
// team/user regardless of what the fake service returns.
func TestUserProfileCmd_TenSecondDeadline(t *testing.T) {
	a := newTestApp(t, normalOpts()...)
	a.activeTeamID = "T1"
	focusMessages(t, a)

	var gotDeadline time.Time
	var hasDeadline bool
	a.setProfileFetcherForTest(func(ctx context.Context, teamID, userID string) (core.UserProfile, error) {
		gotDeadline, hasDeadline = ctx.Deadline()
		return core.UserProfile{}, nil
	})

	cmd := a.openUserProfile()
	if cmd == nil {
		t.Fatal("cmd = nil")
	}
	before := time.Now()
	msg := cmd()
	after := time.Now()

	if !hasDeadline {
		t.Fatal("context carried no deadline")
	}
	wantMin := before.Add(9 * time.Second)
	wantMax := after.Add(11 * time.Second)
	if gotDeadline.Before(wantMin) || gotDeadline.After(wantMax) {
		t.Errorf("deadline = %v, want within 10s (+/-1s) of now (%v..%v)", gotDeadline, wantMin, wantMax)
	}

	loaded, ok := msg.(UserProfileLoadedMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want UserProfileLoadedMsg", msg)
	}
	if loaded.TeamID != "T1" || loaded.UserID != "U1" {
		t.Errorf("loaded = {TeamID:%q UserID:%q}, want {T1 U1}", loaded.TeamID, loaded.UserID)
	}
}
