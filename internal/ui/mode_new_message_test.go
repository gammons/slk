package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/newmessagepicker"
)

// openedConversationMsg is what the injected OpenConversation returns,
// so running the handler's tea.Cmd proves which recipients and which
// request ID were captured.
type openedConversationMsg struct {
	userIDs []string
	reqID   uint64
}

// newMessageUsers is the picker's fixture. Recency is left at zero on
// all three so the empty-query order is the documented DisplayName ASC
// tiebreak: alice, bob, carol.
func newMessageUsers() []newmessagepicker.User {
	return []newmessagepicker.User{
		{ID: "U1", DisplayName: "alice", Username: "alice"},
		{ID: "U2", DisplayName: "bob", Username: "bob"},
		{ID: "U3", DisplayName: "carol", Username: "carol"},
	}
}

func newMessageOpts() []testOpt {
	return []testOpt{withChannelService(ChannelServiceFuncs{
		OpenConversation: func(userIDs []string, requestID uint64) tea.Cmd {
			ids := append([]string(nil), userIDs...)
			return func() tea.Msg {
				return openedConversationMsg{userIDs: ids, reqID: requestID}
			}
		},
	})}
}

func openNewMessagePicker(t *testing.T, a *App) {
	a.newMessagePicker.SetUsers(newMessageUsers())
	a.newMessagePicker.Open()
	if !a.newMessagePicker.IsVisible() {
		t.Fatal("precondition: new-message picker did not open")
	}
	if got := modalHighlightedRow(t, a.newMessagePicker.View(120)); !strings.Contains(got, "alice") {
		t.Fatalf("precondition: highlighted row = %q, want it to contain %q", got, "alice")
	}
	if got := newMessagePillCount(a); got != "0 / 8" {
		t.Fatalf("precondition: pill counter = %q, want %q", got, "0 / 8")
	}
}

// newMessagePillCount reads the modal's footer counter ("N / 8"), the
// only externally visible record of the pill-bar selection — the
// picker keeps its selected set unexported and offers no getter.
func newMessagePillCount(a *App) string {
	view := stripANSI(a.newMessagePicker.View(120))
	for n := 0; n <= newmessagepicker.MaxRecipients; n++ {
		want := fmt.Sprintf("%d / %d", n, newmessagepicker.MaxRecipients)
		if strings.Contains(view, want) {
			return want
		}
	}
	return ""
}

// TestNewMessageModeKeys characterizes handleNewMessageMode
// (mode_new_message.go:15).
func TestNewMessageModeKeys(t *testing.T) {
	runKeyCases(t, ModeNewMessage, []keyCase{
		{
			// The picker closes itself on esc; the handler notices via
			// IsVisible. newMessageCancelled stays false here because
			// newMessageInFlightID is still 0 — there is nothing in
			// flight to cancel.
			name:     "esc closes the picker and returns to Normal",
			opts:     newMessageOpts(),
			setup:    openNewMessagePicker,
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.newMessagePicker.IsVisible() {
					t.Error("picker still visible after esc")
				}
				if a.newMessageCancelled {
					t.Error("newMessageCancelled = true with nothing in flight")
				}
				if a.newMessageInFlightID != 0 {
					t.Errorf("newMessageInFlightID = %d, want 0", a.newMessageInFlightID)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name: "esc after a submit marks the in-flight request cancelled",
			opts: newMessageOpts(),
			setup: func(t *testing.T, a *App) {
				openNewMessagePicker(t, a)
				if cmd := dispatchModeKey(a, keyCode(tea.KeyEnter)); cmd == nil {
					t.Fatal("precondition: enter did not dispatch a submit")
				}
				if a.newMessageInFlightID != 1 {
					t.Fatalf("precondition: newMessageInFlightID = %d, want 1", a.newMessageInFlightID)
				}
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.newMessageCancelled {
					t.Error("newMessageCancelled = false, want true after esc with a submit in flight")
				}
			},
		},
		{
			// The handler does NOT close the picker or leave the mode
			// on submit; that happens later, when the reducer folds in
			// NewMessageOpenedMsg (reducer_new_message.go).
			name:     "enter submits the highlighted user and stays in the mode",
			opts:     newMessageOpts(),
			setup:    openNewMessagePicker,
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNewMessage,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if !a.newMessagePicker.IsVisible() {
					t.Error("picker closed on submit; the handler should leave it open")
				}
				if a.newMessageInFlightID != 1 {
					t.Errorf("newMessageInFlightID = %d, want 1", a.newMessageInFlightID)
				}
				if a.newMessageCancelled {
					t.Error("newMessageCancelled = true, want false on a fresh submit")
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want the OpenConversation cmd")
				}
				msg, ok := cmd().(openedConversationMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want openedConversationMsg", cmd())
				}
				if len(msg.userIDs) != 1 || msg.userIDs[0] != "U1" {
					t.Errorf("userIDs = %v, want [U1]", msg.userIDs)
				}
				if msg.reqID != 1 {
					t.Errorf("requestID = %d, want 1", msg.reqID)
				}
			},
		},
		{
			name: "enter with pills submits the pill set instead of the highlight",
			opts: newMessageOpts(),
			setup: func(t *testing.T, a *App) {
				openNewMessagePicker(t, a)
				// Space toggles alice, then down+space toggles bob.
				_ = dispatchModeKey(a, keyCode(tea.KeySpace))
				_ = dispatchModeKey(a, keyCode(tea.KeyDown))
				_ = dispatchModeKey(a, keyCode(tea.KeySpace))
				if got := newMessagePillCount(a); got != "2 / 8" {
					t.Fatalf("precondition: pill counter = %q, want %q", got, "2 / 8")
				}
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNewMessage,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want the OpenConversation cmd")
				}
				msg, ok := cmd().(openedConversationMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want openedConversationMsg", cmd())
				}
				if len(msg.userIDs) != 2 || msg.userIDs[0] != "U1" || msg.userIDs[1] != "U2" {
					t.Errorf("userIDs = %v, want [U1 U2]", msg.userIDs)
				}
			},
		},
		{
			name: "enter with no matching users is a no-op",
			opts: newMessageOpts(),
			setup: func(t *testing.T, a *App) {
				openNewMessagePicker(t, a)
				for _, r := range "zzz" {
					_ = dispatchModeKey(a, keyPress(r))
				}
				if !strings.Contains(stripANSI(a.newMessagePicker.View(120)), "No users match") {
					t.Fatal("precondition: query \"zzz\" should have matched nothing")
				}
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNewMessage,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
				if a.newMessageInFlightID != 0 {
					t.Errorf("newMessageInFlightID = %d, want 0", a.newMessageInFlightID)
				}
				if !a.newMessagePicker.IsVisible() {
					t.Error("picker closed on a no-match enter")
				}
			},
		},
		{
			name:     "down moves the highlight",
			opts:     newMessageOpts(),
			setup:    openNewMessagePicker,
			key:      keyCode(tea.KeyDown),
			wantMode: ModeNewMessage,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalHighlightedRow(t, a.newMessagePicker.View(120)); !strings.Contains(got, "bob") {
					t.Errorf("highlighted row = %q, want it to contain %q", got, "bob")
				}
			},
		},
		{
			name:     "ctrl+n moves the highlight like down",
			opts:     newMessageOpts(),
			setup:    openNewMessagePicker,
			key:      keyMod('n', tea.ModCtrl),
			wantMode: ModeNewMessage,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalHighlightedRow(t, a.newMessagePicker.View(120)); !strings.Contains(got, "bob") {
					t.Errorf("highlighted row = %q, want it to contain %q", got, "bob")
				}
			},
		},
		{
			name: "up moves the highlight back",
			opts: newMessageOpts(),
			setup: func(t *testing.T, a *App) {
				openNewMessagePicker(t, a)
				_ = dispatchModeKey(a, keyCode(tea.KeyDown))
				if got := modalHighlightedRow(t, a.newMessagePicker.View(120)); !strings.Contains(got, "bob") {
					t.Fatalf("precondition: highlighted row = %q, want %q", got, "bob")
				}
			},
			key:      keyCode(tea.KeyUp),
			wantMode: ModeNewMessage,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalHighlightedRow(t, a.newMessagePicker.View(120)); !strings.Contains(got, "alice") {
					t.Errorf("highlighted row = %q, want it to contain %q", got, "alice")
				}
			},
		},
		{
			name:     "ctrl+p at the top clamps rather than wrapping",
			opts:     newMessageOpts(),
			setup:    openNewMessagePicker,
			key:      keyMod('p', tea.ModCtrl),
			wantMode: ModeNewMessage,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalHighlightedRow(t, a.newMessagePicker.View(120)); !strings.Contains(got, "alice") {
					t.Errorf("highlighted row = %q, want it to contain %q", got, "alice")
				}
			},
		},
		{
			// mode_new_message.go is the only handler in this group
			// that normalises tea.KeySpace, and it has to: Key.String()
			// answers "space", while the picker's toggle arm matches
			// the single-space string " ".
			name:     "space toggles the highlighted user into the pill bar",
			opts:     newMessageOpts(),
			setup:    openNewMessagePicker,
			key:      keyCode(tea.KeySpace),
			wantMode: ModeNewMessage,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := newMessagePillCount(a); got != "1 / 8" {
					t.Errorf("pill counter = %q, want %q", got, "1 / 8")
				}
			},
		},
		{
			name:     "tab toggles like space",
			opts:     newMessageOpts(),
			setup:    openNewMessagePicker,
			key:      keyCode(tea.KeyTab),
			wantMode: ModeNewMessage,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := newMessagePillCount(a); got != "1 / 8" {
					t.Errorf("pill counter = %q, want %q", got, "1 / 8")
				}
			},
		},
		{
			name: "space on an already-selected user removes the pill",
			opts: newMessageOpts(),
			setup: func(t *testing.T, a *App) {
				openNewMessagePicker(t, a)
				_ = dispatchModeKey(a, keyCode(tea.KeySpace))
				if got := newMessagePillCount(a); got != "1 / 8" {
					t.Fatalf("precondition: pill counter = %q, want %q", got, "1 / 8")
				}
			},
			key:      keyCode(tea.KeySpace),
			wantMode: ModeNewMessage,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := newMessagePillCount(a); got != "0 / 8" {
					t.Errorf("pill counter = %q, want %q", got, "0 / 8")
				}
			},
		},
		{
			name:     "a printable key filters the list",
			opts:     newMessageOpts(),
			setup:    openNewMessagePicker,
			key:      keyPress('b'),
			wantMode: ModeNewMessage,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := modalHighlightedRow(t, a.newMessagePicker.View(120)); !strings.Contains(got, "bob") {
					t.Errorf("highlighted row = %q, want it to contain %q", got, "bob")
				}
				if got := stripANSI(a.newMessagePicker.View(120)); strings.Contains(got, "carol") {
					t.Error("carol survived the \"b\" filter")
				}
			},
		},
		{
			name: "backspace deletes the last query rune",
			opts: newMessageOpts(),
			setup: func(t *testing.T, a *App) {
				openNewMessagePicker(t, a)
				_ = dispatchModeKey(a, keyPress('b'))
				if strings.Contains(stripANSI(a.newMessagePicker.View(120)), "carol") {
					t.Fatal("precondition: \"b\" should have filtered carol out")
				}
			},
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeNewMessage,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !strings.Contains(stripANSI(a.newMessagePicker.View(120)), "carol") {
					t.Error("carol did not come back after backspace")
				}
			},
		},
		{
			// The picker's handleBackspace falls back to popping the
			// last pill when the query is already empty.
			name: "backspace with an empty query removes the last pill",
			opts: newMessageOpts(),
			setup: func(t *testing.T, a *App) {
				openNewMessagePicker(t, a)
				_ = dispatchModeKey(a, keyCode(tea.KeySpace))
				if got := newMessagePillCount(a); got != "1 / 8" {
					t.Fatalf("precondition: pill counter = %q, want %q", got, "1 / 8")
				}
			},
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeNewMessage,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := newMessagePillCount(a); got != "0 / 8" {
					t.Errorf("pill counter = %q, want %q", got, "0 / 8")
				}
			},
		},
		{
			name:     "an unhandled modified key changes nothing",
			opts:     newMessageOpts(),
			setup:    openNewMessagePicker,
			key:      keyMod('x', tea.ModCtrl),
			wantMode: ModeNewMessage,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := modalHighlightedRow(t, a.newMessagePicker.View(120)); !strings.Contains(got, "alice") {
					t.Errorf("highlighted row = %q, want it to contain %q", got, "alice")
				}
				if got := newMessagePillCount(a); got != "0 / 8" {
					t.Errorf("pill counter = %q, want %q", got, "0 / 8")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name: "key with the picker closed falls through to Normal",
			opts: newMessageOpts(),
			setup: func(t *testing.T, a *App) {
				if a.newMessagePicker.IsVisible() {
					t.Fatal("precondition: picker should start hidden")
				}
			},
			key:      keyPress('x'),
			wantMode: ModeNormal,
		},
	})
}
