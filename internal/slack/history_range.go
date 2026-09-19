// This file holds the bulk, time-bounded history reads behind
// `slk export`: a paged walk over conversations.history and a bounded
// conversations.replies fetch. Unlike the TUI's fetches they cover an
// arbitrarily large span, so both wait out rate limits instead of
// failing on them.

package slackclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/slack-go/slack"
)

// historyPageSize is the conversations.history page size for bulk
// walks, matching GetHistorySince.
const historyPageSize = 200

// defaultRateLimitWait is how long to back off when Slack rate-limits
// a call without saying for how long.
const defaultRateLimitWait = 30 * time.Second

// WaitOutRateLimit reports whether err is a Slack rate-limit error
// and, if so, sleeps for the advised interval first. A false return
// means err is some other failure the caller should surface. The sleep
// is cut short, returning ctx's error, when ctx is cancelled.
func WaitOutRateLimit(ctx context.Context, err error) (bool, error) {
	var rlErr *slack.RateLimitedError
	if !errors.As(err, &rlErr) {
		return false, nil
	}
	wait := rlErr.RetryAfter
	if wait == 0 {
		wait = defaultRateLimitWait
	}
	select {
	case <-ctx.Done():
		return true, ctx.Err()
	case <-time.After(wait):
		return true, nil
	}
}

// WalkHistory pages through conversations.history between oldest and
// latest and hands each page to visit as Slack delivered it
// (newest-first, newest page first). Either bound may be "" to leave
// that side open; both are inclusive, so callers needing a half-open
// window filter the boundary themselves. Pages are streamed rather than
// accumulated because the span can be a channel's entire history. A
// non-nil error from visit stops the walk and is returned as-is.
func (c *Client) WalkHistory(ctx context.Context, channelID, oldest, latest string, visit func([]slack.Message) error) error {
	cursor := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		resp, err := c.api.GetConversationHistory(&slack.GetConversationHistoryParameters{
			ChannelID: channelID,
			Oldest:    oldest,
			Latest:    latest,
			Inclusive: true,
			Limit:     historyPageSize,
			Cursor:    cursor,
		})
		if err != nil {
			limited, waitErr := WaitOutRateLimit(ctx, err)
			if waitErr != nil {
				return waitErr
			}
			if limited {
				continue
			}
			return fmt.Errorf("walking history of %s: %w", channelID, err)
		}
		if err := visit(resp.Messages); err != nil {
			return err
		}
		if !resp.HasMore || resp.ResponseMetaData.NextCursor == "" {
			return nil
		}
		cursor = resp.ResponseMetaData.NextCursor
	}
}

// GetRepliesBetween retrieves the messages of a thread whose
// timestamps lie between oldest and latest (both inclusive; either may
// be "" to leave that side open). Whether the thread parent comes back
// when it lies outside the bounds, and whether it is repeated per page,
// is Slack's choice and undocumented: callers must filter by timestamp
// rather than assume the parent appears exactly once, or at all.
func (c *Client) GetRepliesBetween(ctx context.Context, channelID, threadTS, oldest, latest string) ([]slack.Message, error) {
	var all []slack.Message
	cursor := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		msgs, hasMore, nextCursor, err := c.api.GetConversationReplies(&slack.GetConversationRepliesParameters{
			ChannelID: channelID,
			Timestamp: threadTS,
			Oldest:    oldest,
			Latest:    latest,
			Inclusive: true,
			Cursor:    cursor,
		})
		if err != nil {
			limited, waitErr := WaitOutRateLimit(ctx, err)
			if waitErr != nil {
				return nil, waitErr
			}
			if limited {
				continue
			}
			return nil, fmt.Errorf("getting replies of %s between %q and %q: %w", threadTS, oldest, latest, err)
		}
		all = append(all, msgs...)
		if !hasMore || nextCursor == "" {
			return all, nil
		}
		cursor = nextCursor
	}
}
