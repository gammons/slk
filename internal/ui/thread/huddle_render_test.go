package thread

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/ui/messages"
)

// The thread parent of a huddle with replies is drawn as its starter's
// message, even while the parent's own ReplyCount lags the loaded replies.
func TestThreadParentHuddleShowsStarter(t *testing.T) {
	m := New()
	m.SetUserNames(map[string]string{"UFAKE0001": "Alice"})
	parent := messages.MessageItem{
		TS: "1700000000.000100", UserID: "USLACKBOT", UserName: "Slackbot", Timestamp: "10:30 AM",
		Subtype: "huddle_thread", Text: "A huddle started",
		HuddleRoom: &messages.HuddleRoom{CreatedBy: "UFAKE0001", DateStart: 1700000000, DateEnd: 1700001380, HasEnded: true},
	}
	m.SetThread(parent, []messages.MessageItem{{
		TS: "1700000100.000100", UserID: "UFAKE0002", UserName: "Bob", Timestamp: "10:32 AM", Text: "notes",
	}}, "C1", parent.TS)
	out := ansi.Strip(m.View(20, 70))
	if !strings.Contains(out, "Alice") || !strings.Contains(out, "Started a huddle · lasted 23 min") {
		t.Errorf("parent should be a message by the starter:\n%s", out)
	}
	if strings.Contains(out, "Slackbot") {
		t.Errorf("parent still shows the system bot:\n%s", out)
	}
}

// With no replies the parent stays a centred notice naming the starter.
func TestThreadParentHuddleNoRepliesIsNotice(t *testing.T) {
	m := New()
	m.SetUserNames(map[string]string{"UFAKE0001": "Alice"})
	parent := messages.MessageItem{
		TS: "1700000000.000100", UserID: "USLACKBOT", Timestamp: "10:30 AM", Subtype: "huddle_thread",
		HuddleRoom: &messages.HuddleRoom{CreatedBy: "UFAKE0001", DateStart: 1700000000},
	}
	m.SetThread(parent, nil, "C1", parent.TS)
	out := ansi.Strip(m.View(20, 70))
	if !strings.Contains(out, "Alice started a huddle") {
		t.Errorf("notice text missing:\n%s", out)
	}
}
