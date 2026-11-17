package main

import (
	"sync"
	"testing"

	"github.com/slack-go/slack"
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
