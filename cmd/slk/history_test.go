package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gammons/slk/internal/cache"
)

// TestFetchThreadReplies_PrunesCachedRepliesSlackDidNotReturn is the
// regression for a thread that flickered unread forever: a Slackbot
// ephemeral cached from the WebSocket sat in the thread as its newest
// reply. conversations.replies never returns it, so the fetch must drop
// it from the cache, or the cached open marks the thread read at the
// ephemeral's ts and the fetched open marks it back behind it.
func TestFetchThreadReplies_PrunesCachedRepliesSlackDidNotReturn(t *testing.T) {
	db := newTestDB(t)
	const parent = "1700000000.000100"
	for _, ts := range []string{parent, "1700000001.000100", "1700000002.000000"} {
		if err := db.UpsertMessage(cache.Message{TS: ts, ChannelID: "C1", WorkspaceID: "T1",
			UserID: "USLACKBOT", ThreadTS: parent, CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"has_more":false,"messages":[
			{"type":"message","user":"U1","ts":"1700000000.000100","thread_ts":"1700000000.000100","text":"parent"},
			{"type":"message","user":"U2","ts":"1700000001.000100","thread_ts":"1700000000.000100","text":"reply"}]}`))
	}))
	defer srv.Close()

	replies := fetchThreadReplies(newTestClient(t, srv), "C1", parent, db, newUserNameStore(nil), "15:04", nil, nil)
	if len(replies) != 1 {
		t.Fatalf("fetchThreadReplies returned %d replies, want 1", len(replies))
	}

	rows, err := db.GetThreadReplies("C1", parent)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.TS)
	}
	if want := []string{parent, "1700000001.000100"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("cached thread after fetch = %v, want %v", got, want)
	}
}
