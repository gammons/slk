package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/threadsview"
)

// TestFetchThreadReplies_RacesThreadsViewRender reproduces the
// 2026-10-02 crash ("fatal error: concurrent map read and map write",
// top frames messages.renderInlineFormattingWith <- threadsview.renderCard)
// inside ONE slk process: no second instance, no WebSocket.
//
// fetchThreadReplies runs as a bubbletea Cmd, off the UI goroutine. For
// a reply author found in SQLite but not yet in memory it records the
// name (resolveUserCached). The Threads view renders on the UI
// goroutine. They used to share one map, so under -race the detector
// reported the write in resolveUserCached against the read in
// renderInlineFormattingWith. The UI now holds a Snapshot.
//
// Wired the way main.go wires it: the UI gets wctx.UserNames.Snapshot()
// via WorkspaceReadyMsg; the fetcher gets wctx.UserNames.
func TestFetchThreadReplies_RacesThreadsViewRender(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertUser(cache.User{ID: "U2", WorkspaceID: "T1", Name: "bob", DisplayName: "Bob"}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"has_more":false,"messages":[
			{"type":"message","user":"U1","ts":"1700000000.000100","thread_ts":"1700000000.000100","text":"parent"},
			{"type":"message","user":"U2","ts":"1700000001.000100","thread_ts":"1700000000.000100","text":"reply"}]}`))
	}))
	defer srv.Close()
	client := newTestClient(t, srv)

	store := newUserNameStore(map[string]string{"U1": "Alice"})

	tv := threadsview.New(nil, "USELF")
	tv.SetUserNames(store.Snapshot())
	tv.SetSummaries([]core.ThreadSummary{{
		ChannelID: "C1", ThreadTS: "1700000000.000100", ParentTS: "1700000000.000100",
		ParentUserID: "U1", ParentText: "ping <@U1> and <@U2>",
	}})

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() { // the UI goroutine, rendering
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = tv.View(20, 80)
			}
		}
	}()

	// The bubbletea Cmd goroutine, fetching the selected thread.
	replies := fetchThreadReplies(client, "C1", "1700000000.000100", db, store, "15:04", nil, nil)
	close(stop)
	wg.Wait()

	// The fix must not cost the memoization: the DB hit is recorded in
	// the store, and the reply renders with the resolved name.
	if name, ok := store.Get("U2"); !ok || name != "Bob" {
		t.Errorf("store.Get(U2) = (%q, %v), want (\"Bob\", true)", name, ok)
	}
	if len(replies) != 1 || replies[0].UserName != "Bob" {
		t.Errorf("replies = %+v, want one reply by Bob", replies)
	}
}
