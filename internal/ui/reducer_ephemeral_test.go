package ui

import (
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
)

func paneHasTS(a *App, ts string) bool {
	for _, m := range a.messagepane.Messages() {
		if m.TS == ts {
			return true
		}
	}
	return false
}

// An ephemeral on screen and focused is shown, but its ts must never be
// staged as a read cursor: Slack does not know it as a message, so the
// mark would land on a ts Slack never returns.
func TestEphemeralArrival_ShownButNoChannelMark(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", UserID: "USLACKBOT", Text: "not in channel", Ephemeral: true},
	})
	feed(t, app, cmd, 0)

	if !paneHasTS(app, "5.000000") {
		t.Error("the ephemeral was not appended to the pane")
	}
	if len(*calls) != 0 || app.pendingChannelMark.ts != "" {
		t.Errorf("calls = %v, pending = %q; want no channel mark", *calls, app.pendingChannelMark.ts)
	}
}

// An ephemeral reply in the open thread: shown in the panel, no thread
// mark, and not counted as a reply on the parent.
func TestEphemeralThreadReply_ShownButNotCountedOrMarked(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "1.000000", UserID: "U1", Text: "parent"}})
	openThreadPanel(app, "C1", "1.000000")

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS: "2.000000", ThreadTS: "1.000000", UserID: "USLACKBOT", Text: "not in channel", Ephemeral: true,
		},
	})
	feed(t, app, cmd, 0)

	if !hasReplyTS(app, "2.000000") {
		t.Error("the ephemeral reply did not reach the open thread panel")
	}
	if len(*calls) != 0 || app.pendingThreadMark.ts != "" {
		t.Errorf("calls = %v, pending = %q; want no thread mark", *calls, app.pendingThreadMark.ts)
	}
	if got := app.messagepane.Messages()[0].ReplyCount; got != 0 {
		t.Errorf("parent ReplyCount = %d, want 0: an ephemeral is not a reply", got)
	}
}

// The in-flight guard drops WS echoes of the user's own chat.postMessage.
// An ephemeral is never such an echo, even when it carries the user's ID.
func TestEphemeralFromSelf_ShownWhileSendInFlight(t *testing.T) {
	app, _ := markCapture(t)
	app.activeChannelID = "C1"
	app.currentUserID = "USELF"
	app.selfSend.MarkInFlight("C1")

	_, _ = app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", UserID: "USELF", Text: "preview", Ephemeral: true},
	})

	if !paneHasTS(app, "5.000000") {
		t.Error("an ephemeral from the current user was dropped by the self-send in-flight guard")
	}
}
