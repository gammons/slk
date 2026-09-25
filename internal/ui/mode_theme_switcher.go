// internal/ui/mode_theme_switcher.go
//
// Theme-switcher mode key handler (Phase 5g).
//
// Forwards normalised keys to the theme-switcher overlay. On a
// result:
//   - Applies the theme immediately via styles.Apply.
//   - Invalidates the render caches of messagepane / threadPanel /
//     sidebar so they rebuild with the new theme colors on the
//     next View.
//   - Refreshes compose / threadCompose textarea styles.
//   - Forwards to themeSaveFn for persistence (per-workspace vs
//     global is encoded in result.Scope).
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/styles"
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
		// Apply theme immediately.
		styles.Apply(result.Name, a.themeOverrides)
		// Invalidate render caches so they rebuild with new theme colors.
		a.invalidateAllWinModelCaches()
		a.threadPanel.InvalidateCache()
		a.sidebar.InvalidateCache()
		// Refresh compose textarea styles for new theme.
		a.compose.RefreshStyles()
		a.threadCompose.RefreshStyles()
		// Save selection.
		if a.settings != nil {
			a.settings.SaveTheme(result.Name, result.Scope)
		}
		return nil
	}
	if !a.themeSwitcher.IsVisible() {
		a.SetMode(ModeNormal)
	}
	return nil
}
