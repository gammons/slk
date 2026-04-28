package messages

import (
	"fmt"
	"testing"
)

// BenchmarkViewScroll simulates rapid j/k scrolling: a message pane with many
// messages where only m.selected changes between View() calls. This is the hot
// path the user complained about.
func BenchmarkViewScroll(b *testing.B) {
	msgs := make([]MessageItem, 200)
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

	// Prime caches.
	_ = m.View(40, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			m.MoveUp()
		} else {
			m.MoveDown()
		}
		_ = m.View(40, 100)
	}
}
