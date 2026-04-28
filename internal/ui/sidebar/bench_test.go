package sidebar

import (
	"fmt"
	"testing"
)

// BenchmarkViewScroll simulates rapid j/k scrolling through the channel list
// where only m.selected changes between View() calls.
func BenchmarkViewScroll(b *testing.B) {
	channels := make([]ChannelItem, 100)
	for i := range channels {
		channels[i] = ChannelItem{
			ID:          fmt.Sprintf("C%d", i),
			Name:        fmt.Sprintf("channel-%d", i),
			Type:        "channel",
			UnreadCount: i % 5,
		}
		if i%10 == 0 {
			channels[i].Type = "dm"
			channels[i].Presence = "active"
			channels[i].DMUserID = fmt.Sprintf("U%d", i)
		}
	}
	m := New(channels)

	// Prime cache.
	_ = m.View(40, 30)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			m.MoveDown()
		} else {
			m.MoveUp()
		}
		_ = m.View(40, 30)
	}
}
