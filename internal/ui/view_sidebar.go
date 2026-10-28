// internal/ui/view_sidebar.go
//
// Sidebar renderer for App.View (Phase 6f).
//
// Renders the channel-list sidebar with its rounded border and
// SidebarBackground panel color. Background matters: themes with
// a distinct dark sidebar (e.g. Slack Default) need both the
// rounded border's own background AND the right-padding fill to
// match the sidebar's panel color rather than the message pane's
// -- otherwise the seam between sidebar and message pane shows a
// jarring color band.
//
// SetFocused MUST run BEFORE the panel-cache hit-check: it bumps
// the sidebar's Version on a focus flip, and the cache key
// includes Version. Without this ordering, a focus change would
// be silently dropped by a stale cache hit.
//
// Layout key mixes themeVer << 1 with the focused bit so a theme
// swap OR a focus flip invalidates the cached output; no other
// inputs affect the rendered string.
package ui

import (
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/ui/styles"
)

// renderSidebar returns the composed sidebar panel string,
// reading-or-storing through the panel-render cache. Caller is
// responsible for gating on a.sidebarVisible before invoking;
// this helper does not check visibility.
//
// As a side effect, pushes the resolved sidebar height back into
// a.layout (used by panelAt for mouse hit-testing).
func (a *App) renderSidebar(sidebarWidth, sidebarBorder, contentHeight int, themeVer int64) string {
	sbFocused := a.focusedPanel == PanelSidebar && a.mode != ModeInsert
	// Push focus into the sidebar so the cursor "▌" glyph dims
	// when the panel is unfocused. MUST happen BEFORE the
	// panelCache hit-check below: SetFocused bumps Version on a
	// flip and the cache key includes that version.
	a.sidebar.SetFocused(sbFocused)
	sbLayoutKey := themeVer<<1 | boolToInt(sbFocused)
	mainRows, dmRows, split := a.sidebarSplit(sidebarWidth, contentHeight)
	if split {
		a.layout.SetSidebarHeight(mainRows)
	} else {
		a.layout.SetSidebarHeight(contentHeight - 2)
	}

	c := &a.renderCache.sidebar
	if c.hit(a.sidebar.Version(), sidebarWidth, contentHeight, sbLayoutKey) {
		return c.output
	}

	box := func(focused bool) lipgloss.Style {
		if focused {
			return lipgloss.NewStyle().
				BorderStyle(lipgloss.ThickBorder()).
				BorderForeground(styles.Primary).
				BorderBackground(styles.SidebarBackground).
				Background(styles.SidebarBackground).
				Width(sidebarWidth)
		}
		return lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(styles.Border).
			BorderBackground(styles.SidebarBackground).
			Background(styles.SidebarBackground).
			Width(sidebarWidth)
	}
	var sidebarView string
	if split {
		top, dms := a.sidebar.ViewSplit(mainRows, dmRows, sidebarWidth)
		inDMs := a.sidebar.CursorInDMPane()
		sidebarView = box(sbFocused && !inDMs).Render(top) + "\n" + box(sbFocused && inDMs).Render(dms)
	} else {
		sidebarView = box(sbFocused).Render(a.sidebar.View(contentHeight-2, sidebarWidth))
	}
	out := exactSizeBg(sidebarView, sidebarWidth+sidebarBorder, contentHeight, styles.SidebarBackground)
	c.store(out, a.sidebar.Version(), sidebarWidth, contentHeight, sbLayoutKey)
	return out
}

// sidebarSplit returns the content heights of the channel box and the
// Direct Messages box when [sidebar] dm_pane is on, there are DMs to show
// and the column is tall enough for both; ok is false otherwise. The DM
// box fits its rows, between 3 and two fifths of the column.
func (a *App) sidebarSplit(sidebarWidth, contentHeight int) (mainRows, dmRows int, ok bool) {
	rows := a.sidebar.DMPaneRows(sidebarWidth)
	inner := contentHeight - 4 // two boxes, two border rows each
	if rows == 0 || inner < 8 {
		return 0, 0, false
	}
	dmRows = min(rows, max(3, inner*2/5))
	return inner - dmRows, dmRows, true
}
