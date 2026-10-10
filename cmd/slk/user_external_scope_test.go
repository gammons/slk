package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// Profiles are shared by user ID, but the external flag shown after a switch
// must be derived relative to the destination workspace.
func TestUserResolutionPreservesWorkspaceExternalClassification(t *testing.T) {
	for _, mode := range []string{"per-user", "edge batch"} {
		t.Run(mode, func(t *testing.T) {
			db := newTestDB(t)
			if err := db.UpsertWorkspace(cache.Workspace{ID: "T2", Name: "Other"}); err != nil {
				t.Fatal(err)
			}
			if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1"}); err != nil {
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
			r := newUserResolver("T2", newTestClient(t, srv), db, nil, nil, batcher, nil)
			if mode == "per-user" {
				r.resolveOne("U1")
			} else {
				r.ResolveNow([]string{"U1"})
			}
			users, err := db.ListUsers("T1")
			if err != nil || len(users) != 1 || users[0].IsExternal {
				t.Fatalf("inactive resolution corrupted T1 cache: %+v, %v", users, err)
			}
			app := ui.NewApp()
			app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			for _, team := range []string{"T2", "T1"} {
				external, err := db.ExternalUsers(team)
				if err != nil {
					t.Fatal(err)
				}
				app.Update(ui.WorkspaceSwitchedMsg{TeamID: team, ExternalUsers: external,
					UserNames: map[string]string{"U1": "Alice"},
					Channels:  []sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}}})
				app.Update(ui.ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
				app.SetChannelMembership("C1", []string{"U1"})
				app.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
				app.Update(tea.KeyPressMsg{Code: '@', Text: "@"})
				view := app.View().Content
				if !strings.Contains(view, "Alice") || strings.Contains(view, "(ext)") != (team == "T2") {
					t.Fatalf("workspace %s restored incorrect picker flags: %s", team, view)
				}
			}
		})
	}
}
