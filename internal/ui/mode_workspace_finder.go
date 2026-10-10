// internal/ui/mode_workspace_finder.go
//
// Workspace-finder mode key handler (Phase 5e).
//
// Forwards normalised keys to the workspace finder. On a result
// (Enter on a selected workspace), closes the finder and -- if
// the selection is a different workspace -- dispatches the
// workspace-switcher callback. Esc-driven close from the finder
// itself drops back to Normal.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

func handleWorkspaceFinderMode(a *App, msg tea.KeyMsg) tea.Cmd {
	keyStr := msg.String()
	switch msg.Key().Code {
	case tea.KeyEnter:
		keyStr = "enter"
	case tea.KeyEscape:
		keyStr = "esc"
	case tea.KeyUp:
		keyStr = "up"
	case tea.KeyDown:
		keyStr = "down"
	case tea.KeyBackspace:
		keyStr = "backspace"
	}

	result := a.workspaceFinder.HandleKey(keyStr)
	if result != nil {
		if a.compose.Uploading() || a.threadCompose.Uploading() {
			return a.uploadToastCmd("Upload in progress", 2*time.Second)
		}
		a.workspaceFinder.Close()
		a.SetMode(ModeNormal)
		if a.workspaceSvc != nil && result.ID != a.workspaceRail.SelectedID() {
			switcher := a.workspaceSvc
			teamID := result.ID
			return func() tea.Msg {
				return switcher.Switch(teamID)
			}
		}
	}
	if !a.workspaceFinder.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return nil
}
