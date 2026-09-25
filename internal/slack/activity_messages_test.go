package slackclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gammons/slk/internal/core"
)

// fakeMessagesList serves messages.list the way Slack was observed to:
// a request naming more than maxChannels channels is rejected with
// too_many_channels (a live workspace accepted 9 and rejected 39).
// Channels in failChannels make their whole request fail with a
// different error. Every other requested ts comes back with text
// "<channel>/<ts>".
type fakeMessagesList struct {
	maxChannels  int
	failChannels map[string]bool

	mu       sync.Mutex
	rejected int
}

func (f *fakeMessagesList) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var groups []messageIDsGroup
	if err := json.Unmarshal([]byte(r.FormValue("message_ids")), &groups); err != nil {
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_arguments"}`))
		return
	}
	if len(groups) > f.maxChannels {
		f.mu.Lock()
		f.rejected++
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":false,"error":"too_many_channels"}`))
		return
	}
	data := map[string]rawChannelMessages{}
	for _, g := range groups {
		if f.failChannels[g.Channel] {
			_, _ = w.Write([]byte(`{"ok":false,"error":"internal_error"}`))
			return
		}
		var msgs []rawListedMessage
		for _, ts := range g.Timestamps {
			msgs = append(msgs, rawListedMessage{TS: ts, User: "U1", Text: g.Channel + "/" + ts})
		}
		data[g.Channel] = rawChannelMessages{Messages: msgs}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages_data": data})
}

func newMessagesListClient(t *testing.T, f *fakeMessagesList) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &Client{
		token:      "xoxc-test",
		cookie:     "d-cookie",
		apiBaseURL: srv.URL + "/api/",
		httpClient: srv.Client(),
	}
}

// refsAcross builds one ts per channel across n channels.
func refsAcross(n int) map[string][]string {
	refs := map[string][]string{}
	for i := 0; i < n; i++ {
		refs[fmt.Sprintf("C%03d", i)] = []string{fmt.Sprintf("%d.000100", 1700000000+i)}
	}
	return refs
}

func assertAllBodies(t *testing.T, refs map[string][]string, got map[string]ActivityMessage) {
	t.Helper()
	for ch, tss := range refs {
		for _, ts := range tss {
			m, ok := got[core.ActivityMsgKey(ch, ts)]
			if !ok {
				t.Errorf("missing body for %s/%s", ch, ts)
				continue
			}
			if want := ch + "/" + ts; m.Text != want {
				t.Errorf("body for %s/%s = %q, want %q", ch, ts, m.Text, want)
			}
		}
	}
}

// The live failure: a 50-item page spanning 39 channels was sent as one
// messages.list call and rejected with too_many_channels, so no card
// showed a body.
func TestGetActivityMessages_PageSpanningManyChannels(t *testing.T) {
	fake := &fakeMessagesList{maxChannels: 9}
	c := newMessagesListClient(t, fake)
	refs := refsAcross(39)

	got, err := c.GetActivityMessages(context.Background(), refs)
	if err != nil {
		t.Fatalf("GetActivityMessages: %v", err)
	}
	assertAllBodies(t, refs, got)
	// Split-and-retry would also recover, but at the cost of a rejected
	// call on every open; batching keeps a known-good limit rejection-free.
	if fake.rejected != 0 {
		t.Errorf("%d messages.list calls were rejected as too_many_channels, want 0", fake.rejected)
	}
}

// Slack's limit is undocumented; a workspace whose limit is below the
// batch size must still get every body.
func TestGetActivityMessages_LimitBelowBatchSize(t *testing.T) {
	c := newMessagesListClient(t, &fakeMessagesList{maxChannels: 3})
	refs := refsAcross(20)

	got, err := c.GetActivityMessages(context.Background(), refs)
	if err != nil {
		t.Fatalf("GetActivityMessages: %v", err)
	}
	assertAllBodies(t, refs, got)
}

// One failing batch must not discard the bodies the others returned.
func TestGetActivityMessages_PartialFailureKeepsOtherBodies(t *testing.T) {
	refs := refsAcross(20)
	c := newMessagesListClient(t, &fakeMessagesList{
		maxChannels:  100,
		failChannels: map[string]bool{"C000": true},
	})

	got, err := c.GetActivityMessages(context.Background(), refs)
	if err != nil {
		t.Fatalf("GetActivityMessages with one failing batch: %v", err)
	}
	if _, ok := got[core.ActivityMsgKey("C019", refs["C019"][0])]; !ok {
		t.Fatalf("bodies from the batches that succeeded were dropped (got %d)", len(got))
	}
}

// When nothing succeeds the error surfaces, so the caller keeps the
// bodies it already has instead of blanking them.
func TestGetActivityMessages_AllBatchesFailReturnsError(t *testing.T) {
	refs := refsAcross(5)
	fail := map[string]bool{}
	for ch := range refs {
		fail[ch] = true
	}
	c := newMessagesListClient(t, &fakeMessagesList{maxChannels: 100, failChannels: fail})

	if _, err := c.GetActivityMessages(context.Background(), refs); err == nil {
		t.Fatal("want an error when every messages.list call fails")
	}
}
