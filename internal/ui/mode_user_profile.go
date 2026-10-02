// internal/ui/mode_user_profile.go
//
// User-profile-dialog key handler. K, esc and q all close the modal
// and return to Normal mode; e copies the loaded email (the same action
// a click on the 📋 beside it synthesises). Every other key is
// swallowed, since the dialog is read-only and has no scroll state of
// its own.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

func handleUserProfileMode(a *App, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "K", "esc", "q":
		a.userProfile.Close()
		a.SetMode(ModeNormal)
	case "e":
		return copyProfileEmail(a)
	}
	return nil
}

// copyProfileEmail writes the dialog's loaded email to the clipboard
// and toasts the outcome. The dialog stays open.
func copyProfileEmail(a *App) tea.Cmd {
	email := a.userProfile.Email()
	if email == "" {
		return toastWithClear(a, "No email to copy", 2*time.Second)
	}
	return tea.Batch(
		a.clipboardWrite(email),
		toastWithClear(a, "Copied email", 2*time.Second),
	)
}
