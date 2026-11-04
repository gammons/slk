// internal/ui/copy_huddle_test.go
//
// Copying the profile dialog's huddle link: the c key and a click on
// the 📋 on the "In a huddle" row. Both write the huddle deep link to
// the clipboard and toast "Copied huddle link", leaving the dialog
// open; with no channel named, or with the live status saying the
// huddle has ended, they toast "No huddle link" and write nothing.
// Mirrors copy_email_test.go, which owns the clipboard/click helpers.
package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/userprofile"
)

const (
	copiedHuddleToast = "Copied huddle link"
	noHuddleToast     = "No huddle link"
	testHuddleChannel = "C9"
)

// openProfileWithHuddle opens the dialog through the production path,
// delivers a loaded profile carrying the huddle channel, sets the
// author's live status to in-a-huddle (or not), and renders a frame so
// the modal's click geometry reflects what is on screen.
func openProfileWithHuddle(t *testing.T, channel string, inHuddle bool) *App {
	t.Helper()
	a := openUserProfileWith(t)
	teamID, userID := a.userProfile.Target()
	if inHuddle {
		// The production path for live status: the same message a
		// user_change event or a peer-status refetch delivers.
		a.Update(UserStatusChangeMsg{TeamID: teamID, UserID: userID,
			Huddle: peerstatus.HuddleActive})
	}
	a.Update(UserProfileLoadedMsg{TeamID: teamID, UserID: userID,
		Profile: core.UserProfile{DisplayName: "Priya Raman", HuddleChannelID: channel}})
	_ = a.View()
	return a
}

// huddleIconScreenPos finds the copy icon on the "In a huddle" row of
// the App's rendered frame, in terminal cells.
func huddleIconScreenPos(t *testing.T, a *App) (x, y int) {
	t.Helper()
	lines := strings.Split(ansi.Strip(a.View().Content), "\n")
	for row, l := range lines {
		if !strings.Contains(l, "In a huddle") {
			continue
		}
		if i := strings.Index(l, userprofile.CopyIcon); i >= 0 {
			return emoji.Width(l[:i]), row
		}
	}
	t.Fatalf("no copy icon on the huddle row in the frame:\n%s", strings.Join(lines, "\n"))
	return 0, 0
}

func TestCopyHuddleKey_CopiesAndToasts(t *testing.T) {
	a := openProfileWithHuddle(t, testHuddleChannel, true)
	got := recordClipboard(a)

	a.Update(keyPress('c'))

	if len(*got) != 1 || (*got)[0] != "https://app.slack.com/huddle/T1/"+testHuddleChannel {
		t.Fatalf("clipboard writes = %q, want exactly the T1/C9 huddle link", *got)
	}
	if s := statusbarText(a); !strings.Contains(s, copiedHuddleToast) {
		t.Errorf("statusbar = %q, want %q", s, copiedHuddleToast)
	}
	if a.mode != ModeUserProfile || !a.userProfile.IsVisible() {
		t.Errorf("dialog closed after copy (mode %v, visible %v); want it to stay open",
			a.mode, a.userProfile.IsVisible())
	}
}

func TestCopyHuddleKey_NothingToCopy(t *testing.T) {
	cases := map[string]func(t *testing.T) *App{
		"no channel named": func(t *testing.T) *App { return openProfileWithHuddle(t, "", true) },
		"huddle has ended": func(t *testing.T) *App { return openProfileWithHuddle(t, testHuddleChannel, false) },
		"still loading":    openUserProfileWith,
	}
	for name, open := range cases {
		t.Run(name, func(t *testing.T) {
			a := open(t)
			got := recordClipboard(a)

			a.Update(keyPress('c'))

			if len(*got) != 0 {
				t.Errorf("clipboard writes = %q, want none", *got)
			}
			if s := statusbarText(a); !strings.Contains(s, noHuddleToast) {
				t.Errorf("statusbar = %q, want %q", s, noHuddleToast)
			}
			if a.mode != ModeUserProfile {
				t.Errorf("mode = %v, want ModeUserProfile", a.mode)
			}
		})
	}
}

func TestCopyHuddleClick_OnIconCopies(t *testing.T) {
	a := openProfileWithHuddle(t, testHuddleChannel, true)
	got := recordClipboard(a)
	x, y := huddleIconScreenPos(t, a)

	a.Update(click(x, y))

	if len(*got) != 1 || (*got)[0] != "https://app.slack.com/huddle/T1/"+testHuddleChannel {
		t.Fatalf("clipboard writes = %q, want exactly the T1/C9 huddle link", *got)
	}
	if s := statusbarText(a); !strings.Contains(s, copiedHuddleToast) {
		t.Errorf("statusbar = %q, want %q", s, copiedHuddleToast)
	}
	if !a.userProfile.IsVisible() {
		t.Error("dialog closed after clicking the huddle icon")
	}
}

func TestCopyHuddleClick_InsideBoxElsewhereIsNoop(t *testing.T) {
	a := openProfileWithHuddle(t, testHuddleChannel, true)
	got := recordClipboard(a)
	x, y := huddleIconScreenPos(t, a)

	// The "In a huddle" text itself, left of the icon.
	a.Update(click(x-4, y))

	if len(*got) != 0 {
		t.Errorf("clipboard writes = %q, want none", *got)
	}
	if !a.userProfile.IsVisible() || a.mode != ModeUserProfile {
		t.Errorf("click inside the box closed the dialog (mode %v)", a.mode)
	}
}
