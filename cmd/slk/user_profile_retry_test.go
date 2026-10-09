package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui"
	"github.com/slack-go/slack"
)

// A retry is a full profile, unlike placeholder UpsertUser callers. Cache
// persistence must precede both status and resolved-name notifications.
func TestUserResolverRetryPersistsProfileStatusBeforeNotification(t *testing.T) {
	for _, profile := range []slack.UserProfile{
		{DisplayName: "Alice", StatusEmoji: ":calendar:", StatusText: "Fresh status", StatusExpiration: 2000000000, HuddleState: "in_a_huddle", HuddleStateExpirationTS: 2000000100},
		{DisplayName: "Alice", HuddleState: "default_unset"},
	} {
		for _, notify := range []bool{false, true} {
			t.Run(profile.StatusText+map[bool]string{false: "/cache-only", true: "/notifications"}[notify], func(t *testing.T) {
				db := newTestDB(t)
				if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", StatusEmoji: ":old:", StatusText: "Old status", StatusExpiration: 1900000000, HuddleState: "in_a_huddle", HuddleExpiration: 1900000100}); err != nil {
					t.Fatal(err)
				}
				srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "user": slack.User{ID: "U1", Name: "alice", TeamID: "T1", Profile: profile}})
				}))
				defer srv.Close()
				assertCached := func() {
					cached, err := db.GetUser("U1")
					if err != nil || cached.StatusEmoji != profile.StatusEmoji || cached.StatusText != profile.StatusText || cached.StatusExpiration != int64(profile.StatusExpiration) || cached.HuddleState != profile.HuddleState || cached.HuddleExpiration != int64(profile.HuddleStateExpirationTS) {
						t.Fatalf("retry restored stale status/huddle: %+v, %v", cached, err)
					}
				}
				var notices []tea.Msg
				var send func(tea.Msg)
				if notify {
					send = func(m tea.Msg) {
						assertCached()
						notices = append(notices, m)
					}
				}
				r := newUserResolver("T1", newTestClient(t, srv), db, nil, send, nil, nil)
				r.resolveOne("U1")
				assertCached()
				if notify {
					if len(notices) < 2 {
						t.Fatalf("missing status/name notifications: %+v", notices)
					}
					status, ok := notices[0].(ui.UserStatusChangeMsg)
					if !ok || status.Text != profile.StatusText || status.Huddle != profile.HuddleState {
						t.Fatalf("fresh status must precede resolution: %+v", notices)
					}
					if _, ok := notices[1].(ui.UserResolvedMsg); !ok {
						t.Fatalf("second notification = %T, want UserResolvedMsg", notices[1])
					}
				}
			})
		}
	}
}
