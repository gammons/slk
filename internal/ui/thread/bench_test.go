package thread

import (
	"fmt"
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
)

// BenchmarkViewScroll simulates rapid j/k scrolling: a thread with many
// replies where only m.selected changes between View() calls. Mirrors
// internal/ui/messages/bench_test.go.
func BenchmarkViewScroll(b *testing.B) {
	parent := messages.MessageItem{
		TS: "1700000000.000000", UserName: "alice", UserID: "U1",
		Text: "Parent message kicking off the thread", Timestamp: "10:00 AM",
	}
	replies := make([]messages.MessageItem, 200)
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
