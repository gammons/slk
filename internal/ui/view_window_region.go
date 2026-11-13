// internal/ui/view_window_region.go
//
// Multi-window messages-region renderer (window-management design
// §6, Phase 3). With a single window the existing renderMessagesRegion
// path runs untouched — identical output, identical caching. With
// splits, the wintree layout is walked recursively: the FOCUSED
// window renders the real (cached) messages panel sized to its rect;
// every other window renders a live, read-only pane from its own
// per-window model (a.winModels), cached per window in
// renderCache.winPanes.
//
// NAMING: this file would naturally be view_windows.go, but a
// `_windows` filename suffix is a GOOS build constraint — Go would
// silently exclude the file from every non-Windows build (it lands in
// IgnoredGoFiles, no error). Hence view_window_region.go.
package ui

import (
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/ui/wintree"
)

// renderWindowsRegion is the messages-region entry point called from
// App.View. Preview mode and the single-window tree delegate to the
// existing path unchanged.
func (a *App) renderWindowsRegion(frame panelLayoutFrame, themeVer int64, previewActive bool) string {
	if previewActive || a.wins.Len() == 1 {
		return a.renderMessagesRegion(frame, themeVer, previewActive)
	}
	bounds := wintree.Rect{X: 0, Y: 0, W: frame.MsgWidth + frame.MsgBorder, H: frame.ContentHeight}
	return a.renderWindowNode(a.wins.Layout(bounds), frame, themeVer)
}

// renderWindowNode renders one layout-tree node to a string of
// exactly Rect.W x Rect.H cells. Callers guarantee Rect.W >= 1 and
// Rect.H >= 1 (zero-extent children are skipped below). exactSize
// enforces a minimum width and exact height at every leaf; widths
// can only exceed a rect on the sub-minimum focused leaf, which
// hard-truncates back via MaxWidth. Sub-minimum windows thus degrade
// to garbled-but-correctly-sized cells instead of corrupting the
// region geometry.
func (a *App) renderWindowNode(n wintree.LayoutNode, frame panelLayoutFrame, themeVer int64) string {
	if n.Leaf {
		if n.ID == a.focusedWin {
			sub := frame
			sub.MsgWidth = n.Rect.W - 2
			// Floor: a hard shrink can leave rects narrower than the
			// panel chrome (W-2 < 2). Below 2, renderMessagesTop's
			// borderedTopPane computes strings.Repeat(top, MsgWidth-2)
			// with a negative count and panics (mirrors the
			// msgContentHeight < 3 clamp downstream). The over-wide
			// result is wrapped back to Rect.W by exactSize.
			if sub.MsgWidth < 2 {
				sub.MsgWidth = 2
			}
			sub.MsgBorder = 2
			sub.ContentHeight = n.Rect.H
			out := exactSize(a.renderMessagesRegion(sub, themeVer, false), n.Rect.W, n.Rect.H)
			if sub.MsgWidth+sub.MsgBorder > n.Rect.W {
				// The floored frame renders wider than the rect
				// (exactSize pads to a minimum width but never
				// shrinks). Hard-truncate each row back, ANSI-aware,
				// so the column tiling stays exact.
				out = lipgloss.NewStyle().MaxWidth(n.Rect.W).Render(out)
			}
			return out
		}
		return a.renderUnfocusedWindow(n, themeVer)
	}
	parts := make([]string, 0, len(n.Children))
	for _, c := range n.Children {
		// Zero-extent rects (more windows than rows/cols after a hard
		// shrink) contribute zero cells and are skipped: exactSize
		// treats a 0 dimension as "unset" and would render at natural
		// size, breaking the region dimension contract. childRects
		// extents sum exactly to the parent extent along the split
		// axis, so the remaining children still tile the parent.
		if c.Rect.W < 1 || c.Rect.H < 1 {
			continue
		}
		parts = append(parts, a.renderWindowNode(c, frame, themeVer))
	}
	if n.Dir == wintree.SplitSideBySide {
		return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderUnfocusedWindow renders a live, read-only pane for an
// unfocused window: dimmed border, channel content via ViewBare
// (whose chrome includes the channel-name header). No compose/typing
// rows (focused window only). Cached per window on
// (version, rect, themeVer).
func (a *App) renderUnfocusedWindow(n wintree.LayoutNode, themeVer int64) string {
	m := a.winModels[n.ID]
	if m == nil || n.Rect.W < 4 || n.Rect.H < 4 {
		return a.renderBlankWindow(n)
	}
	m.SetFocused(false) // before Version read; bumps only on flip
	m.SetCursorHighlight(a.mode != ModeInsert)
	c := a.renderCache.getWinPane(n.ID)
	key := themeVer << 1
	if c.hit(m.Version(), n.Rect.W, n.Rect.H, key) {
		return c.output
	}
	// borderedPane (NOT a lipgloss border render): lipgloss v2's
	// Style.Width is border-inclusive, so the old
	// UnfocusedBorder.Width(W-2).Render(view) gave the model's
	// exactly (W-2)-cell lines a (W-4)-cell budget — every line with
	// content in its last 2 cells (e.g. ALL of them when the
	// scrollbar shows) re-wrapped, garbling the pane. borderedPane
	// assembles the border by concatenation around the model's
	// width-padded lines and emits exactly Rect.W x Rect.H cells, so
	// no exactSize pass is needed either.
	innerW := n.Rect.W - 2
	contentH := n.Rect.H - 2
	view := m.ViewBare(contentH, innerW)
	view = messages.ReapplyBgAfterResets(view, messages.BgANSI())
	out := borderedPane(view, innerW, n.Rect.W, n.Rect.H, false, styles.Background)
	c.store(out, m.Version(), n.Rect.W, n.Rect.H, key)
	return out
}

// renderBlankWindow is the degenerate-rect fallback: a blank block of
// exactly Rect.W x Rect.H keeps the tiling intact (caller guarantees
// both >= 1; smaller rects can't fit border + content and would flow
// negative inner dims into the pane renderers).
func (a *App) renderBlankWindow(n wintree.LayoutNode) string {
	return exactSize("", n.Rect.W, n.Rect.H)
}
