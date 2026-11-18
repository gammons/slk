package main

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/slack-go/slack"

	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/messages"
)

func TestSubteamIDsInText(t *testing.T) {
	got := subteamIDsInText("<!subteam^S02SRE> review svp — cc <!subteam^S02SEC> and <!subteam^S03LBL|@labeled>")
	if want := []string{"S02SRE", "S02SEC"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v (labeled tokens skipped)", got, want)
	}
}

func TestSubteamIDsInMessage_BlocksAndAttachments(t *testing.T) {
	blocks := slack.Blocks{BlockSet: []slack.Block{
		slack.NewRichTextBlock("b1", slack.NewRichTextSection(
			&slack.RichTextSectionUserGroupElement{Type: slack.RTSEUserGroup, UsergroupID: "S0BLOCK"},
		)),
	}}
	attachments := []slack.Attachment{{Text: "ping <!subteam^S0ATTACH>"}}
	got := subteamIDsInMessage("<!subteam^S0TEXT> <!subteam^S0BLOCK>", blocks, attachments)
	slices.Sort(got)
	if want := []string{"S0ATTACH", "S0BLOCK", "S0TEXT"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSubteamIDsInItems_Dedupes(t *testing.T) {
	items := []messages.MessageItem{{Text: "<!subteam^S1>"}, {Text: "<!subteam^S1> <!subteam^S2>"}}
	if got, want := subteamIDsInItems(items), []string{"S1", "S2"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

type fakeSubteamLookup struct {
	mu    sync.Mutex
	calls map[string]int
	res   map[string]slackclient.UserGroupInfo
	errs  map[string]error
}

func (f *fakeSubteamLookup) lookup(_ context.Context, id string) (slackclient.UserGroupInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[id]++
	if err := f.errs[id]; err != nil {
		return slackclient.UserGroupInfo{}, err
	}
	if g, ok := f.res[id]; ok {
		return g, nil
	}
	return slackclient.UserGroupInfo{}, slackclient.ErrUserGroupNotFound
}

func (f *fakeSubteamLookup) count(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[id]
}

func waitSubteamsIdle(t *testing.T, r *subteamResolver) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		idle := !r.running && len(r.queue) == 0
		r.mu.Unlock()
		if idle {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("resolver did not go idle")
}

func newTestSubteamResolver(f *fakeSubteamLookup, w *WorkspaceContext) (*subteamResolver, *[]map[string]string, *sync.Mutex) {
	var mu sync.Mutex
	var published []map[string]string
	r := newSubteamResolver(context.Background(), f.lookup, w.UserGroups, w.MergeUserGroups,
		func(m map[string]string) {
			mu.Lock()
			published = append(published, m)
			mu.Unlock()
		}, func(string, ...any) {})
	return r, &published, &mu
}

func TestSubteamResolver_ResolvesAndMerges(t *testing.T) {
	w := &WorkspaceContext{}
	w.SetUserGroups(map[string]string{"S0ORG": "org-group"})
	f := &fakeSubteamLookup{res: map[string]slackclient.UserGroupInfo{
		"S02SRE": {ID: "S02SRE", Handle: "sre"},
		"S02SEC": {ID: "S02SEC", Name: "Security Team"},
	}}
	r, published, mu := newTestSubteamResolver(f, w)

	r.Request([]string{"S0ORG", "S02SRE", "S02SEC", "S02SRE"})
	waitSubteamsIdle(t, r)

	if f.count("S0ORG") != 0 {
		t.Errorf("looked up a group already in the map")
	}
	if f.count("S02SRE") != 1 {
		t.Errorf("S02SRE looked up %d times, want 1", f.count("S02SRE"))
	}
	got := w.UserGroups()
	want := map[string]string{"S0ORG": "org-group", "S02SRE": "sre", "S02SEC": "security-team"}
	if len(got) != len(want) {
		t.Fatalf("map = %v, want %v", got, want)
	}
	for id, h := range want {
		if got[id] != h {
			t.Fatalf("map = %v, want %v", got, want)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*published) == 0 || (*published)[len(*published)-1]["S02SRE"] != "sre" {
		t.Fatalf("published = %v, want the merged map", *published)
	}
}

func TestSubteamResolver_NotFoundIsRemembered_OtherErrorsRetry(t *testing.T) {
	w := &WorkspaceContext{}
	f := &fakeSubteamLookup{errs: map[string]error{"S0FLAKY": errors.New("timeout")}}
	r, published, mu := newTestSubteamResolver(f, w)

	r.Request([]string{"S0GONE", "S0FLAKY"})
	waitSubteamsIdle(t, r)
	r.Request([]string{"S0GONE", "S0FLAKY"})
	waitSubteamsIdle(t, r)

	if n := f.count("S0GONE"); n != 1 {
		t.Errorf("not-found ID looked up %d times, want 1", n)
	}
	if n := f.count("S0FLAKY"); n != 2 {
		t.Errorf("transiently failing ID looked up %d times, want 2", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*published) != 0 {
		t.Errorf("published %v with nothing resolved", *published)
	}
}

func TestMergeUserGroups_KeepsConcurrentWrites(t *testing.T) {
	w := &WorkspaceContext{}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "S" + string(rune('A'+i%26)) + string(rune('A'+i/26))
			w.MergeUserGroups(map[string]string{id: "h"})
		}(i)
	}
	wg.Wait()
	if n := len(w.UserGroups()); n != 50 {
		t.Fatalf("map has %d entries after 50 concurrent merges, want 50", n)
	}
}

func TestSubteamResolver_NilSafe(t *testing.T) {
	var r *subteamResolver
	r.Request([]string{"S1"})
	r.RequestItems([]messages.MessageItem{{Text: "<!subteam^S1>"}})
}

func TestPublishUserGroups_SendsCurrentMaps(t *testing.T) {
	w := &WorkspaceContext{TeamID: "E1"}
	w.MergeUserGroups(map[string]string{"S0ORG": "org"})
	w.MergeUserGroups(map[string]string{"S0SRE": "sre"})
	w.SetSelfUserGroups(map[string]bool{"S0SRE": true})
	var got []tea.Msg
	w.PublishUserGroups(func(m tea.Msg) { got = append(got, m) })
	if len(got) != 1 {
		t.Fatalf("sent %d messages, want 1", len(got))
	}
	msg, ok := got[0].(ui.UserGroupsLoadedMsg)
	if !ok || msg.TeamID != "E1" || msg.UserGroups["S0ORG"] != "org" || msg.UserGroups["S0SRE"] != "sre" || !msg.SelfUserGroups["S0SRE"] {
		t.Fatalf("sent %#v, want both groups and self membership for E1", got[0])
	}
	w.PublishUserGroups(nil)
}
