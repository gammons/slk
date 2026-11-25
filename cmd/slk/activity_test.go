package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gammons/slk/internal/core"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
)

// The Activity closures must resolve the workspace they were asked for,
// not whichever is active when they run. A workspace switch can land
// between dispatch and execution; resolving via the active workspace
// then fetches the wrong feed and labels it with the requested team.
//
// T1 is active and its Client is nil, so any call through it panics;
// T2 is requested and not connected, so the only correct result is a
// nil Msg without touching T1.
func TestActivityClosures_ResolveRequestedTeamNotActive(t *testing.T) {
	router := newWorkspaceRouter()
	active := &WorkspaceContext{TeamID: "T1"}
	router.Add(active)
	router.Set(active)

	cases := []struct {
		name string
		call func() core.Msg
	}{
		{"fetch", func() core.Msg { return activityFetchFunc(router)("T2", 50, false) }},
		{"hydrate", func() core.Msg {
			return activityHydrateFunc(router)("T2", map[string][]string{"C1": {"1.1"}})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s for T2 went through the active workspace T1: %v", c.name, r)
				}
			}()
			if msg := c.call(); msg != nil {
				t.Fatalf("%s for unconnected T2 = %#v, want nil", c.name, msg)
			}
		})
	}
}

// SetRead must always answer, even for a workspace that is not
// connected, so the UI can undo its optimistic change. Like the other
// closures it resolves the requested team: T1 is active with a nil
// Client, so going through it would panic.
func TestActivitySetRead_UnconnectedTeamReportsError(t *testing.T) {
	router := newWorkspaceRouter()
	active := &WorkspaceContext{TeamID: "T1"}
	router.Add(active)
	router.Set(active)

	msg := activitySetReadFunc(router)("T2", core.ActivityItem{Key: "k1", Type: "at_user", FeedTS: "1.1"}, true)
	got, ok := msg.(ui.ActivityReadSetMsg)
	if !ok {
		t.Fatalf("SetRead returned %#v, want ui.ActivityReadSetMsg", msg)
	}
	if got.TeamID != "T2" || got.Key != "k1" || !got.Read || got.Err == nil {
		t.Fatalf("SetRead for unconnected T2 = %+v, want TeamID T2, Key k1, Read true and an error", got)
	}
}

// filterActivityItemsToWorkspace keeps the slice of an org-wide feed a
// member workspace can act on: its own conversations (including shared
// channels, which the workspace knows even when another session cached
// them first) and the items with no conversation (a Slack reminder). A
// workspace that knows no conversation yet - a fetch racing the first
// boot - keeps everything.
func TestFilterActivityItemsToWorkspace(t *testing.T) {
	items := []core.ActivityItem{
		{Key: "own-channel", Type: "at_user", ChannelID: "C1", TS: "1.1"},
		{Key: "own-dm", Type: "dm", ChannelID: "D1", TS: "1.2"},
		{Key: "shared", Type: "at_user", ChannelID: "CS", TS: "1.4"},
		{Key: "foreign", Type: "at_user", ChannelID: "C9", TS: "1.3"},
		{Key: "unjoined-mention", Type: "unjoined_channel_mention", ChannelID: "C8", TS: "1.5"},
		{Key: "reminder", Type: "saved_reminder"},
	}

	t.Run("member workspace with known conversations", func(t *testing.T) {
		wctx := &WorkspaceContext{TeamID: "T2"}
		for _, id := range []string{"C1", "D1", "CS"} {
			wctx.MarkConversation(id)
		}

		got := filterActivityItemsToWorkspace(items, wctx)
		var keys []string
		for _, it := range got {
			keys = append(keys, it.Key)
		}
		want := []string{"own-channel", "own-dm", "shared", "unjoined-mention", "reminder"}
		if len(keys) != len(want) {
			t.Fatalf("filtered keys = %v; want %v", keys, want)
		}
		for i := range want {
			if keys[i] != want[i] {
				t.Fatalf("filtered keys = %v; want %v", keys, want)
			}
		}
	})

	t.Run("no conversations known yet keeps every item", func(t *testing.T) {
		wctx := &WorkspaceContext{TeamID: "T2"}
		if got := filterActivityItemsToWorkspace(items, wctx); len(got) != len(items) {
			t.Fatalf("filtered %d items; want all %d kept for a workspace still booting", len(got), len(items))
		}
	})
}

// collectActivityItems pages the feed until the limit survives the
// filter: a member workspace's org-wide feed may hold whole pages of
// other workspaces' items, so one page can answer with nothing the
// member can open even though later pages hold its activity. Any
// page's failure is an error, so the feed already on screen stays; the
// feed ending short of the limit returns what there is.
func TestCollectActivityItems_PagesUntilLimitMet(t *testing.T) {
	foreign := func(n int) []core.ActivityItem {
		items := make([]core.ActivityItem, 0, n)
		for i := 0; i < n; i++ {
			items = append(items, core.ActivityItem{Key: fmt.Sprintf("foreign-%d", i), ChannelID: "C9"})
		}
		return items
	}
	own := func(n int) []core.ActivityItem {
		items := make([]core.ActivityItem, 0, n)
		for i := 0; i < n; i++ {
			items = append(items, core.ActivityItem{Key: fmt.Sprintf("own-%d", i), ChannelID: "C1"})
		}
		return items
	}
	dropForeign := func(items []core.ActivityItem) []core.ActivityItem {
		out := make([]core.ActivityItem, 0, len(items))
		for _, it := range items {
			if it.ChannelID != "C9" {
				out = append(out, it)
			}
		}
		return out
	}

	t.Run("keeps paging past foreign pages", func(t *testing.T) {
		pages := [][]core.ActivityItem{foreign(50), foreign(50), own(3)}
		calls := 0
		got, err := collectActivityItems(3, func(_ string) (slackclient.ActivityFeedResult, error) {
			cursor := "more"
			if calls == len(pages)-1 {
				cursor = ""
			}
			result := slackclient.ActivityFeedResult{Items: pages[calls], NextCursor: cursor}
			calls++
			return result, nil
		}, dropForeign)
		if err != nil {
			t.Fatalf("collectActivityItems: %v", err)
		}
		if calls != len(pages) {
			t.Fatalf("fetched %d pages; want %d", calls, len(pages))
		}
		if len(got) != 3 || got[0].Key != "own-0" {
			t.Fatalf("collected %v; want the 3 own items from the last page", keysOf(got))
		}
	})

	t.Run("feed ends short of the limit", func(t *testing.T) {
		got, err := collectActivityItems(10, func(_ string) (slackclient.ActivityFeedResult, error) {
			return slackclient.ActivityFeedResult{Items: own(2)}, nil
		}, dropForeign)
		if err != nil {
			t.Fatalf("collectActivityItems: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("collected %d items; want the 2 the feed held", len(got))
		}
	})

	t.Run("first page failure is the error", func(t *testing.T) {
		_, err := collectActivityItems(10, func(_ string) (slackclient.ActivityFeedResult, error) {
			return slackclient.ActivityFeedResult{}, errors.New("team_is_restricted")
		}, dropForeign)
		if err == nil {
			t.Fatal("collectActivityItems with a failing first page returned no error")
		}
	})

	t.Run("later page failure surfaces the error so the feed stays", func(t *testing.T) {
		calls := 0
		got, err := collectActivityItems(10, func(_ string) (slackclient.ActivityFeedResult, error) {
			calls++
			if calls == 1 {
				return slackclient.ActivityFeedResult{Items: own(1), NextCursor: "more"}, nil
			}
			return slackclient.ActivityFeedResult{}, errors.New("transient")
		}, dropForeign)
		if err == nil {
			t.Fatal("collectActivityItems with a failing later page returned no error; the partial list would replace the loaded feed")
		}
		if len(got) != 0 {
			t.Fatalf("collected %d items alongside the error; want none, so the caller keeps the feed it has", len(got))
		}
	})
}

// keysOf lists the items' keys, for failure messages.
func keysOf(items []core.ActivityItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Key)
	}
	return out
}

// orgOrOwnClient routes a grid member's org-served calls through the
// org-level sibling of the SAME authenticated user: browser
// credentials for a second account on the grid connect as sibling
// sessions too, and routing through one of those would read (and
// write) another user's data. No matching sibling falls back to the
// workspace's own client.
func TestOrgOrOwnClient_MatchesTheSameUser(t *testing.T) {
	member := &WorkspaceContext{TeamID: "T1", Client: slackclient.NewSessionClient("T1", "E1", "U1")}
	otherUser := &WorkspaceContext{TeamID: "E9", Client: slackclient.NewSessionClient("E9", "E1", "U2")}
	sameUser := &WorkspaceContext{TeamID: "E1", Client: slackclient.NewSessionClient("E1", "E1", "U1")}

	router := newWorkspaceRouter()
	router.Add(member)
	router.Add(otherUser)

	if got := orgOrOwnClient(router, member); got != member.Client {
		t.Fatal("orgOrOwnClient routed through another user's session; the sibling must match the authenticated user")
	}

	router.Add(sameUser)
	if got := orgOrOwnClient(router, member); got != sameUser.Client {
		t.Fatal("orgOrOwnClient did not route through the same user's org-level session")
	}
}

// sameUserOrgClient is the one org-session selector every grid
// fallback shares (Activity calls, conversation discovery): same grid,
// org-level, same authenticated user; nil off a grid, without an org
// session, or when the sibling is another account's credentials.
func TestSameUserOrgClient(t *testing.T) {
	member := &WorkspaceContext{TeamID: "T1", Client: slackclient.NewSessionClient("T1", "E1", "U1")}
	otherUser := &WorkspaceContext{TeamID: "E9", Client: slackclient.NewSessionClient("E9", "E1", "U2")}
	sameUser := &WorkspaceContext{TeamID: "E1", Client: slackclient.NewSessionClient("E1", "E1", "U1")}
	member2 := &WorkspaceContext{TeamID: "T2", Client: slackclient.NewSessionClient("T2", "E1", "U1")}
	plain := &WorkspaceContext{TeamID: "T9", Client: slackclient.NewSessionClient("T9", "", "U9")}

	router := newWorkspaceRouter()
	router.Add(member)
	if got := sameUserOrgClient(router, member); got != nil {
		t.Fatal("an org-less grid answered with a session")
	}
	router.Add(otherUser)
	router.Add(member2)
	if got := sameUserOrgClient(router, member); got != nil {
		t.Fatal("another user's org session or a member sibling stood in for the org session")
	}
	router.Add(sameUser)
	if got := sameUserOrgClient(router, member); got != sameUser.Client {
		t.Fatal("the same user's org-level session was not selected")
	}
	if got := sameUserOrgClient(router, plain); got != nil {
		t.Fatal("a non-grid workspace answered with an org session")
	}
	if got := sameUserOrgClient(router, &WorkspaceContext{TeamID: "T0"}); got != nil {
		t.Fatal("a workspace without a client answered with an org session")
	}
}
