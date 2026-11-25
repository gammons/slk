package ansi

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"

	"github.com/gammons/slk/internal/emoji"
)

// Cells is the display width of s as both of its readers measure it:
// lipgloss, which wraps and pads, and emoji.Width, which knows an emoji
// image's footprint. The larger wins, so neither overflows.
func Cells(s string) int {
	return max(lipgloss.Width(s), emoji.Width(s))
}

// Truncate cuts s to at most budget cells, ending in "…" when it cuts.
// The cut is between grapheme clusters, never inside one, so a ZWJ emoji
// or a combining mark is kept or dropped whole. A string that already
// fits is returned unchanged. budget <= 0 returns "".
func Truncate(s string, budget int) string {
	if budget <= 0 {
		return ""
	}
	if Cells(s) <= budget {
		return s
	}
	var b strings.Builder
	used := 0
	rest := s
	state := -1
	for len(rest) > 0 {
		var cluster string
		cluster, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
		w := Cells(cluster)
		// Leave the last cell for the ellipsis.
		if used+w > budget-1 {
			break
		}
		b.WriteString(cluster)
		used += w
	}
	return b.String() + "…"
}
