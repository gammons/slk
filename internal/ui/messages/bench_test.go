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

// BenchmarkBuildCache measures a full cache build of a 200-message
// channel: the cost of every channel switch, SetMessages, width change and
// theme change.
func BenchmarkBuildCache(b *testing.B) {
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
	_ = m.View(40, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.buildCache(m.cacheWidth)
	}
}

// BenchmarkViewFocusFlip simulates the slow operations the user reported:
// pressing i / arrow keys / opening or closing the thread panel all flip
// the messages-pane focus bit, which (prior to the partial-rebuild fix)
// invalidated the entire pre-rendered viewEntry cache and forced a full
// per-message re-render of every loaded message.
//
// Expected costs on a 200-message channel (matches the [perf] trace at
// docs/perf notes from 2026-05-24):
//   - Pre-fix:  ~12 ms per message  -> ~600 ms / iter at N=50, much higher at N=200
//   - Post-fix: O(1) re-render of the selected row only -> sub-millisecond / iter
func BenchmarkViewFocusFlip(b *testing.B) {
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

	// Prime caches with focus=true (the steady-state after first paint).
	m.SetFocused(true)
	_ = m.View(40, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Flip true->false (mimics pressing `i` or Left/Right).
		m.SetFocused(false)
		_ = m.View(40, 100)
		// Flip false->true (mimics Esc or returning to messages pane).
		m.SetFocused(true)
		_ = m.View(40, 100)
	}
}
