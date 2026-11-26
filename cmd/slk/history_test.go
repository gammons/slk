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
// Fetched history advances latest_synced_ts, so a DM that got messages
// while slk was closed sorts by its real newest message after the
// restart fetch.
func TestFetchChannelMessages_AdvancesLatestTS(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "stefanha", Type: "dm"}); err != nil {
		t.Fatal(err)
	}
	const newest = "1795702916.042989"
	if err := db.SetChannelLatestSyncedTS("C1", "1700000000.000100"); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"messages":[
			{"type":"message","user":"U1","ts":"` + newest + `","text":"hi"}]}`))
	}))
	defer srv.Close()

	items := fetchChannelMessages(newTestClient(t, srv), "C1", db,
		newUserNameStore(map[string]string{"U1": "Alice"}), "3:04 PM", nil, nil)
	if len(items) != 1 {
		t.Fatalf("fetchChannelMessages returned %d items, want 1", len(items))
	}
	if got := db.GetChannelLatestSyncedTS("C1"); got != newest {
		t.Errorf("latest_synced_ts = %q, want %q", got, newest)
	}
}

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
