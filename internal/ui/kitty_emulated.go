package ui

import (
	"github.com/charmbracelet/x/ansi"

	imgpkg "github.com/gammons/slk/internal/image"
)

// This file is the App's half of kitty emulated placement
// (imgpkg.KittyEmulated): on a terminal with kitty graphics but no
// unicode placeholders (WezTerm), every surface still draws kitty
// placeholder cells, and the frame exit resolves them here into a
// frame of plain spaces plus direct placements for FrameOutput to
// paint after the text diff.

// kittyFrameMemo caches the last resolved frame. finalizeSixelView runs
// on every View, including the screen-memo hits that hand back the
// identical screen string, so a steady frame must not be parsed again.
type kittyFrameMemo struct {
	in, out       string
	width, height int
	graphemeWidth bool
	placements    []imgpkg.SixelPlacement
	valid         bool
	// resolved counts the frames actually parsed, for tests.
	resolved int
	// resolver keeps its cell buffer between frames.
	resolver imgpkg.PlaceholderResolver
}

// kittyEmulated reports whether this App resolves kitty placeholders
// itself.
func (a *App) kittyEmulated() bool {
	return a.imgProtocol == imgpkg.ProtoKitty && imgpkg.KittyPlacementMode() == imgpkg.KittyEmulated
}

// resolveKittyFrame returns content with its placeholder cells blanked
// and the placements that paint them, guards already stamped. The
// returned slice is shared with the memo; the frame store clones it on
// Publish.
func (a *App) resolveKittyFrame(content string) (string, []imgpkg.SixelPlacement) {
	m := &a.kittyFrame
	if m.valid && m.in == content && m.width == a.width && m.height == a.height &&
		m.graphemeWidth == a.graphemeWidth {
		return m.out, m.placements
	}
	method := ansi.WcWidth
	if a.graphemeWidth {
		method = ansi.GraphemeWidth
	}
	out, rects := m.resolver.Resolve(content, a.width, a.height, method)
	placements := imgpkg.EmulatedPlacements(rects, imgpkg.KittyRendererInstance().Geometry)
	stampFrameGuards(out, placements)
	m.in, m.out = content, out
	m.width, m.height = a.width, a.height
	m.graphemeWidth = a.graphemeWidth
	m.placements = placements
	m.valid = true
	m.resolved++
	return out, placements
}
