package scrollbar

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// overlayOracle is Overlay equivalent to its form before the pad was built
// from a cached prefix and suffix: every short row renders its own pad
// through lipgloss. It is the reference the new code must match byte for byte.
func overlayOracle(visible []string, width, total, yOffset, visibleHeight int, bg, trackFg, thumbFg color.Color) []string {
	if visibleHeight <= 0 || width < 1 {
		return visible
	}
	if total <= visibleHeight {
		return visible
	}
	thumbStart, thumbEnd := Thumb(total, yOffset, visibleHeight)
	thumbCell := lipgloss.NewStyle().Background(bg).Foreground(thumbFg).Render("\u2588")
	trackCell := lipgloss.NewStyle().Background(bg).Foreground(trackFg).Render("\u2502")
	gutterCol := max(width-1, 0)
	rowLimit := min(visibleHeight, len(visible))
	for i := 0; i < rowLimit; i++ {
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
		glyph := trackCell
		if i >= thumbStart && i < thumbEnd {
			glyph = thumbCell
		}
		visible[i] = prefix + glyph
	}
	return visible
}

func TestOverlayMatchesOracle(t *testing.T) {
	bgs := map[string]color.Color{
		"hex":     lipgloss.Color("#123456"),
		"ansi":    lipgloss.Color("5"),
		"nil":     nil,
		"nocolor": lipgloss.NoColor{},
	}
	var widths []int
	for w := 1; w <= 200; w++ {
		widths = append(widths, w)
	}
	const total = 40
	for name, bgc := range bgs {
		for _, width := range widths {
			rows := []string{
				"",
				"short",
				strings.Repeat("x", max(width-1, 0)),
				strings.Repeat("y", width+7),
				"\x1b[31mred\x1b[0m",
				"\x1b[1;38;2;1;2;3m" + strings.Repeat("z", max(width-1, 0)) + "\x1b[0m",
				"\x1b[32m" + strings.Repeat("w", width+3) + "\x1b[0m",
				"\u4f60\u597d",
				"\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\",
				"\x1b]8;;https://example.com\x07" + strings.Repeat("l", width+2) + "\x1b]8;;\x07",
				"\U0001F600 smile \U0001F44D\U0001F3FD",
				strings.Repeat("\u4f60", width/2+1),
				"e\u0301 \U0001F468\u200d\U0001F469\u200d\U0001F467",
			}
			in := rows
			for _, off := range []int{0, (total - len(in)) / 2, total - len(in)} {
				got := Overlay(append([]string(nil), in...), width, total, off, len(in), bgc, track, thumb)
				want := overlayOracle(append([]string(nil), in...), width, total, off, len(in), bgc, track, thumb)
				for i := range want {
					if got[i] != want[i] {
						t.Fatalf("bg=%s width=%d off=%d row %d:\n got  %q\n want %q", name, width, off, i, got[i], want[i])
					}
				}
			}
		}
	}
}

func TestPadCellsMatchesRender(t *testing.T) {
	for _, bgc := range []color.Color{lipgloss.Color("#abcdef"), nil} {
		var c padCache
		// Overlay only pads rows narrower than the gutter, so n >= 1.
		// (lipgloss renders Width(0) as a bare SGR pair, which the cache
		// is never asked to reproduce.)
		for n := 1; n <= 200; n++ {
			want := lipgloss.NewStyle().Background(bgc).Width(n).Render("")
			if got := padCells(&c, bgc, n); got != want {
				t.Fatalf("bg=%v n=%d: got %q want %q", bgc, n, got, want)
			}
		}
	}
}
