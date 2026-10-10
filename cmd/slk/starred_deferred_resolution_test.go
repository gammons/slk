package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/service"
	slk "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

func TestResolvedPeerQueueCoalescesWithoutDroppingPeers(t *testing.T) {
	r := newUserResolver("T1", nil, nil, nil, nil, nil, nil)
	names := newUserNameStore(map[string]string{"U1": "Alice", "U2": "Bob"})
	wctx := &WorkspaceContext{UserResolver: r, UserNames: names,
		Channels: []sidebar.ChannelItem{
			{ID: "D1", Name: "U1", Type: "dm", DMUserID: "U1"},
			{ID: "D2", Name: "U2", Type: "dm", DMUserID: "U2"},
		},
		FinderItems: []core.ChannelFinderItem{
			{ID: "D1", Name: "U1", Type: "dm", Joined: true},
			{ID: "D2", Name: "U2", Type: "dm", Joined: true},
		},
	}
	h := &rtmEventHandler{wsCtx: wctx}
	for range 1000 {
		r.queueResolvedPeer("U1")
		r.queueResolvedPeer("U2")
	}
	if len(r.resolvedWake) != 1 {
		t.Fatal("wake-ups were not coalesced")
	}
	<-h.PendingEvents()
	h.OnPendingEvents()
	for i, want := range []string{"Alice", "Bob"} {
		if wctx.Channels[i].Name != want || wctx.FinderItems[i].Name != want {
			t.Fatalf("coalesced repair lost %s: %+v / %+v", want, wctx.Channels[i], wctx.FinderItems[i])
		}
	}
	r.resolvedPeers.Range(func(key, _ any) bool {
		t.Errorf("resolved peer %s was not drained", key)
		return true
	})
}

func TestResolvedPeerDrainSkipsUsersWithoutDMRows(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertUser(cache.User{ID: "U_OTHER", WorkspaceID: "T1", DisplayName: "Other Bot", IsBot: true}); err != nil {
		t.Fatal(err)
	}
	r := newUserResolver("T1", nil, db, nil, nil, nil, nil)
	wctx := &WorkspaceContext{UserResolver: r, UserNames: newUserNameStore(map[string]string{"U1": "Alice"}),
		Channels: []sidebar.ChannelItem{{ID: "D1", Name: "U1", Type: "dm", DMUserID: "U1"}},
	}
	h := &rtmEventHandler{wsCtx: wctx, db: db}
	r.queueResolvedPeer("U_OTHER")
	r.queueResolvedPeer("U1")
	<-h.PendingEvents()
	h.OnPendingEvents()
	if wctx.IsBotUser("U_OTHER") {
		t.Fatal("drain read and imported a cached profile with no DM row")
	}
	if wctx.Channels[0].Name != "Alice" {
		t.Fatal("drain skipped an existing DM peer")
	}
	if _, pending := r.resolvedPeers.Load("U_OTHER"); pending {
		t.Fatal("irrelevant resolution was not consumed")
	}
}

func TestDMSweepFallbackWithoutResolver(t *testing.T) {
	srv := newFakeSlack(t, map[string]string{
		"/api/auth.test":  `{"ok":true,"url":"","team":"T1","team_id":"T1","user_id":"USELF"}`,
		"/api/users.info": `{"ok":true,"user":{"id":"U1","name":"buildbot","is_bot":true,"profile":{"display_name":"Build Bot"}}}`,
	})
	client := newTestClient(t, srv.Server)
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	db := newTestDB(t)
	wctx := &WorkspaceContext{TeamID: "T1", Client: client, UserNames: newUserNameStore(nil),
		UnresolvedDMs: []UnresolvedDM{{ChannelID: "D1", UserID: "U1"}},
	}
	resolveDMNames(wctx, db, nil, nil)
	if name, _ := wctx.UserNames.Get("U1"); name != "Build Bot" || !wctx.IsBotUser("U1") {
		t.Fatalf("nil-resolver fallback: name=%q, bot=%v", name, wctx.IsBotUser("U1"))
	}
}

func TestDMSweepFallbackRepairsInactiveWorkspace(t *testing.T) {
	srv := newFakeSlack(t, map[string]string{
		"/api/auth.test":  `{"ok":true,"url":"","team":"T1","team_id":"T1","user_id":"USELF"}`,
		"/api/users.info": `{"ok":true,"user":{"id":"U1","name":"buildbot","is_bot":true,"profile":{"display_name":"Build Bot","status_text":"Ready"}}}`,
	})
	client := newTestClient(t, srv.Server)
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	db := newTestDB(t)
	r := newUserResolver("T1", nil, db, nil, nil, &fakeBatcher{}, nil)
	wctx := &WorkspaceContext{TeamID: "T1", Client: client, UserResolver: r, UserNames: newUserNameStore(nil),
		UnresolvedDMs: []UnresolvedDM{{ChannelID: "D1", UserID: "U1"}},
		Channels:      []sidebar.ChannelItem{{ID: "D1", Name: "U1", Type: "dm", DMUserID: "U1"}},
		FinderItems:   []core.ChannelFinderItem{{ID: "D1", Name: "U1", Type: "dm", Joined: true}},
	}
	h := &rtmEventHandler{wsCtx: wctx, db: db, isActive: func() bool { return false }}
	resolveDMNames(wctx, db, nil, nil) // edge misses; per-user fallback resolves
	if wctx.Channels[0].Name != "U1" || wctx.FinderItems[0].Name != "U1" {
		t.Fatal("background sweep mutated conversation snapshots")
	}
	select {
	case <-h.PendingEvents():
	case <-time.After(5 * time.Second):
		t.Fatal("sweep fallback did not queue a repair without a UI sender")
	}
	h.OnPendingEvents()
	if item := wctx.Channels[0]; item.Name != "Build Bot" || item.Type != "app" || item.Status.Text != "Ready" {
		t.Fatalf("inactive sidebar = %+v", item)
	}
	if item := wctx.FinderItems[0]; item.Name != "Build Bot" || item.Type != "app" {
		t.Fatalf("inactive finder = %+v", item)
	}
}

func TestUserResolverRequestsUnnamedCachedPeer(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1"}); err != nil {
		t.Fatal(err)
	}
	watch := newResolvedWatch(1, nil)
	r := newUserResolver("T1", nil, db, nil, watch.send,
		&fakeBatcher{res: []edge.User{edgeUserRecord("U1", "alice", "Alice", "", "T1", 1, false)}}, nil)
	r.Request("U1")
	select {
	case <-watch.done:
	case <-time.After(5 * time.Second):
		t.Fatal("an unnamed cached placeholder suppressed deferred resolution")
	}
	u, err := db.GetUser("U1")
	if err != nil || u.DisplayName != "Alice" {
		t.Fatalf("resolved placeholder = %+v, %v", u, err)
	}
}

func TestStarredDeferredResolutionRepairsRowsWithoutReconnect(t *testing.T) {
	for _, active := range []bool{true, false} {
		for _, mode := range []string{"per-user", "edge batch"} {
			for _, failure := range []string{"failed profile", "canceled budget"} {
				t.Run(map[bool]string{true: "active", false: "inactive"}[active]+"/"+mode+"/"+failure, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					var calls atomic.Int32
					allowRetry := make(chan struct{})
					var releaseOnce sync.Once
					releaseRetry := func() { releaseOnce.Do(func() { close(allowRetry) }) }
					srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path != "/api/users.info" {
							t.Errorf("unexpected endpoint: %s", r.URL.Path)
						}
						w.Header().Set("Content-Type", "application/json")
						if calls.Add(1) == 1 {
							if failure == "canceled budget" {
								cancel()
								return
							}
							_, _ = w.Write([]byte(`{"ok":false,"error":"internal_error"}`))
							return
						}
						<-allowRetry
						_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U1","name":"buildbot","is_bot":true,"profile":{"display_name":"Build Bot","status_emoji":":robot_face:","status_text":"Ready"}}}`))
					}))
					defer srv.Close()
					// Unblock a scheduled retry even if an earlier assertion fails.
					defer releaseRetry()
					db := newTestDB(t)
					store := service.NewSectionStore()
					if err := store.Bootstrap(context.Background(), &fakeSectionsClient{sections: []slk.SidebarSection{{ID: "ST", Type: "stars"}}, starIDs: []string{"D1"}}); err != nil {
						t.Fatal(err)
					}
					watch := newResolvedWatch(1, nil)
					var batcher userBatcher
					if mode == "edge batch" {
						batcher = contextUserBatcherFunc(func(batchCtx context.Context, _ map[string]int64) ([]edge.User, error) {
							if batchCtx == ctx {
								return nil, nil // initial batch misses; synchronous fallback fails
							}
							<-allowRetry
							u := edgeUserRecord("U1", "buildbot", "Build Bot", "", "T1", 1, true)
							u.Profile.StatusEmoji = ":robot_face:"
							u.Profile.StatusText = "Ready"
							return []edge.User{u}, nil
						})
					}
					resolver := newUserResolver("T1", newTestClient(t, srv), db, nil, watch.send, batcher, nil)
					names := newUserNameStore(nil)
					resolver.names = names
					wctx := &WorkspaceContext{TeamID: "T1", SectionStore: store, UserNames: names, UserResolver: resolver, LastVisitedByChannel: map[string]int64{"D1": 10}}
					sender := &captureSender{}
					h := &rtmEventHandler{
						wsCtx: wctx, workspaceID: "T1", db: db, program: sender, isActive: func() bool { return active },
						channelNames: map[string]string{}, channelTypes: map[string]string{},
						resolveConversation: func(context.Context, string) (*slack.Channel, error) {
							return &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1", IsIM: true, User: "U1"}}}, nil
						},
					}
					// This is the reconnect recovery pass. Its failed profile lookup
					// must not prevent adding the conversation with its fallback name.
					h.reconcileStarredConversations(ctx)
					if len(wctx.Channels) != 1 || len(wctx.FinderItems) != 1 || wctx.Channels[0].Name != "U1" || wctx.FinderItems[0].Name != "U1" {
						t.Fatalf("initial recovery = %+v / %+v", wctx.Channels, wctx.FinderItems)
					}
					if len(wctx.UnresolvedDMs) != 0 {
						t.Fatal("recovered row unexpectedly belongs to the startup sweep")
					}
					wctx.FinderItems[0].LastVisited = 99
					before := wctx.Channels[0]
					sender.sent = nil
					// Production must schedule the retry for this quiet peer itself:
					// no message, membership lookup, or manual Request triggers it.
					releaseRetry()
					select {
					case <-watch.done:
					case <-time.After(5 * time.Second):
						t.Fatal("background profile lookup did not complete")
					}
					// The background goroutine may update synchronized profile state,
					// but must leave the conversation slices to the event owner.
					if wctx.Channels[0] != before || wctx.FinderItems[0].Name != "U1" || len(sender.sent) != 0 {
						t.Fatal("background resolution mutated conversation snapshots directly")
					}
					pending := slk.PendingEventHandler(h)
					select {
					case <-pending.PendingEvents():
					case <-time.After(5 * time.Second):
						t.Fatal("successful resolution did not queue a repair")
					}
					pending.OnPendingEvents() // no second OnConnect/reconciliation
					item, finder := wctx.Channels[0], wctx.FinderItems[0]
					if item.Name != "Build Bot" || item.Type != "app" || item.Status.Text != "Ready" || item.Section != before.Section || item.SectionOrder != before.SectionOrder || item.ChannelOrder != before.ChannelOrder {
						t.Fatalf("repaired sidebar = %+v", item)
					}
					if finder.Name != "Build Bot" || finder.Type != "app" || finder.LastVisited != 99 || !finder.Joined {
						t.Fatalf("repaired finder = %+v", finder)
					}
					if len(wctx.Channels) != 1 || len(wctx.FinderItems) != 1 || h.channelNames["D1"] != "Build Bot" || h.channelTypes["D1"] != "app" {
						t.Fatal("repair duplicated rows or left stale notifier metadata")
					}
					cached, err := db.GetChannel("D1")
					if err != nil || cached.Type != "app" {
						t.Fatalf("cached conversation = %+v, %v", cached, err)
					}
					wantPublications := 0
					if active {
						wantPublications = 1
					}
					if len(sender.sent) != wantPublications {
						t.Fatalf("repair publications = %d, want %d", len(sender.sent), wantPublications)
					}
					if active {
						msg, ok := sender.sent[0].(ui.ConversationOpenedMsg)
						if !ok || msg.TeamID != "T1" || msg.Item != item || msg.FinderItem != finder {
							t.Fatalf("repair publication = %+v", sender.sent[0])
						}
					}
					pending.OnPendingEvents()
					if len(sender.sent) != wantPublications {
						t.Fatal("draining an empty repair queue republished rows")
					}
				})
			}
		}
	}
}
