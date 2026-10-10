// internal/ui/theme.go
//
// Theme application shared by the theme switcher, the per-workspace
// theme on workspace switch, and the normal-mode theme-cycle keys
// (alt+y / alt+shift+y).
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/ui/themeswitcher"
)

// themeSaveDelay is how long a cycle waits after the last press before
// it saves. Each save rewrites config.toml and syncs it to disk on the
// Update goroutine, so holding alt+y must write once, for the theme it
// stops on, and not once for every theme it passes.
const themeSaveDelay = time.Second

// themeSaveDueMsg is the tick cycleTheme schedules. gen is the
// themeSaveGen of the press that scheduled it; a later press bumps the
// counter, so only the tick of the last press saves.
type themeSaveDueMsg struct{ gen int }

// pendingThemeSave is a theme a cycle applied but has not saved yet
// (zero when nothing is pending). teamID is the workspace it belongs
// to: the one on screen at the press. A switch moves the backend's
// active workspace before the UI shows the switch, so the save must
// not ask the backend which workspace is active when it runs.
type pendingThemeSave struct {
	name   string
	teamID string
}

// applyTheme applies the named theme with the config overrides, then
// invalidates the render caches of messagepane / threadPanel / sidebar
// and refreshes components holding style snapshots (compose textareas
// and the confirm prompt) so the next View uses the new theme colors.
// Every in-App theme change goes through here; add style pushes for new
// components here, not at each call site. It also clears a cycle's "Theme: …" toast
// that is still showing, because that toast names the theme this call
// replaces. A toast shown after it stays.
func (a *App) applyTheme(name string) {
	styles.Apply(name, a.themeOverrides)
	a.invalidateAllWinModelCaches()
	a.threadPanel.InvalidateCache()
	a.sidebar.InvalidateCache()
	a.compose.RefreshStyles()
	a.threadCompose.RefreshStyles()
	a.confirmPrompt.SetStyles(confirmPromptStyles())
	if a.themeToastSeq != 0 && a.statusbar.ToastSeq() == a.themeToastSeq {
		a.statusbar.SetToast("")
	}
}

// cycleTheme applies the theme step places from the current one, in
// the theme switcher's order and wrapping around, and shows its name as
// a status-bar toast. It saves the theme for the workspace on screen
// (the scope ctrl+y saves to) themeSaveDelay after the last press. With
// no workspace on screen yet there is nothing to save it for: the first
// workspace to connect applies its own theme.
func (a *App) cycleTheme(step int) tea.Cmd {
	name := a.themeSwitcher.Neighbor(styles.CurrentTheme(), step)
	if name == "" {
		return nil
	}
	a.applyTheme(name)
	toast := toastWithClear(a, "Theme: "+name, 2*time.Second)
	a.themeToastSeq = a.statusbar.ToastSeq()
	if a.activeTeamID == "" {
		return toast
	}
	// One pending save at a time: keep the choice of a workspace the
	// UI has left by saving it before this press replaces it.
	if a.pendingTheme.teamID != a.activeTeamID {
		a.SavePendingTheme()
	}
	a.pendingTheme = pendingThemeSave{name: name, teamID: a.activeTeamID}
	a.themeSaveGen++
	gen := a.themeSaveGen
	return tea.Batch(toast, tea.Tick(themeSaveDelay, func(time.Time) tea.Msg {
		return themeSaveDueMsg{gen: gen}
	}))
}

// SavePendingTheme saves the theme a cycle left pending, if any, for
// the workspace it belongs to. Its callers are the save tick; a later
// press for another workspace; switchWorkspace, so a switch back to
// that workspace reads the new theme; the theme switcher, so its own
// save lands last; and cmd/slk after the program exits, so quitting
// within themeSaveDelay of the last press keeps the theme.
func (a *App) SavePendingTheme() {
	p := a.pendingTheme
	if p.name == "" {
		return
	}
	a.pendingTheme = pendingThemeSave{}
	if a.settings != nil {
		a.settings.SaveTheme(p.teamID, p.name, themeswitcher.ScopeWorkspace)
	}
}
