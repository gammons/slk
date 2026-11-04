package main

import (
	"context"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/slack/boot"
	"github.com/gammons/slk/internal/slackhttp"
	"github.com/gammons/slk/internal/ui"
)

// infoReason is the web client's _x_reason for its app-home
// conversations.info request; slackhttp's table has no entry for it.
const infoReason = "app-homes-store/conditional-fetching"

// probeMessagingCapability decides whether an app DM accepts a message.
// conversations.info (the web client's own app-home request) answers
// first; when it fails or carries no app_home, conversations.view is
// tried. If both fail it fails open (CanSend=true): the read-only mark
// set after a refused send is the backstop.
func probeMessagingCapability(ctx context.Context, post boot.PostFunc, chID string) ui.ChannelMessagingCapabilityMsg {
	home, err := boot.ConversationsInfo(slackhttp.WithReason(ctx, infoReason), post, chID)
	if err != nil {
		debuglog.General("compose: conversations.info probe failed channel=%s err=%q", chID, err)
	}
	if home != nil {
		return capabilityResult(chID, "conversations.info", home)
	}

	view, err := boot.ConversationsView(ctx, post, chID)
	if err != nil {
		debuglog.General("compose: conversations.view probe failed channel=%s err=%q (fail-open)", chID, err)
		return ui.ChannelMessagingCapabilityMsg{ChannelID: chID, CanSend: true}
	}
	// Slack may ignore the channel param and answer for the last-viewed
	// conversation; that verdict is not about this channel.
	if view.Channel.ID != chID {
		debuglog.General("compose: conversations.view answered for another channel channel=%s got=%s (fail-open)", chID, view.Channel.ID)
		return ui.ChannelMessagingCapabilityMsg{ChannelID: chID, CanSend: true}
	}
	return capabilityResult(chID, "conversations.view", view.AppHome)
}

func capabilityResult(chID, source string, home *boot.AppHome) ui.ChannelMessagingCapabilityMsg {
	canSend := home.CanReceiveMessages()
	if home == nil {
		debuglog.General("compose: probe channel=%s source=%s app_home=absent CanSend=%v", chID, source, canSend)
	} else {
		debuglog.General("compose: probe channel=%s source=%s messages_tab_enabled=%v messages_tab_read_only_enabled=%v CanSend=%v",
			chID, source, home.MessagesTabEnabled, home.MessagesTabReadOnlyEnabled, canSend)
	}
	return ui.ChannelMessagingCapabilityMsg{ChannelID: chID, CanSend: canSend}
}
