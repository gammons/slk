package slackclient

// Starred conversations: the Slack Web API's stars.add / stars.remove,
// the calls behind the web client's "Star channel" / "Star
// conversation". A star is per-conversation (no timestamp pins the
// whole conversation); the starred set comes back to slk through the
// section endpoints (SectionStore) and stars.list, so no dedicated
// list call lives here.

import (
	"context"
	"net/url"
)

// StarsAdd stars channelID's conversation.
func (c *Client) StarsAdd(ctx context.Context, channelID string) error {
	return c.starCall(ctx, "stars.add", channelID)
}

// StarsRemove un-stars channelID's conversation.
func (c *Client) StarsRemove(ctx context.Context, channelID string) error {
	return c.starCall(ctx, "stars.remove", channelID)
}

// starCall posts one stars.* method with the conversation as the
// channel parameter. Routed through postForm so HTTP status, 429 and
// Slack's {"ok":false} envelope are all surfaced as errors — the UI's
// optimistic star move must be reverted for a star Slack rejected.
func (c *Client) starCall(ctx context.Context, method, channelID string) error {
	raw, err := c.postForm(ctx, method, url.Values{"channel": {channelID}})
	if err != nil {
		return err
	}
	return parseOKResponse(method, raw)
}
