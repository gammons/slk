package ui

import (
	"hash/fnv"
	"strconv"
	"strings"

	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/wintree"
)

// This file contains the pure View-time sixel placement pipeline: it
// collects which images the current frame wants, converts pane-local
// coordinates into absolute 1-based terminal coordinates, and computes
// the frame guards that detect rewrites underneath an image.
//
// It owns no painter and no writer. Publication happens in
// finalizeSixelView, and the actual terminal writes happen later in
// internal/image's FrameOutput, after the exact Bubble Tea frame's text
// diff has been flushed.

// absoluteWindowSixelPlacements converts one window leaf's pane-local
// placements into absolute 1-based terminal coordinates. rect is the
// leaf's rectangle relative to the messages region; messagesRegionX is
// the absolute column where the messages region begins (the sidebar's
// end).
//
// Leaf-relative mapping (mirrors renderUnfocusedWindow / borderedPane):
//
//	baseX       = messagesRegionX + rect.X
//	contentLeft = baseX + 1                         // left border
//	contentTop  = rect.Y + 1 + chromeHeight         // top border + model chrome
//	right       = baseX + rect.W - 1                // right border starts here
//	bottom      = rect.Y + rect.H - 1               // bottom border starts here
//
// A placement is rejected when its footprint reaches past the leaf's
// right or bottom exclusive boundary — each window clips to its own
// border, not to a global pane edge or status bar. Guards are NOT
// attached here: finalizeSixelView hashes the complete screen string
// and stamps every placement after the frame exists.
func absoluteWindowSixelPlacements(
	paints []imgpkg.SixelPaint,
	chromeHeight int,
	rect wintree.Rect,
	messagesRegionX int,
) []imgpkg.SixelPlacement {
	baseX := messagesRegionX + rect.X
	contentLeft := baseX + 1
	contentTop := rect.Y + 1 + chromeHeight
	right := baseX + rect.W - 1
	bottom := rect.Y + rect.H - 1

	out := make([]imgpkg.SixelPlacement, 0, len(paints))
	for _, paint := range paints {
		if paint.Rows <= 0 || paint.Cols <= 0 {
			continue
		}
		col := contentLeft + paint.Col
		row := contentTop + paint.Row
		if col+paint.Cols > right || row+paint.Rows > bottom {
			continue
		}
		out = append(out, imgpkg.SixelPlacement{
			Key:   paint.Key,
			Row:   row + 1, // CUP is 1-based
			Col:   col + 1,
			Rows:  paint.Rows,
			Cols:  paint.Cols,
			Bytes: paint.Bytes,
		})
	}
	return out
}

// absolutePreviewSixelPlacement converts the preview overlay's
// panel-local placement into absolute 1-based terminal coordinates.
//
// Unlike the messages pane, the preview panel has no border of its own
// (see renderPreviewPanel: exactSize wraps the raw overlay content
// directly) and always starts at the top of the content area — its left
// edge is the same column the messages pane's left border sits at
// (a.layout.SidebarEnd()), just without the "+1 for the border" the
// messages pane needs. Preview.View() already fits the image inside its
// own bounds via fitInto, so unlike the messages pane there is no
// partial-visibility/scrolling case to clip against here.
func (a *App) absolutePreviewSixelPlacement(p imgpkg.SixelPaint) (imgpkg.SixelPlacement, bool) {
	if p.Rows <= 0 || p.Cols <= 0 {
		return imgpkg.SixelPlacement{}, false
	}
	left := a.layout.SidebarEnd() + p.Col
	top := p.Row

	return imgpkg.SixelPlacement{
		Key:   p.Key,
		Row:   top + 1, // CUP is 1-based
		Col:   left + 1,
		Rows:  p.Rows,
		Cols:  p.Cols,
		Bytes: p.Bytes,
	}, true
}

// frameGuard fingerprints the rows of the frame that sit under a
// placement, starting at 0-based screen row top.
//
// A change here means bubbletea rewrote those lines and painted over the
// image, so the painter must repaint even though the placement is
// otherwise identical. Hashing a handful of lines per frame is cheap
// next to re-emitting 60KB of sixel.
func frameGuard(screen string, top, rows int) string {
	if screen == "" {
		return ""
	}
	lines := strings.Split(screen, "\n")
	h := fnv.New64a()
	for r := top; r < top+rows && r < len(lines); r++ {
		h.Write([]byte(lines[r]))
		h.Write([]byte{0})
	}
	return strconv.FormatUint(h.Sum64(), 36)
}
