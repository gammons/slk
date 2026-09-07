package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/reactionpicker"
)

// reactionCalls records what the injected ReactionService was asked to
// do. Add/Remove are only reached when the handler's tea.Cmd is run.
type reactionCalls struct {
	added   []string
	removed []string
	frecent []string
}

// errReactionFailed is returned by the fake Add/Remove so the
// ReactionSentMsg's Err field is provably wired.
var errReactionFailed = errors.New("boom")

// reactedMessage builds the one message the picker targets. reacted
// controls whether the current user already has the "tada" reaction,
// which is what flips the picker's add/remove behaviour.
func reactedMessage(reacted bool) []messages.MessageItem {
	uids := []string{"U9"}
	if reacted {
		uids = append(uids, reactionTestUserID)
	}
	return []messages.MessageItem{{
		TS:        "1.0",
		UserID:    "U9",
		UserName:  "bob",
		Text:      "msg-1",
		Timestamp: "1:00 PM",
		Reactions: []messages.ReactionItem{{
			Emoji:      "tada",
			Count:      len(uids),
			UserIDs:    uids,
			HasReacted: reacted,
		}},
	}}
}

const reactionTestUserID = "UME"

// reactionPickerOpts puts one reacted-to message in the active channel
// so updateReactionOnMessage has a real target to mutate.
func reactionPickerOpts(reacted bool) []testOpt {
	return []testOpt{
		withActiveChannel("C1"),
		withMessages(reactedMessage(reacted)...),
	}
}

// openReactionPicker wires a recording ReactionService whose frecent
// list is the deterministic two-entry fixture, then drives the
// production open path (App.openPickerFromMessage).
func openReactionPicker(calls *reactionCalls, frecent ...string) func(*testing.T, *App) {
	return func(t *testing.T, a *App) {
		*calls = reactionCalls{}
		entries := make([]reactionpicker.EmojiEntry, 0, len(frecent))
		for _, n := range frecent {
			entries = append(entries, reactionpicker.EmojiEntry{Name: n})
		}
		a.SetCurrentUserID(reactionTestUserID)
		a.SetReactionService(NewReactionService(
			func(_ ids.ChannelID, _ ids.MessageTS, emoji string) error {
				calls.added = append(calls.added, emoji)
				return errReactionFailed
			},
			func(_ ids.ChannelID, _ ids.MessageTS, emoji string) error {
				calls.removed = append(calls.removed, emoji)
				return errReactionFailed
			},
			func(int) []reactionpicker.EmojiEntry { return entries },
			func(emoji string) { calls.frecent = append(calls.frecent, emoji) },
		))
		a.openPickerFromMessage()
		if !a.reactionPicker.IsVisible() {
			t.Fatal("precondition: openPickerFromMessage did not open the picker")
		}
		if got := a.reactionPicker.ChannelID(); got != "C1" {
			t.Fatalf("precondition: picker ChannelID = %q, want %q", got, "C1")
		}
		if got := a.reactionPicker.MessageTS(); got != "1.0" {
			t.Fatalf("precondition: picker MessageTS = %q, want %q", got, "1.0")
		}
		// BoxSize floors its row count at 1, so an empty frecent list
		// still reports one row.
		wantRows := len(frecent)
		if wantRows < 1 {
			wantRows = 1
		}
		if got := modalRows(a.reactionPicker); got != wantRows {
			t.Fatalf("precondition: %d rows, want %d for %d frecent entries", got, wantRows, len(frecent))
		}
	}
}

// reactionOn returns the message's reaction for emoji, or false.
func reactionOn(a *App, emoji string) (messages.ReactionItem, bool) {
	msgs := a.messagepane.Messages()
	if len(msgs) == 0 {
		return messages.ReactionItem{}, false
	}
	for _, r := range msgs[0].Reactions {
		if r.Emoji == emoji {
			return r, true
		}
	}
	return messages.ReactionItem{}, false
}

// TestReactionPickerModeKeys characterizes handleReactionPickerMode
// (mode_reaction_picker.go:25), the largest handler in this group.
func TestReactionPickerModeKeys(t *testing.T) {
	var calls reactionCalls
	open := openReactionPicker(&calls, "tada", "rocket")

	runKeyCases(t, ModeReactionPicker, []keyCase{
		{
			name:     "esc closes the picker and touches nothing",
			opts:     reactionPickerOpts(false),
			setup:    open,
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.reactionPicker.IsVisible() {
					t.Error("picker still visible after esc")
				}
				if len(calls.frecent) != 0 || len(calls.added) != 0 {
					t.Errorf("esc touched the reaction service: %+v", calls)
				}
				r, ok := reactionOn(a, "tada")
				if !ok || r.Count != 1 {
					t.Errorf("tada reaction = %+v (ok=%v), want the original count 1", r, ok)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name:     "enter adds the highlighted emoji optimistically and fires the API call",
			opts:     reactionPickerOpts(false),
			setup:    open,
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.reactionPicker.IsVisible() {
					t.Error("picker still visible after enter")
				}
				if len(calls.frecent) != 1 || calls.frecent[0] != "tada" {
					t.Errorf("RecordFrecent calls = %v, want [tada]", calls.frecent)
				}
				r, ok := reactionOn(a, "tada")
				if !ok {
					t.Fatal("tada reaction disappeared from the message")
				}
				if !r.HasReacted || r.Count != 2 {
					t.Errorf("tada reaction = %+v, want HasReacted with count 2", r)
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want the reactions.Add cmd")
				}
				msg, ok := cmd().(ReactionSentMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want ReactionSentMsg", cmd())
				}
				if msg.ChannelID != "C1" || msg.MessageTS != "1.0" {
					t.Errorf("ReactionSentMsg target = %q/%q, want C1/1.0", msg.ChannelID, msg.MessageTS)
				}
				if msg.Emoji != "tada" || msg.Remove || msg.UserID != reactionTestUserID {
					t.Errorf("ReactionSentMsg = %+v, want tada/add/%s", msg, reactionTestUserID)
				}
				if !errors.Is(msg.Err, errReactionFailed) {
					t.Errorf("ReactionSentMsg.Err = %v, want the service error threaded through", msg.Err)
				}
				if len(calls.added) != 1 || calls.added[0] != "tada" {
					t.Errorf("Add calls = %v, want [tada]", calls.added)
				}
			},
		},
		{
			// reactionpicker.isExistingReaction flips the result to a
			// removal, and the handler skips RecordFrecent for those.
			name:     "enter on an already-applied reaction removes it and skips frecency",
			opts:     reactionPickerOpts(true),
			setup:    open,
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if len(calls.frecent) != 0 {
					t.Errorf("RecordFrecent calls = %v, want none on a removal", calls.frecent)
				}
				r, ok := reactionOn(a, "tada")
				if !ok {
					t.Fatal("tada reaction disappeared entirely; U9 should still hold it")
				}
				if r.HasReacted || r.Count != 1 {
					t.Errorf("tada reaction = %+v, want count 1 without HasReacted", r)
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want the reactions.Remove cmd")
				}
				msg, ok := cmd().(ReactionSentMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want ReactionSentMsg", cmd())
				}
				if !msg.Remove {
					t.Error("ReactionSentMsg.Remove = false, want true")
				}
				if len(calls.removed) != 1 || calls.removed[0] != "tada" {
					t.Errorf("Remove calls = %v, want [tada]", calls.removed)
				}
				if len(calls.added) != 0 {
					t.Errorf("Add calls = %v, want none", calls.added)
				}
			},
		},
		{
			// The picker offers iamcal aliases; the handler stores the
			// name Slack records. "thumbsup" is an alias of "+1".
			name:     "the chosen emoji is canonicalised to its Slack name",
			opts:     reactionPickerOpts(false),
			setup:    openReactionPicker(&calls, "thumbsup"),
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if len(calls.frecent) != 1 || calls.frecent[0] != "+1" {
					t.Errorf("RecordFrecent calls = %v, want [+1]", calls.frecent)
				}
				if _, ok := reactionOn(a, "+1"); !ok {
					t.Error("message did not gain a \"+1\" reaction")
				}
				if _, ok := reactionOn(a, "thumbsup"); ok {
					t.Error("message gained the alias \"thumbsup\" instead of the canonical name")
				}
				if cmd == nil {
					t.Fatal("cmd = nil")
				}
				if msg, ok := cmd().(ReactionSentMsg); !ok || msg.Emoji != "+1" {
					t.Errorf("ReactionSentMsg = %#v, want Emoji \"+1\"", cmd())
				}
			},
		},
		{
			// displayedList() is the frecent list while the query is
			// empty, so a picker opened with no frecency has nothing
			// to commit and enter falls through the result==nil arm.
			name:     "enter with an empty list is a no-op",
			opts:     reactionPickerOpts(false),
			setup:    openReactionPicker(&calls),
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeReactionPicker,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if !a.reactionPicker.IsVisible() {
					t.Error("picker closed on a no-op enter")
				}
				if len(calls.frecent) != 0 || len(calls.added) != 0 {
					t.Errorf("a no-op enter touched the reaction service: %+v", calls)
				}
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
			},
		},
		{
			name:     "down moves the cursor to the next frecent emoji",
			opts:     reactionPickerOpts(false),
			setup:    open,
			key:      keyCode(tea.KeyDown),
			wantMode: ModeReactionPicker,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				assertReactionCommitsTo(t, a, &calls, "rocket")
			},
		},
		{
			// Pins the normalisation switch at the top of the handler.
			// Key.String() prefixes active modifiers before the
			// special-key name (ultraviolet key.go:413-431, 459), so
			// shift+down arrives as "shift+down" and
			// reactionpicker.HandleKey ignores it; `case tea.KeyDown`
			// rewrites it to "down". No unmodified row can distinguish
			// the arm from a no-op.
			name:     "shift+down navigates: the Code switch strips the modifier",
			opts:     reactionPickerOpts(false),
			setup:    open,
			key:      keyMod(tea.KeyDown, tea.ModShift),
			wantMode: ModeReactionPicker,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				assertReactionCommitsTo(t, a, &calls, "rocket")
			},
		},
		{
			name: "up moves the cursor back",
			opts: reactionPickerOpts(false),
			setup: func(t *testing.T, a *App) {
				open(t, a)
				_ = dispatchModeKey(a, keyCode(tea.KeyDown))
			},
			key:      keyCode(tea.KeyUp),
			wantMode: ModeReactionPicker,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				assertReactionCommitsTo(t, a, &calls, "tada")
			},
		},
		{
			// BUG?: reactionpicker.HandleKey's "up" arm is the only
			// navigation arm that does not consult displayedList, so
			// pressing up first is a clamp to 0 either way. Recorded
			// because the asymmetry with "down" is easy to break.
			//
			// The setup moves DOWN off the boundary and back UP with
			// the same key under test, so this row separates "clamped
			// at the top" from "ignored entirely": an ignored up
			// leaves the cursor on "rocket" and the commit probe says
			// so. The intermediate position cannot be asserted —
			// reactionpicker exposes neither its query nor its
			// selection, and the only probe (commit an enter) closes
			// the picker.
			name: "up at the top clamps rather than wrapping",
			opts: reactionPickerOpts(false),
			setup: func(t *testing.T, a *App) {
				open(t, a)
				_ = dispatchModeKey(a, keyCode(tea.KeyDown))
				_ = dispatchModeKey(a, keyCode(tea.KeyUp))
			},
			key:      keyCode(tea.KeyUp),
			wantMode: ModeReactionPicker,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				assertReactionCommitsTo(t, a, &calls, "tada")
			},
		},
		{
			// A non-empty query switches displayedList from the
			// frecent list to a filter over the whole emoji table,
			// which fills the picker's 10-row window.
			name:     "a printable key filters the full emoji list",
			opts:     reactionPickerOpts(false),
			setup:    open,
			key:      keyPress('t'),
			wantMode: ModeReactionPicker,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalRows(a.reactionPicker); got != 10 {
					t.Errorf("rows = %d, want the full 10-row window after filtering", got)
				}
				// BUG?: reactionpicker.filter (reactionpicker/model.go:242-265)
				// means to rank prefix matches ahead of substring ones,
				// but it breaks out of the scan at 50 accumulated
				// candidates and allEmoji is sorted alphabetically, so
				// for a late-alphabet query the substring matches from
				// "a..." exhaust the budget before any "t..." prefix
				// match is even visited. The tier-0-first ordering is
				// therefore unreachable for such queries.
				//
				// Both halves of this assertion are the pin. Contains
				// records that the query filters at all; the NEGATIVE
				// HasPrefix records the bug — fix the 50-cap and row 0
				// becomes a "t..." prefix match, and this row fails and
				// says so. A Contains-only assertion would survive the
				// fix silently, which is the one thing a BUG?: marker
				// must not do.
				saveCommitted(t, a, &calls, func(name string) {
					if !strings.Contains(name, "t") {
						t.Errorf("committed %q, want a name matching the query \"t\"", name)
					}
					if strings.HasPrefix(name, "t") {
						t.Errorf("committed %q, a prefix match: the 50-candidate cap in "+
							"reactionpicker.filter appears to be fixed, so tier-0-first "+
							"ranking now works. Re-characterize this row (and drop the BUG?).", name)
					}
				})
			},
		},
		{
			name: "backspace clears the query and restores the frecent list",
			opts: reactionPickerOpts(false),
			setup: func(t *testing.T, a *App) {
				open(t, a)
				_ = dispatchModeKey(a, keyPress('t'))
				if got := modalRows(a.reactionPicker); got != 10 {
					t.Fatalf("precondition: rows = %d, want 10", got)
				}
			},
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeReactionPicker,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalRows(a.reactionPicker); got != 2 {
					t.Errorf("rows = %d, want the 2 frecent entries back", got)
				}
			},
		},
		{
			name:     "backspace on an empty query is inert",
			opts:     reactionPickerOpts(false),
			setup:    open,
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeReactionPicker,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalRows(a.reactionPicker); got != 2 {
					t.Errorf("rows = %d, want 2", got)
				}
			},
		},
		{
			name:     "an unhandled modified key changes nothing",
			opts:     reactionPickerOpts(false),
			setup:    open,
			key:      keyMod('x', tea.ModCtrl),
			wantMode: ModeReactionPicker,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := modalRows(a.reactionPicker); got != 2 {
					t.Errorf("rows = %d, want 2: ctrl+x should not have filtered", got)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
				assertReactionCommitsTo(t, a, &calls, "tada")
			},
		},
		{
			// The handler's IsVisible check runs before the result
			// check, so a key that arrives with the picker already
			// closed exits to Normal without consulting the result.
			name: "key with the picker closed falls through to Normal",
			opts: reactionPickerOpts(false),
			setup: func(t *testing.T, a *App) {
				calls = reactionCalls{}
				if a.reactionPicker.IsVisible() {
					t.Fatal("precondition: picker should start hidden")
				}
			},
			key:      keyPress('x'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
				if len(calls.frecent) != 0 {
					t.Errorf("RecordFrecent calls = %v, want none", calls.frecent)
				}
			},
		},
	})
}

// assertReactionCommitsTo presses enter and asserts which emoji the
// cursor was on, which is how this table observes cursor position.
func assertReactionCommitsTo(t *testing.T, a *App, calls *reactionCalls, want string) {
	t.Helper()
	saveCommitted(t, a, calls, func(got string) {
		if got != want {
			t.Errorf("cursor committed %q, want %q", got, want)
		}
	})
}

// saveCommitted presses enter and hands the committed emoji name to
// check. It fails the test when nothing was committed, so a probe
// whose precondition collapsed cannot pass vacuously.
func saveCommitted(t *testing.T, a *App, calls *reactionCalls, check func(string)) {
	t.Helper()
	calls.frecent = nil
	_ = dispatchModeKey(a, keyCode(tea.KeyEnter))
	if len(calls.frecent) != 1 {
		t.Fatalf("enter recorded %d frecent emoji, want 1; the cursor probe proves nothing", len(calls.frecent))
	}
	check(calls.frecent[0])
}
