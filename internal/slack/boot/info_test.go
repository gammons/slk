package boot

import (
	"context"
	"errors"
	"net/url"
	"testing"
)

func infoWith(t *testing.T, body string) (*AppHome, url.Values, string) {
	t.Helper()
	var gotMethod string
	var gotForm url.Values
	post := func(_ context.Context, method string, form url.Values) ([]byte, error) {
		gotMethod, gotForm = method, form
		return []byte(body), nil
	}
	home, err := ConversationsInfo(context.Background(), post, "D000")
	if err != nil {
		t.Fatalf("ConversationsInfo: %v", err)
	}
	return home, gotForm, gotMethod
}

// A conversations.info capture for a read-only app DM (IDs replaced).
func TestConversationsInfo_DecodesReadOnlyAppHome(t *testing.T) {
	const body = `{"ok":true,
	"channel":{"id":"D000","created":1,"is_im":true,"user":"U000","properties":{"is_dormant":true},"latest":null},
	"app_home":{"app_id":"A000","app_installed_team_id":"T000","conversation_id":"D000","home_tab_enabled":false,"messages_tab_enabled":true,"messages_tab_read_only_enabled":true,"agent_tasks_enabled":true,"home_view_id":null},
	"home_view":null}`
	home, form, method := infoWith(t, body)
	if home == nil || !home.MessagesTabReadOnlyEnabled || !home.MessagesTabEnabled || home.AppID != "A000" {
		t.Fatalf("AppHome = %+v, want read-only app A000", home)
	}
	if home.CanReceiveMessages() {
		t.Error("CanReceiveMessages = true for a read-only app DM")
	}
	if method != "conversations.info" {
		t.Errorf("method = %q", method)
	}
	if form.Get("channel") != "D000" || form.Get("return_app_home") != "true" || len(form) != 2 {
		t.Errorf("form = %v, want channel and return_app_home=true only", form)
	}
}

func TestConversationsInfo_WritableAppHome(t *testing.T) {
	home, _, _ := infoWith(t, `{"ok":true,"app_home":{"messages_tab_enabled":true,"messages_tab_read_only_enabled":false}}`)
	if home == nil || !home.CanReceiveMessages() {
		t.Errorf("AppHome = %+v, want sendable", home)
	}
}

func TestConversationsInfo_AbsentAppHomeIsNil(t *testing.T) {
	home, _, _ := infoWith(t, `{"ok":true,"channel":{"id":"C000"}}`)
	if home != nil {
		t.Errorf("AppHome = %+v, want nil", home)
	}
	if !home.CanReceiveMessages() {
		t.Error("nil AppHome must be sendable")
	}
}

func TestConversationsInfo_Errors(t *testing.T) {
	ctx := context.Background()
	if _, err := ConversationsInfo(ctx, func(context.Context, string, url.Values) ([]byte, error) {
		return []byte(`{"ok":false,"error":"channel_not_found"}`), nil
	}, "D000"); err == nil {
		t.Error("ok=false: want error")
	}
	if _, err := ConversationsInfo(ctx, func(context.Context, string, url.Values) ([]byte, error) {
		return nil, errors.New("net")
	}, "D000"); err == nil {
		t.Error("post error: want error")
	}
	if _, err := ConversationsInfo(ctx, func(context.Context, string, url.Values) ([]byte, error) {
		return []byte(`not json`), nil
	}, "D000"); err == nil {
		t.Error("bad json: want error")
	}
}
