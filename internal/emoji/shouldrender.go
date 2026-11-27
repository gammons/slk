package emoji

import (
	"strings"
	"sync/atomic"

	"github.com/rivo/uniseg"
)

// graphemeClusters records that the terminal reported unicode core
// mode (DECRQM 2027): it measures text by grapheme cluster, as lipgloss
// does, so a multi-codepoint emoji (base+VS16, ZWJ, flags, skin tones)
// takes the same cells on screen as in slk's width math.
//
// Process-global like the image mode (imagemode.go), because the glyph
// rule is read from render code with no App in reach. Set from the
// terminal-mode reducer when the report arrives; false until then.
var graphemeClusters atomic.Bool

// SetGraphemeClusters records whether the terminal measures text by
// grapheme cluster (mode 2027). It reports whether the value changed,
// so the caller knows to drop render caches built under the old rule.
func SetGraphemeClusters(on bool) bool {
	return graphemeClusters.Swap(on) != on
}

// GraphemeClusters reports whether the terminal measures text by
// grapheme cluster (see SetGraphemeClusters).
func GraphemeClusters() bool {
	return graphemeClusters.Load()
}

// ShouldRenderUnicode reports whether the resolved Unicode form of an
// emoji is safe to render as a glyph.
//
// By default it returns true iff the string contains exactly one
// codepoint. Any multi-codepoint sequence — including base+VS16 (`❤️`,
// `⚠️`, `🌩️`), ZWJ sequences, regional-indicator flag pairs, and
// skin-tone modifiers — returns false.
//
// VS16 (U+FE0F) was previously treated as a "safe composition" but
// field experience disagreed: a terminal measuring by wcwidth draws
// U+2764 at one cell and U+FE0F at zero, while lipgloss measures the
// cluster as two, breaking border alignment.
//
// When the terminal reported grapheme-cluster widths (mode 2027,
// GraphemeClusters), the terminal and lipgloss agree on every cluster,
// so any single grapheme cluster is safe: the whole emoji family draws
// as glyphs. A font without a color glyph then costs color, not layout.
//
// Trailing whitespace is ignored — Sprint appends a trailing space
// after each resolved emoji and callers occasionally pass that form.
// Empty strings (after trim) return false.
func ShouldRenderUnicode(s string) bool {
	s = strings.TrimRight(s, " \t")
	if s == "" {
		return false
	}
	if GraphemeClusters() {
		return uniseg.GraphemeClusterCount(s) == 1
	}
	return len([]rune(s)) == 1
}
