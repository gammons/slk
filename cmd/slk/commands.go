package main

import (
	"context"
	"errors"
	"strings"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	slackclient "github.com/gammons/slk/internal/slack"
)

// commandRunner is the part of *slackclient.Client running a slash
// command needs.
type commandRunner interface {
	RunCommand(ctx context.Context, req slackclient.CommandRequest) error
	TeamID() string
}

// runCommand runs a slash command through client and translates Slack's
// refusal into core.CommandError, so the UI never sees slackclient types.
// team_id is sent only when command is in listed, the workspace's
// client.appCommands result. An unknown command, or a list that never
// loaded, omits it — that is what the web client sends for a command
// it does not know.
func runCommand(ctx context.Context, client commandRunner, channelID ids.ChannelID,
	threadTS ids.ThreadTS, command, args string, listed []core.SlashCommand) error {
	teamID := ""
	if slashCommandListed(listed, command) {
		teamID = client.TeamID()
	}
	err := client.RunCommand(ctx, slackclient.CommandRequest{
		ChannelID: string(channelID),
		ThreadTS:  string(threadTS),
		Command:   command,
		Args:      args,
		TeamID:    teamID,
	})
	var rej *slackclient.CommandRejectedError
	if errors.As(err, &rej) {
		return &core.CommandError{Code: rej.Code}
	}
	return err
}

func slashCommandListed(listed []core.SlashCommand, command string) bool {
	for _, c := range listed {
		if strings.EqualFold(c.Name, command) {
			return true
		}
	}
	return false
}
