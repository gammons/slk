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
	// threadTS is the source's thread when it is a reply, so a permalink
	// built without chat.getPermalink (Enterprise Grid) opens the thread.
	threadTS ids.ThreadTS
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
		threadTS: a.selectedThreadTS(),
	}
	// Invalidate any search queued by a previous channel-switching session.
	// Forwarding only offers joined conversations, all available locally.
	a.pendingChannelSearchGen++
	a.channelFinder.OpenForForwarding()
	a.SetMode(ModeChannelFinder)
	return nil
}

// maxForwardStandIns bounds forwardStandIns: an echo that never arrives
// must not grow it forever.
const maxForwardStandIns = 16

// noteForwardStandIn records that ts is shown as a local stand-in for a
// forward, so the message's real echo replaces it instead of being dropped.
func (a *App) noteForwardStandIn(ts string) {
	if ts == "" {
		return
	}
	a.forwardStandIns = append(a.forwardStandIns, ts)
	if n := len(a.forwardStandIns); n > maxForwardStandIns {
		a.forwardStandIns = append([]string(nil), a.forwardStandIns[n-maxForwardStandIns:]...)
	}
}

// takeForwardStandIn reports whether ts is a pending forward stand-in, and
// forgets it.
func (a *App) takeForwardStandIn(ts string) bool {
	for i, s := range a.forwardStandIns {
		if s == ts {
			a.forwardStandIns = append(a.forwardStandIns[:i], a.forwardStandIns[i+1:]...)
			return true
		}
	}
	return false
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
		result, err := service.Forward(ctx, source.teamID, source.channelID, source.ts, source.threadTS, ids.ChannelID(destination.ID))
		if err != nil {
			return ToastMsg{Text: "Failed to forward message: " + err.Error()}
		}
		// The local stand-in until the share's own echo replaces it: the
		// source permalink in Slack's link mrkdwn (so it is clickable), or
		// plain words when the best-effort permalink was unavailable.
		item.TS, item.Text = string(result.TS), "Forwarded message"
		if result.Text != "" {
			item.Text = "<" + result.Text + ">"
		}
		return messageForwardedMsg{
			teamID: source.teamID, channelID: destination.ID, destination: name, message: item,
		}
	}
}
