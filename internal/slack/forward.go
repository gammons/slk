package slackclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/google/uuid"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/slackhttp"
)

// ForwardMessage shares a message into another conversation the way the web
// client's Forward does: chat.shareMessage (undocumented), so the recipient
// gets a real shared message rather than a pasted link. It returns the
// destination message's timestamp and, best effort, the source permalink the
// UI uses for its local echo (empty when it could not be fetched; the share
// has already happened by then, so that is not an error).
func (c *Client) ForwardMessage(ctx context.Context, sourceChannelID, ts, destinationChannelID string) (postedTS string, permalink string, err error) {
	// The _x_reason the web client sends on chat.shareMessage.
	shareCtx := slackhttp.WithReason(ctx, "sendShareMessage")
	raw, err := c.postForm(shareCtx, "chat.shareMessage", shareMessageForm(sourceChannelID, ts, destinationChannelID, uuid.NewString()))
	if err != nil {
		return "", "", fmt.Errorf("forwarding message: %w", err)
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		TS    string `json:"ts"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", "", fmt.Errorf("forwarding message: parsing chat.shareMessage: %w (body=%s)", err, truncateForLog(raw))
	}
	if !resp.OK {
		return "", "", fmt.Errorf("forwarding message: chat.shareMessage: %s", resp.Error)
	}
	if resp.TS == "" {
		return "", "", fmt.Errorf("forwarding message: chat.shareMessage: empty message timestamp")
	}
	permalink, perr := c.GetPermalink(ctx, sourceChannelID, ts)
	if perr != nil {
		debuglog.General("chat.shareMessage: permalink for local echo unavailable: %v", perr)
		permalink = ""
	}
	return resp.TS, permalink, nil
}

// shareMessageForm builds chat.shareMessage's business fields (postForm adds
// the token, slackhttp the _x_* envelope), as the web client sends them with
// no comment typed. Capture: testdata/chat_share_message.json.
func shareMessageForm(sourceChannelID, ts, destinationChannelID, clientMsgID string) url.Values {
	return url.Values{
		"timestamp":             {ts},
		"channel":               {sourceChannelID},
		"share_channel":         {destinationChannelID},
		"client_msg_id":         {clientMsgID},
		"text":                  {""},
		"pending_slug_urls":     {"[]"},
		"skip_dlp_user_warning": {"false"},
	}
}
