package ui

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// emojiHistoryApp returns an App whose emoji history is kept in
// memory, most recent first, and the history itself.
func emojiHistoryApp(t *testing.T, opts ...testOpt) (*App, *[]string) {
	t.Helper()
	a := newTestApp(t, opts...)
	var history []string
	a.SetReactionService(core.NewReactionService(
		func(ids.ChannelID, ids.MessageTS, string) error { return nil },
		func(ids.ChannelID, ids.MessageTS, string) error { return nil },
		func(limit int) []core.EmojiEntry {
			var out []core.EmojiEntry
			for _, n := range history {
				if len(out) == limit {
					break
				}
				out = append(out, core.EmojiEntry{Name: n})
			}
			return out
		},
		func(name string) {
			history = append([]string{name}, slices.DeleteFunc(history, func(n string) bool { return n == name })...)
		},
	))
	return a, &history
}

// autocompleteRows is what the channel composer's emoji autocomplete
// shows after typing s, one name per row in order.
func autocompleteRows(t *testing.T, a *App, s string) []string {
	t.Helper()
	a.SetMode(ModeInsert)
	_ = a.compose.Focus()
	typeInCompose(a, s)
	view := stripANSI(a.compose.EmojiPickerView(80))
	if view == "" {
		t.Fatalf("no emoji autocomplete after typing %q", s)
	}
	var rows []string
	for _, line := range strings.Split(view, "\n") {
		fields := strings.Fields(strings.Trim(line, "│╭╮╰╯─▌ "))
		if len(fields) > 0 {
			rows = append(rows, strings.Trim(fields[len(fields)-1], ":"))
		}
	}
	return rows
}

func TestSentMessageEmojiLeadTheAutocomplete(t *testing.T) {
	a, history := emojiHistoryApp(t, withActiveChannel("C1"))
	if rows := autocompleteRows(t, a, ":ro"); len(rows) == 0 || rows[0] == "rose" {
		t.Fatalf("precondition: rows = %v, want an alphabetical list not led by rose", rows)
	}
	a.compose.SetValue("")
	a.compose.CloseEmoji()

	a.Update(SendMessageMsg{ChannelID: "C1", Text: "for you :rose: :thumbsup: 12:30:45"})

	if want := []string{"+1", "rose"}; !reflect.DeepEqual(*history, want) {
		t.Errorf("history = %v, want %v (canonical names, no :30:)", *history, want)
	}
	if rows := autocompleteRows(t, a, ":ro"); len(rows) == 0 || rows[0] != "rose" {
		t.Errorf("autocomplete rows = %v, want rose first", rows)
	}
}

func TestSentThreadReplyEmojiAreRecorded(t *testing.T) {
	a, history := emojiHistoryApp(t, withActiveChannel("C1"))
	a.Update(SendThreadReplyMsg{ChannelID: "C1", ThreadTS: "1.0", Text: "done :tada:"})
	if want := []string{"tada"}; !reflect.DeepEqual(*history, want) {
		t.Errorf("history = %v, want %v", *history, want)
	}
}

func TestTogglingAPillOnRecordsTheEmoji(t *testing.T) {
	a, history := emojiHistoryApp(t, withActiveChannel("C1"))
	a.SetCurrentUserID("U1")
	msg := messages.MessageItem{TS: "1.0", Reactions: []messages.ReactionItem{
		{Emoji: "eyes", Count: 1, UserIDs: []string{"U2"}},
		{Emoji: "tada", Count: 1, UserIDs: []string{"U1"}, HasReacted: true},
	}}

	_ = a.toggleReactionOnMessageItem("C1", msg, "eyes")
	if want := []string{"eyes"}; !reflect.DeepEqual(*history, want) {
		t.Errorf("after adding eyes, history = %v, want %v", *history, want)
	}
	// Taking a reaction back is not a use.
	_ = a.toggleReactionOnMessageItem("C1", msg, "tada")
	if want := []string{"eyes"}; !reflect.DeepEqual(*history, want) {
		t.Errorf("after removing tada, history = %v, want %v", *history, want)
	}
}
