package sidebar

import (
	"strings"
	"testing"
)

// The unread blue dot is "●" (U+25CF). Muted-with-unreads channels
// must NOT carry it — Slack's contract is "muted = no notification
// surface". The dimmer ChannelMuted style is what tells the user a
// muted row has activity, not the dot.
func TestMutedChannel_SuppressesUnreadDot(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "noisy", Type: "channel", UnreadCount: 5, IsMuted: true},
	})
	m.ToggleCollapse("Channels") // expand so the row renders
	view := m.View(10, 30)

	var line string
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "noisy") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("noisy row not rendered:\n%s", view)
	}
	if strings.Contains(line, "●") {
		t.Errorf("muted channel rendered an unread dot:\n%q", line)
	}
}

// Sanity: an unmuted channel with the same unread count *does* get
// the dot. Guards against the suppression accidentally firing for
// every row.
func TestUnmutedChannel_StillGetsUnreadDot(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "noisy", Type: "channel", UnreadCount: 5, IsMuted: false},
	})
	m.ToggleCollapse("Channels")
	view := m.View(10, 30)

	var line string
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "noisy") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("noisy row not rendered:\n%s", view)
	}
	if !strings.Contains(line, "●") {
		t.Errorf("unmuted channel with unreads is missing the dot:\n%q", line)
	}
}

// Aggregate badge on a collapsed section header sums per-row unread
// counts. Muted channels are explicitly excluded so the badge matches
// the per-row treatment (no dot, dim foreground).
func TestAggregateBadge_ExcludesMutedChannels(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel", UnreadCount: 2, IsMuted: false},
		{ID: "C2", Name: "noisy", Type: "channel", UnreadCount: 5, IsMuted: true},
		{ID: "C3", Name: "alerts", Type: "channel", UnreadCount: 3, IsMuted: false},
	})
	// Collapsed by default; aggregate badge should be 2+3=5 (excluding
	// the muted C2's contribution of 5).
	view := m.View(15, 30)
	if !strings.Contains(view, "•5") {
		t.Errorf("expected aggregate badge •5 (muted excluded), got:\n%s", view)
	}
	if strings.Contains(view, "•10") {
		t.Errorf("aggregate badge counted muted channel toward total:\n%s", view)
	}
}

// When every channel in a section is muted, the collapsed header's
// aggregate badge should disappear entirely. Guards against rendering
// a stray "•0" (or worse, the unmuted code path).
func TestAggregateBadge_AllMutedDropsBadge(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "noisy", Type: "channel", UnreadCount: 9, IsMuted: true},
		{ID: "C2", Name: "spammy", Type: "channel", UnreadCount: 4, IsMuted: true},
	})
	view := m.View(15, 30)
	// Find the Channels header line.
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "Channels") {
			if strings.Contains(l, "•") {
				t.Errorf("collapsed header showed a badge despite every channel being muted:\n%q", l)
			}
			return
		}
	}
}
