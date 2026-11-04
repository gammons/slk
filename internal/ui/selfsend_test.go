package ui

import (
	"testing"
	"time"

	"github.com/gammons/slk/internal/ui/messages"
)

// clockedSelfSend is a selfSendDedup whose clock the test moves.
func clockedSelfSend() (*selfSendDedup, *time.Time) {
	d := newSelfSendDedup()
	now := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { return now }
	return d, &now
}

func TestSelfSendDedup_MarkInFlightLastsTheWindow(t *testing.T) {
	d, now := clockedSelfSend()
	d.MarkInFlight("C1")
	*now = now.Add(selfSendWindow - time.Millisecond)
	if !d.InFlight("C1") {
		t.Error("not in flight inside the window")
	}
	*now = now.Add(time.Millisecond)
	if d.InFlight("C1") {
		t.Error("still in flight after the window")
	}
}

// A send that is still waiting (link chip lookups delay chat.postMessage)
// keeps its channel in flight past the window until it answers, and no
// longer: after that, the same user's messages from another client show.
func TestSelfSendDedup_PendingSendOutlastsTheWindowUntilItAnswers(t *testing.T) {
	d, now := clockedSelfSend()
	d.BeginSend("C1")
	*now = now.Add(selfSendWindow + time.Second)
	if !d.InFlight("C1") {
		t.Error("pending send not in flight past the window")
	}
	if d.InFlight("C2") {
		t.Error("another channel is in flight")
	}
	d.EndSend("C1")
	if d.InFlight("C1") {
		t.Error("still in flight after the send answered")
	}
}

func TestSelfSendDedup_TwoPendingSendsNeedTwoAnswers(t *testing.T) {
	d, now := clockedSelfSend()
	d.BeginSend("C1")
	d.BeginSend("C1")
	*now = now.Add(selfSendWindow + time.Second)
	d.EndSend("C1")
	if !d.InFlight("C1") {
		t.Error("one answer ended both sends")
	}
	d.EndSend("C1")
	d.EndSend("C1") // an answer with no send (tests fire them directly)
	if d.InFlight("C1") {
		t.Error("in flight with no send pending")
	}
}

// A send whose answer never comes stops hiding the channel's messages.
func TestSelfSendDedup_PendingSendIsBounded(t *testing.T) {
	d, now := clockedSelfSend()
	d.BeginSend("C1")
	*now = now.Add(selfSendPendingLimit)
	if d.InFlight("C1") {
		t.Error("an unanswered send hides self-user messages past the limit")
	}
}

// Through Update: a self-user echo 4s after a send that has not answered
// is still dropped; once it answers, the user's message from another
// client in that channel shows.
func TestSelfSendInFlight_SlowSendKeepsSuppressingUntilAnswered(t *testing.T) {
	app := NewApp()
	app.SetCurrentUserID("USELF")
	app.activeChannelID = "C1"
	now := time.Now()
	app.selfSend.now = func() time.Time { return now }

	app.Update(SendMessageMsg{ChannelID: "C1", Text: "see https://linear.app/x"})
	now = now.Add(4 * time.Second)
	app.Update(NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{
		TS: "1700000999.000001", UserID: "USELF", Text: "see <https://linear.app/x|X>",
	}})
	if got := len(app.messagepane.Messages()); got != 1 {
		t.Fatalf("echo of a pending send rendered; %d messages, want the placeholder only", got)
	}
	localTS := app.messagepane.Messages()[0].TS
	app.Update(MessageSentMsg{ChannelID: "C1", LocalTS: localTS, Message: messages.MessageItem{
		TS: "1700000999.000001", UserID: "USELF", Text: "see <https://linear.app/x|X>",
	}})
	app.Update(NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{
		TS: "1700001000.000001", UserID: "USELF", Text: "from my phone",
	}})
	got := app.messagepane.Messages()
	if len(got) != 2 || got[1].Text != "from my phone" {
		t.Errorf("messages = %+v, want the sent message and the phone's", got)
	}
}
