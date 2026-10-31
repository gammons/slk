package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui"
	"github.com/slack-go/slack"
)

func TestChannelResolver_PublicUnjoinedInfoCachesAndCatchesUp(t *testing.T) {
	db := newTestDB(t)
	srv := newFakeSlack(t, map[string]string{
		"/api/conversations.info": `{"ok":true,"channel":{"id":"C9","name":"public-news","is_channel":true,"is_member":false,"topic":{"value":"Public updates"}}}`,
	})
	client := newTestClient(t, srv.Server)
	r := newChannelResolver("T1", db, func(ctx context.Context, id string) (*slack.Channel, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Error("conversations.info has no 10-second deadline")
		}
		return client.GetConversationInfo(ctx, id)
	})
	snapshot, since := r.names.SnapshotForUI()
	learned := make(chan string, 1)
	// The inactive workspace drops this first notice; the next switch must
	// replay the name learned after the snapshot without reading the UI map.
	r.names.NotifyFrom(since, func(id, name string) { learned <- id + ":" + name })
	r.Request("C9")
	if got := <-learned; got != "C9:public-news" {
		t.Fatalf("resolved %q; want C9:public-news", got)
	}

	ch, err := db.GetChannel("C9")
	if err != nil || ch.WorkspaceID != "T1" || ch.Name != "public-news" || ch.Type != "channel" || ch.Topic != "Public updates" || ch.IsMember {
		t.Fatalf("cached channel = %+v, %v; want unjoined public-news in T1", ch, err)
	}
	joined, err := db.ListChannels("T1", true)
	if err != nil || len(joined) != 0 {
		t.Fatalf("joined channels = %+v, %v; resolving a mention must not join it", joined, err)
	}
	if name, ok := r.names.Get("C9"); !ok || name != "public-news" {
		t.Fatalf("name = (%q, %v); want public-news", name, ok)
	}
	if _, ok := snapshot["C9"]; ok {
		t.Fatal("background resolution mutated the UI-owned snapshot")
	}
	snapshot["C9"] = "UI-owned"
	sent := make(chan tea.Msg, 1)
	r.names.NotifyFrom(since, uiChannelNameNotifier("T1", func(msg tea.Msg) { sent <- msg }))
	if got := <-sent; got != (ui.ChannelResolvedMsg{TeamID: "T1", ChannelID: "C9", Name: "public-news"}) {
		t.Fatalf("catch-up sent %#v; want T1/C9/public-news", got)
	}

	req := srv.requestTo(t, "/api/conversations.info")
	if req.form.Get("channel") != "C9" {
		t.Errorf("conversations.info channel = %q; want C9", req.form.Get("channel"))
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.reqs) != 1 {
		t.Errorf("requests = %+v; resolving a mention must only call conversations.info", srv.reqs)
	}
}

func TestChannelResolver_CacheIsWorkspaceAndChannelScoped(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertWorkspace(cache.Workspace{ID: "T2", Name: "Other"}); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []cache.Channel{
		{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"},
		{ID: "G1", WorkspaceID: "T1", Name: "project", Type: "private"},
		{ID: "C2", WorkspaceID: "T2", Name: "wrong-workspace", Type: "channel"},
		{ID: "D1", WorkspaceID: "T1", Name: "alice", Type: "dm"},
		{ID: "G2", WorkspaceID: "T1", Name: "mpdm-a--b-1", Type: "group_dm"},
		{ID: "C3", WorkspaceID: "T1", Type: "channel"},
	} {
		if err := db.UpsertChannel(ch); err != nil {
			t.Fatal(err)
		}
	}
	r := newChannelResolver("T1", db, func(context.Context, string) (*slack.Channel, error) {
		t.Error("cached names must not fetch conversations.info")
		return nil, nil
	})
	if got, want := r.names.Snapshot(), map[string]string{"C1": "general", "G1": "project"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("seeded names = %v; want %v", got, want)
	}
	r.Request("C1")
	r.Request("G1")

	// Cache rows can arrive after construction, before their first mention.
	if err := db.UpsertChannel(cache.Channel{ID: "C4", WorkspaceID: "T1", Name: "later", Type: "channel"}); err != nil {
		t.Fatal(err)
	}
	r.Request("C4")
	if got, ok := r.names.Get("C4"); !ok || got != "later" {
		t.Fatalf("late cache hit = (%q, %v); want later", got, ok)
	}
}

func TestChannelResolver_WrongWorkspaceCacheDoesNotSupplyOrOverwriteName(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertWorkspace(cache.Workspace{ID: "T2", Name: "Other"}); err != nil {
		t.Fatal(err)
	}
	foreign := cache.Channel{ID: "C9", WorkspaceID: "T2", Name: "foreign-name", Type: "channel", IsMember: true, IsStarred: true}
	if err := db.UpsertChannel(foreign); err != nil {
		t.Fatal(err)
	}
	srv := newFakeSlack(t, map[string]string{
		"/api/conversations.info": `{"ok":true,"channel":{"id":"C9","name":"source-name","is_channel":true}}`,
	})
	r := newChannelResolver("T1", db, newTestClient(t, srv.Server).GetConversationInfo)
	if _, ok := r.names.Get("C9"); ok {
		t.Fatal("constructor imported a different workspace's cached name")
	}
	learned := make(chan string, 1)
	r.names.NotifyFrom(0, func(_, name string) { learned <- name })
	r.Request("C9")
	if name := <-learned; name != "source-name" {
		t.Fatalf("resolved name = %q; want source-name", name)
	}
	if got, err := db.GetChannel("C9"); err != nil || got != foreign {
		t.Fatalf("foreign cache row changed to %+v, %v; want %+v", got, err, foreign)
	}
	other := newChannelResolver("T2", db, nil)
	if got, ok := other.names.Get("C9"); !ok || got != "foreign-name" {
		t.Fatalf("T2 name = (%q, %v); want foreign-name", got, ok)
	}
}

func TestChannelResolver_PreservesCachedMembershipAndStar(t *testing.T) {
	for _, private := range []bool{false, true} {
		t.Run(fmt.Sprintf("private=%v", private), func(t *testing.T) {
			db := newTestDB(t)
			r := newChannelResolver("T1", db, nil)
			// An unnamed row still needs hydration. Membership and the star
			// are independent metadata that the info response may omit.
			if err := db.UpsertChannel(cache.Channel{ID: "C9", WorkspaceID: "T1", Type: "channel", IsMember: true, IsStarred: true}); err != nil {
				t.Fatal(err)
			}
			srv := newFakeSlack(t, map[string]string{
				"/api/conversations.info": fmt.Sprintf(`{"ok":true,"channel":{"id":"C9","name":"project","is_channel":true,"is_private":%v}}`, private),
			})
			r.lookup = newTestClient(t, srv.Server).GetConversationInfo
			learned := make(chan struct{}, 1)
			r.names.NotifyFrom(0, func(string, string) { learned <- struct{}{} })
			r.Request("C9")
			<-learned
			wantType := "channel"
			if private {
				wantType = "private"
			}
			if ch, err := db.GetChannel("C9"); err != nil || ch.Name != "project" || ch.Type != wantType || !ch.IsMember || !ch.IsStarred {
				t.Fatalf("cached channel = %+v, %v; want %s project with membership and star preserved", ch, err, wantType)
			}
		})
	}
}

func TestChannelResolver_ConcurrentRequestsDeduplicate(t *testing.T) {
	db := newTestDB(t)
	started := make(chan struct{}, 100)
	release := make(chan struct{})
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":{"id":"C9","name":"public-news","is_channel":true}}`))
	}))
	defer srv.Close()
	r := newChannelResolver("T1", db, newTestClient(t, srv).GetConversationInfo)
	learned := make(chan struct{}, 100)
	r.names.NotifyFrom(0, func(string, string) { learned <- struct{}{} })
	r.Request("C9")
	<-started

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Request("C9")
			// UI-owned copies may be written while Request reads the store.
			copy, _ := r.names.SnapshotForUI()
			copy["C9"] = "UI-owned"
		}()
	}
	wg.Wait() // every Request returned while the HTTP response was blocked
	close(release)
	<-learned // cache write completed before the notification
	r.Request("C9")
	if got := calls.Load(); got != 1 {
		t.Fatalf("conversations.info requests = %d; want 1", got)
	}
}

func TestChannelResolver_BoundsConcurrentLookupsWithoutBlockingCaller(t *testing.T) {
	const requests = userResolverConcurrency * 3
	db := newTestDB(t)
	var active, peak atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		current := active.Add(1)
		for old := peak.Load(); current > old; old = peak.Load() {
			if peak.CompareAndSwap(old, current) {
				break
			}
		}
		<-release
		// As in the user resolver regression, this stimulus gives an
		// unbounded implementation room to reveal excess concurrency.
		time.Sleep(25 * time.Millisecond)
		active.Add(-1)
		_ = req.ParseForm()
		id := req.Form.Get("channel")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"channel":{"id":%q,"name":%q,"is_channel":true}}`, id, "name-"+id)
	}))
	defer srv.Close()
	r := newChannelResolver("T1", db, newTestClient(t, srv).GetConversationInfo)
	learned := make(chan struct{}, requests)
	r.names.NotifyFrom(0, func(string, string) { learned <- struct{}{} })
	for i := range requests {
		r.Request(fmt.Sprintf("C%d", i))
	}
	close(release) // all Requests returned before any lookup could complete
	for range requests {
		<-learned
	}
	if got := peak.Load(); got > userResolverConcurrency {
		t.Fatalf("peak conversations.info concurrency = %d; want at most %d", got, userResolverConcurrency)
	}
}

func TestChannelResolver_FailureCooldownIsPerChannel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		r := newChannelResolver("T1", nil, func(_ context.Context, id string) (*slack.Channel, error) {
			calls.Add(1)
			return nil, fmt.Errorf("channel %s not found", id)
		})
		for range 3 {
			r.Request("C9")
			synctest.Wait()
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("repeated failed lookups = %d; want 1 during cooldown", got)
		}
		r.Request("C10")
		synctest.Wait()
		if got := calls.Load(); got != 2 {
			t.Fatalf("lookups after a different channel = %d; want 2", got)
		}
	})
}

func TestChannelResolver_LaterFailuresPruneExpiredCooldowns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		r := newChannelResolver("T1", nil, func(_ context.Context, id string) (*slack.Channel, error) {
			calls.Add(1)
			return nil, fmt.Errorf("channel %s not found", id)
		})
		r.Request("C_OLD")
		synctest.Wait()
		time.Sleep(channelResolverRetryDelay / 2)
		r.Request("C_RECENT")
		synctest.Wait()
		time.Sleep(channelResolverRetryDelay / 2)
		r.Request("C_NEW")
		synctest.Wait()
		if _, retained := r.retryAfter.Load("C_OLD"); retained {
			t.Fatal("expired cooldown retained even though another channel failed")
		}
		r.Request("C_RECENT")
		r.Request("C_NEW")
		synctest.Wait()
		if got := calls.Load(); got != 3 {
			t.Fatalf("lookups after pruning = %d; want 3 with active cooldowns preserved", got)
		}
	})
}

func TestChannelResolver_RetriesOnDemandAfterCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		r := newChannelResolver("T1", nil, func(_ context.Context, id string) (*slack.Channel, error) {
			if calls.Add(1) == 1 {
				return nil, fmt.Errorf("channel %s not found", id)
			}
			ch := new(slack.Channel)
			ch.ID, ch.Name, ch.IsChannel = id, "public-news", true
			return ch, nil
		})
		r.Request("C9")
		synctest.Wait()
		time.Sleep(channelResolverRetryDelay - time.Nanosecond)
		r.Request("C9")
		synctest.Wait()
		if got := calls.Load(); got != 1 {
			t.Fatalf("lookups before retry deadline = %d; want 1", got)
		}

		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if got := calls.Load(); got != 1 {
			t.Fatalf("automatic lookups at retry deadline = %d; want 1", got)
		}
		r.Request("C9")
		synctest.Wait()
		if got := calls.Load(); got != 2 {
			t.Fatalf("lookups after retry deadline = %d; want 2", got)
		}
		if name, ok := r.names.Get("C9"); !ok || name != "public-news" {
			t.Fatalf("name after access changed = (%q, %v); want public-news", name, ok)
		}

		time.Sleep(channelResolverRetryDelay)
		r.Request("C9")
		synctest.Wait()
		if got := calls.Load(); got != 2 {
			t.Fatalf("lookups after successful resolution = %d; want 2", got)
		}
	})
}

func TestChannelResolver_CacheHitBypassesFailureCooldown(t *testing.T) {
	db := newTestDB(t)
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		r := newChannelResolver("T1", db, func(_ context.Context, id string) (*slack.Channel, error) {
			calls.Add(1)
			return nil, fmt.Errorf("channel %s not found", id)
		})
		r.Request("C9")
		synctest.Wait()
		if err := db.UpsertChannel(cache.Channel{ID: "C9", WorkspaceID: "T1", Name: "now-accessible", Type: "channel"}); err != nil {
			t.Fatal(err)
		}
		r.Request("C9")
		synctest.Wait()
		if name, ok := r.names.Get("C9"); !ok || name != "now-accessible" {
			t.Fatalf("name during cooldown = (%q, %v); want now-accessible", name, ok)
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("lookups after cache hit = %d; want 1", got)
		}
	})
}

func TestChannelResolver_RefusalsAndNonChannelMetadataStayUnresolved(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"inaccessible", `{"ok":false,"error":"channel_not_found"}`},
		{"missing scope", `{"ok":false,"error":"missing_scope"}`},
		{"different ID", `{"ok":true,"channel":{"id":"COTHER","name":"wrong","is_channel":true}}`},
		{"private without name", `{"ok":true,"channel":{"id":"C9","is_channel":true,"is_private":true}}`},
		{"direct message", `{"ok":true,"channel":{"id":"C9","name":"alice","is_im":true}}`},
		{"group direct message", `{"ok":true,"channel":{"id":"C9","name":"mpdm-a--b-1","is_mpim":true,"is_private":true}}`},
		{"unknown type", `{"ok":true,"channel":{"id":"C9","name":"unclassified"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			srv := newFakeSlack(t, map[string]string{"/api/conversations.info": tc.body})
			r := newChannelResolver("T1", db, newTestClient(t, srv.Server).GetConversationInfo)
			r.names.NotifyFrom(0, func(id, name string) {
				t.Errorf("refused metadata notified %s=%s", id, name)
			})
			// Run the same worker synchronously so negative results have a
			// completion barrier without polling or a timing assertion.
			r.resolve("C9")
			if got := r.names.Snapshot(); len(got) != 0 {
				t.Fatalf("unresolved names = %v; want empty", got)
			}
			if got, err := db.ListChannels("T1", false); err != nil || len(got) != 0 {
				t.Fatalf("cached channels = %+v, %v; refused metadata must not be persisted", got, err)
			}
		})
	}
}

func TestChannelResolver_Optional(t *testing.T) {
	var absent *channelResolver
	absent.Request("C9")
	r := newChannelResolver("T1", nil, nil)
	r.Request("")
	r.Request("C9")
	if got := r.names.Snapshot(); len(got) != 0 {
		t.Fatalf("optional resolver invented names: %v", got)
	}
}

func TestUIChannelNameNotifier_DoesNotBlockCaller(t *testing.T) {
	sent := make(chan tea.Msg, 1)
	release := make(chan struct{})
	defer close(release)
	r := newChannelResolver("T1", nil, nil)
	r.names.NotifyFrom(0, uiChannelNameNotifier("T1", func(msg tea.Msg) {
		sent <- msg
		<-release
	}))
	returned := make(chan struct{})
	go func() {
		r.names.Set("C9", "public-news")
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("name Set blocked on UI send; a synchronous cache read would deadlock")
	}
	if got := <-sent; got != (ui.ChannelResolvedMsg{TeamID: "T1", ChannelID: "C9", Name: "public-news"}) {
		t.Fatalf("sent %#v; want T1/C9/public-news", got)
	}
}
