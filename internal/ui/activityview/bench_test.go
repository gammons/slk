package activityview

import (
	"fmt"
	"testing"

	"github.com/gammons/slk/internal/core"
)

// manyItems returns n distinct activity items (cycling through the
// mention / thread / reaction / DM card shapes) and their hydrated
// bodies. Timestamps are far in the past so formatRelTime ("Nd ago") is
// stable across the renders a test compares.
func manyItems(n int) ([]core.ActivityItem, map[string]core.ActivityMessage) {
	types := []string{"at_user", "thread_v2", "message_reaction", "dm"}
	items := make([]core.ActivityItem, n)
	bodies := make(map[string]core.ActivityMessage, n)
	for i := range items {
		ch := fmt.Sprintf("C%04d", i%7)
		ts := fmt.Sprintf("%d.000000", 1700000000+i)
		items[i] = core.ActivityItem{
			Key:       fmt.Sprintf("k%04d", i),
			Type:      types[i%len(types)],
			IsUnread:  i%3 == 0,
			FeedTS:    ts,
			ChannelID: ch,
			TS:        ts,
			AuthorID:  "U1",
			Reaction:  "tada",
		}
		bodies[core.ActivityMsgKey(ch, ts)] = core.ActivityMessage{
			Text:   fmt.Sprintf("body %d with *bold* and `code` and <@U2> mention", i),
			UserID: []string{"U1", "USELF"}[i%2],
		}
	}
	return items, bodies
}

func newBenchModel(n int) Model {
	m := New(map[string]string{"U1": "alice", "U2": "bob"}, "USELF")
	m.SetChannelNames(map[string]string{"C0000": "general", "C0001": "design"})
	m.SetChannelTypes(map[string]string{"C0001": "private", "C0003": "dm"})
	items, bodies := manyItems(n)
	m.SetItems(items)
	m.SetBodies(bodies)
	return m
}

// BenchmarkViewFocusFlip is the h/l focus toggle in the Activity view:
// each flip re-renders the panel. activity.feed pages are 50 items.
func BenchmarkViewFocusFlip(b *testing.B) {
	for _, size := range []struct{ h, w int }{{38, 120}, {119, 235}} {
		b.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(b *testing.B) {
			m := newBenchModel(50)
			_ = m.View(size.h, size.w)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.SetFocused(i%2 == 0)
				_ = m.View(size.h, size.w)
			}
		})
	}
}
