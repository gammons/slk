// internal/ui/view_status.go
//
// Status row renderer for App.View (Phase 6d).
//
// The status row spans the bottom of the screen, composed of a
// rail-colored spacer matching the workspace rail's width plus
// the statusbar widget rendered at (a.width - railWidth). The
// composed row is cached on (statusbar.Version, statusWidth,
// themeVer) so a render-only keystroke (typing into compose) is
// a single cache hit rather than a re-join + style-walk.
package ui

import (
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/ui/styles"
)

// renderStatusRow returns the composed status row (rail-spacer +
// statusbar), reading-or-storing through the panel-render cache.
// themeVer is passed in so the call site stays the canonical
// source of theme freshness (App.View captures it once at the
// top of the render path).
func (a *App) renderStatusRow(railWidth, statusWidth int, themeVer int64) string {
	c := &a.renderCache.status
	if c.hit(a.statusbar.Version(), statusWidth, 1, themeVer) {
		return c.output
	}
	railSpacer := lipgloss.NewStyle().
		Width(railWidth).
		Background(styles.RailBackground).
		Render("")
	out := lipgloss.JoinHorizontal(lipgloss.Center, railSpacer, a.statusbar.View(statusWidth))
	c.store(out, a.statusbar.Version(), statusWidth, 1, themeVer)
	return out
}
