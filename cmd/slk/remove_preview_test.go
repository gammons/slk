package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gammons/slk/internal/core"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
	"github.com/slack-go/slack"
)

type fakeRemover struct {
	channel, ts string
	id          int
	deadline    time.Duration
	hasDeadline bool
	calls       int
	err         error
}

func (f *fakeRemover) DeleteAttachment(ctx context.Context, channelID, ts string, attachmentID int) error {
	f.calls++
	f.channel, f.ts, f.id = channelID, ts, attachmentID
	var d time.Time
	d, f.hasDeadline = ctx.Deadline()
	f.deadline = time.Until(d)
	return f.err
}

func TestRemoveAttachmentMapsFieldsAndBoundsTheCall(t *testing.T) {
	f := &fakeRemover{}
	got := removeAttachment(context.Background(), f, "T1", "C1", "1.000100", 2)
	if f.calls != 1 || f.channel != "C1" || f.ts != "1.000100" || f.id != 2 {
		t.Errorf("call = %+v, want one for C1 1.000100 #2", f)
	}
	if !f.hasDeadline || f.deadline <= 0 || f.deadline > removeAttachmentTimeout {
		t.Errorf("deadline in %v (set %v), want within %v", f.deadline, f.hasDeadline, removeAttachmentTimeout)
	}
	if want := (ui.AttachmentRemovedMsg{ChannelID: "C1", TS: "1.000100"}); got != want {
		t.Errorf("msg = %+v, want %+v", got, want)
	}
}

// refusal is the reason of err's *core.AttachmentRefusedError, if any.
func refusal(err error) (string, bool) {
	var ref *core.AttachmentRefusedError
	if errors.As(err, &ref) {
		return ref.Reason, true
	}
	return "", false
}

// With no client the call is never sent: certainly not done.
func TestRemoveAttachmentWithoutAClientIsRefused(t *testing.T) {
	got := removeAttachment(context.Background(), nil, "T9", "C1", "1.0", 1)
	if got.ChannelID != "C1" || got.TS != "1.0" {
		t.Errorf("msg = %+v, want C1 1.0", got)
	}
	if _, ok := refusal(got.Err); !ok {
		t.Errorf("err = %v, want *core.AttachmentRefusedError", got.Err)
	}
}

// Slack's refusal and a rate limit are certain: the UI may clear its guard.
// The reason is the bare code, never the response body.
func TestRemoveAttachmentTranslatesDefiniteRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		reason string
	}{
		"refused":      {&slackclient.AttachmentRejectedError{Code: "cant_delete_message"}, "cant_delete_message"},
		"rate limited": {&slack.RateLimitedError{RetryAfter: time.Second}, "rate limited"},
	} {
		t.Run(name, func(t *testing.T) {
			got := removeAttachment(context.Background(), &fakeRemover{err: tc.err}, "T1", "C1", "1.0", 1)
			if reason, ok := refusal(got.Err); !ok || reason != tc.reason {
				t.Errorf("err = %v, want a refusal %q", got.Err, tc.reason)
			}
		})
	}
}

// Anything else leaves it unknown whether Slack acted, and passes through.
func TestRemoveAttachmentPassesAmbiguousErrorsThrough(t *testing.T) {
	for name, err := range map[string]error{
		"transport":      errors.New("calling chat.deleteAttachment: EOF"),
		"internal_error": &slackclient.AttachmentRejectedError{Code: "internal_error"},
		"timeout":        context.DeadlineExceeded,
	} {
		t.Run(name, func(t *testing.T) {
			got := removeAttachment(context.Background(), &fakeRemover{err: err}, "T1", "C1", "1.0", 1)
			if !errors.Is(got.Err, err) {
				t.Errorf("err = %v, want %v", got.Err, err)
			}
			if _, ok := refusal(got.Err); ok {
				t.Errorf("err = %v counted as a refusal", got.Err)
			}
		})
	}
}
