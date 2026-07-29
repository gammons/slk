package ui

import (
	"testing"

	slack "github.com/gammons/slk/internal/slack"
)

func TestActivityRefs(t *testing.T) {
	items := []slack.ActivityItem{
		{ChannelID: "C1", TS: "1.1"},
		{ChannelID: "C1", TS: "1.1"}, // duplicate (channel, ts)
		{ChannelID: "C1", TS: "2.2"},
		{ChannelID: "", TS: "3.3"}, // empty channel -> skipped
		{ChannelID: "C2", TS: ""},  // empty ts -> skipped
		{ChannelID: "C2", TS: "4.4"},
	}
	refs := activityRefs(items)

	if len(refs) != 2 {
		t.Fatalf("want 2 channels, got %d: %v", len(refs), refs)
	}
	if got := refs["C1"]; len(got) != 2 || got[0] != "1.1" || got[1] != "2.2" {
		t.Fatalf("C1 refs want [1.1 2.2] (deduped, first-seen order), got %v", got)
	}
	if got := refs["C2"]; len(got) != 1 || got[0] != "4.4" {
		t.Fatalf("C2 refs want [4.4] (empty ts skipped), got %v", got)
	}
	if _, ok := refs[""]; ok {
		t.Fatalf("empty channel must not appear")
	}
}

func TestActivityRefs_Empty(t *testing.T) {
	if refs := activityRefs(nil); len(refs) != 0 {
		t.Fatalf("nil items -> empty refs, got %v", refs)
	}
	// All refs unhydratable -> empty (so the reducer skips the fetch).
	items := []slack.ActivityItem{{ChannelID: "", TS: ""}, {ChannelID: "C1", TS: ""}}
	if refs := activityRefs(items); len(refs) != 0 {
		t.Fatalf("no hydratable refs -> empty, got %v", refs)
	}
}
