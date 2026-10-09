package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/ui"
)

func TestUserResolverScopesExternalNotices(t *testing.T) {
	for _, mode := range []string{"per-user", "edge batch"} {
		t.Run(mode, func(t *testing.T) {
			db := newTestDB(t)
			if err := db.UpsertWorkspace(cache.Workspace{ID: "T2", Name: "Other"}); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U1","name":"alice","team_id":"T1","profile":{"display_name":"Alice"}}}`))
			}))
			defer srv.Close()
			var batcher userBatcher
			if mode == "edge batch" {
				batcher = &fakeBatcher{res: []edge.User{edgeUserRecord("U1", "alice", "Alice", "", "T1", 1, false)}}
			}
			var notices []tea.Msg
			r := newUserResolver("T2", newTestClient(t, srv), db, nil, func(m tea.Msg) { notices = append(notices, m) }, batcher, nil)
			if mode == "per-user" {
				r.resolveOne("U1")
			} else {
				r.ResolveNow([]string{"U1"})
			}
			found := false
			for _, notice := range notices {
				if external, ok := notice.(ui.UserExternalMsg); ok {
					found = true
					if external.TeamID != "T2" || external.UserID != "U1" || !external.IsExternal {
						t.Fatalf("wrong external notification: %+v", external)
					}
				}
			}
			if !found {
				t.Fatal("resolver emitted no external notification")
			}
		})
	}
}
