package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

func TestWorkspacePresenceIDs(t *testing.T) {
	wctx := &WorkspaceContext{
		UserID: "USELF",
		Channels: []sidebar.ChannelItem{
			{Type: "dm", DMUserID: "U1"},
			{Type: "channel"}, // not a DM — skipped
			{Type: "dm", DMUserID: "U2"},
			{Type: "dm", DMUserID: "U1"},     // duplicate peer — deduped
			{Type: "app", DMUserID: "UBOT"},  // app/bot DM — no presence dot
			{Type: "group_dm", DMUserID: ""}, // group DM — skipped
			{Type: "dm", DMUserID: ""},       // malformed — skipped
		},
	}

	got := workspacePresenceIDs(wctx)
	want := []string{"USELF", "U1", "U2"} // self first, then DM peers in order, deduped
	if len(got) != len(want) {
		t.Fatalf("workspacePresenceIDs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("workspacePresenceIDs()[%d] = %q, want %q (full %v)", i, got[i], want[i], got)
		}
	}
}

func TestWorkspacePresenceIDsOwnsSnapshot(t *testing.T) {
	wctx := &WorkspaceContext{
		UserID: "USELF",
		Channels: []sidebar.ChannelItem{
			{Type: "dm", DMUserID: "U1"},
		},
	}
	ids := workspacePresenceIDs(wctx)
	wctx.Channels[0].DMUserID = "U2"
	wctx.Channels = append(wctx.Channels, sidebar.ChannelItem{Type: "dm", DMUserID: "U3"})
	if want := []string{"USELF", "U1"}; !slices.Equal(ids, want) {
		t.Fatalf("snapshot = %v, want %v", ids, want)
	}
}

func TestOnConnectPresenceSnapshotRacesConversationOpened(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/users.getPresence":
			_, _ = w.Write([]byte(`{"ok":true,"presence":"active"}`))
		case "/api/dnd.info":
			_, _ = w.Write([]byte(`{"ok":true,"dnd_enabled":false}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.Error(w, `{"ok":false,"error":"unexpected_path"}`, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	for attempt := range 20 {
		t.Run(fmt.Sprintf("connect-%d", attempt), func(t *testing.T) {
			done := make(chan struct{})
			wctx := &WorkspaceContext{
				Client: newTestClient(t, srv), TeamID: "T1", UserID: "USELF",
				UserNames: newUserNameStore(nil),
				Channels:  []sidebar.ChannelItem{{ID: "D1", Type: "dm", DMUserID: "U1"}},
			}
			h := &rtmEventHandler{wsCtx: wctx, program: sendFunc(func(msg tea.Msg) {
				if _, ok := msg.(ui.StatusChangeMsg); ok {
					close(done)
				}
			})}
			h.OnConnect()
			for i := range 300 {
				ch := slack.Channel{}
				ch.ID = fmt.Sprintf("D%04d", i)
				ch.IsIM = true
				ch.User = fmt.Sprintf("UP%04d", i)
				h.OnConversationOpened(ch)
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("presence bootstrap did not finish")
			}
			if got := len(wctx.Channels); got != 301 {
				t.Fatalf("len(Channels) = %d, want 301", got)
			}
		})
	}
}

func TestOnConnectPresenceUsesPeerSnapshot(t *testing.T) {
	presenceStarted := make(chan struct{})
	releasePresence := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releasePresence) }) }
	done := make(chan struct{})
	dndIDs := make(chan []string, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/users.getPresence":
			close(presenceStarted)
			<-releasePresence
			_, _ = w.Write([]byte(`{"ok":true,"presence":"active"}`))
		case "/api/dnd.info":
			_, _ = w.Write([]byte(`{"ok":true,"dnd_enabled":false}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.Error(w, `{"ok":false,"error":"unexpected_path"}`, http.StatusNotFound)
		}
	}))
	defer srv.Close()
	defer release()
	wctx := &WorkspaceContext{
		Client: newTestClient(t, srv), TeamID: "T1", UserID: "USELF",
		Channels: []sidebar.ChannelItem{{Type: "dm", DMUserID: "U1"}},
		PeerStatus: &peerStatusRefresher{
			selfID: "USELF", now: time.Now,
			dndTeamInfo: func(_ context.Context, ids []string) (map[string]slack.DNDStatus, error) {
				dndIDs <- slices.Clone(ids)
				return nil, nil
			},
		},
	}
	h := &rtmEventHandler{wsCtx: wctx, program: sendFunc(func(msg tea.Msg) {
		if _, ok := msg.(ui.StatusChangeMsg); ok {
			close(done)
		}
	})}
	h.OnConnect()
	select {
	case <-presenceStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("presence bootstrap did not start")
	}
	wctx.Channels[0].DMUserID = "U2"
	wctx.Channels = append(wctx.Channels, sidebar.ChannelItem{Type: "dm", DMUserID: "U3"})
	release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("presence bootstrap did not finish")
	}
	select {
	case got := <-dndIDs:
		if want := []string{"U1"}; !slices.Equal(got, want) {
			t.Fatalf("DND peer IDs = %v, want connect-time snapshot %v", got, want)
		}
	default:
		t.Fatal("peer DND refresh did not run")
	}
}
