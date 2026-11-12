package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

// A message lands in a conversation that is open (the active channel,
// or the thread in the open panel) but not drawn: full screen shows
// another pane, or a narrow terminal stacks the thread over the
// channel. The user has not seen it, so it must stay unread: no read
// mark goes to Slack and the read state is re-read so the dot shows.
// Where the pane is drawn the arrival is still marked read.
func TestArrivalInAHiddenPaneStaysUnread(t *testing.T) {
	type arrival int
	const (
		channelMessage arrival = iota
		threadReply
	)
	tests := []struct {
		name     string
		width    int
		setup    []tea.Msg
		arrival  arrival
		wantMark bool
	}{
		{
			name: "full-screen sidebar hides the channel", width: 200,
			setup:   []tea.Msg{keyMod(tea.KeyTab, tea.ModShift), keyPress('f')},
			arrival: channelMessage,
		},
		{
			name: "stacked thread in front hides the channel", width: 120,
			setup:   []tea.Msg{keyCode(tea.KeyEnter)},
			arrival: channelMessage,
		},
		{
			name: "full-screen channel hides the thread", width: 200,
			setup:   []tea.Msg{keyCode(tea.KeyEnter), keyMod(tea.KeyTab, tea.ModShift), keyPress('f')},
			arrival: threadReply,
		},
		{
			name: "full-screen channel shows the channel", width: 200,
			setup:   []tea.Msg{keyPress('f')},
			arrival: channelMessage, wantMark: true,
		},
		{
			name: "full-screen thread shows the thread", width: 200,
			setup:   []tea.Msg{keyCode(tea.KeyEnter), keyPress('f')},
			arrival: threadReply, wantMark: true,
		},
		{
			name: "side by side, sidebar focused, shows the channel", width: 200,
			setup:   []tea.Msg{keyCode(tea.KeyEnter), keyCode(tea.KeyTab)},
			arrival: channelMessage, wantMark: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := stackedApp(t, tt.width)
			calls := wireMarkCapture(t, a)
			for _, msg := range tt.setup {
				updateAndRender(t, a, msg)
			}
			var reports int
			a.setStatusReporterForTest(func(int, int, string, string) { reports++ })

			msg := NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{TS: "99.000000", UserID: "U2"}}
			if tt.arrival == threadReply {
				if !a.threadVisible {
					t.Fatal("precondition: no thread open")
				}
				msg.ChannelID = a.threadPanel.ChannelID()
				msg.Message.ThreadTS = a.threadPanel.ThreadTS()
			}
			_, cmd := a.Update(msg)
			feed(t, a, cmd, 0)

			if tt.wantMark {
				if len(*calls) != 1 {
					t.Fatalf("marks = %v, want one: the pane is drawn", *calls)
				}
				return
			}
			if len(*calls) != 0 {
				t.Fatalf("marks = %v, want none: the pane is not drawn", *calls)
			}
			if reports == 0 {
				t.Error("read state not re-read, so the unread dot does not show")
			}
		})
	}
}
