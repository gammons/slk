// internal/ui/reducer_theme.go
//
// Theme reducer for App.Update. Owns the message the theme-cycle keys
// produce after the press itself, and the reload of custom themes:
//
//	themeSaveDueMsg         - themeSaveDelay passed since a cycle press.
//	                          Save the pending theme, unless a later
//	                          press made this tick stale (theme.go).
//	CustomThemesReloadedMsg - the custom theme files changed. Register
//	                          them and reapply the selected theme.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/styles"
)

var reduceTheme reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case themeSaveDueMsg:
		if m.gen == a.themeSaveGen {
			a.SavePendingTheme()
		}
		return nil, true
	case CustomThemesReloadedMsg:
		styles.RegisterCustomThemes(m.Themes)
		a.SetThemeItems(styles.ThemeNames())
		// The name asked for, not the one on screen: a configured theme
		// whose file was missing at startup shows Dark until it appears.
		name := a.themeName
		if name == "" {
			name = styles.CurrentTheme()
		}
		a.applyTheme(name)
		return nil, true
	}
	return nil, false
}
