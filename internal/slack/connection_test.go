package slackclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slack-go/slack"
)

func TestBackoffSequence(t *testing.T) {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	expected := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}

	for i, want := range expected {
		if backoff != want {
			t.Errorf("step %d: expected %v, got %v", i, want, backoff)
		}
		backoff = nextBackoff(backoff, maxBackoff)
	}
}

func TestLoginReplaced(t *testing.T) {
	refused := slack.SlackErrorResponse{Err: "invalid_auth"}
	offline := errors.New("dial tcp: connection refused")
	yes, no := true, false
	tests := []struct {
		name string
		// check is what auth.test answers.
		check error
		// relogin is Relogin's answer; nil sets no Relogin.
		relogin     *bool
		want        bool
		wantChecks  int
		wantRelogin int
	}{
		{name: "no Relogin: no auth.test either", check: refused, relogin: nil, want: false, wantChecks: 0, wantRelogin: 0},
		{name: "login accepted", check: nil, relogin: &yes, want: false, wantChecks: 1, wantRelogin: 0},
		{name: "network down", check: offline, relogin: &yes, want: false, wantChecks: 1, wantRelogin: 0},
		{name: "login refused, new one set", check: refused, relogin: &yes, want: true, wantChecks: 1, wantRelogin: 1},
		{name: "login refused, nothing newer", check: refused, relogin: &no, want: false, wantChecks: 1, wantRelogin: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var checks, relogins int
			c := &Client{api: &mockSlackAPI{authTestFn: func() (*slack.AuthTestResponse, error) {
				checks++
				if tt.check != nil {
					return nil, tt.check
				}
				return &slack.AuthTestResponse{}, nil
			}}}
			cm := NewConnectionManager(c, &mockEventHandler{})
			if tt.relogin != nil {
				cm.Relogin = func(context.Context) bool {
					relogins++
					return *tt.relogin
				}
			}
			if got := cm.loginReplaced(context.Background()); got != tt.want {
				t.Errorf("loginReplaced = %v, want %v", got, tt.want)
			}
			if checks != tt.wantChecks {
				t.Errorf("auth.test calls = %d, want %d", checks, tt.wantChecks)
			}
			if relogins != tt.wantRelogin {
				t.Errorf("Relogin calls = %d, want %d", relogins, tt.wantRelogin)
			}
		})
	}
}

// TestLoginReplaced_CancelEndsTheCheck: when the loop shuts down while
// auth.test hangs, the check ends at once, and the cancelled request
// does not count as a refused login, so Relogin is not called.
func TestLoginReplaced_CancelEndsTheCheck(t *testing.T) {
	arrived := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(arrived) })
		// Answer nothing until the test ends. The body is read first:
		// only then does the server notice a client that hangs up.
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release) // runs before srv.Close, which waits for the handler
	c := NewClient("xoxc-test", "d-test")
	c.apiBaseURL = srv.URL + "/api/"
	c.api = c.newWebAPI("xoxc-test")
	cm := NewConnectionManager(c, &mockEventHandler{})
	var relogins atomic.Int32
	cm.Relogin = func(context.Context) bool {
		relogins.Add(1)
		return true
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() { done <- cm.loginReplaced(ctx) }()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("auth.test never reached the server")
	}
	cancel()
	select {
	case got := <-done:
		if got {
			t.Error("loginReplaced = true after cancel, want false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loginReplaced did not return after cancel")
	}
	if n := relogins.Load(); n != 0 {
		t.Errorf("Relogin calls = %d, want 0", n)
	}
}

// TestConnectionManager_ReconnectsWithARefreshedLogin drives Run: the
// handshake fails, auth.test says that Slack refuses the login, Relogin
// gives the client the login Slack accepts, and Run connects with it.
// newLoginServer and clientOn are in login_test.go.
func TestConnectionManager_ReconnectsWithARefreshedLogin(t *testing.T) {
	srv := newLoginServer(t, "xoxc-new")
	c := clientOn(srv, "xoxc-old", "d-old")
	cm := NewConnectionManager(c, &mockEventHandler{})
	var relogins atomic.Int32
	cm.Relogin = func(context.Context) bool {
		relogins.Add(1)
		c.SetLogin("xoxc-new", "d-new")
		return true
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		cm.Run(ctx)
		close(done)
	}()
	select {
	case got := <-srv.connected:
		if got != "xoxc-new" {
			t.Errorf("WebSocket token = %q, want xoxc-new", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run never connected with the refreshed login")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if n := relogins.Load(); n != 1 {
		t.Errorf("Relogin calls = %d, want 1", n)
	}
}
