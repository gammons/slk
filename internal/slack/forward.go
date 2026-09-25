package slackclient

import (
	"context"
	"fmt"
	"net/url"

	"github.com/slack-go/slack"
)

// ForwardMessage posts a permalink with Slack's native message preview rather
// than copying the body through SendMessage's rich-text conversion. It returns
// the destination message's actual timestamp and the source permalink on success.
func (c *Client) ForwardMessage(ctx context.Context, sourceChannelID, ts, destinationChannelID string) (postedTS string, permalink string, err error) {
	permalink, err = c.GetPermalink(ctx, sourceChannelID, ts)
	if err != nil {
		return "", "", fmt.Errorf("forwarding message: %w", err)
	}
	if permalink == "" {
		return "", "", fmt.Errorf("forwarding message: empty permalink")
	}

	_, postedTS, err = c.api.PostMessageContext(ctx, destinationChannelID,
		slack.MsgOptionText(permalink, false),
		// slack-go has no enable-media-unfurl option. Set both flags
		// explicitly, then restore the SDK's configured post endpoint
		// (including enterprise workspace URLs) with MsgOptionPost.
		slack.UnsafeMsgOptionEndpoint("", func(values url.Values) {
			values.Set("unfurl_links", "true")
			values.Set("unfurl_media", "true")
		}),
		slack.MsgOptionPost(),
	)
	if err != nil {
		return "", "", fmt.Errorf("forwarding message: posting permalink: %w", err)
	}
	if postedTS == "" {
		return "", "", fmt.Errorf("forwarding message: posting permalink: empty message timestamp")
	}
	return postedTS, permalink, nil
}
