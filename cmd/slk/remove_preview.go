package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
	"github.com/slack-go/slack"
)

// removeAttachmentTimeout bounds one chat.deleteAttachment call.
const removeAttachmentTimeout = 10 * time.Second

// attachmentRemover is the part of *slackclient.Client removing a link
// preview needs.
type attachmentRemover interface {
	DeleteAttachment(ctx context.Context, channelID, ts string, attachmentID int) error
}

// removeAttachment removes attachment attachmentID (a link preview) from
// a message through client, the client of workspace teamID, and reports
// the result for the UI. client is nil when that workspace is gone: the
// user may have switched or signed out since pressing the key.
//
// An error that says the preview was certainly not removed becomes
// *core.AttachmentRefusedError, so the UI may let the user try again; any
// other error is passed on as it is, and the UI keeps the removal pending.
func removeAttachment(ctx context.Context, client attachmentRemover, teamID string,
	channelID ids.ChannelID, ts ids.MessageTS, attachmentID int) ui.AttachmentRemovedMsg {
	chIDStr, tsStr := string(channelID), string(ts)
	if client == nil {
		log.Printf("Warning: not removing preview %s/%s #%d: workspace %q is unavailable", chIDStr, tsStr, attachmentID, teamID)
		return ui.AttachmentRemovedMsg{ChannelID: chIDStr, TS: tsStr, Err: &core.AttachmentRefusedError{Reason: "workspace unavailable"}}
	}
	ctx, cancel := context.WithTimeout(ctx, removeAttachmentTimeout)
	defer cancel()
	// One attempt, never WaitOutRateLimit: see DeleteAttachment.
	err := client.DeleteAttachment(ctx, chIDStr, tsStr, attachmentID)
	if err != nil {
		log.Printf("Warning: failed to remove preview %s/%s #%d: %v", chIDStr, tsStr, attachmentID, err)
	}
	return ui.AttachmentRemovedMsg{ChannelID: chIDStr, TS: tsStr, Err: refusedRemoval(err)}
}

// refusedRemoval translates a DeleteAttachment error that says nothing was
// done (Slack's refusal, other than one Slack says may follow a partial
// success, or a rate limit) into *core.AttachmentRefusedError, and returns
// any other error unchanged.
func refusedRemoval(err error) error {
	var rej *slackclient.AttachmentRejectedError
	if errors.As(err, &rej) && !rej.MayHaveActed() {
		reason := rej.Code
		if reason == "" {
			reason = "refused by Slack"
		}
		return &core.AttachmentRefusedError{Reason: reason}
	}
	var rl *slack.RateLimitedError
	if errors.As(err, &rl) {
		return &core.AttachmentRefusedError{Reason: "rate limited"}
	}
	return err
}
