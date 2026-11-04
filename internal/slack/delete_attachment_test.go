package slackclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/slack-go/slack"
)

// The request shape here comes from emacs-slack's slack-remove-preview
// (method chat.deleteAttachment; form fields channel, ts, attachment;
// _x_reason=delete-single-attachment). It is NOT a capture of our own:
// no HAR of the web client's "Remove preview" has been recorded, and
// slk sends the fields url-encoded where emacs-slack sends multipart.
// A scrubbed capture would replace these expectations.
func deleteAttachmentServer(t *testing.T, status int, body string) (*Client, *http.Request, *int) {
	t.Helper()
	var (
		mu    sync.Mutex
		got   = &http.Request{}
		calls int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		_ = r.ParseForm()
		*got = *r
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "0")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)
	return c, got, &calls
}

func TestDeleteAttachment_PostsDocumentedForm(t *testing.T) {
	c, got, _ := deleteAttachmentServer(t, http.StatusOK, `{"ok":true}`)
	if err := c.DeleteAttachment(context.Background(), "C1", "1700000000.000100", 2); err != nil {
		t.Fatalf("DeleteAttachment: %v", err)
	}
	if got.URL.Path != "/api/chat.deleteAttachment" {
		t.Errorf("path = %q, want /api/chat.deleteAttachment", got.URL.Path)
	}
	for field, want := range map[string]string{
		"channel":    "C1",
		"ts":         "1700000000.000100",
		"attachment": "2",
		"token":      "xoxc-test",
		"_x_reason":  "delete-single-attachment",
	} {
		if v := got.PostForm.Get(field); v != want {
			t.Errorf("form %s = %q, want %q", field, v, want)
		}
	}
}

// Slack's refusal comes back typed, with its code and without the body,
// so callers can tell it from an answer that never arrived.
func TestDeleteAttachment_RefusalIsTyped(t *testing.T) {
	c, _, _ := deleteAttachmentServer(t, http.StatusOK, `{"ok":false,"error":"cant_delete_message"}`)
	err := c.DeleteAttachment(context.Background(), "C1", "1.0", 1)
	var rej *AttachmentRejectedError
	if !errors.As(err, &rej) || rej.Code != "cant_delete_message" {
		t.Fatalf("err = %v, want *AttachmentRejectedError{cant_delete_message}", err)
	}
	if strings.Contains(err.Error(), "body=") {
		t.Errorf("err = %q carries the body", err)
	}
	if rej.MayHaveActed() {
		t.Error("cant_delete_message must not count as possibly done")
	}
}

// Slack documents internal_error and fatal_error as errors it may return
// after part of the operation succeeded.
func TestDeleteAttachment_InternalErrorsMayHaveActed(t *testing.T) {
	for _, code := range []string{"internal_error", "fatal_error"} {
		c, _, _ := deleteAttachmentServer(t, http.StatusOK, `{"ok":false,"error":"`+code+`"}`)
		var rej *AttachmentRejectedError
		if err := c.DeleteAttachment(context.Background(), "C1", "1.0", 1); !errors.As(err, &rej) || !rej.MayHaveActed() {
			t.Errorf("%s: err = %v, want a refusal that may have acted", code, err)
		}
	}
}

// A body that is not a Slack envelope is no refusal: it says nothing
// about whether Slack acted.
func TestDeleteAttachment_UnreadableAnswerIsNotARefusal(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"ok":null}`, `<html>`} {
		c, _, _ := deleteAttachmentServer(t, http.StatusOK, body)
		err := c.DeleteAttachment(context.Background(), "C1", "1.0", 1)
		var rej *AttachmentRejectedError
		if err == nil || errors.As(err, &rej) {
			t.Errorf("body %s: err = %v, want an error that is not a refusal", body, err)
		}
	}
}

// A rate limit is returned as it comes, once, as slack-go's type so callers
// can tell it apart: a resend after a lost answer can hit an id Slack has
// already renumbered.
func TestDeleteAttachment_DoesNotRetryOn429(t *testing.T) {
	c, _, calls := deleteAttachmentServer(t, http.StatusTooManyRequests, ``)
	err := c.DeleteAttachment(context.Background(), "C1", "1.0", 1)
	var rl *slack.RateLimitedError
	if !errors.As(err, &rl) {
		t.Errorf("err = %v, want *slack.RateLimitedError", err)
	}
	if *calls != 1 {
		t.Errorf("calls = %d, want exactly 1", *calls)
	}
}
