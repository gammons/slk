// internal/ui/copy_email_test.go
//
// Commands returned by Update are not run: the clipboard writer and the
// toast both take effect synchronously, and running the batch would
// fire the 2s toast-clear tick.
//
// Copying the profile dialog's email: the e key and a click on the 📋
// beside the address. Both write the loaded email to the clipboard and
// toast "Copied email", leaving the dialog open; with no email loaded
// they toast "No email to copy" and write nothing. See
// mode_user_profile.go and the pointClickable routing in
// reducer_modal_click.go.
package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/userprofile"
)

const (
	copiedEmailToast = "Copied email"
	noEmailToast     = "No email to copy"
	testEmail        = "priya@example.com"
)

// recordClipboard swaps in a clipboard writer that records every text
// it is handed. The returned slice pointer grows per write.
func recordClipboard(a *App) *[]string {
	var got []string
	a.SetClipboardWriter(func(text string) tea.Cmd {
		got = append(got, text)
		return nil
	})
	return &got
}

// openProfileWithEmail opens the dialog through the production path and
// delivers a loaded profile carrying email, then renders a frame so the
// modal's click geometry reflects what is on screen.
func openProfileWithEmail(t *testing.T, email string) *App {
	t.Helper()
	a := openUserProfileWith(t)
	teamID, userID := a.userProfile.Target()
	a.Update(UserProfileLoadedMsg{TeamID: teamID, UserID: userID,
		Profile: core.UserProfile{DisplayName: "Priya Raman", Email: email}})
	_ = a.View()
	return a
}

func TestCopyEmailKey_CopiesAndToasts(t *testing.T) {
	a := openProfileWithEmail(t, testEmail)
	got := recordClipboard(a)

	a.Update(keyPress('e'))

	if len(*got) != 1 || (*got)[0] != testEmail {
		t.Fatalf("clipboard writes = %q, want exactly [%q]", *got, testEmail)
	}
	if s := statusbarText(a); !strings.Contains(s, copiedEmailToast) {
		t.Errorf("statusbar = %q, want %q", s, copiedEmailToast)
	}
	if a.mode != ModeUserProfile || !a.userProfile.IsVisible() {
		t.Errorf("dialog closed after copy (mode %v, visible %v); want it to stay open",
			a.mode, a.userProfile.IsVisible())
	}
}

func TestCopyEmailKey_NothingToCopy(t *testing.T) {
	cases := map[string]func(t *testing.T) *App{
		"still loading": openUserProfileWith,
		"no email":      func(t *testing.T) *App { return openProfileWithEmail(t, "") },
	}
	for name, open := range cases {
		t.Run(name, func(t *testing.T) {
			a := open(t)
			got := recordClipboard(a)

			a.Update(keyPress('e'))

			if len(*got) != 0 {
				t.Errorf("clipboard writes = %q, want none", *got)
			}
			if s := statusbarText(a); !strings.Contains(s, noEmailToast) {
				t.Errorf("statusbar = %q, want %q", s, noEmailToast)
			}
			if a.mode != ModeUserProfile {
				t.Errorf("mode = %v, want ModeUserProfile", a.mode)
			}
		})
	}
}

// emailIconScreenPos finds the copy icon on the Email row of the App's
// rendered frame, in terminal cells.
func emailIconScreenPos(t *testing.T, a *App) (x, y int) {
	t.Helper()
	lines := strings.Split(ansi.Strip(a.View().Content), "\n")
	for row, l := range lines {
		if !strings.Contains(l, "Email") {
			continue
		}
		if i := strings.Index(l, userprofile.CopyIcon); i >= 0 {
			return emoji.Width(l[:i]), row
		}
	}
	t.Fatalf("no copy icon on an Email row in the frame:\n%s", strings.Join(lines, "\n"))
	return 0, 0
}

func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y}
}

func TestCopyEmailClick_OnIconCopies(t *testing.T) {
	a := openProfileWithEmail(t, testEmail)
	got := recordClipboard(a)
	x, y := emailIconScreenPos(t, a)

	a.Update(click(x, y))

	if len(*got) != 1 || (*got)[0] != testEmail {
		t.Fatalf("clipboard writes = %q, want exactly [%q]", *got, testEmail)
	}
	if s := statusbarText(a); !strings.Contains(s, copiedEmailToast) {
		t.Errorf("statusbar = %q, want %q", s, copiedEmailToast)
	}
	if !a.userProfile.IsVisible() {
		t.Error("dialog closed after clicking the copy icon")
	}
}

func TestCopyEmailClick_InsideBoxElsewhereIsNoop(t *testing.T) {
	a := openProfileWithEmail(t, testEmail)
	got := recordClipboard(a)
	x, y := emailIconScreenPos(t, a)

	// The email text itself, left of the icon: inside the box, not the icon.
	a.Update(click(x-4, y))

	if len(*got) != 0 {
		t.Errorf("clipboard writes = %q, want none", *got)
	}
	if !a.userProfile.IsVisible() || a.mode != ModeUserProfile {
		t.Errorf("click inside the box closed the dialog (mode %v)", a.mode)
	}
}

func TestCopyEmailClick_OutsideBoxCloses(t *testing.T) {
	a := openProfileWithEmail(t, testEmail)
	got := recordClipboard(a)

	a.Update(click(0, 0))

	if len(*got) != 0 {
		t.Errorf("clipboard writes = %q, want none", *got)
	}
	if a.userProfile.IsVisible() || a.mode != ModeNormal {
		t.Errorf("click outside the box left the dialog open (mode %v)", a.mode)
	}
}
