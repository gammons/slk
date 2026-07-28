package slackclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMintTokenScrapesAPIToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, _ := r.Cookie("d"); c == nil || c.Value != "xoxd-abc" {
			t.Errorf("missing/incorrect d cookie: %+v", c)
		}
		w.Write([]byte(`<html>...,"api_token":"xoxc-12345",...</html>`))
	}))
	defer srv.Close()

	got, err := mintTokenAt(context.Background(), srv.Client(), srv.URL, "xoxd-abc")
	if err != nil {
		t.Fatal(err)
	}
	if got != "xoxc-12345" {
		t.Errorf("got %q, want xoxc-12345", got)
	}
}

func TestMintTokenNoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>no token here</html>`))
	}))
	defer srv.Close()
	if _, err := mintTokenAt(context.Background(), srv.Client(), srv.URL, "xoxd-abc"); err == nil {
		t.Error("expected error when api_token absent")
	}
}

// Loading a workspace page is a top-level navigation, not an XHR/CORS call.
// Sending API-XHR headers (Origin: app.slack.com, Sec-Fetch-Mode: cors) on
// this GET can be rejected with 403 by strict corporate edges/proxies (#111).
// Guard that the mint request is navigation-shaped.
func TestMintTokenSendsNavigationHeaders(t *testing.T) {
	got := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Clone()
		w.Write([]byte(`<html>"api_token":"xoxc-ok"</html>`))
	}))
	defer srv.Close()

	if _, err := mintTokenAt(context.Background(), srv.Client(), srv.URL, "xoxd-abc"); err != nil {
		t.Fatal(err)
	}
	h := <-got
	if h.Get("Sec-Fetch-Mode") != "navigate" {
		t.Errorf("Sec-Fetch-Mode = %q, want navigate", h.Get("Sec-Fetch-Mode"))
	}
	if h.Get("Sec-Fetch-Dest") != "document" {
		t.Errorf("Sec-Fetch-Dest = %q, want document", h.Get("Sec-Fetch-Dest"))
	}
	if h.Get("Origin") != "" {
		t.Errorf("Origin = %q, want empty (a navigation sends no Origin)", h.Get("Origin"))
	}
	if !strings.HasPrefix(h.Get("Accept"), "text/html") {
		t.Errorf("Accept = %q, want a text/html navigation Accept", h.Get("Accept"))
	}
}

func TestMintTokenRetriesOn429(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "0") // retry immediately, keep test fast
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`<html>"api_token":"xoxc-ok"</html>`))
	}))
	defer srv.Close()

	got, err := mintTokenAt(context.Background(), srv.Client(), srv.URL, "xoxd-abc")
	if err != nil {
		t.Fatal(err)
	}
	if got != "xoxc-ok" {
		t.Errorf("got %q, want xoxc-ok", got)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("expected 2 calls (one retry), got %d", calls)
	}
}

func TestMintTokenGivesUpAfterRepeated429(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := mintTokenAt(context.Background(), srv.Client(), srv.URL, "xoxd-abc")
	if err == nil {
		t.Fatal("expected error after repeated 429s")
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("expected 3 attempts (maxAttempts), got %d", calls)
	}
}
