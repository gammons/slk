// Package scrollbar renders a 1-column proportional scrollbar gutter onto
// pre-rendered visible lines. It is the same look used by the modal
// overlays (channelfinder, workspacefinder, themeswitcher, reactionpicker):
// a Border-colored track (│) with a Primary-colored thumb (█) sized
// proportionally to the visible window, positioned proportionally to the
// scroll offset.
package scrollbar

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Overlay returns visible with a 1-column scrollbar overlaid onto the last
// display column of every line. Each line is truncated to width-1 columns
// and a track or thumb glyph is appended.
//
// total is the total number of scrollable lines in the underlying content.
// yOffset is the line index of the first visible row. visibleHeight is the
// number of visible rows (typically len(visible), but can be smaller if the
// caller wants to leave trailing rows alone).
//
// When total <= visibleHeight (no overflow) the function returns visible
// unchanged: callers should decide separately whether to reserve a gutter.
//
// width is the full panel content width (the last column is replaced by the
// scrollbar). bg is the panel background color used to keep the gutter cell
// flush with the surrounding theme; trackFg / thumbFg control the track and
// thumb glyph colors.
func Overlay(visible []string, width, total, yOffset, visibleHeight int, bg, trackFg, thumbFg color.Color) []string {
	if visibleHeight <= 0 || width < 1 {
		return visible
	}
	if total <= visibleHeight {
		return visible
	}

	// Proportional thumb sizing: thumbHeight = visible^2 / total, clamped
	// to [1, visibleHeight]. Same formula as the modal overlays.
	thumbHeight := visibleHeight * visibleHeight / total
	if thumbHeight < 1 {
		thumbHeight = 1
	}
	if thumbHeight > visibleHeight {
		thumbHeight = visibleHeight
	}
	denom := total - visibleHeight
	if denom < 1 {
		denom = 1
	}
	thumbStart := yOffset * (visibleHeight - thumbHeight) / denom
	if thumbStart < 0 {
		thumbStart = 0
	}
	if thumbStart > visibleHeight-thumbHeight {
		thumbStart = visibleHeight - thumbHeight
	}
	thumbEnd := thumbStart + thumbHeight

	thumbStyle := lipgloss.NewStyle().Background(bg).Foreground(thumbFg)
	trackStyle := lipgloss.NewStyle().Background(bg).Foreground(trackFg)
	thumbCell := thumbStyle.Render("\u2588") // █
	trackCell := trackStyle.Render("\u2502") // │

	gutterCol := width - 1
	if gutterCol < 0 {
		gutterCol = 0
	}

	rowLimit := visibleHeight
	if rowLimit > len(visible) {
		rowLimit = len(visible)
	}

	for i := 0; i < rowLimit; i++ {
		// Truncate this row to the gutter column. ansi.Cut preserves
		// SGR state; rows shorter than gutterCol are padded by appending
		// background-colored spaces so the scrollbar lands at a consistent
		// column across rows of varying width.
		line := visible[i]
		w := ansi.StringWidth(line)
		var prefix string
		switch {
		case w > gutterCol:
			prefix = ansi.Cut(line, 0, gutterCol)
		case w == gutterCol:
			prefix = line
		default:
			pad := lipgloss.NewStyle().Background(bg).Width(gutterCol - w).Render("")
			prefix = line + pad
		}

		var glyph string
		if i >= thumbStart && i < thumbEnd {
			glyph = thumbCell
		} else {
			glyph = trackCell
		}
		visible[i] = prefix + glyph
	}
	return visible
}

// Visible reports whether a scrollbar should be drawn for the given content
// and viewport size. Callers can use this to decide whether to reserve a
// gutter or to skip the overlay entirely.
func Visible(total, visibleHeight int) bool {
	return total > visibleHeight
}
