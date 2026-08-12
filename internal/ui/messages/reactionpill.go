// internal/ui/messages/reactionpill.go
//
// Shared reaction-pill text construction (issue #133).
//
// The messages pane and the thread panel each render their own copy of
// the reaction-pill loop. The kitty-placement fix below has to be
// present in both or the bug returns in whichever one drifts, so the
// logic lives here and both call it.
package messages

import (
	"strconv"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ReactionPillText builds a pill's inner text: the emoji followed by
// its count.
//
// placedAsImage reports whether emojiStr is a kitty image placement
// rather than a glyph. A placement encodes the image ID in the
// foreground color and ends by resetting it (\e[39m, fg → default),
// which wipes the pill style's own foreground before the count digit
// prints — leaving the count uncolored while the rest of the pill
// keeps its color. Re-asserting the style's foreground between the
// placement and the count restores it.
//
// No nil check on the foreground: lipgloss returns black rather than
// nil for a style that never set one, so a guard would be dead code.
// Every pill style sets a foreground anyway.
func ReactionPillText(emojiStr string, count int, style lipgloss.Style, placedAsImage bool) string {
	if placedAsImage {
		return emojiStr + ansi.Style{}.ForegroundColor(style.GetForeground()).String() + strconv.Itoa(count)
	}
	return emojiStr + strconv.Itoa(count)
}
