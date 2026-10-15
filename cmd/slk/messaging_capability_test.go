package main

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/gammons/slk/internal/slackhttp"
)

const infoReadOnlyBody = `{"ok":true,"channel":{"id":"D000","is_im":true,"user":"U000","properties":{"is_dormant":true},"latest":null},
"app_home":{"app_id":"A000","app_installed_team_id":"T000","conversation_id":"D000","home_tab_enabled":false,"messages_tab_enabled":true,"messages_tab_read_only_enabled":true,"agent_tasks_enabled":true,"home_view_id":null},"home_view":null}`

const infoWritableBody = `{"ok":true,"app_home":{"app_id":"A000","conversation_id":"D000","messages_tab_enabled":true,"messages_tab_read_only_enabled":false}}`

const viewReadOnlyBody = `{"ok":true,"channel":{"id":"D000"},"app_home":{"app_id":"A000","messages_tab_enabled":true,"messages_tab_read_only_enabled":true}}`

// fakePost answers per method; a nil body with an error fails the call.
func fakePost(answers map[string]struct {
	body string
	err  error
}, calls *[]string, forms map[string]url.Values) func(context.Context, string, url.Values) ([]byte, error) {
	return func(ctx context.Context, method string, form url.Values) ([]byte, error) {
		if method == "conversations.info" {
			if got := slackhttp.ReasonFrom(ctx); got != "app-homes-store/conditional-fetching" {
				return nil, errors.New("conversations.info reason = " + got)
			}
		}
		*calls = append(*calls, method)
		forms[method] = form
		a := answers[method]
		if a.err != nil {
			return nil, a.err
		}
		return []byte(a.body), nil
	}
}

type answer = struct {
	body string
	err  error
}

func TestProbeMessagingCapability(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name      string
		answers   map[string]answer
		wantSend  bool
		wantCalls []string
	}{
		{"info read-only decides", map[string]answer{"conversations.info": {body: infoReadOnlyBody}}, false, []string{"conversations.info"}},
		{"info writable decides", map[string]answer{"conversations.info": {body: infoWritableBody}}, true, []string{"conversations.info"}},
		{"info without app_home falls back to view", map[string]answer{
			"conversations.info": {body: `{"ok":true}`},
			"conversations.view": {body: viewReadOnlyBody},
		}, false, []string{"conversations.info", "conversations.view"}},
		{"info error falls back to view", map[string]answer{
			"conversations.info": {err: boom},
			"conversations.view": {body: viewReadOnlyBody},
		}, false, []string{"conversations.info", "conversations.view"}},
		{"view answer for another channel fails open", map[string]answer{
			"conversations.info": {body: `{"ok":true}`},
			"conversations.view": {body: `{"ok":true,"channel":{"id":"DOTHER"},"app_home":{"app_id":"A000","messages_tab_enabled":true,"messages_tab_read_only_enabled":true}}`},
		}, true, []string{"conversations.info", "conversations.view"}},
		{"both fail open", map[string]answer{
			"conversations.info": {err: boom},
			"conversations.view": {err: boom},
		}, true, []string{"conversations.info", "conversations.view"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			forms := map[string]url.Values{}
			post := fakePost(tc.answers, &calls, forms)
			got := probeMessagingCapability(context.Background(), post, "D000")
			if got.ChannelID != "D000" || got.CanSend != tc.wantSend {
				t.Errorf("got %+v, want CanSend=%v", got, tc.wantSend)
			}
			if len(calls) != len(tc.wantCalls) {
				t.Fatalf("calls = %v, want %v", calls, tc.wantCalls)
			}
			for i := range calls {
				if calls[i] != tc.wantCalls[i] {
					t.Fatalf("calls = %v, want %v", calls, tc.wantCalls)
				}
			}
			f := forms["conversations.info"]
			if f.Get("channel") != "D000" || f.Get("return_app_home") != "true" || len(f) != 2 {
				t.Errorf("conversations.info form = %v, want channel and return_app_home only", f)
			}
		})
	}
}
