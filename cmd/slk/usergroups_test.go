package main

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/slack/boot"
)

func TestUsergroupHandlesPrefersHandle(t *testing.T) {
	byID := usergroupHandles([]slack.UserGroup{
		{ID: "S1", Handle: "platform-team", Name: "Platform Team"},
	})
	if byID["S1"] != "platform-team" {
		t.Errorf("handle = %q, want platform-team", byID["S1"])
	}
}

func TestUsergroupHandlesSlugifiesNameFallback(t *testing.T) {
	byID := usergroupHandles([]slack.UserGroup{
		{ID: "S1", Name: "Platform Team"},
	})
	if byID["S1"] != "platform-team" {
		t.Errorf("name fallback = %q, want platform-team (no spaces)", byID["S1"])
	}
}

func TestUsergroupHandlesSkipsUnmentionableGroups(t *testing.T) {
	byID := usergroupHandles([]slack.UserGroup{
		{ID: "S1"},
		{ID: "S2", Name: "   "},
		{ID: "S3", Name: "!!!"},
	})
	if len(byID) != 0 {
		t.Errorf("byID = %v, want empty (nothing mentionable)", byID)
	}
}

func TestSlugifyHandle(t *testing.T) {
	cases := map[string]string{
		"Platform Team":     "platform-team",
		"  Data   Eng  ":    "data-eng",
		"Design/Research":   "design-research",
		"on-call_rota.v2":   "on-call_rota.v2",
		"Team (EU) — North": "team-eu-north",
		"":                  "",
		"???":               "",
	}
	for in, want := range cases {
		if got := slugifyHandle(in); got != want {
			t.Errorf("slugifyHandle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorkspaceContextUserGroupsDefaultsEmpty(t *testing.T) {
	var wctx WorkspaceContext
	if got := wctx.UserGroups(); got == nil || len(got) != 0 {
		t.Errorf("UserGroups() before load = %v, want empty non-nil map", got)
	}
}

// The usergroups.list fetch publishes the map from a background
// goroutine while the RTM event loop and bubbletea cmds read it. Run
// under -race to catch a regression back to a plain map field.
func TestWorkspaceContextUserGroupsConcurrentAccess(t *testing.T) {
	wctx := &WorkspaceContext{}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			wctx.SetUserGroups(map[string]string{"S1": "platform-team"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = wctx.UserGroups()["S1"]
		}
	}()
	wg.Wait()
	if got := wctx.UserGroups()["S1"]; got != "platform-team" {
		t.Errorf("UserGroups()[S1] = %q, want platform-team", got)
	}
}

func TestSelfUserGroups(t *testing.T) {
	self := selfUserGroups([]slack.UserGroup{
		{ID: "S1", Users: []string{"U2", "U1"}},
		{ID: "S2", Users: []string{"U2"}},
		{ID: "S3"},
	}, "U1")
	if len(self) != 1 || !self["S1"] {
		t.Errorf("selfUserGroups = %v, want only S1", self)
	}
}

// Off Grid every conversation belongs to the workspace, so there are no
// member teams; on Grid each other owning team is listed once.
func TestGridMemberTeams(t *testing.T) {
	if got := gridMemberTeams([]boot.Channel{{ContextTeamID: "T1"}, {}}, []boot.IM{{ContextTeamID: "T1"}}, "T1"); len(got) != 0 {
		t.Errorf("single workspace: member teams = %v, want none", got)
	}
	got := gridMemberTeams(
		[]boot.Channel{{ContextTeamID: "T2"}, {ContextTeamID: "E1"}, {ContextTeamID: "T3"}, {ContextTeamID: "T2"}},
		[]boot.IM{{ContextTeamID: "T4"}},
		"E1")
	if want := []string{"T2", "T3", "T4"}; !slices.Equal(got, want) {
		t.Errorf("grid: member teams = %v, want %v", got, want)
	}
}

// The org-level call may fail on Grid; the member teams' groups must
// still load. Only when every call fails is it an error.
func TestFetchUserGroups(t *testing.T) {
	var asked []string
	list := func(_ context.Context, team string) ([]slack.UserGroup, error) {
		asked = append(asked, team)
		if team == "" {
			return nil, errors.New("org token requires team_id")
		}
		return []slack.UserGroup{{ID: "S-" + team}}, nil
	}
	logf := func(string, ...any) {}
	groups, err := fetchUserGroups(context.Background(), list, []string{"T2", "T3"}, logf)
	if err != nil {
		t.Fatalf("fetchUserGroups: %v", err)
	}
	if want := []string{"", "T2", "T3"}; !slices.Equal(asked, want) {
		t.Errorf("teams asked = %q, want %q", asked, want)
	}
	if len(groups) != 2 || groups[0].ID != "S-T2" || groups[1].ID != "S-T3" {
		t.Errorf("groups = %+v, want the two member teams' groups", groups)
	}

	asked = nil
	if _, err := fetchUserGroups(context.Background(), list, nil, logf); err == nil {
		t.Error("the only call failed but no error was returned")
	}
	if want := []string{""}; !slices.Equal(asked, want) {
		t.Errorf("off Grid: teams asked = %q, want %q", asked, want)
	}
}

// Each team's call gets its own deadline, so a stalled team is skipped
// instead of holding back the others' groups.
func TestFetchUserGroupsDeadline(t *testing.T) {
	list := func(ctx context.Context, _ string) ([]slack.UserGroup, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("usergroups call has no deadline")
		}
		return nil, nil
	}
	if _, err := fetchUserGroups(context.Background(), list, []string{"T2"}, t.Logf); err != nil {
		t.Fatal(err)
	}
}
