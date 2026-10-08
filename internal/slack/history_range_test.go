package slackclient

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/slack-go/slack"
)

// historyPage builds one conversations.history response.
func historyPage(nextCursor string, timestamps ...string) *slack.GetConversationHistoryResponse {
	resp := &slack.GetConversationHistoryResponse{HasMore: nextCursor != ""}
	resp.ResponseMetaData.NextCursor = nextCursor
	for _, ts := range timestamps {
		resp.Messages = append(resp.Messages, slack.Message{Msg: slack.Msg{Timestamp: ts}})
	}
	return resp
}

// pageRecorder is a WalkHistory visitor that records what it was given.
type pageRecorder struct {
	pages [][]string
	err   error
}

// visit records a page's timestamps and returns the configured error.
func (r *pageRecorder) visit(page []slack.Message) error {
	var ts []string
	for _, m := range page {
		ts = append(ts, m.Timestamp)
	}
	r.pages = append(r.pages, ts)
	return r.err
}

func TestWalkHistory_StreamsEveryPageWithBounds(t *testing.T) {
	var calls []slack.GetConversationHistoryParameters
	responses := []*slack.GetConversationHistoryResponse{
		historyPage("cur1", "300.000000", "200.000000"),
		historyPage("", "100.000000"),
	}
	mock := &mockSlackAPI{
		getConversationHistoryFn: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			calls = append(calls, *params)
			return responses[len(calls)-1], nil
		},
	}
	client := &Client{api: mock}
	rec := &pageRecorder{}

	if err := client.WalkHistory(context.Background(), "C1", "50.000000", "400.000000", rec.visit); err != nil {
		t.Fatalf("WalkHistory: %v", err)
	}
	if got := fmt.Sprint(rec.pages); got != "[[300.000000 200.000000] [100.000000]]" {
		t.Errorf("pages = %s", got)
	}
	if len(calls) != 2 {
		t.Fatalf("API calls = %d, want 2", len(calls))
	}
	for i, call := range calls {
		if call.ChannelID != "C1" || call.Oldest != "50.000000" || call.Latest != "400.000000" {
			t.Errorf("call %d bounds = %q %q %q", i, call.ChannelID, call.Oldest, call.Latest)
		}
		if !call.Inclusive {
			t.Errorf("call %d: Inclusive = false, want true", i)
		}
		if call.Limit != historyPageSize {
			t.Errorf("call %d: Limit = %d, want %d", i, call.Limit, historyPageSize)
		}
	}
	if calls[0].Cursor != "" || calls[1].Cursor != "cur1" {
		t.Errorf("cursors = %q, %q; want \"\", \"cur1\"", calls[0].Cursor, calls[1].Cursor)
	}
}

func TestWalkHistory_RetriesSamePageAfterRateLimit(t *testing.T) {
	var cursors []string
	mock := &mockSlackAPI{
		getConversationHistoryFn: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			cursors = append(cursors, params.Cursor)
			switch len(cursors) {
			case 1:
				return historyPage("cur1", "300.000000"), nil
			case 2:
				return nil, &slack.RateLimitedError{RetryAfter: time.Millisecond}
			default:
				return historyPage("", "100.000000"), nil
			}
		},
	}
	client := &Client{api: mock}
	rec := &pageRecorder{}

	if err := client.WalkHistory(context.Background(), "C1", "", "", rec.visit); err != nil {
		t.Fatalf("WalkHistory: %v", err)
	}
	if got := fmt.Sprint(cursors); got != "[ cur1 cur1]" {
		t.Errorf("cursors = %s, want the rate-limited page retried", got)
	}
	if got := fmt.Sprint(rec.pages); got != "[[300.000000] [100.000000]]" {
		t.Errorf("pages = %s", got)
	}
}

func TestWalkHistory_RateLimitWaitHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mock := &mockSlackAPI{
		getConversationHistoryFn: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			cancel()
			return nil, &slack.RateLimitedError{RetryAfter: time.Hour}
		},
	}
	client := &Client{api: mock}

	err := client.WalkHistory(ctx, "C1", "", "", (&pageRecorder{}).visit)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestWalkHistory_WrapsAPIError(t *testing.T) {
	apiErr := errors.New("channel_not_found")
	mock := &mockSlackAPI{
		getConversationHistoryFn: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			return nil, apiErr
		},
	}
	client := &Client{api: mock}

	err := client.WalkHistory(context.Background(), "C1", "", "", (&pageRecorder{}).visit)
	if !errors.Is(err, apiErr) {
		t.Errorf("err = %v, want wrapped channel_not_found", err)
	}
}

func TestWalkHistory_VisitorErrorStopsTheWalk(t *testing.T) {
	calls := 0
	mock := &mockSlackAPI{
		getConversationHistoryFn: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			calls++
			return historyPage("more", "300.000000"), nil
		},
	}
	client := &Client{api: mock}
	stop := errors.New("stop")

	err := client.WalkHistory(context.Background(), "C1", "", "", (&pageRecorder{err: stop}).visit)
	if !errors.Is(err, stop) {
		t.Errorf("err = %v, want the visitor's error", err)
	}
	if calls != 1 {
		t.Errorf("API calls = %d, want 1", calls)
	}
}

func TestGetRepliesBetween_PaginatesWithBounds(t *testing.T) {
	var calls []slack.GetConversationRepliesParameters
	mock := &mockSlackAPI{
		getConversationRepliesFn: func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
			calls = append(calls, *params)
			if len(calls) == 1 {
				return []slack.Message{{Msg: slack.Msg{Timestamp: "100.000000"}}, {Msg: slack.Msg{Timestamp: "200.000000"}}}, true, "cur1", nil
			}
			return []slack.Message{{Msg: slack.Msg{Timestamp: "300.000000"}}}, false, "", nil
		},
	}
	client := &Client{api: mock}

	msgs, err := client.GetRepliesBetween(context.Background(), "C1", "100.000000", "150.000000", "350.000000")
	if err != nil {
		t.Fatalf("GetRepliesBetween: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("messages = %d, want 3 across both pages", len(msgs))
	}
	if len(calls) != 2 {
		t.Fatalf("API calls = %d, want 2", len(calls))
	}
	for i, call := range calls {
		if call.ChannelID != "C1" || call.Timestamp != "100.000000" || call.Oldest != "150.000000" || call.Latest != "350.000000" || !call.Inclusive {
			t.Errorf("call %d params = %+v", i, call)
		}
	}
	if calls[1].Cursor != "cur1" {
		t.Errorf("second call cursor = %q, want cur1", calls[1].Cursor)
	}
}

func TestGetRepliesBetween_RetriesAfterRateLimit(t *testing.T) {
	calls := 0
	mock := &mockSlackAPI{
		getConversationRepliesFn: func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
			calls++
			if calls == 1 {
				return nil, false, "", &slack.RateLimitedError{RetryAfter: time.Millisecond}
			}
			return []slack.Message{{Msg: slack.Msg{Timestamp: "200.000000"}}}, false, "", nil
		},
	}
	client := &Client{api: mock}

	msgs, err := client.GetRepliesBetween(context.Background(), "C1", "100.000000", "", "")
	if err != nil {
		t.Fatalf("GetRepliesBetween: %v", err)
	}
	if calls != 2 || len(msgs) != 1 {
		t.Errorf("calls = %d, messages = %d; want 2, 1", calls, len(msgs))
	}
}

func TestGetRepliesBetween_WrapsAPIError(t *testing.T) {
	apiErr := errors.New("thread_not_found")
	mock := &mockSlackAPI{
		getConversationRepliesFn: func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
			return nil, false, "", apiErr
		},
	}
	client := &Client{api: mock}

	if _, err := client.GetRepliesBetween(context.Background(), "C1", "100.000000", "", ""); !errors.Is(err, apiErr) {
		t.Errorf("err = %v, want wrapped thread_not_found", err)
	}
}

func TestWaitOutRateLimit(t *testing.T) {
	limited, err := WaitOutRateLimit(context.Background(), errors.New("boom"))
	if limited || err != nil {
		t.Errorf("plain error: limited = %v, err = %v; want false, nil", limited, err)
	}

	wrapped := fmt.Errorf("getting user info: %w", &slack.RateLimitedError{RetryAfter: time.Millisecond})
	limited, err = WaitOutRateLimit(context.Background(), wrapped)
	if !limited || err != nil {
		t.Errorf("wrapped rate limit: limited = %v, err = %v; want true, nil", limited, err)
	}
}
