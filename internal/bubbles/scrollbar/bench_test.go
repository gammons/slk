package scrollbar

import (
	"fmt"
	"strings"
	"testing"
)

// benchRows builds n deterministic rows of mixed shape for a panel of the
// given width: full-width plain rows, wider rows that must be cut, short
// plain rows that take the pad path, and short rows carrying SGR.
func benchRows(n, width int) []string {
	rows := make([]string, n)
	for i := range rows {
		switch i % 4 {
		case 0: // exactly the gutter column: no cut, no pad
			rows[i] = strings.Repeat("x", width-1)
		case 1: // wider than the gutter column: Cut path
			rows[i] = strings.Repeat("y", width)
		case 2: // short plain row: pad path
			rows[i] = fmt.Sprintf("short row %d", i)
		default: // short row with SGR: pad path
			rows[i] = fmt.Sprintf("\x1b[1;31mbold %d\x1b[0m plain tail", i)
		}
	}
	return rows
}

func BenchmarkOverlay(b *testing.B) {
	positions := []struct {
		name string
		off  func(total, h int) int
	}{
		{"top", func(total, h int) int { return 0 }},
		{"mid", func(total, h int) int { return (total - h) / 2 }},
		{"bottom", func(total, h int) int { return total - h }},
	}
	for _, h := range []int{40, 100, 300} {
		for _, w := range []int{80, 200} {
			for _, p := range positions {
				b.Run(fmt.Sprintf("h%d/w%d/%s", h, w, p.name), func(b *testing.B) {
					src := benchRows(h, w)
					buf := make([]string, h)
					total := h * 4
					off := p.off(total, h)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						// Overlay rewrites its input; the copy is part of the
						// measured cost (one memmove of h string headers).
						copy(buf, src)
						Overlay(buf, w, total, off, h, bg, track, thumb)
					}
				})
			}
		}
	}
}

// BenchmarkOverlayNoOverflow is the early-return path, for reference.
func BenchmarkOverlayNoOverflow(b *testing.B) {
	rows := benchRows(40, 80)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Overlay(rows, 80, 40, 0, 40, bg, track, thumb)
	}
}

func BenchmarkThumb(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var s, e int
	for i := 0; i < b.N; i++ {
		s, e = Thumb(1200, i%900, 300)
	}
	_, _ = s, e
}
