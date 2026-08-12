package edge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestClient_PostsJSONAsTextPlain(t *testing.T) {
	var gotCT, gotPath, gotBody, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Write([]byte(`{"ok":true,"results":[]}`))
	}))
	defer srv.Close()

	c := New("xoxc-test", "T04T4TH8W", srv.Client())
	c.baseURL = srv.URL

	var out struct {
		OK bool `json:"ok"`
	}
	err := c.call(context.Background(), "T04T4TH8W", "users/info",
		map[string]any{"check_interaction": true}, &out)
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	// edgeapi takes JSON with a text/plain content type — this is how
	// the official client avoids a CORS preflight, and matching it is
	// the point of this package.
	if gotCT != "text/plain;charset=UTF-8" {
		t.Errorf("Content-Type = %q; want text/plain;charset=UTF-8", gotCT)
	}
	// Every edgeapi request in the captures is a POST.
	if gotMethod != "POST" {
		t.Errorf("method = %q; want POST", gotMethod)
	}
	if gotPath != "/cache/T04T4TH8W/users/info" {
		t.Errorf("path = %q; want /cache/T04T4TH8W/users/info", gotPath)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, gotBody)
	}
	if body["token"] != "xoxc-test" {
		t.Errorf("body token = %v; want xoxc-test", body["token"])
	}
	if body["check_interaction"] != true {
		t.Errorf("caller payload not merged: %v", body)
	}
	if !out.OK {
		t.Error("response not decoded")
	}
}

func TestClient_PropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		// Deliberately a body that decodes cleanly as a success: the
		// status code alone has to fail the call. With an empty body
		// this test passes even with the status check removed,
		// because the JSON decode fails instead.
		w.Write([]byte(`{"ok":true,"results":[]}`))
	}))
	defer srv.Close()

	c := New("xoxc-test", "T1", srv.Client())
	c.baseURL = srv.URL
	var out struct{}
	err := c.call(context.Background(), "T1", "users/info", map[string]any{}, &out)
	if err == nil {
		t.Fatal("call returned nil error on HTTP 500")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error = %v; want it to mention the 500 status", err)
	}
	// The response body is the only diagnostic edgeapi gives when it
	// rejects a request; dropping it makes Grid failures unreadable.
	if !strings.Contains(err.Error(), `"results":[]`) {
		t.Errorf("error = %v; want it to include the response body", err)
	}
}

func TestClient_PropagatesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	}))
	defer srv.Close()

	c := New("xoxc-test", "T1", srv.Client())
	c.baseURL = srv.URL
	var out struct{}
	err := c.call(context.Background(), "T1", "users/info", map[string]any{}, &out)
	if err == nil {
		t.Fatal("call returned nil error on ok:false")
	}
	if !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("error = %v; want it to mention invalid_auth", err)
	}
}

func TestClient_RespectsContextCancellation(t *testing.T) {
	// The handler answers successfully on purpose. The context is
	// already cancelled, so a client that honours it never reaches
	// here; one that drops it gets a clean success and fails the
	// assertion below immediately. A handler that blocks on
	// r.Context().Done() instead — as an earlier draft did — is
	// unreachable in the passing case and turns the ctx-dropped
	// regression into a test-binary timeout rather than a failure.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New("xoxc-test", "T1", srv.Client())
	c.baseURL = srv.URL
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out struct{}
	err := c.call(ctx, "T1", "users/info", map[string]any{}, &out)
	if err == nil {
		t.Fatal("call ignored a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v; want it to wrap context.Canceled", err)
	}
}

// markerTransport records that it was asked to carry a request, then
// delegates to the real transport.
type markerTransport struct {
	used atomic.Bool
	next http.RoundTripper
}

func (m *markerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.used.Store(true)
	return m.next.RoundTrip(req)
}

// TestClient_SendsThroughTheCallerSuppliedHTTPClient is not a tautology
// — do not delete it as one.
//
// Every other test here passes srv.Client() to a plain non-TLS
// httptest server, where http.DefaultClient behaves identically. So a
// New that dropped its httpClient argument and used http.DefaultClient
// would pass the entire rest of this suite. In production that
// substitution is not invisible at all: the injected client is the one
// built with slackhttp.BrowserTransport, and it is the *only* thing
// that puts the browser headers and the edgeapi query envelope on
// these requests. Losing it means every edgeapi call goes out
// unbrowser-shaped — exactly the divergence that gets Grid users
// flagged for scraping. This test pins the injection itself by
// watching for traffic on a transport only the caller could have
// supplied.
func TestClient_SendsThroughTheCallerSuppliedHTTPClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	marker := &markerTransport{next: srv.Client().Transport}
	c := New("xoxc-test", "T1", &http.Client{Transport: marker})
	c.baseURL = srv.URL

	if err := c.call(context.Background(), "T1", "users/info", map[string]any{}, nil); err != nil {
		t.Fatalf("call: %v", err)
	}
	if !marker.used.Load() {
		t.Error("request bypassed the caller-supplied http.Client; " +
			"edgeapi traffic would carry no browser headers and no query envelope")
	}
}

// TestClient_AcceptsNilOutToDiscardTheBody pins the out != nil guard.
// Endpoints that are called only for their status (membership pokes,
// counts) pass a nil out; without the guard that is an unmarshal into
// nil and a spurious error.
func TestClient_AcceptsNilOutToDiscardTheBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"results":[]}`))
	}))
	defer srv.Close()

	c := New("xoxc-test", "T1", srv.Client())
	c.baseURL = srv.URL
	if err := c.call(context.Background(), "T1", "users/info", map[string]any{}, nil); err != nil {
		t.Fatalf("call with out=nil: %v", err)
	}
}

// TestClient_ReportsAPIErrorWithNoErrorField covers ok:false with the
// error field absent: without a fallback the message is "edge
// users/info: " — a dangling colon and no diagnostic.
func TestClient_ReportsAPIErrorWithNoErrorField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":false}`))
	}))
	defer srv.Close()

	c := New("xoxc-test", "T1", srv.Client())
	c.baseURL = srv.URL
	err := c.call(context.Background(), "T1", "users/info", map[string]any{}, nil)
	if err == nil {
		t.Fatal("call returned nil error on ok:false")
	}
	if strings.HasSuffix(err.Error(), ": ") || strings.HasSuffix(err.Error(), ":") {
		t.Errorf("error = %q; want a diagnostic, not a dangling colon", err)
	}
	if !strings.Contains(err.Error(), "ok=false") {
		t.Errorf("error = %q; want it to say the response was ok=false with no error field", err)
	}
}

func TestNew_DefaultsToEdgeAPIHost(t *testing.T) {
	c := New("xoxc-test", "T1", nil)
	// slackhttp.BrowserTransport keys the edgeapi query envelope on
	// this exact host (isEdgeAPIHost). Pointing the client anywhere
	// else silently sends the 13-param workspace envelope instead.
	if c.baseURL != "https://edgeapi.slack.com" {
		t.Errorf("baseURL = %q; want https://edgeapi.slack.com", c.baseURL)
	}
}

func TestNew_FallsBackToDefaultHTTPClient(t *testing.T) {
	// A nil *http.Client would panic in call; the fallback is the
	// only thing preventing that.
	c := New("xoxc-test", "T1", nil)
	if c.http != http.DefaultClient {
		t.Errorf("http = %v; want http.DefaultClient", c.http)
	}
}

func TestTruncate_KeepsShortBodiesVerbatim(t *testing.T) {
	short := []byte(`{"ok":false,"error":"invalid_auth"}`)
	if got := truncate(short); got != string(short) {
		t.Errorf("truncate(short) = %q; want %q", got, short)
	}
	// Exactly at the cap still fits; nothing should be dropped or
	// labelled truncated.
	atCap := bytes.Repeat([]byte("y"), 512)
	if got := truncate(atCap); got != string(atCap) {
		t.Errorf("truncate(exactly 512 bytes) returned %d bytes; want the body verbatim", len(got))
	}
}

func TestTruncate_CapsLongBodies(t *testing.T) {
	long := bytes.Repeat([]byte("x"), 600)
	// Exact, not just "shorter than the input": a loose bound lets an
	// off-by-one in the cap through unnoticed.
	want := strings.Repeat("x", 512) + "...(truncated)"
	if got := truncate(long); got != want {
		t.Errorf("truncate(600 bytes) = %q (%d bytes); want %d leading bytes plus a truncation marker",
			got, len(got), 512)
	}
}
