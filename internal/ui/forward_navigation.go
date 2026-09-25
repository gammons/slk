package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/slackurl"
	"github.com/gammons/slk/internal/ui/messages"
)

// openForwardedSelected opens the original message when the selected row is
// the permalink-only message created by forwarding. It deliberately requires
// exactly one Slack archive permalink, so ordinary messages containing links
// keep their existing Enter behavior.
func (a *App) openForwardedSelected() (tea.Cmd, bool) {
	var text string
	switch a.focusedPanel {
	case PanelMessages:
		if a.view == ViewThreads {
			return nil, false
		}
		msg, ok := a.messagepane.SelectedMessage()
		if !ok {
			return nil, false
		}
		text = msg.Text
	case PanelThread:
		reply := a.threadPanel.SelectedReply()
		if reply == nil {
			return nil, false
		}
		text = reply.Text
	default:
		return nil, false
	}

	links := messages.ExtractLinks(text)
	if len(links) != 1 || strings.TrimSpace(text) != "<"+links[0].URL+">" {
		return nil, false
	}
	if _, ok := slackurl.Parse(links[0].URL); !ok {
		return nil, false
	}
	return a.routeLink(links[0].URL), true
}
