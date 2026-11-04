package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/sidebar"
)

func TestActivityRefs(t *testing.T) {
	items := []core.ActivityItem{
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
	items := []core.ActivityItem{{ChannelID: "", TS: ""}, {ChannelID: "C1", TS: ""}}
	if refs := activityRefs(items); len(refs) != 0 {
		t.Fatalf("no hydratable refs -> empty, got %v", refs)
	}
}

// App.SetChannels hands the Activity view each conversation's type, so a
// reaction in a 1:1 DM reads "Reacted in DM" rather than "#<person>".
func TestActivityView_ChannelTypesComeFromSetChannels(t *testing.T) {
	a := newTestApp(t, withSize(120, 30), withActiveTeam("T1"), withView(ViewActivity),
		withChannels(sidebar.ChannelItem{ID: "D1", Name: "Drew Gilliam", Type: "dm"}))
	a.Update(ActivityListLoadedMsg{TeamID: "T1", Items: []core.ActivityItem{
		{Key: "k1", Type: "message_reaction", ChannelID: "D1", TS: "1.1", Reaction: "eyes"},
	}})

	out := ansi.Strip(a.View().Content)
	if !strings.Contains(out, "Reacted in DM") || strings.Contains(out, "#Drew Gilliam") {
		t.Errorf("activity view does not name the DM as a DM:\n%s", out)
	}
}

// The badge must be the workspace's activity total, not the loaded
// feed page's unread count: the page holds only the newest items, so
// its count both undercounts and moves when the unread filter (u)
// re-fetches a different page. A badge derived from the page made the
// number jump between the filtered and unfiltered views of the same
// workspace.
func TestActivityBadge_StaysOnTotalAcrossRefetch(t *testing.T) {
	a := newTestApp(t, withSize(120, 30), withActiveTeam("T1"))
	a.SetActivityService(core.NewActivityService(core.ActivityServiceFuncs{}))

	// Boot total: 5 unread, workspace-wide.
	a.Update(ActivityBadgeUpdatedMsg{TeamID: "T1", Total: 5})
	if got := a.sidebar.ActivityUnreadCount(); got != 5 {
		t.Fatalf("badge after boot total = %d, want 5", got)
	}

	// The unfiltered page arrives: 3 of its 50 items are unread. The
	// badge must not adopt 3.
	a.Update(ActivityViewActivatedMsg{})
	a.Update(ActivityListLoadedMsg{TeamID: "T1", Items: []core.ActivityItem{
		{Key: "k1", ChannelID: "C1", TS: "1.1", IsUnread: true},
		{Key: "k2", ChannelID: "C1", TS: "2.2", IsUnread: true},
		{Key: "k3", ChannelID: "C2", TS: "3.3"},
	}})
	if got := a.sidebar.ActivityUnreadCount(); got != 5 {
		t.Fatalf("badge after unfiltered page = %d, want 5 (the total, not the page's 2)", got)
	}

	// The unread filter (u) re-fetches: the server returns only
	// unread items, 17 of them. The badge must not adopt 17 either.
	a.Update(ActivityToggleUnreadMsg{})
	a.Update(ActivityListLoadedMsg{TeamID: "T1", Items: make([]core.ActivityItem, 17)})
	if got := a.sidebar.ActivityUnreadCount(); got != 5 {
		t.Fatalf("badge after unread-filtered page = %d, want 5 (the total, not the page's 17)", got)
	}
}

// The server's badge_counts_updated recalculation is absolute: it
// replaces the stored total whatever the optimistic adjustments in
// flight did, and it updates the badge only for the workspace on
// screen.
func TestActivityBadge_ServerUpdateIsAbsoluteAndScoped(t *testing.T) {
	a := newTestApp(t, withSize(120, 30), withActiveTeam("T1"))
	a.SetActivityService(core.NewActivityService(core.ActivityServiceFuncs{}))

	a.Update(ActivityBadgeUpdatedMsg{TeamID: "T1", Total: 5})
	a.Update(ActivityBadgeUpdatedMsg{TeamID: "T2", Total: 40})
	if got := a.sidebar.ActivityUnreadCount(); got != 5 {
		t.Fatalf("badge for T1 = %d, want 5", got)
	}

	// A recalculation for the inactive workspace must not move the
	// on-screen badge.
	a.Update(ActivityBadgeUpdatedMsg{TeamID: "T2", Total: 39})
	if got := a.sidebar.ActivityUnreadCount(); got != 5 {
		t.Fatalf("badge after T2 update = %d, want 5 (T2 is not on screen)", got)
	}

	// A recalculation for the active workspace replaces the total.
	a.Update(ActivityBadgeUpdatedMsg{TeamID: "T1", Total: 4})
	if got := a.sidebar.ActivityUnreadCount(); got != 4 {
		t.Fatalf("badge after T1 update = %d, want 4", got)
	}
}

// A failed read request must roll the workspace's stored total back
// even when the user has switched away from that workspace in the
// meantime: the optimistic change adjusted the total, a failed request
// changed nothing on the server (so no badge_counts_updated will ever
// correct it), and switching back shows the stored value. The rollback
// therefore runs before the active-workspace early return.
func TestActivityBadge_FailedReadRollsBackSwitchedWorkspace(t *testing.T) {
	a, calls := activityReadApp(t, errors.New("nope"), unreadMention("k1"))
	_, cmd := a.Update(keyPress('U'))

	// Switch away FIRST: the failure for T1 must arrive while T2 is on
	// screen, so the test exercises the switched-away rollback path -
	// delivering it before the switch would pass even if the early
	// return came back.
	a.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "T2"})
	if got := a.sidebar.ActivityUnreadCount(); got != 0 {
		t.Fatalf("badge after switch = %d, want 0 (T2 has no stored total yet)", got)
	}
	if got := a.activityBadgeTotals["T1"]; got != 0 {
		t.Fatalf("T1 stored total while the read is in flight = %d, want 0 (optimistically adjusted)", got)
	}

	a.Update(deliveredMsgs[ActivityReadSetMsg](t, cmd)[0])
	<-calls
	if got := a.activityBadgeTotals["T1"]; got != 1 {
		t.Fatalf("T1 stored total after failed read = %d, want 1 (rolled back)", got)
	}
}
