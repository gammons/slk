package sidebar

import (
	"slices"
	"testing"
	"time"

	"github.com/gammons/slk/internal/cache"
)

func TestIsStale(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	threshold := 30 * 24 * time.Hour

	tsAt := func(t time.Time) string {
		// Slack ts format: "<seconds>.<microseconds>"
		return formatSlackTS(t)
	}

	cases := []struct {
		name       string
		item       ChannelItem
		hasUnread  bool
		lastReadTS string
		threshold  time.Duration
		want       bool
	}{
		{
			name:       "fresh: read 1 day ago",
			item:       ChannelItem{ID: "C1"},
			lastReadTS: tsAt(now.Add(-24 * time.Hour)),
			threshold:  threshold,
			want:       false,
		},
		{
			name:       "stale: read 60 days ago",
			item:       ChannelItem{ID: "C1"},
			lastReadTS: tsAt(now.Add(-60 * 24 * time.Hour)),
			threshold:  threshold,
			want:       true,
		},
		{
			name:       "edge: read exactly 30 days ago is NOT stale",
			item:       ChannelItem{ID: "C1"},
			lastReadTS: tsAt(now.Add(-30 * 24 * time.Hour)),
			threshold:  threshold,
			want:       false,
		},
		{
			name:       "edge: read 30 days + 1 minute ago IS stale",
			item:       ChannelItem{ID: "C1"},
			lastReadTS: tsAt(now.Add(-30*24*time.Hour - time.Minute)),
			threshold:  threshold,
			want:       true,
		},
		{
			name:       "exception: unread > 0 is never stale",
			item:       ChannelItem{ID: "C1"},
			lastReadTS: tsAt(now.Add(-90 * 24 * time.Hour)),
			hasUnread:  true,
			threshold:  threshold,
			want:       false,
		},
		{
			name:       "exception: custom-section channel is never stale",
			item:       ChannelItem{ID: "C1", Section: "Engineering"},
			lastReadTS: tsAt(now.Add(-90 * 24 * time.Hour)),
			threshold:  threshold,
			want:       false,
		},
		{
			// Public/private channels always include a last_read in
			// client.counts, so an empty value here likely means
			// "brand-new join, counts hasn't refreshed yet" -- show
			// rather than hide.
			name:       "empty LastReadTS on public channel is NOT stale",
			item:       ChannelItem{ID: "C1", Type: "channel"},
			lastReadTS: "",
			threshold:  threshold,
			want:       false,
		},
		{
			name:       "empty LastReadTS on private channel is NOT stale",
			item:       ChannelItem{ID: "C1", Type: "private"},
			lastReadTS: "",
			threshold:  threshold,
			want:       false,
		},
		{
			// Slack's client.counts only returns dm/group_dm entries
			// for currently-open conversations. Absence (empty
			// lastReadTS) is the canonical "this conversation is
			// closed/stale" signal for these types.
			name:       "empty LastReadTS on dm IS stale",
			item:       ChannelItem{ID: "DM1", Type: "dm"},
			lastReadTS: "",
			threshold:  threshold,
			want:       true,
		},
		{
			name:       "empty LastReadTS on group_dm IS stale",
			item:       ChannelItem{ID: "MPDM1", Type: "group_dm"},
			lastReadTS: "",
			threshold:  threshold,
			want:       true,
		},
		{
			name:       "empty LastReadTS on stale dm respects unread exception",
			item:       ChannelItem{ID: "DM1", Type: "dm"},
			lastReadTS: "",
			hasUnread:  true,
			threshold:  threshold,
			want:       false,
		},
		{
			name:       "empty LastReadTS respects threshold=0 disable",
			item:       ChannelItem{ID: "MPDM1", Type: "group_dm"},
			lastReadTS: "",
			threshold:  0,
			want:       false,
		},
		{
			name:       "malformed LastReadTS is treated as not stale",
			item:       ChannelItem{ID: "C1"},
			lastReadTS: "not-a-timestamp",
			threshold:  threshold,
			want:       false,
		},
		{
			name:       "future LastReadTS is treated as not stale",
			item:       ChannelItem{ID: "C1"},
			lastReadTS: tsAt(now.Add(24 * time.Hour)),
			threshold:  threshold,
			want:       false,
		},
		{
			// Slack uses "0000000000.000000" as the LastRead value for
			// channels the user has never read. These should be
			// treated as the most-stale of all (never opened) and
			// auto-hidden. Regression: previously parseSlackTS
			// rejected sec<=0, so these stayed visible forever.
			name:       "Slack 'never read' sentinel '0000000000.000000' is stale",
			item:       ChannelItem{ID: "MPDM1"},
			lastReadTS: "0000000000.000000",
			threshold:  threshold,
			want:       true,
		},
		{
			name:       "Slack 'never read' bare zero '0' is stale",
			item:       ChannelItem{ID: "MPDM2"},
			lastReadTS: "0",
			threshold:  threshold,
			want:       true,
		},
		{
			name:       "threshold 0 disables staleness entirely",
			item:       ChannelItem{ID: "C1"},
			lastReadTS: tsAt(now.Add(-365 * 24 * time.Hour)),
			threshold:  0,
			want:       false,
		},
		{
			name:       "negative threshold disables staleness",
			item:       ChannelItem{ID: "C1"},
			lastReadTS: tsAt(now.Add(-365 * 24 * time.Hour)),
			threshold:  -time.Hour,
			want:       false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsStale(tc.item, tc.hasUnread, tc.lastReadTS, tc.threshold, now)
			if got != tc.want {
				t.Errorf("IsStale(%+v, hasUnread=%v, lastReadTS=%q, %v) = %v, want %v",
					tc.item, tc.hasUnread, tc.lastReadTS, tc.threshold, got, tc.want)
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
		{ID: "C-fresh", Name: "general", Type: "channel"},
		{ID: "C-stale", Name: "old-project", Type: "channel"},
		{ID: "C-stale-unread", Name: "old-but-pinged", Type: "channel"},
		{ID: "C-stale-section", Name: "alerts-old", Type: "channel", Section: "Alerts"},
		{ID: "DM-stale-active", Name: "alice", Type: "dm"},
	}

	// Per-channel read state, mirroring what the live readStateReader
	// (DB-backed) would return. C-stale-unread carries a HasUnread=true
	// signal to exercise the unread-exception branch of IsStale.
	readState := map[string]cache.ReadState{
		"C-fresh":         {LastReadTS: fresh},
		"C-stale":         {LastReadTS: stale},
		"C-stale-unread":  {LastReadTS: stale, HasUnread: true},
		"C-stale-section": {LastReadTS: stale},
		"DM-stale-active": {LastReadTS: stale},
	}

	m := New(items)
	m.SetReadStateReader(func() map[string]cache.ReadState { return readState })
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

// TestSidebar_RevealsHiddenDMWhenItTurnsUnread covers a DM someone opens
// mid-session: im_created upserts it before its first message, with no
// last_read, so the staleness filter hides it. The message then marks it
// unread and the App calls Invalidate, which must bring the row back.
func TestSidebar_RevealsHiddenDMWhenItTurnsUnread(t *testing.T) {
	readState := map[string]cache.ReadState{}
	m := New([]ChannelItem{{ID: "C-general", Name: "general", Type: "channel"}})
	m.SetReadStateReader(func() map[string]cache.ReadState { return readState })
	m.SetStaleThreshold(30 * 24 * time.Hour)

	visible := func(id string) bool {
		for _, it := range m.VisibleItems() {
			if it.ID == id {
				return true
			}
		}
		return false
	}

	m.UpsertItem(ChannelItem{ID: "D-new", Name: "newcomer", Type: "dm"})
	if visible("D-new") {
		t.Fatal("a DM with no last_read and nothing unread should start hidden")
	}

	readState["D-new"] = cache.ReadState{HasUnread: true}
	m.Invalidate()
	if !visible("D-new") {
		t.Fatal("the DM stayed hidden after it turned unread")
	}
	m.SelectByID("D-new")
	if got := m.SelectedID(); got != "D-new" {
		t.Errorf("the revealed DM can't be selected: SelectedID() = %q", got)
	}
}

// TestSidebar_RevealKeepsAgedSelectedRow: revealing a DM must not run the
// staleness filter over the rows already shown. A selected row that has
// aged past the threshold since the last rebuild stays, and stays
// selected, until the next full rebuild.
func TestSidebar_RevealKeepsAgedSelectedRow(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	threshold := 30 * 24 * time.Hour
	readState := map[string]cache.ReadState{
		"D-old": {LastReadTS: formatSlackTS(now.Add(-threshold + time.Hour))},
	}
	m := New([]ChannelItem{
		{ID: "C-general", Name: "general", Type: "channel"},
		{ID: "D-old", Name: "zed", Type: "dm"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState { return readState })
	m.SetNowFunc(func() time.Time { return now })
	m.SetStaleThreshold(threshold)
	m.UpsertItem(ChannelItem{ID: "D-new", Name: "amy", Type: "dm"})
	m.SelectByID("D-old")
	if got := m.SelectedID(); got != "D-old" {
		t.Fatalf("setup: SelectedID() = %q, want D-old", got)
	}

	now = now.Add(2 * time.Hour) // D-old is now past the threshold
	readState["D-new"] = cache.ReadState{HasUnread: true}
	m.Invalidate()

	if got := m.SelectedID(); got != "D-old" {
		t.Errorf("SelectedID() = %q after the reveal, want D-old", got)
	}
	var ids []string
	for _, it := range m.VisibleItems() {
		ids = append(ids, it.ID)
	}
	// The revealed row is sorted into place (amy before zed), not
	// appended at the end.
	if want := []string{"D-new", "D-old", "C-general"}; !slices.Equal(ids, want) {
		t.Errorf("visible = %v, want %v", ids, want)
	}
}

// TestSidebar_RevealOnlyAddsHiddenUnreadMatches: Invalidate reveals a
// hidden item only when it is unread and matches the name filter, and
// never adds a row that is already shown, however often it runs.
func TestSidebar_RevealOnlyAddsHiddenUnreadMatches(t *testing.T) {
	readState := map[string]cache.ReadState{}
	m := New([]ChannelItem{
		{ID: "D-shown", Name: "alex", Type: "dm"},
		{ID: "D-read", Name: "alma", Type: "dm"},
		{ID: "D-match", Name: "alva", Type: "dm"},
		{ID: "D-other", Name: "zoe", Type: "dm"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState { return readState })
	m.SetStaleThreshold(30 * 24 * time.Hour)
	readState["D-shown"] = cache.ReadState{HasUnread: true}
	m.SetFilter("al")

	visible := func() []string {
		var ids []string
		for _, it := range m.VisibleItems() {
			ids = append(ids, it.ID)
		}
		return ids
	}
	if got, want := visible(), []string{"D-shown"}; !slices.Equal(got, want) {
		t.Fatalf("setup: visible = %v, want %v", got, want)
	}

	readState["D-match"] = cache.ReadState{HasUnread: true}
	readState["D-other"] = cache.ReadState{HasUnread: true}
	m.Invalidate()
	m.Invalidate()
	if got, want := visible(), []string{"D-shown", "D-match"}; !slices.Equal(got, want) {
		t.Errorf("visible = %v, want %v", got, want)
	}
}
