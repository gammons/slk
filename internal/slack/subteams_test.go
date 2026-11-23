package slackclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/slack-go/slack"
)

func TestGetUserGroupInfo(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    UserGroupInfo
		wantErr error
		anyErr  bool
	}{
		{
			name: "found",
			body: `{"ok":true,"subteams":[{"id":"S0SRE","team_id":"T0MEMBER","name":"SRE","handle":"sre"}]}`,
			want: UserGroupInfo{ID: "S0SRE", TeamID: "T0MEMBER", Name: "SRE", Handle: "sre"},
		},
		{name: "not found", body: `{"ok":false,"error":"subteam_not_found"}`, wantErr: ErrUserGroupNotFound},
		{name: "other id only", body: `{"ok":true,"subteams":[{"id":"S0OTHER","handle":"x"}]}`, wantErr: ErrUserGroupNotFound},
		{name: "other error", body: `{"ok":false,"error":"invalid_auth"}`, anyErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotGroup, gotToken string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_ = r.ParseForm()
				gotGroup = r.PostForm.Get("usergroup")
				gotToken = r.PostForm.Get("token")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			got, err := newTestClient(srv).GetUserGroupInfo(context.Background(), "S0SRE")
			if gotPath != "/api/subteams.info" || gotGroup != "S0SRE" || gotToken != "xoxc-test" {
				t.Fatalf("request = %s usergroup=%q token=%q", gotPath, gotGroup, gotToken)
			}
			switch {
			case tc.anyErr:
				if err == nil || errors.Is(err, ErrUserGroupNotFound) {
					t.Fatalf("err = %v, want a non-not-found error", err)
				}
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			default:
				if err != nil || got != tc.want {
					t.Fatalf("got %+v, %v; want %+v", got, err, tc.want)
				}
			}
		})
	}
}

// A 429 must reach the caller as slack-go's RateLimitedError with
// Slack's Retry-After, so the resolver can wait it out and retry
// instead of treating it as a plain failure.
func TestGetUserGroupInfo_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetUserGroupInfo(context.Background(), "S0SRE")
	var rl *slack.RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v (%T), want *slack.RateLimitedError", err, err)
	}
	if rl.RetryAfter != 7*time.Second {
		t.Fatalf("RetryAfter = %v, want 7s", rl.RetryAfter)
	}
}
