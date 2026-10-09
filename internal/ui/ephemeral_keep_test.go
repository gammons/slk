package ui

import (
	"fmt"
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
)

func tsList(items []messages.MessageItem) string {
	var out []string
	for _, m := range items {
		out = append(out, m.TS)
	}
	return fmt.Sprint(out)
}

func TestKeepEphemerals(t *testing.T) {
	eph := func(ts string) messages.MessageItem { return messages.MessageItem{TS: ts, Ephemeral: true} }
	msg := func(ts string) messages.MessageItem { return messages.MessageItem{TS: ts} }
	cases := []struct {
		name           string
		fetched, shown []messages.MessageItem
		want           string
	}{
		{"none shown", []messages.MessageItem{msg("1.0"), msg("2.0")}, []messages.MessageItem{msg("1.0")}, "[1.0 2.0]"},
		{"merged in ts order", []messages.MessageItem{msg("1.0"), msg("3.0")}, []messages.MessageItem{msg("1.0"), eph("2.0"), eph("4.0")}, "[1.0 2.0 3.0 4.0]"},
		{"empty fetch keeps them", []messages.MessageItem{}, []messages.MessageItem{eph("2.0")}, "[2.0]"},
		{"never duplicated", []messages.MessageItem{msg("2.0")}, []messages.MessageItem{eph("2.0")}, "[2.0]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tsList(keepEphemerals(tc.fetched, tc.shown)); got != tc.want {
				t.Errorf("keepEphemerals = %s, want %s", got, tc.want)
			}
		})
	}
}

// A private message that arrives while the channel on screen is being
// refetched (e.g. the reconnect refresh) must survive the fetch: Slack
// never returns it, so replacing the list would erase it unseen.
func TestEphemeralSurvivesChannelRefetch(t *testing.T) {
	app := newTestApp(t, withActiveChannel("C1"), withMessages(messages.MessageItem{TS: "1.000000", UserID: "U1"}))
	app.Update(NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{
		TS: "2.000000", UserID: "USLACKBOT", Ephemeral: true,
	}})
	app.Update(MessagesLoadedMsg{ChannelID: "C1", Messages: []messages.MessageItem{
		{TS: "1.000000", UserID: "U1"}, {TS: "3.000000", UserID: "U2"},
	}})

	if got, want := tsList(app.messagepane.Messages()), "[1.000000 2.000000 3.000000]"; got != want {
		t.Errorf("pane = %s, want %s", got, want)
	}
}

func TestEphemeralReplySurvivesThreadFetch(t *testing.T) {
	app := newTestApp(t, withActiveChannel("C1"))
	openThreadPanel(app, "C1", "100.000000")
	app.Update(NewMessageMsg{ChannelID: "C1", Message: messages.MessageItem{
		TS: "102.000000", ThreadTS: "100.000000", UserID: "USLACKBOT", Ephemeral: true,
	}})
	app.Update(ThreadRepliesLoadedMsg{ThreadTS: "100.000000", Replies: []messages.MessageItem{
		{TS: "101.000000", ThreadTS: "100.000000", UserID: "U2"},
	}})

	if got, want := tsList(app.threadPanel.Replies()), "[101.000000 102.000000]"; got != want {
		t.Errorf("replies = %s, want %s", got, want)
	}
}
