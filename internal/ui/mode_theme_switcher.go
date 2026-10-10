// internal/ui/mode_theme_switcher.go
//
// Theme-switcher mode key handler (Phase 5g).
//
// Forwards normalised keys to the theme-switcher overlay. On a
// result:
//   - Applies the theme immediately via App.applyTheme (theme.go), which
//     invalidates the render caches of messagepane / threadPanel /
//     sidebar and refreshes the components holding style snapshots
//     (compose / threadCompose, the confirm prompt).
//   - Forwards to themeSaveFn for persistence (per-workspace vs
//     global is encoded in result.Scope; a per-workspace save names
//     the workspace on screen).
package ui

import (
	tea "charm.land/bubbletea/v2"
)

func handleThemeSwitcherMode(a *App, msg tea.KeyMsg) tea.Cmd {
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

	result := a.themeSwitcher.HandleKey(keyStr)
	if result != nil {
		a.themeSwitcher.Close()
		a.SetMode(ModeNormal)
		a.applyTheme(result.Name)
		// Save selection, after any save a cycle left pending, so
		// this one is the last write.
		a.SavePendingTheme()
		if a.settings != nil {
			a.settings.SaveTheme(a.activeTeamID, result.Name, result.Scope)
		}
		return nil
	}
	if !a.themeSwitcher.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return nil
}
