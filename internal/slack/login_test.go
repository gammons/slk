package slackclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// loginServer is a fake Slack that accepts one login. auth.test answers
// ok for the token it accepts and invalid_auth for any other, and the
// WebSocket handshake succeeds only for that token. It records the token
// of every Web API request.
type loginServer struct {
	*httptest.Server
	accept string

	mu     sync.Mutex
	tokens []string
	// connected gets the token of every WebSocket it accepts.
	connected chan string
}

func newLoginServer(t *testing.T, accept string) *loginServer {
	t.Helper()
	s := &loginServer{accept: accept, connected: make(chan string, 4)}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" { // StartWebSocket dials wsBaseURL + "/?token=..."
			token := r.URL.Query().Get("token")
			if token != s.accept {
				http.Error(w, "invalid_auth", http.StatusUnauthorized)
				return
			}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			s.connected <- token
			// Keep the socket open until the client closes it.
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					conn.Close()
					return
				}
			}
		}
		_ = r.ParseForm()
		token := r.PostForm.Get("token")
		s.mu.Lock()
		s.tokens = append(s.tokens, token)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if token != s.accept {
			_, _ = io.WriteString(w, `{"ok":false,"error":"invalid_auth"}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"team_id":"T1","user_id":"U1"}`)
	}))
	t.Cleanup(s.Close)
	return s
}

// requestTokens returns the token of every Web API request so far.
func (s *loginServer) requestTokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.tokens)
}

// clientOn returns a NewClient whose Web API and WebSocket requests go to
// srv. The d cookie cannot be checked on the wire: the jar sends it only
// to https *.slack.com hosts, so tests read the jar (jarDCookies).
func clientOn(srv *loginServer, token, cookie string) *Client {
	c := NewClient(token, cookie)
	c.apiBaseURL = srv.URL + "/api/"
	c.wsBaseURL = "ws://" + srv.Listener.Addr().String()
	c.teamID = "T1"
	// What Connect does once it knows the workspace's API host.
	c.api = c.newWebAPI(token)
	return c
}

// jarDCookies returns the d cookies c's jar sends to Slack.
func jarDCookies(c *Client) []string {
	u, _ := url.Parse("https://slack.com/api/")
	var out []string
	for _, ck := range c.httpClient.Jar.Cookies(u) {
		if ck.Name == "d" {
			out = append(out, ck.Value)
		}
	}
	return out
}

// TestSetLogin_EveryLaterRequestCarriesTheNewLogin covers each way a
// request gets the login: the slack-go client (CheckLogin), the
// hand-rolled form posts (PostForm), the cookie jar, and the WebSocket
// URL.
func TestSetLogin_EveryLaterRequestCarriesTheNewLogin(t *testing.T) {
	srv := newLoginServer(t, "xoxc-new")
	c := clientOn(srv, "xoxc-old", "d-old")
	ctx := context.Background()
	if _, err := c.PostForm(ctx, "conversations.info", nil); err != nil {
		t.Fatalf("PostForm before SetLogin: %v", err)
	}

	c.SetLogin("xoxc-new", "d-new")

	if _, _, err := c.CheckLogin(ctx); err != nil {
		t.Errorf("CheckLogin after SetLogin: %v", err)
	}
	if _, err := c.PostForm(ctx, "conversations.info", nil); err != nil {
		t.Fatalf("PostForm after SetLogin: %v", err)
	}
	if got, want := srv.requestTokens(), []string{"xoxc-old", "xoxc-new", "xoxc-new"}; !slices.Equal(got, want) {
		t.Errorf("tokens sent = %q, want %q", got, want)
	}
	if got := jarDCookies(c); !slices.Equal(got, []string{"d-new"}) {
		t.Errorf("d cookies in the jar = %q, want only d-new", got)
	}

	if err := c.StartWebSocket(&mockEventHandler{}); err != nil {
		t.Fatalf("StartWebSocket after SetLogin: %v", err)
	}
	defer func() {
		c.StopWebSocket()
		<-c.WsDone()
	}()
	select {
	case got := <-srv.connected:
		if got != "xoxc-new" {
			t.Errorf("WebSocket token = %q, want xoxc-new", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the WebSocket never connected")
	}
}

// TestSetLogin_WhileRequestsRun is for the race detector: requests read
// the login while SetLogin replaces it.
func TestSetLogin_WhileRequestsRun(t *testing.T) {
	srv := newLoginServer(t, "xoxc-0")
	c := clientOn(srv, "xoxc-0", "d-0")
	ctx := context.Background()
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				_, _ = c.PostForm(ctx, "conversations.info", nil)
				_, _, _ = c.CheckLogin(ctx)
			}
		}()
	}
	for i := range 10 {
		c.SetLogin(fmt.Sprintf("xoxc-%d", i), fmt.Sprintf("d-%d", i))
	}
	wg.Wait()
}

// TestCheckLogin_ReturnsTheAccountOrARefusal: an accepted login gives
// the team and the user Slack accepts it as; a refused one is an auth
// error.
func TestCheckLogin_ReturnsTheAccountOrARefusal(t *testing.T) {
	srv := newLoginServer(t, "xoxc-good")
	ctx := context.Background()
	team, user, err := clientOn(srv, "xoxc-good", "d").CheckLogin(ctx)
	if err != nil || team != "T1" || user != "U1" {
		t.Errorf("CheckLogin with an accepted login = %q, %q, %v; want T1, U1, nil", team, user, err)
	}
	if _, _, err := clientOn(srv, "xoxc-bad", "d").CheckLogin(ctx); !IsAuthError(err) {
		t.Errorf("CheckLogin with a refused login = %v, want an auth error", err)
	}
}
