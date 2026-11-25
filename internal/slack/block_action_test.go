package slackclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func refreshRequest() BlockActionRequest {
	return BlockActionRequest{
		ViewID:    "V0EXAMPLE1",
		ServiceID: "B0EXAMPLE1",
		ActionID:  "home_refresh",
		BlockID:   "actions1",
		Label:     "Refresh",
		Value:     "home_refresh",
	}
}

func TestMarshalBlockActions(t *testing.T) {
	got, err := marshalBlockActions(refreshRequest())
	if err != nil {
		t.Fatal(err)
	}
	var actions []map[string]any
	if err := json.Unmarshal([]byte(got), &actions); err != nil {
		t.Fatalf("actions is not a JSON array: %v (%s)", err, got)
	}
	if len(actions) != 1 {
		t.Fatalf("action count = %d, want 1", len(actions))
	}
	a := actions[0]
	want := map[string]any{
		"action_id": "home_refresh",
		"block_id":  "actions1",
		"type":      "button",
		"value":     "home_refresh",
	}
	for k, v := range want {
		if a[k] != v {
			t.Errorf("%s = %v, want %v", k, a[k], v)
		}
	}
	text, _ := a["text"].(map[string]any)
	if text["type"] != "plain_text" || text["text"] != "Refresh" {
		t.Errorf("text = %v, want the button label as plain_text", text)
	}
	if ts, _ := a["action_ts"].(string); ts == "" {
		t.Error("action_ts is empty")
	}
	if _, ok := a["style"]; ok {
		t.Errorf("style is present for an unstyled button: %v", a["style"])
	}
}

func TestMarshalBlockActions_EchoesConfirm(t *testing.T) {
	req := refreshRequest()
	req.Confirm = &BlockActionConfirm{
		Title: "Email credentials?", Text: "A fresh link <will> be emailed.",
		OKText: "Send email", DismissText: "Cancel",
	}
	got, err := marshalBlockActions(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"confirm":{"title":{"type":"plain_text","text":"Email credentials?","emoji":true}`,
		`"text":{"type":"mrkdwn","text":"A fresh link <will> be emailed.","verbatim":false}`,
		`"confirm":{"type":"plain_text","text":"Send email","emoji":true}`,
		`"deny":{"type":"plain_text","text":"Cancel","emoji":true}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("payload %s\nmissing %s (HTML must not be escaped)", got, want)
		}
	}
}

func TestMarshalBlockActionContainer(t *testing.T) {
	got, err := marshalBlockActionContainer(BlockActionRequest{ViewID: "V0EXAMPLE1"})
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"type":"view","view_id":"V0EXAMPLE1"}` {
		t.Errorf("container = %s", got)
	}
}

func TestMarshalBlockActionContainer_Message(t *testing.T) {
	got, err := marshalBlockActionContainer(BlockActionRequest{ChannelID: "C0EXAMPLE1", MessageTS: "1700000000.000100"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"message","message_ts":"1700000000.000100","channel_id":"C0EXAMPLE1","is_ephemeral":false}`
	if got != want {
		t.Errorf("container = %s\nwant        %s", got, want)
	}
}

func TestBlockAction_PostsWebClientForm(t *testing.T) {
	var gotPath, gotServiceID, gotToken, gotActions, gotContainer, gotClientToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = r.ParseForm()
		gotServiceID = r.PostForm.Get("service_id")
		gotToken = r.PostForm.Get("token")
		gotActions = r.PostForm.Get("actions")
		gotContainer = r.PostForm.Get("container")
		gotClientToken = r.PostForm.Get("client_token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)

	if err := c.BlockAction(context.Background(), refreshRequest()); err != nil {
		t.Fatalf("BlockAction: %v", err)
	}
	if gotPath != "/api/blocks.actions" {
		t.Errorf("path = %q, want /api/blocks.actions", gotPath)
	}
	if gotServiceID != "B0EXAMPLE1" {
		t.Errorf("service_id = %q, want the app's bot id", gotServiceID)
	}
	if gotToken != "xoxc-test" {
		t.Errorf("token = %q, want the xoxc token in the form body", gotToken)
	}
	if !strings.HasPrefix(gotClientToken, "web-") {
		t.Errorf("client_token = %q, want a web-<millis> token", gotClientToken)
	}
	if !strings.Contains(gotActions, `"action_id":"home_refresh"`) {
		t.Errorf("actions = %s, want the pressed button", gotActions)
	}
	if gotContainer != `{"type":"view","view_id":"V0EXAMPLE1"}` {
		t.Errorf("container = %s", gotContainer)
	}
}

func TestBlockAction_OKFalseIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_arguments"}`))
	}))
	defer srv.Close()
	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)

	err := c.BlockAction(context.Background(), refreshRequest())
	if err == nil || !strings.Contains(err.Error(), "invalid_arguments") {
		t.Errorf("err = %v, want one carrying invalid_arguments", err)
	}
}
