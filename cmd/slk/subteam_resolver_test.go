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
	// block holds a lookup of the ID until the channel is closed.
	block map[string]chan struct{}
	// limitOnce answers the ID's first lookup with a 429 carrying
	// this Retry-After.
	limitOnce map[string]time.Duration
}

func (f *fakeSubteamLookup) lookup(_ context.Context, id string) (slackclient.UserGroupInfo, error) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[id]++
	gate := f.block[id]
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if d, ok := f.limitOnce[id]; ok && f.calls[id] == 1 {
		return slackclient.UserGroupInfo{}, &slack.RateLimitedError{RetryAfter: d}
	}
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

func waitSubteamLookup(t *testing.T, f *fakeSubteamLookup, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f.count(id) > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%s was never looked up", id)
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

// An ID resolved earlier in a batch stays pending until its handle is
// merged, so a request for it while a later lookup of the same batch is
// still running doesn't fetch it again.
func TestSubteamResolver_ResolvedIDStaysPendingUntilMerged(t *testing.T) {
	w := &WorkspaceContext{}
	gate := make(chan struct{})
	f := &fakeSubteamLookup{
		res: map[string]slackclient.UserGroupInfo{
			"S0A": {ID: "S0A", Handle: "a"},
			"S0B": {ID: "S0B", Handle: "b"},
		},
		block: map[string]chan struct{}{"S0B": gate},
	}
	r, _, _ := newTestSubteamResolver(f, w)

	r.Request([]string{"S0A", "S0B"})
	waitSubteamLookup(t, f, "S0B") // S0A done, its handle not yet merged
	r.Request([]string{"S0A"})
	close(gate)
	waitSubteamsIdle(t, r)

	if n := f.count("S0A"); n != 1 {
		t.Fatalf("S0A looked up %d times, want 1", n)
	}
	if got := w.UserGroups(); got["S0A"] != "a" || got["S0B"] != "b" {
		t.Fatalf("map = %v, want both groups", got)
	}
}

// A queued ID whose handle arrives some other way (usergroups.list)
// before the worker reaches it is not looked up.
func TestSubteamResolver_SkipsIDKnownBeforeLookup(t *testing.T) {
	w := &WorkspaceContext{}
	gate := make(chan struct{})
	f := &fakeSubteamLookup{
		res:   map[string]slackclient.UserGroupInfo{"S0A": {ID: "S0A", Handle: "a"}},
		block: map[string]chan struct{}{"S0A": gate},
	}
	r, _, _ := newTestSubteamResolver(f, w)

	r.Request([]string{"S0A", "S0B"})
	waitSubteamLookup(t, f, "S0A")
	w.MergeUserGroups(map[string]string{"S0B": "b-from-list"})
	close(gate)
	waitSubteamsIdle(t, r)

	if n := f.count("S0B"); n != 0 {
		t.Fatalf("S0B looked up %d times after it became known, want 0", n)
	}
}

// A 429 publishes what the batch already found, waits out Slack's
// Retry-After without trying the rest of the batch, then retries the
// same ID and carries on: nothing is dropped.
func TestSubteamResolver_RateLimitWaitsAndRetries(t *testing.T) {
	w := &WorkspaceContext{}
	f := &fakeSubteamLookup{
		res: map[string]slackclient.UserGroupInfo{
			"S0A": {ID: "S0A", Handle: "a"},
			"S0B": {ID: "S0B", Handle: "b"},
			"S0C": {ID: "S0C", Handle: "c"},
		},
		limitOnce: map[string]time.Duration{"S0B": 3 * time.Second},
	}
	r, published, mu := newTestSubteamResolver(f, w)
	var waited []time.Duration
	var cDuringWait int
	var aPublishedBeforeWait bool
	r.wait = func(_ context.Context, err error) (bool, error) {
		var rl *slack.RateLimitedError
		if !errors.As(err, &rl) {
			t.Errorf("wait got %v, want a RateLimitedError", err)
		}
		waited = append(waited, rl.RetryAfter)
		cDuringWait = f.count("S0C")
		mu.Lock()
		aPublishedBeforeWait = len(*published) > 0 && (*published)[len(*published)-1]["S0A"] == "a"
		mu.Unlock()
		return true, nil
	}

	r.Request([]string{"S0A", "S0B", "S0C"})
	waitSubteamsIdle(t, r)

	if !slices.Equal(waited, []time.Duration{3 * time.Second}) {
		t.Fatalf("waited %v, want [3s]", waited)
	}
	if cDuringWait != 0 {
		t.Errorf("S0C looked up during the cooldown")
	}
	if !aPublishedBeforeWait {
		t.Errorf("S0A, resolved before the 429, was not published before the wait")
	}
	if n := f.count("S0B"); n != 2 {
		t.Errorf("S0B looked up %d times, want 2 (429, then retry)", n)
	}
	got := w.UserGroups()
	for id, h := range map[string]string{"S0A": "a", "S0B": "b", "S0C": "c"} {
		if got[id] != h {
			t.Fatalf("map = %v, want all three groups", got)
		}
	}
}

// A cancelled cooldown stops the worker and leaves nothing pending, so
// a later request can queue the IDs again.
func TestSubteamResolver_RateLimitCancelledStopsCleanly(t *testing.T) {
	w := &WorkspaceContext{}
	f := &fakeSubteamLookup{limitOnce: map[string]time.Duration{"S0A": time.Second}}
	r, _, _ := newTestSubteamResolver(f, w)
	r.wait = func(context.Context, error) (bool, error) { return true, context.Canceled }

	r.Request([]string{"S0A", "S0B"})
	waitSubteamsIdle(t, r)

	if n := f.count("S0B"); n != 0 {
		t.Errorf("S0B looked up %d times after the wait was cancelled, want 0", n)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending) != 0 || r.running {
		t.Fatalf("pending = %v, running = %v after cancel, want none and stopped", r.pending, r.running)
	}
}
