package slackclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/core/blocks"
)

// capturedDismissPayload is the `payload` the Slack web client sent for
// "Dismiss" on Slackbot's mention ephemeral (placeholder IDs). Slack
// accepted it; slk must send the same bytes.
const capturedDismissPayload = `{"actions":[{"id":"2","name":"ignore","text":"Dismiss","type":"button","value":"ignore","style":""}],"attachment_id":"1","callback_id":"consistentephemeralmentions_U0EXAMPLE01_1791541587716039_0","channel_id":"C0EXAMPLE01","is_ephemeral":true,"message_ts":"1791541587.716040","prompt_app_install":false}`

func dismissRequest() AttachmentActionRequest {
	return AttachmentActionRequest{
		ChannelID:    "C0EXAMPLE01",
		MessageTS:    "1791541587.716040",
		AttachmentID: 1,
		CallbackID:   "consistentephemeralmentions_U0EXAMPLE01_1791541587716039_0",
		Ephemeral:    true,
		Action:       blocks.LegacyAction{ID: "2", Name: "ignore", Text: "Dismiss", Type: "button", Value: "ignore"},
	}
}

func TestMarshalAttachmentAction_MatchesWebClientCapture(t *testing.T) {
	got, err := marshalAttachmentAction(dismissRequest())
	if err != nil {
		t.Fatal(err)
	}
	if got != capturedDismissPayload {
		t.Errorf("payload =\n  %s\nwant\n  %s", got, capturedDismissPayload)
	}
}

// A pressed action is echoed as received, confirm dialog included. The
// capture covers only Dismiss, so this shape is unverified; the order
// follows the inbound frame's confirm object.
func TestMarshalAttachmentAction_EchoesConfirm(t *testing.T) {
	req := dismissRequest()
	req.Action = blocks.LegacyAction{
		ID: "1", Name: "invite", Text: "Add Them", Type: "button", Value: "invite",
		Confirm: &blocks.ActionConfirm{Title: "Are you sure?", Text: "History & files <visible>", OKText: "Add", DismissText: "Cancel"},
	}
	got, err := marshalAttachmentAction(req)
	if err != nil {
		t.Fatal(err)
	}
	want := `"confirm":{"text":"History & files <visible>","title":"Are you sure?","ok_text":"Add","dismiss_text":"Cancel"}`
	if !strings.Contains(got, want) {
		t.Errorf("payload %s\nmissing %s (HTML must not be escaped)", got, want)
	}
}

func TestAttachmentAction_PostsPayloadForm(t *testing.T) {
	var gotPath, gotPayload, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = r.ParseForm()
		gotPayload = r.PostForm.Get("payload")
		gotToken = r.PostForm.Get("token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)

	if err := c.AttachmentAction(context.Background(), dismissRequest()); err != nil {
		t.Fatalf("AttachmentAction: %v", err)
	}
	if gotPath != "/api/chat.attachmentAction" {
		t.Errorf("path = %q, want /api/chat.attachmentAction", gotPath)
	}
	if gotPayload != capturedDismissPayload {
		t.Errorf("payload form field =\n  %s\nwant\n  %s", gotPayload, capturedDismissPayload)
	}
	if gotToken != "xoxc-test" {
		t.Errorf("token = %q, want the xoxc token in the form body", gotToken)
	}
}

func TestAttachmentAction_OKFalseIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_callback"}`))
	}))
	defer srv.Close()
	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)

	err := c.AttachmentAction(context.Background(), dismissRequest())
	if err == nil || !strings.Contains(err.Error(), "invalid_callback") {
		t.Errorf("err = %v, want one carrying invalid_callback", err)
	}
}
