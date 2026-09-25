package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/messages"
)

type forwardSource struct {
	teamID    string
	channelID ids.ChannelID
	ts        ids.MessageTS
}

type messageForwardedMsg struct {
	teamID      string
	channelID   string
	destination string
	message     messages.MessageItem
}

func (a *App) beginForwardOfSelected() tea.Cmd {
	if a.view == ViewThreads && a.focusedPanel == PanelMessages {
		return nil // The Threads list is not the hidden channel message pane.
	}
	channelID, ts, _, _, _, ok := a.selectedMessageContext()
	if !ok || channelID == "" || ts == "" || a.activeTeamID == "" {
		return nil
	}
	a.pendingForward = &forwardSource{
		teamID: a.activeTeamID, channelID: ids.ChannelID(channelID), ts: ids.MessageTS(ts),
	}
	// Invalidate any search queued by a previous channel-switching session.
	// Forwarding only offers joined conversations, all available locally.
	a.pendingChannelSearchGen++
	a.channelFinder.OpenForForwarding()
	a.SetMode(ModeChannelFinder)
	return nil
}

func (a *App) forwardToChannel(destination channelfinder.ChannelResult) tea.Cmd {
	source := *a.pendingForward
	a.SetMode(ModeNormal)
	if source.teamID != a.activeTeamID {
		return func() tea.Msg { return ToastMsg{Text: "Forward cancelled: workspace changed"} }
	}
	service := a.messageSvc
	item := messages.MessageItem{
		UserID: a.currentUserID, UserName: a.userNameFor(a.currentUserID), Timestamp: a.nowFormatted(),
	}
	name := destination.Name
	if destination.Type == "channel" || destination.Type == "private" {
		name = "#" + name
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := service.Forward(ctx, source.teamID, source.channelID, source.ts, ids.ChannelID(destination.ID))
		if err != nil {
			return ToastMsg{Text: "Failed to forward message: " + err.Error()}
		}
		// Match Slack's link mrkdwn so the local fallback is clickable too.
		item.TS, item.Text = string(result.TS), "<"+result.Text+">"
		return messageForwardedMsg{
			teamID: source.teamID, channelID: destination.ID, destination: name, message: item,
		}
	}
}
