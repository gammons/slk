// internal/ui/mode_app_view.go
//
// Key handler for a modal an app opened. The dialog scrolls like the
// messages pane, b raises its button chooser, and esc (or q) dismisses
// it locally.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

func handleAppViewMode(a *App, msg tea.KeyMsg) tea.Cmd {
	if a.appView.Current() == nil {
		// The view is gone (a refresh found none): do not trap the keys.
		a.SetMode(ModeNormal)
		return nil
	}
	switch msg.Key().Code {
	case tea.KeyEscape:
		a.closeAppView()
		return nil
	case tea.KeyUp:
		a.appView.ScrollUp(1)
		return nil
	case tea.KeyDown:
		a.appView.ScrollDown(1)
		return nil
	case tea.KeyPgUp:
		a.appView.ScrollUp(a.appView.PageSize())
		return nil
	case tea.KeyPgDown, tea.KeySpace:
		a.appView.ScrollDown(a.appView.PageSize())
		return nil
	case tea.KeyHome:
		a.appView.GoToTop()
		return nil
	case tea.KeyEnd:
		a.appView.GoToBottom()
		return nil
	case tea.KeyEnter:
		if v := a.appView.Current(); v != nil && v.SubmitLabel != "" {
			// The dialog has a form to submit, which slk cannot fill
			// in yet. Say so rather than swallow the key.
			return toastWithClear(a, "slk can't submit this dialog yet", 3*time.Second)
		}
		return nil
	}
	switch msg.String() {
	case "q":
		a.closeAppView()
	case "j":
		a.appView.ScrollDown(1)
	case "k":
		a.appView.ScrollUp(1)
	case "g":
		a.appView.GoToTop()
	case "G":
		a.appView.GoToBottom()
	case "b":
		return a.openAppViewActions()
	}
	return nil
}
