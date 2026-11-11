// View runs once per frame while the picker is open; SetQuery once per
// keystroke typed after the "#". Both report allocations so a later change to
// either is visible.
//
// View has no cache: every frame rebuilds every visible row (label, style
// render, marker) and the popup. The rows are capped at MaxVisible, so View
// does not scale with the number of channels; the scaling benchmarks show
// that. There is no warm/cold split in the usual sense: "warm" repeats the
// same input and "cold" moves the selected row each frame, which changes which
// row takes the selected style.
//
// SetQuery runs filter, which folds the query and every channel name and
// scans all channels, so it is the part expected to scale with the channel
// count. The "few" query matches one channel, "many" matches all of them (the
// result is then cut to MaxVisible), and "none" matches nothing.
package channelpicker

import (
	"fmt"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/chrome"
)

// benchSink keeps results alive so the compiler cannot drop the calls.
var (
	benchSink       string
	benchResultSink *ChannelResult
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
			Box:         lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted).BorderBackground(bg).Background(bg).Foreground(fg).Padding(0, 1),
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
		Name: lipgloss.NewStyle().Foreground(fg).Background(bg),
	}
}

// benchChannels builds n synthetic channels named chan-0000, chan-0001, ...
// with the four types mixed.
func benchChannels(n int) []Channel {
	types := []string{"channel", "private", "dm", "group_dm"}
	chs := make([]Channel, n)
	for i := range chs {
		chs[i] = Channel{
			ID:   fmt.Sprintf("C%05d", i),
			Name: fmt.Sprintf("chan-%04d", i),
			Type: types[i%len(types)],
		}
	}
	return chs
}

// benchModel returns an open picker holding n channels and showing query.
func benchModel(n int, query string) *Model {
	m := New(WithStyles(benchStyles()), WithWidth(40))
	m.SetChannels(benchChannels(n))
	m.Open()
	if query != "" {
		m.SetQuery(query)
	}
	return &m
}

var benchSizes = []int{5, 50, 500}

func BenchmarkView(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("channels=%d", n), func(b *testing.B) {
			m := benchModel(n, "")
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
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("channels=%d", n), func(b *testing.B) {
			m := benchModel(n, "")
			rows := len(m.Filtered())
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				m.selected = i % rows
				i++
				benchSink = m.View()
			}
		})
	}
}

// BenchmarkViewFewMatches shows a query that leaves a single row.
func BenchmarkViewFewMatches(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("channels=%d", n), func(b *testing.B) {
			m := benchModel(n, "chan-0001")
			b.ReportAllocs()
			for b.Loop() {
				benchSink = m.View()
			}
		})
	}
}

// BenchmarkSetQuery runs the filter loop at each channel count, for a query
// matching one channel, all channels, and none.
func BenchmarkSetQuery(b *testing.B) {
	queries := []struct{ name, q string }{
		{"few", "chan-0001"},
		{"many", "chan-"},
		{"none", "zzz"},
	}
	for _, n := range benchSizes {
		for _, q := range queries {
			b.Run(fmt.Sprintf("channels=%d/%s", n, q.name), func(b *testing.B) {
				m := benchModel(n, "")
				b.ReportAllocs()
				for b.Loop() {
					m.SetQuery(q.q)
				}
			})
		}
	}
}

// BenchmarkSetChannels swaps the channel set; it stores the slice and does
// not copy or filter, so it is expected to be constant.
func BenchmarkSetChannels(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("channels=%d", n), func(b *testing.B) {
			chs := benchChannels(n)
			m := benchModel(n, "")
			b.ReportAllocs()
			for b.Loop() {
				m.SetChannels(chs)
			}
		})
	}
}

// BenchmarkMove alternates down and up so the selection moves on every call.
func BenchmarkMove(b *testing.B) {
	m := benchModel(500, "")
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		if i%2 == 0 {
			m.MoveDown()
		} else {
			m.MoveUp()
		}
		i++
	}
}

// BenchmarkSelect returns the highlighted channel; it allocates the result.
func BenchmarkSelect(b *testing.B) {
	m := benchModel(500, "")
	b.ReportAllocs()
	for b.Loop() {
		benchResultSink = m.Select()
	}
}
