package sidebar

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
)

// MentionTotal adds up the badges: a muted channel's mention counts, a
// plain unread adds nothing, and a stale count on a read channel is
// ignored, the same as the rows.
func TestMentionTotal(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "one", Type: "channel", Section: "Eng"},
		{ID: "C2", Name: "two", Type: "channel", Section: "Eng"},
		{ID: "C3", Name: "muted", Type: "channel", Section: "Eng", IsMuted: true},
		{ID: "D1", Name: "dm", Type: "dm", Section: "Eng"},
		{ID: "D2", Name: "read dm", Type: "dm", Section: "Eng"},
	})
	if got := m.MentionTotal(); got != 0 {
		t.Fatalf("no reader: MentionTotal = %d, want 0", got)
	}
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true, MentionCount: 2},
			"C2": {HasUnread: true},
			"C3": {HasUnread: true, MentionCount: 1},
			"D1": {HasUnread: true, MentionCount: 4},
			"D2": {MentionCount: 9},
		}
	})
	if got := m.MentionTotal(); got != 7 {
		t.Errorf("MentionTotal = %d, want 7", got)
	}
}
