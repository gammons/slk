package thread

import (
	"fmt"
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
)

// BenchmarkViewScrollScale is BenchmarkViewScroll at 200 and 1000 replies:
// one j/k step plus View per op, to show whether per-frame cost follows the
// window or the reply count.
func BenchmarkViewScrollScale(b *testing.B) {
	for _, n := range []int{200, 1000} {
		b.Run(fmt.Sprintf("replies=%d", n), func(b *testing.B) {
			parent := messages.MessageItem{
				TS: "1700000000.000000", UserName: "alice", UserID: "U1",
				Text: "Parent message kicking off the thread", Timestamp: "10:00 AM",
			}
			replies := make([]messages.MessageItem, n)
			for i := range replies {
				replies[i] = messages.MessageItem{
					TS:        fmt.Sprintf("%d.000000", 1700000001+i),
					UserName:  "bob",
					UserID:    "U2",
					Text:      "Reply with **bold** _italic_ and a `code` snippet plus a longer trailing sentence.",
					Timestamp: "10:30 AM",
					ThreadTS:  parent.TS,
				}
			}
			m := New()
			m.SetThread(parent, replies, "C1", parent.TS)
			_ = m.View(40, 100) // prime caches

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if i%2 == 0 {
					m.MoveUp()
				} else {
					m.MoveDown()
				}
				_ = m.View(40, 100)
			}
		})
	}
}

// BenchmarkViewIdleScale is View with nothing changed, at the same sizes.
func BenchmarkViewIdleScale(b *testing.B) {
	for _, n := range []int{200, 1000} {
		b.Run(fmt.Sprintf("replies=%d", n), func(b *testing.B) {
			parent := messages.MessageItem{
				TS: "1700000000.000000", UserName: "alice", UserID: "U1",
				Text: "Parent message kicking off the thread", Timestamp: "10:00 AM",
			}
			replies := make([]messages.MessageItem, n)
			for i := range replies {
				replies[i] = messages.MessageItem{
					TS:        fmt.Sprintf("%d.000000", 1700000001+i),
					UserName:  "bob",
					UserID:    "U2",
					Text:      "Reply with **bold** _italic_ and a `code` snippet plus a longer trailing sentence.",
					Timestamp: "10:30 AM",
					ThreadTS:  parent.TS,
				}
			}
			m := New()
			m.SetThread(parent, replies, "C1", parent.TS)
			_ = m.View(40, 100)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = m.View(40, 100)
			}
		})
	}
}
