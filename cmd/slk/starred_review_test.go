package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/service"
	slk "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/ui"
	"github.com/slack-go/slack"
)

func TestStarredRecoveredPeerRetries(t *testing.T) {
	for _, mode := range []string{"transient failure", "shared context canceled", "cache later filled", "delayed bot classification", "inactive workspace"} {
		t.Run(mode, func(t *testing.T) {
			db := newTestDB(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls atomic.Int32
			allowRetry := make(chan struct{})
			var releaseOnce sync.Once
			releaseRetry := func() { releaseOnce.Do(func() { close(allowRetry) }) }
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n := calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if n == 1 {
					if mode == "shared context canceled" {
						cancel()
					}
					_, _ = w.Write([]byte(`{"ok":false,"error":"temporarily_unavailable"}`))
					return
				}
				<-allowRetry
				_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U1","name":"alice","is_bot":true,"profile":{"display_name":"Alice"}}}`))
			}))
			defer srv.Close()
			defer releaseRetry()
			store := service.NewSectionStore()
			if err := store.Bootstrap(context.Background(), &fakeSectionsClient{sections: []slk.SidebarSection{{ID: "ST", Type: "stars"}}, starIDs: []string{"D1"}}); err != nil {
				t.Fatal(err)
			}
			wctx := &WorkspaceContext{TeamID: "T1", SectionStore: store, UserNames: newUserNameStore(nil)}
			watch := newResolvedWatch(1, nil)
			wctx.UserResolver = newUserResolver("T1", newTestClient(t, srv), db, nil, watch.send, nil, nil)
			wctx.UserResolver.names = wctx.UserNames
			if mode == "cache later filled" || mode == "inactive workspace" {
				// Model a lookup already owned by another producer. Reconnect
				// must not schedule a duplicate while that producer fills cache.
				wctx.UserResolver.inflight.Store("U1", struct{}{})
			}
			sender := &captureSender{}
			h := &rtmEventHandler{workspaceID: "T1", wsCtx: wctx, db: db, program: sender, isActive: func() bool { return mode != "inactive workspace" }}
			metadataCalls := 0
			h.resolveConversation = func(context.Context, string) (*slack.Channel, error) {
				metadataCalls++
				return &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1", IsIM: true, User: "U1"}}}, nil
			}
			if mode == "delayed bot classification" {
				if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", DisplayName: "Alice"}); err != nil {
					t.Fatal(err)
				}
			}
			h.reconcileStarredConversations(ctx)
			if len(wctx.Channels) != 1 || len(wctx.FinderItems) != 1 {
				t.Fatalf("first insertion: %+v / %+v", wctx.Channels, wctx.FinderItems)
			}
			if mode != "delayed bot classification" && wctx.Channels[0].Name != "U1" {
				t.Fatalf("failed lookup row = %+v", wctx.Channels[0])
			}
			if mode == "cache later filled" || mode == "delayed bot classification" || mode == "inactive workspace" {
				if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", DisplayName: "Alice", IsBot: true}); err != nil {
					t.Fatal(err)
				}
			}
			releaseRetry()
			if mode == "transient failure" || mode == "shared context canceled" {
				select {
				case <-watch.done:
				case <-time.After(5 * time.Second):
					t.Fatal("scheduled profile retry did not finish")
				}
			}
			wctx.UserResolver.inflight.Delete("U1")
			h.reconcileStarredConversations(context.Background())
			if metadataCalls != 1 || len(wctx.Channels) != 1 || len(wctx.FinderItems) != 1 {
				t.Fatalf("retry duplicated/looked up row: metadata=%d", metadataCalls)
			}
			item, finder := wctx.Channels[0], wctx.FinderItems[0]
			if item.Name != "Alice" || finder.Name != "Alice" || item.Type != "app" || finder.Type != "app" || item.Section != "ST" {
				t.Fatalf("retry left stale workspace/switch snapshots: %+v / %+v", item, finder)
			}
			wantCalls := int32(1)
			if mode == "transient failure" || mode == "shared context canceled" {
				wantCalls = 2
			}
			if mode == "delayed bot classification" {
				wantCalls = 0
			}
			if calls.Load() != wantCalls {
				t.Fatalf("profiles=%d, want %d", calls.Load(), wantCalls)
			}
			if mode == "inactive workspace" {
				if len(sender.sent) != 0 {
					t.Fatalf("inactive UI publication: %+v", sender.sent)
				}
				// main.go passes these snapshots when switching workspaces.
				// Verify the real reducer/view sees the repaired peer, not the
				// failed first lookup's user-ID placeholder.
				app := ui.NewApp()
				app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
				app.Update(ui.WorkspaceSwitchedMsg{TeamID: "other"})
				app.Update(ui.WorkspaceSwitchedMsg{TeamID: "T1", Channels: wctx.Channels,
					FinderItems: wctx.FinderItems, SectionsProvider: sectionsProviderAdapter{store: store}})
				if view := app.View().Content; !strings.Contains(view, "Alice") {
					t.Fatalf("workspace switch did not render repaired peer: %s", view)
				}
			} else {
				last := sender.sent[len(sender.sent)-1].(ui.ConversationOpenedMsg)
				if last.Item.Name != "Alice" || last.FinderItem.Type != "app" {
					t.Fatalf("active UI not repaired: %+v", last)
				}
			}
		})
	}
}

// Exercise the actual background sweep while the serialized event owner both
// hydrates starred rows and builds items. Only bot classification/name/cache
// state is shared: the sweep must never mutate the owner's conversation slices.
func TestStarredReconciliationConcurrentDMSweep(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", DisplayName: "Bot", IsBot: true}); err != nil {
		t.Fatal(err)
	}
	store := service.NewSectionStore()
	if err := store.Bootstrap(context.Background(), &fakeSectionsClient{sections: []slk.SidebarSection{{ID: "ST", Type: "stars"}}, starIDs: []string{"D1"}}); err != nil {
		t.Fatal(err)
	}
	wctx := &WorkspaceContext{TeamID: "T1", SectionStore: store, UserNames: newUserNameStore(map[string]string{"U1": "Bot"}), UnresolvedDMs: []UnresolvedDM{{ChannelID: "D1", UserID: "U1"}}}
	wctx.UserResolver = newUserResolver("T1", nil, db, nil, nil, &fakeBatcher{res: []edge.User{edgeUserRecord("U1", "bot", "Bot", "", "T1", 1, true)}}, nil)
	wctx.UserResolver.names = wctx.UserNames
	ch := slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1", IsIM: true, User: "U1"}}}
	h := &rtmEventHandler{workspaceID: "T1", wsCtx: wctx, db: db, resolveConversation: func(context.Context, string) (*slack.Channel, error) { return &ch, nil }}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 64; i++ {
			resolveDMNames(wctx, db, nil, nil)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 64; i++ {
			wctx.Channels = nil
			wctx.FinderItems = nil
			h.reconcileStarredConversations(context.Background())
			buildChannelItem(ch, wctx, h.cfg, "T1")
		}
	}()
	close(start)
	wg.Wait()
	if len(wctx.Channels) != 1 || wctx.Channels[0].Type != "app" {
		t.Fatalf("classification after concurrent sweep: %+v", wctx.Channels)
	}
}
