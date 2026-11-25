package main

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
)

// startupChannelID warms the configured default_channel when the cache
// holds a joined channel by that name (core.ChannelNameMatcher), and the
// most recently visited channel otherwise. A cached channel the user is
// not in (one a mention resolved) is never warmed: the UI cannot open it.
func TestStartupChannelID(t *testing.T) {
	db := newCacheForTest(t)
	for _, ch := range []cache.Channel{
		{ID: "C1", WorkspaceID: "T1", Name: "rocketeers", Type: "channel", IsMember: true},
		{ID: "C2", WorkspaceID: "T1", Name: "engineering", Type: "private", IsMember: true},
		{ID: "C9", WorkspaceID: "T1", Name: "announcements", Type: "channel"},
		{ID: "D1", WorkspaceID: "T1", Name: "alice", Type: "dm", IsMember: true},
	} {
		if err := db.UpsertChannel(ch); err != nil {
			t.Fatalf("UpsertChannel: %v", err)
		}
	}
	visits := map[string]int64{"C1": 200, "C2": 100}
	for _, tc := range []struct{ name, want string }{
		{"engineering", "C2"},
		{"#Engineering", "C2"},
		{"", "C1"},
		{"missing", "C1"},
		{"alice", "C1"},
		{"announcements", "C1"}, // cached, but not joined
	} {
		if got := startupChannelID(db, "T1", tc.name, visits); got != tc.want {
			t.Errorf("startupChannelID(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}
