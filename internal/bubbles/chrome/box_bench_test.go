package chrome

import (
	"fmt"
	"testing"
)

// benchBody builds n fake lines; with sgr each carries nested styled
// spans (and so resets), the way dialog rows do.
func benchBody(s Styles, n int, sgr bool) string {
	lines := make([]string, n)
	for i := range lines {
		text := fmt.Sprintf("row %03d fake channel topic", i)
		if sgr {
			lines[i] = s.Marker.Render("▌") + s.Row.Render(text) + s.Track.Render("│")
		} else {
			lines[i] = " " + text + "│"
		}
	}
	out := lines[0]
	for _, l := range lines[1:] {
		out += "\n" + l
	}
	return out
}

func BenchmarkBox(b *testing.B) {
	s := benchStyles()
	for _, n := range []int{1, 8, 200} {
		for _, sgr := range []bool{false, true} {
			name := "plain"
			if sgr {
				name = "sgr"
			}
			b.Run(fmt.Sprintf("lines=%d/%s", n, name), func(b *testing.B) {
				content := s.Title.Render("Dialog title") + "\n\n" + benchBody(s, n, sgr) + "\n\n" + s.Footer.Render("esc close")
				b.ReportAllocs()
				for b.Loop() {
					benchSink = Box(s, content, 60)
				}
			})
		}
	}
}
