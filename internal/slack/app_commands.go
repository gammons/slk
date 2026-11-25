package slackclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/slackhttp"
)

// AppCommands fetches the workspace's slash commands (client.appCommands).
// app_actions (global shortcuts) are ignored. The slice is the caller's;
// a failed response returns an error and no commands.
func (c *Client) AppCommands(ctx context.Context) ([]core.SlashCommand, error) {
	// The _x_reason the web client sends, recorded in
	// docs/superpowers/specs/2026-10-12-slash-commands-captures/client_app_commands.json.
	// Not in slackhttp's defaultReasons: that table is pinned to an older capture set.
	ctx = slackhttp.WithReason(ctx, "dispatch-client-extras")
	raw, err := c.postForm(ctx, "client.appCommands", nil)
	if err != nil {
		debuglog.General("client.appCommands: error=%v", err)
		return nil, err
	}
	cmds, err := parseAppCommands(raw)
	if err != nil {
		debuglog.General("client.appCommands: error=%v", err)
		return nil, err
	}
	debuglog.General("client.appCommands: count=%d", len(cmds))
	return cmds, nil
}

// parseAppCommands reads a client.appCommands response. commands[] become
// SlashCommand values; app_actions is left unread.
func parseAppCommands(raw []byte) ([]core.SlashCommand, error) {
	var resp struct {
		OK       bool   `json:"ok"`
		Error    string `json:"error"`
		Commands []struct {
			Name    string `json:"name"`
			Desc    string `json:"desc"`
			Usage   string `json:"usage"`
			AppName string `json:"app_name"`
			AliasOf string `json:"alias_of"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parsing client.appCommands: %w (body=%s)", err, truncateForLog(raw))
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "not ok"
		}
		return nil, fmt.Errorf("client.appCommands: %s", resp.Error)
	}
	out := make([]core.SlashCommand, 0, len(resp.Commands))
	for _, c := range resp.Commands {
		name := c.Name
		if name != "" && !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		out = append(out, core.SlashCommand{
			Name:    name,
			Usage:   c.Usage,
			Desc:    c.Desc,
			AppName: c.AppName,
			AliasOf: c.AliasOf,
		})
	}
	return out, nil
}
