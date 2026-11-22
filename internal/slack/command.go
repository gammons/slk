package slackclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/slack/mrkdwn"
	"github.com/gammons/slk/internal/slackhttp"
)

// CommandRequest is one run of a slash command, as Slack's web client
// sends it to chat.command (undocumented). Captures:
// docs/superpowers/specs/2026-10-12-slash-commands-captures/chat_command_*.json.
type CommandRequest struct {
	ChannelID string
	// ThreadTS runs the command in a thread; "" for the conversation.
	ThreadTS string
	// Command is the name with its slash, lowercased: "/giphy".
	Command string
	// Args is the text after the name in wire form (<@U…>, <#C…|name>);
	// "" or whitespace for none.
	Args string
	// TeamID is the workspace. "" omits team_id, which is what the web
	// client sends for a command it does not know.
	TeamID string
}

// CommandRejectedError is chat.command's {"ok":false,"error":Code}, for
// example "unknown_command". Adapters translate it to core.CommandError.
type CommandRejectedError struct {
	Code string
}

func (e *CommandRejectedError) Error() string { return "chat.command: " + e.Code }

// RunCommand runs a slash command as Slack's web client does. What the
// command produces (a message, an ephemeral, a modal) arrives over the
// WebSocket, so only the error comes back.
func (c *Client) RunCommand(ctx context.Context, req CommandRequest) error {
	// The _x_reason the web client sends on chat.command, recorded in
	// docs/superpowers/specs/2026-10-12-slash-commands-captures/chat_command_*.json.
	// Not in slackhttp's defaultReasons: that table is pinned to an older capture set.
	ctx = slackhttp.WithReason(ctx, "executeCommand")
	form, err := commandForm(req, newClientToken())
	if err != nil {
		return err
	}
	debuglog.General("chat.command: command=%s channel=%s thread_ts=%q has_args=%v",
		req.Command, req.ChannelID, req.ThreadTS, form.Has("blocks"))
	raw, err := c.postForm(ctx, "chat.command", form)
	if err != nil {
		debuglog.General("chat.command: error=%v", err)
		return err
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parsing chat.command: %w (body=%s)", err, truncateForLog(raw))
	}
	if !resp.OK {
		debuglog.General("chat.command: rejected error=%s", resp.Error)
		return &CommandRejectedError{Code: resp.Error}
	}
	debuglog.General("chat.command: ok")
	return nil
}

// commandForm builds chat.command's business fields (postForm adds the
// token, slackhttp the _x_* envelope). blocks carries the arguments as
// rich text, the same converter chat.postMessage uses, so mentions become
// typed elements; it is omitted when there are no arguments.
func commandForm(req CommandRequest, clientToken string) (url.Values, error) {
	form := url.Values{
		"command":      {req.Command},
		"disp":         {req.Command},
		"channel":      {req.ChannelID},
		"client_token": {clientToken},
	}
	if strings.TrimSpace(req.Args) != "" {
		block := mrkdwn.Convert(req.Args)
		if block != nil {
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			if err := enc.Encode([]slack.Block{block}); err != nil {
				return nil, fmt.Errorf("encoding chat.command blocks: %w", err)
			}
			form.Set("blocks", strings.TrimSuffix(buf.String(), "\n"))
		}
	}
	if req.ThreadTS != "" {
		form.Set("thread_ts", req.ThreadTS)
	}
	if req.TeamID != "" {
		form.Set("team_id", req.TeamID)
	}
	return form, nil
}

// newClientToken is the web client's client_token: "web-" and the unix
// time in milliseconds. The only generator of it in slk.
func newClientToken() string {
	return "web-" + strconv.FormatInt(time.Now().UnixMilli(), 10)
}
