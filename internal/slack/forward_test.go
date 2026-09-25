package slackclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

func TestForwardMessageRequests(t *testing.T) {
	const permalink = "https://example.slack.com/archives/Csource/p123000456?thread_ts=123.000000&cid=Csource"
	for _, tc := range []struct {
		name          string
		permalinkBody string
		postBody      string
		wantError     string
		wantPost      bool
	}{
		{"success", `{"ok":true,"permalink":"` + permalink + `"}`, `{"ok":true,"channel":"Cdestination","ts":"124.000000"}`, "", true},
		{"permalink failure", `{"ok":false,"error":"message_not_found"}`, "", "message_not_found", false},
		{"empty permalink", `{"ok":true,"permalink":""}`, "", "empty permalink", false},
		{"post failure", `{"ok":true,"permalink":"` + permalink + `"}`, `{"ok":false,"error":"not_in_channel"}`, "not_in_channel", true},
		{"empty post timestamp", `{"ok":true,"permalink":"` + permalink + `"}`, `{"ok":true,"channel":"Cdestination","ts":""}`, "empty message timestamp", true},
		{"missing post timestamp", `{"ok":true,"permalink":"` + permalink + `"}`, `{"ok":true,"channel":"Cdestination"}`, "empty message timestamp", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if err := r.ParseForm(); err != nil {
					t.Errorf("ParseForm: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/chat.getPermalink":
					if r.Form.Get("channel") != "Csource" || r.Form.Get("message_ts") != "123.000456" {
						t.Errorf("permalink form = %v", r.Form)
					}
					fmt.Fprint(w, tc.permalinkBody)
				case "/chat.postMessage":
					if r.Method != http.MethodPost {
						t.Errorf("post method = %s", r.Method)
					}
					for key, want := range map[string]string{
						"channel": "Cdestination", "text": permalink,
						"unfurl_links": "true", "unfurl_media": "true",
					} {
						if got := r.PostForm.Get(key); got != want {
							t.Errorf("%s = %q, want %q", key, got, want)
						}
					}
					for _, key := range []string{"blocks", "attachments", "thread_ts"} {
						if r.PostForm.Has(key) {
							t.Errorf("unexpected %s in forwarding form: %v", key, r.PostForm)
						}
					}
					fmt.Fprint(w, tc.postBody)
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			c := &Client{api: slack.New("xoxc-test", slack.OptionAPIURL(srv.URL+"/"))}
			postedTS, postedText, err := c.ForwardMessage(context.Background(), "Csource", "123.000456", "Cdestination")
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("ForwardMessage: %v", err)
				}
				if postedTS != "124.000000" || postedText != permalink {
					t.Errorf("result = (%q, %q), want actual posted timestamp and permalink", postedTS, postedText)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Errorf("error = %v, want %q", err, tc.wantError)
				}
				if postedTS != "" || postedText != "" {
					t.Errorf("failed forward result = (%q, %q), want empty values", postedTS, postedText)
				}
			}
			wantPaths := []string{"/chat.getPermalink"}
			if tc.wantPost {
				wantPaths = append(wantPaths, "/chat.postMessage")
			}
			if !reflect.DeepEqual(paths, wantPaths) {
				t.Errorf("requests = %v, want %v", paths, wantPaths)
			}
		})
	}
}

func TestForwardMessageContextAndErrors(t *testing.T) {
	boom := errors.New("transport failed")
	for _, stage := range []string{"permalink", "post"} {
		for _, cause := range []error{boom, context.Canceled, context.DeadlineExceeded} {
			t.Run(stage+"/"+cause.Error(), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				posted := false
				api := &forwardContextAPI{
					mockSlackAPI: &mockSlackAPI{getPermalinkContextFn: func(got context.Context, _ *slack.PermalinkParameters) (string, error) {
						if got != ctx {
							t.Error("permalink context was not preserved")
						}
						if stage == "permalink" {
							return "", cause
						}
						return "https://example.slack.com/archives/Csource/p123000456", nil
					}},
					post: func(got context.Context, _ string, _ ...slack.MsgOption) (string, string, error) {
						posted = true
						if got != ctx {
							t.Error("post context was not preserved")
						}
						return "", "", cause
					},
				}
				postedTS, postedText, err := (&Client{api: api}).ForwardMessage(ctx, "Csource", "123.000456", "Cdestination")
				if postedTS != "" || postedText != "" {
					t.Errorf("failed forward result = (%q, %q), want empty values", postedTS, postedText)
				}
				if !errors.Is(err, cause) {
					t.Errorf("error = %v, want wrapped %v", err, cause)
				}
				if posted != (stage == "post") {
					t.Errorf("posted = %v, stage = %s", posted, stage)
				}
			})
		}
	}
}

type forwardContextAPI struct {
	*mockSlackAPI
	post func(context.Context, string, ...slack.MsgOption) (string, string, error)
}

func (m *forwardContextAPI) PostMessageContext(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error) {
	return m.post(ctx, channelID, options...)
}
