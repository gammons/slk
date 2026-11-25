package main

import (
	"context"

	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ids"
	slackclient "github.com/gammons/slk/internal/slack"
)

// attachmentActioner is the part of *slackclient.Client a button press
// needs.
type attachmentActioner interface {
	AttachmentAction(ctx context.Context, req slackclient.AttachmentActionRequest) error
}

// pressAttachmentAction presses a legacy attachment button through client.
func pressAttachmentAction(ctx context.Context, client attachmentActioner, channelID ids.ChannelID,
	messageTS ids.MessageTS, attachmentID int, callbackID, serviceID string, action blocks.LegacyAction, ephemeral bool) error {
	return client.AttachmentAction(ctx, slackclient.AttachmentActionRequest{
		ChannelID:    string(channelID),
		MessageTS:    string(messageTS),
		AttachmentID: attachmentID,
		CallbackID:   callbackID,
		ServiceID:    serviceID,
		Ephemeral:    ephemeral,
		Action:       action,
	})
}

// blockActioner is the part of *slackclient.Client a Block Kit button
// press needs.
type blockActioner interface {
	BlockAction(ctx context.Context, req slackclient.BlockActionRequest) error
}

// pressBlockAction presses a Block Kit button on a message through
// client. No retry: a press can have side effects on the app's side.
func pressBlockAction(ctx context.Context, client blockActioner, channelID ids.ChannelID,
	messageTS ids.MessageTS, serviceID, blockID string, action blocks.ActionElement) error {
	req := slackclient.BlockActionRequest{
		ChannelID: string(channelID),
		MessageTS: string(messageTS),
		ServiceID: serviceID,
		ActionID:  action.ActionID,
		BlockID:   blockID,
		Label:     action.Label,
		Value:     action.Value,
		Style:     action.Style,
	}
	if c := action.Confirm; c != nil {
		req.Confirm = &slackclient.BlockActionConfirm{
			Title:       c.Title,
			Text:        c.Text,
			OKText:      c.OKText,
			DismissText: c.DismissText,
		}
	}
	return client.BlockAction(ctx, req)
}
