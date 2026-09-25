package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

func TestEnterOnForwardedMessageNavigatesToOriginal(t *testing.T) {
	a, _ := linkTestApp(t)
	a.activeChannelID = "C054JFCBN69"
	a.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "<https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139>"},
		{TS: "2.0", Text: "newer"},
	})
	a.messagepane.SelectByIndex(0)
	a.focusedPanel = PanelMessages
	var fetchedChannel, fetchedTS string
	setChannelFetchAroundForTest(a, func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
		fetchedChannel, fetchedTS = string(channelID), string(ts)
		return nil
	})

	cmd := dispatchModeKey(a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected FetchAround command for the original message")
	}
	cmd()
	if fetchedChannel != "C054JFCBN69" || fetchedTS != "1779284733.270139" {
		t.Fatalf("fetched = %q/%q; want original permalink target", fetchedChannel, fetchedTS)
	}
	if a.threadVisible {
		t.Fatal("Enter opened a thread instead of navigating to the original")
	}
}

func TestEnterOnOrdinaryLinkStillOpensThread(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C1"), withMessages(messages.MessageItem{
		TS: "1.0", Text: "see <https://example.com>"},
	))
	a.focusedPanel = PanelMessages
	if cmd := dispatchModeKey(a, tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		cmd()
	}
	if !a.threadVisible {
		t.Fatal("ordinary link message no longer opens its thread")
	}
}
