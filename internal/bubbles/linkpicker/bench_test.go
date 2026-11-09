// View runs once per frame while the picker is open, HandleKey once per
// keystroke. Both report allocations so a later change to either is visible.
//
// View has no cache: every frame rebuilds every row (join, width
// measurement, truncation, style render) and the frame. There is therefore
// no warm/cold split in the usual sense; "warm" repeats the same input and
// "cold" moves the selected row each frame, which changes which row takes the
// selected style. The scaling benchmarks show the cost per item, and the
// long-row benchmark forces every row through the truncation path.
//
// HandleKey is a string switch; the benchmarks pin it at zero allocations.
package linkpicker

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/chrome"
)

// benchSink keeps results alive so the compiler cannot drop the calls.
var (
	benchSink     string
	benchItemSink Item
	benchOKSink   bool
)

// benchStyles mirrors a themed host: colours and a background on every
// style, and BaseANSI set, so the benchmarks pay for real SGR output.
func benchStyles() Styles {
	bg := lipgloss.Color("#1a1b26")
	fg := lipgloss.Color("#c0caf5")
	accent := lipgloss.Color("#7aa2f7")
	muted := lipgloss.Color("#565f89")
	return Styles{
		Chrome: chrome.Styles{
			Box:         lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted).BorderBackground(bg).Background(bg).Foreground(fg).Padding(1, 1),
			Title:       lipgloss.NewStyle().Foreground(accent).Background(bg).Bold(true),
			Row:         lipgloss.NewStyle().Foreground(fg).Background(bg),
			RowSelected: lipgloss.NewStyle().Foreground(bg).Background(accent),
			Marker:      lipgloss.NewStyle().Foreground(accent).Background(bg),
			Empty:       lipgloss.NewStyle().Foreground(muted).Background(bg),
			Footer:      lipgloss.NewStyle().Foreground(muted).Background(bg),
			Thumb:       lipgloss.NewStyle().Foreground(accent).Background(bg),
			Track:       lipgloss.NewStyle().Foreground(muted).Background(bg),
			BaseANSI:    "\x1b[48;2;26;27;38m",
		},
		Badge: lipgloss.NewStyle().Foreground(accent).Background(bg),
	}
}

// benchItems builds n synthetic links; every third is an in-app link and has
// a label, so rows mix the label, URL, detail and badge paths.
func benchItems(n int) []Item {
	items := make([]Item, n)
	for i := range items {
		it := Item{URL: fmt.Sprintf("https://host%d.example/path/%d", i, i)}
		if i%3 == 0 {
			it.Label = fmt.Sprintf("link label %d", i)
			it.InApp = true
		}
		if i%5 == 0 {
			it.Detail = "from the example fixture"
		}
		items[i] = it
	}
	return items
}

// benchLongItems builds n rows far wider than the box so each is truncated.
func benchLongItems(n int) []Item {
	items := make([]Item, n)
	for i := range items {
		items[i] = Item{
			Label:  strings.Repeat("überlange beschriftung ", 6),
			URL:    fmt.Sprintf("https://host%d.example/%s", i, strings.Repeat("segment/", 20)),
			Detail: strings.Repeat("detail ", 10),
			InApp:  i%2 == 0,
		}
	}
	return items
}

func benchModel(items []Item) *Model {
	m := New(WithStyles(benchStyles()), WithWidth(120))
	m.Open("Open link", items)
	return m
}

func BenchmarkView(b *testing.B) {
	for _, n := range []int{5, 200} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			m := benchModel(benchItems(n))
			b.ReportAllocs()
			for b.Loop() {
				benchSink = m.View()
			}
		})
	}
}

// BenchmarkViewCold moves the selected row every frame. There is no render
// cache to invalidate, so this differs from the warm case only in which row
// takes the selected style.
func BenchmarkViewCold(b *testing.B) {
	for _, n := range []int{5, 200} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			m := benchModel(benchItems(n))
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				m.selected = i % n
				i++
				benchSink = m.View()
			}
		})
	}
}

// BenchmarkViewLongRows sends every row through the truncation path.
func BenchmarkViewLongRows(b *testing.B) {
	for _, n := range []int{5, 200} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			m := benchModel(benchLongItems(n))
			b.ReportAllocs()
			for b.Loop() {
				benchSink = m.View()
			}
		})
	}
}

// BenchmarkHandleKeyMove alternates down and up so the selection moves on
// every call.
func BenchmarkHandleKeyMove(b *testing.B) {
	m := benchModel(benchItems(200))
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		key := "down"
		if i%2 == 1 {
			key = "up"
		}
		i++
		benchItemSink, benchOKSink = m.HandleKey(key)
	}
}

// BenchmarkHandleKeyEnter re-opens the picker each iteration, since enter
// closes it and drops the items. Open is inside the timed loop and is part of
// the number.
func BenchmarkHandleKeyEnter(b *testing.B) {
	items := benchItems(200)
	m := benchModel(items)
	b.ReportAllocs()
	for b.Loop() {
		m.Open("Open link", items)
		benchItemSink, benchOKSink = m.HandleKey("enter")
	}
}
