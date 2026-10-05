package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/service"
	slk "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/ui"
	"github.com/slack-go/slack"
)

type contextUserBatcherFunc func(context.Context, map[string]int64) ([]edge.User, error)

func (f contextUserBatcherFunc) UsersInfo(ctx context.Context, ids map[string]int64) ([]edge.User, error) {
	return f(ctx, ids)
}

func TestStarredPeerBatchCancellationAndRetry(t *testing.T) {
	db := newTestDB(t)
	store := service.NewSectionStore()
	if err := store.Bootstrap(context.Background(), &fakeSectionsClient{sections: []slk.SidebarSection{{ID: "ST", Type: "stars"}}, starIDs: []string{"D1", "D2"}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	batches := 0
	batcher := contextUserBatcherFunc(func(batchCtx context.Context, ids map[string]int64) ([]edge.User, error) {
		batches++
		if len(ids) != 2 {
			t.Fatalf("batch IDs = %v, want both unknown peers", ids)
		}
		if batches == 1 {
			if batchCtx != ctx {
				t.Fatal("batch lost caller's cancellation context")
			}
			cancel() // emulate exhaustion of the shared reconnect HTTP budget
			return nil, batchCtx.Err()
		}
		return []edge.User{
			edgeUserRecord("U1", "alice", "Alice", "", "T1", 1, false),
			edgeUserRecord("U2", "bob", "Bob", "", "T1", 1, true),
		}, nil
	})
	wctx := &WorkspaceContext{TeamID: "T1", SectionStore: store, UserNames: newUserNameStore(nil)}
	// A nil per-user client makes any serial profile fallback a test failure.
	wctx.UserResolver = newUserResolver("T1", nil, db, nil, nil, batcher, nil)
	wctx.UserResolver.names = wctx.UserNames
	metadata := 0
	h := &rtmEventHandler{workspaceID: "T1", wsCtx: wctx, db: db, resolveConversation: func(_ context.Context, id string) (*slack.Channel, error) {
		metadata++
		return &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: id, IsIM: true, User: "U" + id[1:]}}}, nil
	}}
	h.reconcileStarredConversations(ctx)
	if len(wctx.Channels) != 2 || wctx.Channels[0].Name != "U1" {
		t.Fatalf("canceled batch lost rows: %+v", wctx.Channels)
	}
	h.reconcileStarredConversations(context.Background())
	if metadata != 2 || batches != 2 {
		t.Fatalf("metadata=%d batches=%d, want 2 each across both passes", metadata, batches)
	}
	if wctx.Channels[0].Name != "Alice" || wctx.FinderItems[1].Name != "Bob" || wctx.Channels[1].Type != "app" {
		t.Fatalf("batch retry left stale rows: %+v / %+v", wctx.Channels, wctx.FinderItems)
	}
}

func TestInactiveStarredResolutionScopesExternalNotice(t *testing.T) {
	for _, mode := range []string{"per-user", "edge batch"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U1","name":"alice","team_id":"T1","profile":{"display_name":"Alice"}}}`))
			}))
			defer srv.Close()
			db := newTestDB(t)
			if err := db.UpsertWorkspace(cache.Workspace{ID: "T2", Name: "Other"}); err != nil {
				t.Fatal(err)
			}
			store := service.NewSectionStore()
			if err := store.Bootstrap(context.Background(), &fakeSectionsClient{sections: []slk.SidebarSection{{ID: "ST", Type: "stars"}}, starIDs: []string{"D1"}}); err != nil {
				t.Fatal(err)
			}
			wctx := &WorkspaceContext{TeamID: "T2", SectionStore: store, UserNames: newUserNameStore(nil)}
			var notices []tea.Msg
			var batcher userBatcher
			if mode == "edge batch" {
				batcher = &fakeBatcher{res: []edge.User{edgeUserRecord("U1", "alice", "Alice", "", "T1", 1, false)}}
			}
			wctx.UserResolver = newUserResolver("T2", newTestClient(t, srv), db, nil, func(m tea.Msg) { notices = append(notices, m) }, batcher, nil)
			wctx.UserResolver.names = wctx.UserNames
			sender := &captureSender{}
			h := &rtmEventHandler{workspaceID: "T2", wsCtx: wctx, db: db, program: sender, isActive: func() bool { return false }, resolveConversation: func(context.Context, string) (*slack.Channel, error) {
				return &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1", IsIM: true, User: "U1"}}}, nil
			}}
			h.reconcileStarredConversations(context.Background())
			found := false
			for _, notice := range notices {
				if external, ok := notice.(ui.UserExternalMsg); ok {
					found = true
					if external.TeamID != "T2" || external.UserID != "U1" || !external.IsExternal {
						t.Fatalf("unscoped/wrong external notice: %+v", external)
					}
				}
			}
			if !found {
				t.Fatal("resolution did not emit external notice")
			}
			if cached, err := db.GetUser("U1"); err != nil || !cached.IsExternal {
				t.Fatalf("inactive cache was not updated: %+v, %v", cached, err)
			}
			if len(sender.sent) != 0 {
				t.Fatalf("inactive row publication: %+v", sender.sent)
			}
		})
	}
}
