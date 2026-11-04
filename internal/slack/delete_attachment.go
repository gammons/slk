package slackclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/slackhttp"
	"github.com/slack-go/slack"
)

// AttachmentRejectedError is chat.deleteAttachment's
// {"ok":false,"error":Code}, for example "cant_delete_message".
type AttachmentRejectedError struct {
	Code string
}

func (e *AttachmentRejectedError) Error() string { return "chat.deleteAttachment: " + e.Code }

// MayHaveActed reports a code Slack documents as one it may return after
// part of the operation succeeded (internal_error, fatal_error). Any other
// refusal left the message as it was.
func (e *AttachmentRejectedError) MayHaveActed() bool {
	return e.Code == "internal_error" || e.Code == "fatal_error"
}

// DeleteAttachment removes one legacy attachment (a link preview) from a
// message of the user's own, as Slack's web client does
// (chat.deleteAttachment, undocumented). attachmentID is the
// attachment's own id; Slack renumbers the rest afterwards, so an id is
// only good against the list it was read from. Slack then sends a
// message_changed over the WebSocket, so only the error comes back.
//
// The error says how sure it is that nothing happened: Slack's refusal is
// *AttachmentRejectedError and HTTP 429 is *slack.RateLimitedError. Any
// other error (a timeout, a 5xx, a lost or unreadable answer) leaves it
// unknown whether Slack removed the preview.
//
// No retry, a 429 included: a lost answer is ambiguous, and a resend can
// hit a renumbered id. Callers must not wrap this in WaitOutRateLimit,
// which would match the *slack.RateLimitedError and resend.
func (c *Client) DeleteAttachment(ctx context.Context, channelID, ts string, attachmentID int) error {
	// The _x_reason emacs-slack sends; see the test for what is known.
	ctx = slackhttp.WithReason(ctx, "delete-single-attachment")
	form := url.Values{
		"channel":    {channelID},
		"ts":         {ts},
		"attachment": {strconv.Itoa(attachmentID)},
	}
	debuglog.General("chat.deleteAttachment: channel=%s ts=%s attachment=%d", channelID, ts, attachmentID)
	raw, err := c.postForm(ctx, "chat.deleteAttachment", form)
	if err == nil {
		err = parseDeleteAttachment(raw)
	}
	if err != nil {
		debuglog.General("chat.deleteAttachment: error=%v", err)
		var rl *rateLimitError
		if errors.As(err, &rl) {
			return &slack.RateLimitedError{RetryAfter: rl.retryAfter}
		}
		return err
	}
	debuglog.General("chat.deleteAttachment: ok")
	return nil
}

// parseDeleteAttachment reads chat.deleteAttachment's answer. ok must be
// there and a boolean: a body without it is no refusal.
func parseDeleteAttachment(raw []byte) error {
	var resp struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parsing chat.deleteAttachment: %w (body=%s)", err, truncateForLog(raw))
	}
	if resp.OK == nil {
		return fmt.Errorf("parsing chat.deleteAttachment: no ok field (body=%s)", truncateForLog(raw))
	}
	if !*resp.OK {
		return &AttachmentRejectedError{Code: resp.Error}
	}
	return nil
}
