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

// A mark staged while the terminal was blurred is issued when focus
// returns, on the grounds that the user is now looking at the pane.
// If the layout changed meanwhile (a resize stacked the panes, say)
// and the pane is no longer drawn, the user has still not seen it: the
// mark is dropped and the dot shows.
func TestCatchUpFlushSkipsAPaneNoLongerDrawn(t *testing.T) {
	tests := []struct {
		name        string
		setup       []tea.Msg // at 200 cols, before the blur
		threadReply bool
		whileAway   tea.Msg // the layout change while blurred, or nil
		wantMark    bool
	}{
		{
			name:      "channel stacked behind the thread",
			setup:     []tea.Msg{keyCode(tea.KeyEnter)}, // thread open and focused
			whileAway: tea.WindowSizeMsg{Width: 120, Height: 30},
		},
		{
			name:        "thread stacked behind the channel",
			setup:       []tea.Msg{keyCode(tea.KeyEnter), keyMod(tea.KeyTab, tea.ModShift)},
			threadReply: true,
			whileAway:   tea.WindowSizeMsg{Width: 120, Height: 30},
		},
		{
			name:     "layout unchanged: still marked on return",
			setup:    []tea.Msg{keyCode(tea.KeyEnter)},
			wantMark: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := stackedApp(t, 200)
			calls := wireMarkCapture(t, a)
			for _, msg := range tt.setup {
				updateAndRender(t, a, msg)
			}
			updateAndRender(t, a, tea.BlurMsg{})

			msg := NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{TS: "99.000000", UserID: "U2"}}
			if tt.threadReply {
				msg.ChannelID = a.threadPanel.ChannelID()
				msg.Message.ThreadTS = a.threadPanel.ThreadTS()
			}
			_, cmd := a.Update(msg)
			feed(t, a, cmd, 0)
			if len(*calls) != 0 {
				t.Fatalf("marks while blurred = %v, want none", *calls)
			}
			if tt.whileAway != nil {
				updateAndRender(t, a, tt.whileAway)
			}

			var reports int
			a.setStatusReporterForTest(func(int, int, string, string) { reports++ })
			_, cmd = a.Update(tea.FocusMsg{})
			feed(t, a, cmd, 0)

			if tt.wantMark {
				if len(*calls) != 1 {
					t.Fatalf("marks on return = %v, want one", *calls)
				}
				return
			}
			if len(*calls) != 0 {
				t.Fatalf("marks on return = %v, want none: the pane is no longer drawn", *calls)
			}
			if reports == 0 {
				t.Error("read state not re-read, so the unread dot does not show")
			}
		})
	}
}

// The debounce tick is not gated on the layout: a mark it carries was
// staged for an arrival the user saw (focused, pane drawn). Hiding the
// pane before the tick fires (reading a message, then f on the sidebar)
// must not leave that message unread.
func TestDebounceFlushStillMarksAfterThePaneIsHidden(t *testing.T) {
	a := stackedApp(t, 200)
	calls := wireMarkCapture(t, a)

	_, tick := a.Update(NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{TS: "99.000000", UserID: "U2"}})
	updateAndRender(t, a, keyMod(tea.KeyTab, tea.ModShift)) // channel → sidebar
	updateAndRender(t, a, keyPress('f'))
	assertFullscreen(t, a, PanelSidebar)

	feed(t, a, tick, 0)
	if len(*calls) != 1 {
		t.Fatalf("marks = %v, want one: the user saw the message before hiding the pane", *calls)
	}
}
