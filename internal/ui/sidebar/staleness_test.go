package sidebar

import (
	"testing"
	"time"
)

func TestIsStale(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	threshold := 30 * 24 * time.Hour

	tsAt := func(t time.Time) string {
		// Slack ts format: "<seconds>.<microseconds>"
		return formatSlackTS(t)
	}

	cases := []struct {
		name      string
		item      ChannelItem
		threshold time.Duration
		want      bool
	}{
		{
			name:      "fresh: read 1 day ago",
			item:      ChannelItem{ID: "C1", LastReadTS: tsAt(now.Add(-24 * time.Hour))},
			threshold: threshold,
			want:      false,
		},
		{
			name:      "stale: read 60 days ago",
			item:      ChannelItem{ID: "C1", LastReadTS: tsAt(now.Add(-60 * 24 * time.Hour))},
			threshold: threshold,
			want:      true,
		},
		{
			name:      "edge: read exactly 30 days ago is NOT stale",
			item:      ChannelItem{ID: "C1", LastReadTS: tsAt(now.Add(-30 * 24 * time.Hour))},
			threshold: threshold,
			want:      false,
		},
		{
			name:      "edge: read 30 days + 1 minute ago IS stale",
			item:      ChannelItem{ID: "C1", LastReadTS: tsAt(now.Add(-30*24*time.Hour - time.Minute))},
			threshold: threshold,
			want:      true,
		},
		{
			name:      "exception: unread > 0 is never stale",
			item:      ChannelItem{ID: "C1", LastReadTS: tsAt(now.Add(-90 * 24 * time.Hour)), UnreadCount: 1},
			threshold: threshold,
			want:      false,
		},
		{
			name:      "exception: custom-section channel is never stale",
			item:      ChannelItem{ID: "C1", LastReadTS: tsAt(now.Add(-90 * 24 * time.Hour)), Section: "Engineering"},
			threshold: threshold,
			want:      false,
		},
		{
			// Public/private channels always include a last_read in
			// client.counts, so an empty value here likely means
			// "brand-new join, counts hasn't refreshed yet" -- show
			// rather than hide.
			name:      "empty LastReadTS on public channel is NOT stale",
			item:      ChannelItem{ID: "C1", Type: "channel", LastReadTS: ""},
			threshold: threshold,
			want:      false,
		},
		{
			name:      "empty LastReadTS on private channel is NOT stale",
			item:      ChannelItem{ID: "C1", Type: "private", LastReadTS: ""},
			threshold: threshold,
			want:      false,
		},
		{
			// Slack's client.counts only returns dm/group_dm entries
			// for currently-open conversations. Absence (empty
			// LastReadTS) is the canonical "this conversation is
			// closed/stale" signal for these types.
			name:      "empty LastReadTS on dm IS stale",
			item:      ChannelItem{ID: "DM1", Type: "dm", LastReadTS: ""},
			threshold: threshold,
			want:      true,
		},
		{
			name:      "empty LastReadTS on group_dm IS stale",
			item:      ChannelItem{ID: "MPDM1", Type: "group_dm", LastReadTS: ""},
			threshold: threshold,
			want:      true,
		},
		{
			name:      "empty LastReadTS on stale dm respects unread exception",
			item:      ChannelItem{ID: "DM1", Type: "dm", LastReadTS: "", UnreadCount: 1},
			threshold: threshold,
			want:      false,
		},
		{
			name:      "empty LastReadTS respects threshold=0 disable",
			item:      ChannelItem{ID: "MPDM1", Type: "group_dm", LastReadTS: ""},
			threshold: 0,
			want:      false,
		},
		{
			name:      "malformed LastReadTS is treated as not stale",
			item:      ChannelItem{ID: "C1", LastReadTS: "not-a-timestamp"},
			threshold: threshold,
			want:      false,
		},
		{
			name:      "future LastReadTS is treated as not stale",
			item:      ChannelItem{ID: "C1", LastReadTS: tsAt(now.Add(24 * time.Hour))},
			threshold: threshold,
			want:      false,
		},
		{
			// Slack uses "0000000000.000000" as the LastRead value for
			// channels the user has never read. These should be
			// treated as the most-stale of all (never opened) and
			// auto-hidden. Regression: previously parseSlackTS
			// rejected sec<=0, so these stayed visible forever.
			name:      "Slack 'never read' sentinel '0000000000.000000' is stale",
			item:      ChannelItem{ID: "MPDM1", LastReadTS: "0000000000.000000"},
			threshold: threshold,
			want:      true,
		},
		{
			name:      "Slack 'never read' bare zero '0' is stale",
			item:      ChannelItem{ID: "MPDM2", LastReadTS: "0"},
			threshold: threshold,
			want:      true,
		},
		{
			name:      "threshold 0 disables staleness entirely",
			item:      ChannelItem{ID: "C1", LastReadTS: tsAt(now.Add(-365 * 24 * time.Hour))},
			threshold: 0,
			want:      false,
		},
		{
			name:      "negative threshold disables staleness",
			item:      ChannelItem{ID: "C1", LastReadTS: tsAt(now.Add(-365 * 24 * time.Hour))},
			threshold: -time.Hour,
			want:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsStale(tc.item, tc.threshold, now)
			if got != tc.want {
				t.Errorf("IsStale(%+v, %v) = %v, want %v", tc.item, tc.threshold, got, tc.want)
			}
		})
	}
}

// formatSlackTS converts a time.Time to a Slack-style timestamp string
// ("<seconds>.<microseconds>"). Mirrors the format Slack delivers.
func formatSlackTS(t time.Time) string {
	sec := t.Unix()
	usec := t.Nanosecond() / 1000
	return formatTSPair(sec, usec)
}

// TestSidebar_HidesStaleChannels asserts that the Model filters out
// items that IsStale() reports as stale, that the active channel is
// always exempt, and that disabling the threshold keeps everything
// visible.
func TestSidebar_HidesStaleChannels(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	tsAt := func(t time.Time) string { return formatSlackTS(t) }
	stale := tsAt(now.Add(-90 * 24 * time.Hour))
	fresh := tsAt(now.Add(-1 * 24 * time.Hour))

	items := []ChannelItem{
		{ID: "C-fresh", Name: "general", Type: "channel", LastReadTS: fresh},
		{ID: "C-stale", Name: "old-project", Type: "channel", LastReadTS: stale},
		{ID: "C-stale-unread", Name: "old-but-pinged", Type: "channel", LastReadTS: stale, UnreadCount: 1},
		{ID: "C-stale-section", Name: "alerts-old", Type: "channel", LastReadTS: stale, Section: "Alerts"},
		{ID: "DM-stale-active", Name: "alice", Type: "dm", LastReadTS: stale},
	}

	m := New(items)
	m.SetNowFunc(func() time.Time { return now })
	m.SetStaleThreshold(30 * 24 * time.Hour)
	m.SetActiveChannelID("DM-stale-active")

	visibleIDs := func() []string {
		var ids []string
		for _, it := range m.VisibleItems() {
			ids = append(ids, it.ID)
		}
		return ids
	}

	got := visibleIDs()
	wantPresent := map[string]bool{
		"C-fresh":         true, // fresh, never stale
		"C-stale-unread":  true, // unread exception
		"C-stale-section": true, // custom-section exception
		"DM-stale-active": true, // active-channel exception
	}
	wantAbsent := map[string]bool{
		"C-stale": true, // genuinely stale
	}
	gotSet := make(map[string]bool, len(got))
	for _, id := range got {
		gotSet[id] = true
	}
	for id := range wantPresent {
		if !gotSet[id] {
			t.Errorf("expected %q to be visible, got %v", id, got)
		}
	}
	for id := range wantAbsent {
		if gotSet[id] {
			t.Errorf("expected %q to be hidden, got %v", id, got)
		}
	}

	// Disabling the threshold restores all items.
	m.SetStaleThreshold(0)
	got = visibleIDs()
	if len(got) != len(items) {
		t.Errorf("threshold=0 should show all %d items, got %d: %v", len(items), len(got), got)
	}
}
