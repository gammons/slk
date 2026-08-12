package slackclient

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gammons/slk/internal/slackhttp"
)

// The workspace API client carries most of slk's traffic. If it does
// not tally, the per-boot number Phase 2b's criteria are stated in is
// measuring almost nothing.
func TestNewCookieHTTPClient_TalliesToDefaultCounter(t *testing.T) {
	before := slackhttp.DefaultCounter.Snapshot()["client.counts"]

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newCookieHTTPClient("d-cookie", slackhttp.NewEnvelope())
	req, err := http.NewRequest("POST", srv.URL+"/api/client.counts", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if got := slackhttp.DefaultCounter.Snapshot()["client.counts"]; got != before+1 {
		t.Errorf("client.counts = %d; want %d — the workspace API client is not attached to DefaultCounter", got, before+1)
	}
}
