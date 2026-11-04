package messages

import (
	"fmt"
	"testing"
)

// BenchmarkViewScrollScale extends BenchmarkViewScroll to larger channels to
// show whether per-frame cost stays proportional to the window or grows
// with the number of loaded messages. Each op is one j/k step plus View.
func BenchmarkViewScrollScale(b *testing.B) {
	for _, n := range []int{200, 2000, 10000} {
		b.Run(fmt.Sprintf("msgs=%d", n), func(b *testing.B) {
			msgs := make([]MessageItem, n)
			for i := range msgs {
				msgs[i] = MessageItem{
					TS:        fmt.Sprintf("%d.0", 1700000000+i),
					UserName:  "alice",
					UserID:    "U1",
					Text:      "Hello world this is a moderately long message with **bold** and _italic_ and a `code` snippet.",
					Timestamp: "10:30 AM",
				}
			}
			m := New(msgs, "general")
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
	for _, n := range []int{200, 2000, 10000} {
		b.Run(fmt.Sprintf("msgs=%d", n), func(b *testing.B) {
			msgs := make([]MessageItem, n)
			for i := range msgs {
				msgs[i] = MessageItem{
					TS:        fmt.Sprintf("%d.0", 1700000000+i),
					UserName:  "alice",
					UserID:    "U1",
					Text:      "Hello world this is a moderately long message with **bold** and _italic_ and a `code` snippet.",
					Timestamp: "10:30 AM",
				}
			}
			m := New(msgs, "general")
			_ = m.View(40, 100)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = m.View(40, 100)
			}
		})
	}
}
