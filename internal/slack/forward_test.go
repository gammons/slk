package slackclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestShareMessageForm_MatchesCapture(t *testing.T) {
	got := shareMessageForm("CFAKE0001", "1700000000.000100", "CFAKE0002", "00000000-0000-4000-8000-000000000000")
	want := capturedForm(t, "chat_share_message.json")
	for k := range want {
		if got.Get(k) != want.Get(k) {
			t.Errorf("%s = %q, want %q", k, got.Get(k), want.Get(k))
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected field %s=%q", k, got.Get(k))
		}
	}
}

func TestForwardMessage(t *testing.T) {
	const permalink = "https://example.invalid/archives/CFAKE0001/p1700000000000100"
	for _, tc := range []struct {
		name      string
		body      string
		wantTS    string
		wantError string
	}{
		{"success", `{"ok":true,"channel":"CFAKE0002","ts":"1700000001.000200"}`, "1700000001.000200", ""},
		{"rejected", `{"ok":false,"error":"not_in_channel"}`, "", "not_in_channel"},
		{"empty ts", `{"ok":true,"channel":"CFAKE0002","ts":""}`, "", "empty message timestamp"},
		{"missing ts", `{"ok":true,"channel":"CFAKE0002"}`, "", "empty message timestamp"},
		{"bad json", `<html>`, "", "parsing chat.shareMessage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			var gotForm url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_ = r.ParseForm()
				gotForm = r.PostForm
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c := NewClient("xoxc-test", "d-cookie")
			pointClientAtTestServer(t, c, srv)
			c.api = &mockSlackAPI{getPermalinkContextFn: func(context.Context, *slack.PermalinkParameters) (string, error) {
				return permalink, nil
			}}

			ts, link, err := c.ForwardMessage(context.Background(), "CFAKE0001", "1700000000.000100", "CFAKE0002")
			if gotPath != "/api/chat.shareMessage" {
				t.Errorf("path = %q, want /api/chat.shareMessage", gotPath)
			}
			if id := gotForm.Get("client_msg_id"); !uuidRe.MatchString(id) {
				t.Errorf("client_msg_id = %q, want a UUID", id)
			}
			gotForm.Set("client_msg_id", "00000000-0000-4000-8000-000000000000")
			for k, v := range capturedForm(t, "chat_share_message.json") {
				if gotForm.Get(k) != v[0] {
					t.Errorf("%s = %q, want %q", k, gotForm.Get(k), v[0])
				}
			}
			if gotForm.Get("token") != "xoxc-test" {
				t.Errorf("token not sent")
			}
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error = %v, want %q", err, tc.wantError)
				}
				if ts != "" || link != "" {
					t.Errorf("failed result = (%q, %q), want empty", ts, link)
				}
				return
			}
			if err != nil {
				t.Fatalf("ForwardMessage: %v", err)
			}
			if ts != tc.wantTS || link != permalink {
				t.Errorf("result = (%q, %q), want (%q, %q)", ts, link, tc.wantTS, permalink)
			}
		})
	}
}

// The share already happened, so a permalink failure only empties the echo
// text; it must not report the forward as failed (a retry would duplicate it).
func TestForwardMessage_PermalinkFailureIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"channel":"CFAKE0002","ts":"1700000001.000200"}`))
	}))
	defer srv.Close()
	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)
	c.api = &mockSlackAPI{getPermalinkContextFn: func(context.Context, *slack.PermalinkParameters) (string, error) {
		return "", errors.New("boom")
	}}
	ts, link, err := c.ForwardMessage(context.Background(), "CFAKE0001", "1700000000.000100", "CFAKE0002")
	if err != nil || ts != "1700000001.000200" || link != "" {
		t.Errorf("result = (%q, %q, %v), want ts and empty permalink", ts, link, err)
	}
}
