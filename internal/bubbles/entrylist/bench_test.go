package entrylist

import (
	"fmt"
	"strings"
	"testing"
)

const (
	benchWidth  = 80
	benchHeight = 40
	benchRows   = 4 // rows per entry
)

var benchSizes = []int{10, 1000, 10000}

// benchEntries builds n selectable entries of benchRows pre-rendered rows
// each, with a little SGR in every row, like the panes hand over.
func benchEntries(n int) []Entry {
	es := make([]Entry, n)
	for i := range es {
		ls := make([]string, benchRows)
		for k := range ls {
			s := fmt.Sprintf("\x1b[1mbob\x1b[0m message %d row %d \x1b[3mitalic\x1b[0m text", i, k)
			ls[k] = s + strings.Repeat(" ", 20)
		}
		es[i] = Entry{
			ID: fmt.Sprintf("m%d", i), Ref: i, Selectable: true, Lines: ls,
			Plain: PlainLines(strings.Join(ls, "\n")),
		}
	}
	return es
}

func benchList(n int) *Model {
	m := New()
	m.SetHeight(benchHeight, n-1)
	m.SetEntries(benchEntries(n), benchWidth, false)
	return m
}

func frame(m *Model, cursor int, applySelection bool) {
	f := m.Layout(cursor, nil)
	_ = m.Compose(f, "", "", applySelection)
}

// BenchmarkLayoutCompose is one frame with an unmoved cursor mid-list.
func BenchmarkLayoutCompose(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("entries=%d", n), func(b *testing.B) {
			m := benchList(n)
			cur := n / 2
			frame(m, cur, false)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				frame(m, cur, false)
			}
		})
	}
}

// BenchmarkCursorStep is one j/k step plus its frame, mid-list.
func BenchmarkCursorStep(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("entries=%d", n), func(b *testing.B) {
			m := benchList(n)
			cur := n / 2
			frame(m, cur, false)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if i%2 == 0 {
					cur = min(cur+1, n-1)
				} else {
					cur = max(cur-1, 0)
				}
				frame(m, cur, false)
			}
		})
	}
}

// BenchmarkCursorStepAtBottom steps between the last two entries, so every
// frame snaps to the end of the list.
func BenchmarkCursorStepAtBottom(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("entries=%d", n), func(b *testing.B) {
			m := benchList(n)
			cur := n - 1
			frame(m, cur, false)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if i%2 == 0 {
					cur = max(n-2, 0)
				} else {
					cur = n - 1
				}
				frame(m, cur, false)
			}
		})
	}
}

// BenchmarkComposeWithSelection draws frames with a text selection spanning
// the window and applySelection set.
func BenchmarkComposeWithSelection(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("entries=%d", n), func(b *testing.B) {
			m := benchList(n)
			cur := n / 2
			frame(m, cur, false)
			y := m.YOffset()
			m.BeginSelection(y+2, 3)
			m.ExtendSelection(y+benchHeight-3, 30)
			if !m.HasSelection() {
				b.Fatal("selection not set")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				frame(m, cur, true)
			}
		})
	}
}
