package ui

import (
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
)

// A WS thread reply must land in the open thread panel based on the
// PANEL's identity (its channel + thread ts), not the active channel.
// Regression: threads opened from the Threads view (or left open while
// focusing another channel) never received live replies — they only
// appeared after reopening the thread forced a refetch.

// hasReplyTS scans the panel's replies directly — Model.HasReply reads
// a lazy index built during View(), which never runs in these tests.
func hasReplyTS(a *App, ts string) bool {
	for _, r := range a.threadPanel.Replies() {
		if r.TS == ts {
			return true
		}
	}
	return false
}

func openThreadPanel(a *App, channelID, threadTS string) {
	parent := messages.MessageItem{TS: threadTS, Text: "parent", UserID: "U1"}
	a.threadPanel.SetThread(parent, nil, channelID, threadTS)
	a.threadVisible = true
}

func TestThreadReplyRoutedWhenOtherChannelActive(t *testing.T) {
	a := NewApp()
	a.activeChannelID = "C_ACTIVE"
	openThreadPanel(a, "C_THREAD", "100.0")

	reduceNewMessage(a, NewMessageMsg{
		ChannelID: "C_THREAD",
		Message: messages.MessageItem{
			TS:       "101.0",
			ThreadTS: "100.0",
			UserID:   "U2",
			Text:     "live reply",
		},
	})

	if !hasReplyTS(a, "101.0") {
		t.Errorf("thread panel should receive live reply for its own thread; replies=%+v",
			a.threadPanel.Replies())
	}
}

func TestThreadReplyStillRoutedForActiveChannel(t *testing.T) {
	a := NewApp()
	a.activeChannelID = "C1"
	openThreadPanel(a, "C1", "100.0")

	reduceNewMessage(a, NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "101.0", ThreadTS: "100.0", UserID: "U2", Text: "reply"},
	})

	if !hasReplyTS(a, "101.0") {
		t.Error("active-channel thread reply routing regressed")
	}
}

func TestThreadReplyForDifferentThreadNotRouted(t *testing.T) {
	a := NewApp()
	a.activeChannelID = "C_ACTIVE"
	openThreadPanel(a, "C_THREAD", "100.0")

	reduceNewMessage(a, NewMessageMsg{
		ChannelID: "C_THREAD",
		Message:   messages.MessageItem{TS: "201.0", ThreadTS: "200.0", UserID: "U2", Text: "other thread"},
	})

	if hasReplyTS(a, "201.0") {
		t.Error("reply for a different thread must not land in the open panel")
	}
}

// A closed thread panel keeps its last channel+thread identity, so the
// visibility check must gate routing too.
func TestThreadReplyNotRoutedWhenPanelHidden(t *testing.T) {
	a := NewApp()
	a.activeChannelID = "C_ACTIVE"
	openThreadPanel(a, "C_THREAD", "100.0")
	a.threadVisible = false

	reduceNewMessage(a, NewMessageMsg{
		ChannelID: "C_THREAD",
		Message:   messages.MessageItem{TS: "101.0", ThreadTS: "100.0", UserID: "U2", Text: "reply"},
	})

	if hasReplyTS(a, "101.0") {
		t.Error("hidden thread panel must not receive replies")
	}
}
