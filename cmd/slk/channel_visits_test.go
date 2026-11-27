package main

import (
	"testing"

	"github.com/gammons/slk/internal/ui/sidebar"
)

// A shared DM visited from another workspace is not the last channel of
// a workspace that cannot show it; the workspace keeps its own newest
// conversation.
func TestMostRecentlyVisitedChannelAmong(t *testing.T) {
	visits := map[string]int64{"C1": 100, "D-other": 200}
	known := channelIDSet([]sidebar.ChannelItem{{ID: "C1"}})
	if got := mostRecentlyVisitedChannelAmong(visits, known); got != "C1" {
		t.Errorf("got %q, want C1 (the DM is not in this workspace)", got)
	}
	if got := mostRecentlyVisitedChannelAmong(visits, nil); got != "D-other" {
		t.Errorf("unfiltered got %q, want D-other", got)
	}
}
