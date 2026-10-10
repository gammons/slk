package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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
	batchErrors := make(chan error, 2)
	allowRetry := make(chan struct{})
	var releaseOnce sync.Once
	releaseRetry := func() { releaseOnce.Do(func() { close(allowRetry) }) }
	defer releaseRetry()
	batcher := contextUserBatcherFunc(func(batchCtx context.Context, ids map[string]int64) ([]edge.User, error) {
		batches++
		var err error
		if len(ids) != 2 {
			err = fmt.Errorf("batch IDs = %v, want both unknown peers", ids)
		} else if batches == 1 && batchCtx != ctx {
			err = fmt.Errorf("batch lost caller's cancellation context")
		}
		if err != nil {
			t.Error(err) // valid on the flush timer goroutine; never call FailNow here
			batchErrors <- err
			return nil, err
		}
		if batches == 1 {
			cancel() // emulate exhaustion of the shared reconnect HTTP budget
			return nil, batchCtx.Err()
		}
		<-allowRetry
		return []edge.User{
			edgeUserRecord("U1", "alice", "Alice", "", "T1", 1, false),
			edgeUserRecord("U2", "bob", "Bob", "", "T1", 1, true),
		}, nil
	})
	wctx := &WorkspaceContext{TeamID: "T1", SectionStore: store, UserNames: newUserNameStore(nil)}
	// Give error-path fallbacks a real client so a failed batch assertion
	// reports its cause instead of panicking on a nil client.
	srv := newFakeSlack(t, map[string]string{
		"/api/users.info": `{"ok":false,"error":"unexpected_fallback"}`,
	})
	watch := newResolvedWatch(2, nil)
	wctx.UserResolver = newUserResolver("T1", newTestClient(t, srv.Server), db, nil, watch.send, batcher, nil)
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
	releaseRetry()
	select {
	case <-watch.done:
	case err := <-batchErrors:
		t.Fatal(err) // report immediately on the test goroutine
	case <-time.After(5 * time.Second):
		t.Fatal("canceled budget did not schedule a background batch retry")
	}
	h.OnPendingEvents()
	if metadata != 2 || batches != 2 {
		t.Fatalf("metadata=%d batches=%d, want 2 each without another reconnect", metadata, batches)
	}
	if wctx.Channels[0].Name != "Alice" || wctx.FinderItems[1].Name != "Bob" || wctx.Channels[1].Type != "app" {
		t.Fatalf("batch retry left stale rows: %+v / %+v", wctx.Channels, wctx.FinderItems)
	}
	srv.mu.Lock()
	fallbacks := len(srv.reqs)
	srv.mu.Unlock()
	if fallbacks != 0 {
		t.Fatalf("per-user fallbacks = %d, want none", fallbacks)
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
