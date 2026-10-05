package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/service"
	slk "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

func TestReconcileStarredConversations_UncachedPeer(t *testing.T) {
	for _, active := range []bool{true, false} {
		t.Run(map[bool]string{true: "active", false: "inactive"}[active], func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/users.info" {
					t.Errorf("unexpected endpoint: %s", r.URL.Path)
				}
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U1","name":"buildbot","is_bot":true,"profile":{"display_name":"Build Bot","status_emoji":":robot_face:","status_text":"Ready"}}}`))
			}))
			defer srv.Close()
			db := newTestDB(t)
			store := service.NewSectionStore()
			if err := store.Bootstrap(context.Background(), &fakeSectionsClient{sections: []slk.SidebarSection{{ID: "ST", Type: "stars"}}, starIDs: []string{"D1"}}); err != nil {
				t.Fatal(err)
			}
			names := newUserNameStore(nil)
			var resolved []tea.Msg
			resolver := newUserResolver("T1", newTestClient(t, srv), db, nil, func(m tea.Msg) { resolved = append(resolved, m) }, nil, nil)
			resolver.names = names
			wctx := &WorkspaceContext{SectionStore: store, UserNames: names, UserResolver: resolver}
			sender := &captureSender{}
			h := &rtmEventHandler{wsCtx: wctx, workspaceID: "T1", db: db, program: sender, isActive: func() bool { return active }}
			h.resolveConversation = func(context.Context, string) (*slack.Channel, error) {
				return &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1", IsIM: true, User: "U1"}}}, nil
			}
			h.reconcileStarredConversations(context.Background())
			h.reconcileStarredConversations(context.Background())
			if calls.Load() != 1 || len(wctx.Channels) != 1 || len(wctx.FinderItems) != 1 {
				t.Fatalf("calls=%d channels=%+v finder=%+v", calls.Load(), wctx.Channels, wctx.FinderItems)
			}
			item := wctx.Channels[0]
			if item.Name != "Build Bot" || item.Section != "ST" || item.Type != "app" || item.DMUserID != "U1" || item.Status.Text != "Ready" {
				t.Fatalf("hydrated item = %+v", item)
			}
			if wctx.FinderItems[0].Name != "Build Bot" || wctx.FinderItems[0].Type != "app" {
				t.Fatalf("finder = %+v", wctx.FinderItems[0])
			}
			if name, _ := names.Get("U1"); name != "Build Bot" {
				t.Fatalf("switch snapshot name = %s", name)
			}
			if len(resolved) != 2 {
				t.Fatalf("resolver notifications = %+v", resolved)
			}
			if !active {
				if len(sender.sent) != 0 {
					t.Fatalf("inactive publication: %+v", sender.sent)
				}
			} else {
				if len(sender.sent) != 1 {
					t.Fatalf("active publications = %+v", sender.sent)
				}
				msg, ok := sender.sent[0].(ui.ConversationOpenedMsg)
				if !ok || msg.TeamID != "T1" || msg.Item.Name != "Build Bot" {
					t.Fatalf("publication = %+v", sender.sent[0])
				}
			}
		})
	}
}

func TestStarredSectionRefreshClearsObsoleteMembership(t *testing.T) {
	store := service.NewSectionStore()
	wctx := &WorkspaceContext{SectionStore: store, Channels: []sidebar.ChannelItem{
		{ID: "D1", Name: "Alice", Type: "dm"}, {ID: "C1", Name: "general", Type: "channel"},
	}}
	sender := &captureSender{}
	h := &rtmEventHandler{wsCtx: wctx, workspaceID: "T1", program: sender}
	for _, ids := range [][]string{{"C1", "D1"}, {"D1"}, nil} {
		if err := store.Bootstrap(context.Background(), &fakeSectionsClient{sections: []slk.SidebarSection{{ID: "ST", Type: "stars"}}, starIDs: ids}); err != nil {
			t.Fatal(err)
		}
		h.refreshSectionsForActive()
		for _, item := range wctx.Channels {
			want := ""
			for _, id := range ids {
				if item.ID == id {
					want = "ST"
				}
			}
			if item.Section != want {
				t.Fatalf("%s section=%s, want %s", item.ID, item.Section, want)
			}
		}
		// Publication must not alias the workspace's mutable snapshot.
		msg := sender.sent[len(sender.sent)-1].(ui.SectionsRefreshedMsg)
		msg.Channels[0].Name = "UI-owned name"
		if wctx.Channels[0].Name != "Alice" {
			t.Fatal("refresh exposed workspace slice")
		}
		if len(wctx.Channels) != 2 {
			t.Fatal("refresh duplicated a conversation")
		}
	}
}

func TestStarredPeerResolutionCancellation(t *testing.T) {
	db := newTestDB(t)
	r := newUserResolver("T1", nil, db, nil, nil, nil, nil)
	// A canceled synchronous pass must not wait behind background requests
	// or delete an inflight claim owned by one of them.
	r.sem = make(chan struct{}, 1)
	r.sem <- struct{}{}
	r.inflight.Store("U1", struct{}{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.resolveOneContext(ctx, "U1")
	if _, ok := r.inflight.Load("U1"); !ok {
		t.Fatal("direct resolution released another request's claim")
	}
}
