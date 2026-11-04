package chrome

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// benchStyles is testStyles with the colours and background a themed
// dialog has, so the benchmarks pay for real SGR output.
func benchStyles() Styles {
	bg := lipgloss.Color("#1a1b26")
	s := testStyles()
	s.Box = s.Box.Background(bg).Foreground(lipgloss.Color("#c0caf5")).BorderForeground(lipgloss.Color("#565f89"))
	s.Title = s.Title.Foreground(lipgloss.Color("#7aa2f7")).Background(bg)
	s.Row = s.Row.Foreground(lipgloss.Color("#c0caf5")).Background(bg)
	s.RowSelected = s.RowSelected.Foreground(lipgloss.Color("#1a1b26")).Background(lipgloss.Color("#7aa2f7"))
	s.Marker = s.Marker.Foreground(lipgloss.Color("#7aa2f7")).Background(bg)
	s.Thumb = s.Thumb.Foreground(lipgloss.Color("#7aa2f7")).Background(bg)
	s.Track = s.Track.Foreground(lipgloss.Color("#565f89")).Background(bg)
	s.BaseANSI = "\x1b[48;2;26;27;38m"
	return s
}

const benchWindowRows = 12

var benchCursors = []struct {
	name string
	at   func(total int) int
}{
	{"top", func(int) int { return 0 }},
	{"middle", func(total int) int { return total / 2 }},
	{"end", func(total int) int { return total - 1 }},
}

// benchRawTexts is the unstyled fake content of the visible rows.
func benchRawTexts(start, end int) []string {
	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		rows = append(rows, fmt.Sprintf("channel-%04d  some fake topic text", i))
	}
	return rows
}

// benchRowTexts is benchRawTexts rendered and padded to width w, as the
// List.Rows contract requires.
func benchRowTexts(s Styles, w, start, end int) []string {
	rows := benchRawTexts(start, end)
	for i, t := range rows {
		rows[i] = s.Row.Width(w).Render(t)
	}
	return rows
}

var benchSink string
var benchSinkRows []string
var benchSinkInt int
var benchSinkBool bool

// BenchmarkList_Dialog is what a list dialog does per draw: Window, the
// styled row content for the window, then List.Rows.
func BenchmarkList_Dialog(b *testing.B) {
	s := benchStyles()
	for _, total := range []int{10, 100, 1000} {
		for _, c := range benchCursors {
			b.Run(fmt.Sprintf("items=%d/cursor=%s", total, c.name), func(b *testing.B) {
				sel := c.at(total)
				b.ReportAllocs()
				for b.Loop() {
					start, end := Window(sel, total, benchWindowRows)
					l := List{Start: start, End: end, Total: total}
					w := l.ContentWidth(40)
					texts := benchRawTexts(start, end)
					for k, t := range texts {
						st := s.Row
						if start+k == sel {
							st = s.RowSelected
						}
						texts[k] = st.Width(w).Render(t)
					}
					benchSinkRows = l.Rows(s, sel, texts)
				}
			})
		}
	}
}

// BenchmarkList_Rows isolates List.Rows over pre-rendered rows. "gutter"
// scrolls (Total > window); "nogutter" has Total equal to the window.
func BenchmarkList_Rows(b *testing.B) {
	s := benchStyles()
	cases := []struct {
		name  string
		total int
	}{
		{"nogutter", benchWindowRows},
		{"gutter/items=100", 100},
		{"gutter/items=1000", 1000},
	}
	for _, tc := range cases {
		for _, c := range benchCursors {
			b.Run(fmt.Sprintf("%s/cursor=%s", tc.name, c.name), func(b *testing.B) {
				sel := c.at(tc.total)
				start, end := Window(sel, tc.total, benchWindowRows)
				l := List{Start: start, End: end, Total: tc.total}
				in := benchRowTexts(s, l.ContentWidth(40), start, end)
				b.ReportAllocs()
				for b.Loop() {
					benchSinkRows = l.Rows(s, sel, in)
				}
			})
		}
	}
}

func BenchmarkList_Gutter(b *testing.B) {
	s := benchStyles()
	for _, total := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("items=%d", total), func(b *testing.B) {
			start, end := Window(total/2, total, benchWindowRows)
			l := List{Start: start, End: end, Total: total}
			b.ReportAllocs()
			for b.Loop() {
				for i := start; i < end; i++ {
					benchSink = l.Gutter(s, i)
				}
			}
		})
	}
}

func BenchmarkList_ContentWidthScrolls(b *testing.B) {
	l := List{Start: 10, End: 22, Total: 1000}
	b.ReportAllocs()
	for b.Loop() {
		benchSinkBool = l.Scrolls()
		benchSinkInt = l.ContentWidth(40)
	}
}

func BenchmarkWindow(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		benchSinkInt, _ = Window(500, 1000, benchWindowRows)
	}
}

func BenchmarkMarker(b *testing.B) {
	s := benchStyles()
	b.ReportAllocs()
	for b.Loop() {
		benchSink = Marker(s, true)
		benchSink = Marker(s, false)
	}
}

// BenchmarkFrame is a whole dialog frame built per draw: window, padded
// row content, List.Rows, filter input, footer, then Frame.Render.
func BenchmarkFrame(b *testing.B) {
	s := benchStyles()
	for _, total := range []int{10, 1000} {
		b.Run(fmt.Sprintf("items=%d", total), func(b *testing.B) {
			sel := total / 2
			b.ReportAllocs()
			for b.Loop() {
				start, end := Window(sel, total, benchWindowRows)
				l := List{Start: start, End: end, Total: total}
				body := l.Rows(s, sel, benchRowTexts(s, l.ContentWidth(40), start, end))
				input := FilterInput(s, "chan", "Search")
				footer := s.Footer.Render(strings.Repeat("k ", 8))
				f := Frame{Title: "Switch channel", Input: input, Body: body, Footer: footer}
				benchSink = f.Render(s, 48)
			}
		})
	}
}
