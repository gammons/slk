package threadsview

import (
	"fmt"
	"testing"

	"github.com/gammons/slk/internal/cache"
)

// BenchmarkViewScroll simulates j/k scrolling through a long threads list
// where only m.selected changes between View() calls.
func BenchmarkViewScroll(b *testing.B) {
	summaries := make([]cache.ThreadSummary, 200)
	for i := range summaries {
		summaries[i] = cache.ThreadSummary{
			ChannelID:    fmt.Sprintf("C%03d", i),
			ChannelName:  fmt.Sprintf("ch-%03d", i),
			ChannelType:  "channel",
			ThreadTS:     fmt.Sprintf("%d.000000", 1700000000+i),
			ParentUserID: "U1",
			ParentText:   "Parent text with **bold** and `code` and <@U2> mention; medium length.",
			ParentTS:     fmt.Sprintf("%d.000000", 1700000000+i),
			ReplyCount:   3 + i%5,
			LastReplyTS:  fmt.Sprintf("%d.000000", 1700000100+i),
			LastReplyBy:  "U2",
			Unread:       i%4 == 0,
		}
	}
	m := New(map[string]string{"U1": "alice", "U2": "bob"}, "U1")
	m.SetSummaries(summaries)

	// Prime caches.
	_ = m.View(40, 80)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			m.MoveUp()
		} else {
			m.MoveDown()
		}
		_ = m.View(40, 80)
	}
}
