// internal/ui/view_rail.go
//
// Workspace-rail renderer for App.View (Phase 6e).
//
// The rail is the narrow leftmost column showing one tile per
// configured workspace. It uses RailBackground rather than the
// default panel background so empty cells around the workspace
// tiles match the rail color (not the message pane).
//
// The output is cached on (workspaceRail.Version, railWidth,
// contentHeight, themeVer) -- a render-only keystroke (typing
// into compose) is a single cache hit. Rail content changes
// rarely (workspace-list mutation, presence dot flips), so the
// cache hit rate is near-100% in steady state.
package ui

import (
	"github.com/gammons/slk/internal/ui/styles"
)

// renderRail returns the composed workspace-rail panel,
// reading-or-storing through the panel-render cache. themeVer
// is the caller's snapshot of styles.Version() and serves as the
// cache's layoutKey.
func (a *App) renderRail(railWidth, contentHeight int, themeVer int64) string {
	// Focus changes at ~30 sites; syncing it here keeps the cursor
	// marker honest, and SetFocused bumps Version when it flips.
	a.workspaceRail.SetFocused(a.focusedPanel == PanelWorkspace)
	a.workspaceRail.SetHeight(contentHeight)
	c := &a.renderCache.rail
	if c.hit(a.workspaceRail.Version(), railWidth, contentHeight, themeVer) {
		return c.output
	}
	out := exactSizeBg(a.workspaceRail.View(contentHeight), railWidth, contentHeight, styles.RailBackground)
	c.store(out, a.workspaceRail.Version(), railWidth, contentHeight, themeVer)
	return out
}
