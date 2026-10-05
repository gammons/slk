package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/service"
	slk "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

func TestInactiveStarredResolutionPreservesWorkspaceCacheClassification(t *testing.T) {
	for _, mode := range []string{"per-user", "edge batch"} {
		t.Run(mode, func(t *testing.T) {
			db := newTestDB(t)
			if err := db.UpsertWorkspace(cache.Workspace{ID: "T2", Name: "Other"}); err != nil {
				t.Fatal(err)
			}
			// This placeholder belongs to T1, where U1 is internal. The same
			// globally keyed user is external when resolved from inactive T2.
			if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1"}); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U1","name":"alice","team_id":"T1","profile":{"display_name":"Alice"}}}`))
			}))
			defer srv.Close()
			store := service.NewSectionStore()
			if err := store.Bootstrap(context.Background(), &fakeSectionsClient{sections: []slk.SidebarSection{{ID: "ST", Type: "stars"}}, starIDs: []string{"D1"}}); err != nil {
				t.Fatal(err)
			}
			wctx := &WorkspaceContext{TeamID: "T2", SectionStore: store, UserNames: newUserNameStore(nil)}
			var batcher userBatcher
			if mode == "edge batch" {
				batcher = &fakeBatcher{res: []edge.User{edgeUserRecord("U1", "alice", "Alice", "", "T1", 1, false)}}
			}
			wctx.UserResolver = newUserResolver("T2", newTestClient(t, srv), db, nil, nil, batcher, nil)
			wctx.UserResolver.names = wctx.UserNames
			h := &rtmEventHandler{workspaceID: "T2", wsCtx: wctx, db: db, isActive: func() bool { return false }, resolveConversation: func(context.Context, string) (*slack.Channel, error) {
				return &slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1", IsIM: true, User: "U1"}}}, nil
			}}
			h.reconcileStarredConversations(context.Background())
			users, err := db.ListUsers("T1")
			if err != nil || len(users) != 1 || users[0].IsExternal {
				t.Fatalf("switching to T1 would consume an incorrect external flag: %+v, %v", users, err)
			}
			// Drive the real switch reducer and mention picker with the same
			// cache projection main.go uses. T2 must retain its external flag;
			// switching back to T1 must remove it, not consume T2's boolean.
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

func TestConversationDeduplicationRefreshesFinder(t *testing.T) {
	wctx := &WorkspaceContext{
		UserNames:   newUserNameStore(map[string]string{"U1": "Alice"}),
		Channels:    []sidebar.ChannelItem{{ID: "D1", Name: "U1", Type: "dm", DMUserID: "U1"}},
		FinderItems: []core.ChannelFinderItem{{ID: "D1", Name: "U1", Type: "dm", Joined: true, LastVisited: 99}},
		// The finder can hold a newer visit than the startup snapshot.
		LastVisitedByChannel: map[string]int64{"D1": 10},
	}
	wctx.MarkBotUser("U1")
	h := &rtmEventHandler{wsCtx: wctx}
	ch := slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D1", IsIM: true, User: "U1"}}}
	_, finder, ok := h.addConversation(ch)
	if !ok || len(wctx.FinderItems) != 1 || wctx.FinderItems[0].Name != "Alice" || wctx.FinderItems[0].Type != "app" || finder.LastVisited != 99 || finder != wctx.FinderItems[0] {
		t.Fatalf("duplicate open left stale or duplicate finder data: %+v, returned %+v", wctx.FinderItems, finder)
	}
}

func TestRefreshDMPeerRepairsFinderIndependently(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "stale finder", true: "missing finder"}[missing], func(t *testing.T) {
			wctx := &WorkspaceContext{
				TeamID: "T1", UserNames: newUserNameStore(map[string]string{"U1": "Alice"}),
				Channels:             []sidebar.ChannelItem{{ID: "D1", Name: "Alice", Type: "dm", DMUserID: "U1", Section: "ST", SectionOrder: 2, ChannelOrder: 3}},
				LastVisitedByChannel: map[string]int64{"D1": 99},
			}
			if !missing {
				wctx.FinderItems = []core.ChannelFinderItem{{ID: "D1", Name: "U1", Type: "app", Joined: true, LastVisited: 99}}
			}
			sender := &captureSender{}
			h := &rtmEventHandler{workspaceID: "T1", wsCtx: wctx, program: sender}
			h.refreshDMPeerFromCache("U1")
			if len(wctx.FinderItems) != 1 || wctx.FinderItems[0].Name != "Alice" || wctx.FinderItems[0].Type != "dm" || wctx.FinderItems[0].LastVisited != 99 {
				t.Fatalf("correct sidebar suppressed finder repair: %+v", wctx.FinderItems)
			}
			if len(sender.sent) != 1 {
				t.Fatalf("finder-only repair publications = %d, want 1", len(sender.sent))
			}
			if item := wctx.Channels[0]; item.Section != "ST" || item.SectionOrder != 2 || item.ChannelOrder != 3 {
				t.Fatalf("repair lost section/order metadata: %+v", item)
			}
			h.refreshDMPeerFromCache("U1")
			if len(sender.sent) != 1 {
				t.Fatal("unchanged snapshots were republished")
			}
			app := ui.NewApp()
			app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			app.Update(ui.WorkspaceSwitchedMsg{TeamID: "other"})
			app.Update(ui.WorkspaceSwitchedMsg{TeamID: "T1", Channels: wctx.Channels, FinderItems: wctx.FinderItems})
			app.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
			if view := app.View().Content; !strings.Contains(view, "Alice") || strings.Contains(view, "U1") {
				t.Fatalf("switch restored a stale finder: %s", view)
			}
		})
	}
}

func TestRefreshDMPeerUpdatesCachedStatusWithoutRename(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", DisplayName: "Alice", Presence: "active", StatusText: "Fresh status", HuddleState: "default_unset"}); err != nil {
		t.Fatal(err)
	}
	// DND is not in the profile cache and must survive a status-only repair.
	old := peerstatus.Status{Text: "Old status", DND: true, Huddle: "in_a_huddle"}
	wctx := &WorkspaceContext{UserNames: newUserNameStore(map[string]string{"U1": "Alice"}),
		Channels:    []sidebar.ChannelItem{{ID: "D1", Name: "Alice", Type: "dm", DMUserID: "U1", Presence: "away", Status: old}},
		FinderItems: []core.ChannelFinderItem{{ID: "D1", Name: "Alice", Type: "dm", Presence: "away", Joined: true, LastVisited: 99}},
	}
	sender := &captureSender{}
	h := &rtmEventHandler{wsCtx: wctx, db: db, program: sender}
	h.refreshDMPeerFromCache("U1")
	want := peerstatus.Status{Text: "Fresh status", DND: true, Huddle: "default_unset"}
	if item := wctx.Channels[0]; item.Status != want || item.Presence != "active" {
		t.Fatalf("same-name repair lost status/presence/DND: %+v", item)
	}
	if finder := wctx.FinderItems[0]; finder.Presence != "active" || finder.LastVisited != 99 {
		t.Fatalf("same-name finder repair = %+v", finder)
	}
	if len(sender.sent) != 1 || sender.sent[0].(ui.ConversationOpenedMsg).Item.Status != want {
		t.Fatalf("status-only repair did not reach UI: %+v", sender.sent)
	}
}

func TestStarredProfileRetryPersistsFreshStatus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile slack.UserProfile
	}{
		{"status update and huddle start", slack.UserProfile{DisplayName: "Alice", StatusEmoji: ":calendar:", StatusText: "Fresh status", StatusExpiration: 2000000000, HuddleState: "in_a_huddle", HuddleStateExpirationTS: 2000000100}},
		{"status and huddle clear", slack.UserProfile{DisplayName: "Alice", HuddleState: "default_unset"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", StatusEmoji: ":old:", StatusText: "Old status", StatusExpiration: 1900000000, HuddleState: "in_a_huddle", HuddleExpiration: 1900000100}); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "user": slack.User{ID: "U1", Name: "alice", TeamID: "T1", Profile: tc.profile}})
			}))
			defer srv.Close()
			store := service.NewSectionStore()
			if err := store.Bootstrap(context.Background(), &fakeSectionsClient{sections: []slk.SidebarSection{{ID: "ST", Type: "stars"}}, starIDs: []string{"D1"}}); err != nil {
				t.Fatal(err)
			}
			wctx := &WorkspaceContext{TeamID: "T1", SectionStore: store, UserNames: newUserNameStore(nil),
				Channels:    []sidebar.ChannelItem{{ID: "D1", Name: "U1", Type: "dm", DMUserID: "U1"}},
				FinderItems: []core.ChannelFinderItem{{ID: "D1", Name: "U1", Type: "dm", Joined: true}},
			}
			var notices []tea.Msg
			wctx.UserResolver = newUserResolver("T1", newTestClient(t, srv), db, nil, func(m tea.Msg) { notices = append(notices, m) }, nil, nil)
			wctx.UserResolver.names = wctx.UserNames
			sender := &captureSender{}
			h := &rtmEventHandler{workspaceID: "T1", wsCtx: wctx, db: db, program: sender}
			h.reconcileStarredConversations(context.Background())
			cached, err := db.GetUser("U1")
			want := peerstatus.Status{}.WithStatus(tc.profile.StatusEmoji, tc.profile.StatusText, statusExpiry(int64(tc.profile.StatusExpiration))).WithHuddle(tc.profile.HuddleState, statusExpiry(int64(tc.profile.HuddleStateExpirationTS)))
			got := peerstatus.Status{}.WithStatus(cached.StatusEmoji, cached.StatusText, statusExpiry(cached.StatusExpiration)).WithHuddle(cached.HuddleState, statusExpiry(cached.HuddleExpiration))
			if err != nil || got != want || wctx.Channels[0].Status != want {
				t.Fatalf("retry restored stale status: cache=%+v row=%+v want=%+v err=%v", got, wctx.Channels[0].Status, want, err)
			}
			if len(sender.sent) != 1 || sender.sent[0].(ui.ConversationOpenedMsg).Item.Status != want {
				t.Fatalf("row publication restored stale status: %+v", sender.sent)
			}
			if len(notices) < 2 {
				t.Fatalf("missing profile notifications: %+v", notices)
			}
			status, ok := notices[0].(ui.UserStatusChangeMsg)
			if !ok || status.Text != want.Text || status.Huddle != want.Huddle {
				t.Fatalf("fresh status must precede resolution: %+v", notices)
			}
		})
	}
}
