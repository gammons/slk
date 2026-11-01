package edge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSetToken_TheNextRequestCarriesTheNewToken pins that a login
// refreshed while slk runs reaches edgeapi: call reads the token when it
// sends, not when the client was built.
func TestSetToken_TheNextRequestCarriesTheNewToken(t *testing.T) {
	tokens := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		tokens <- body.Token
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New("xoxc-old", "T1", srv.Client())
	c.baseURL = srv.URL
	send := func() string {
		t.Helper()
		if err := c.call(context.Background(), "T1", "users/info", map[string]any{}, nil); err != nil {
			t.Fatalf("call: %v", err)
		}
		return <-tokens
	}

	if got := send(); got != "xoxc-old" {
		t.Errorf("token before SetToken = %q, want xoxc-old", got)
	}
	c.SetToken("xoxc-new")
	if got := send(); got != "xoxc-new" {
		t.Errorf("token after SetToken = %q, want xoxc-new", got)
	}
}
