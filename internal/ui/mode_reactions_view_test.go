package ui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

// reactionsViewMessages builds one message carrying a reaction with
// nUsers reactors, which is what openReactionsView needs to have
// anything to show.
func reactionsViewMessages(nUsers int) []messages.MessageItem {
	uids := make([]string, 0, nUsers)
	for i := 1; i <= nUsers; i++ {
		uids = append(uids, fmt.Sprintf("U%d", i))
	}
	return []messages.MessageItem{{
		TS:        "1.0",
		UserID:    "U1",
		UserName:  "alice",
		Text:      "msg-1",
		Timestamp: "1:00 PM",
		Reactions: []messages.ReactionItem{
			{Emoji: "thumbsup", Count: nUsers, UserIDs: uids},
		},
	}}
}

// openReactionsViewFor returns a setup that drives the production open
// path (App.openReactionsView) and fails the case if the overlay did
// not actually open — without that check every "still visible" row
// below would pass vacuously.
func openReactionsViewFor(nUsers int) func(*testing.T, *App) {
	return func(t *testing.T, a *App) {
		a.focusedPanel = PanelMessages
		a.messagepane.SetMessages(reactionsViewMessages(nUsers))
		_ = a.openReactionsView()
		if !a.reactionsView.IsVisible() {
			t.Fatal("precondition: openReactionsView did not open the overlay")
		}
		// openReactionsView calls SetMode(ModeReactionsView) itself;
		// runKeyCases already did. Assert they agree so a future change
		// to either does not silently redirect the dispatch.
		if a.mode != ModeReactionsView {
			t.Fatalf("precondition: mode = %v, want ModeReactionsView", a.mode)
		}
	}
}

// TestReactionsViewModeKeys characterizes handleReactionsViewMode
// (mode_reactions_view.go:12). The handler normalises esc/up/down to
// their string forms, forwards everything to reactionsview.HandleKey,
// and drops to Normal when the overlay reports itself invisible.
func TestReactionsViewModeKeys(t *testing.T) {
	openView := openReactionsViewFor(3)

	// scrolled opens the overlay with enough reactors to overflow the
	// window AND renders once, because reactionsview.maxOff is only
	// computed during renderBox. Without the render maxOff is 0 and
	// "down" is inert — see the "down before any render" row.
	scrolled := func(t *testing.T, a *App) {
		openReactionsViewFor(40)(t, a)
		_ = a.View()
		if a.reactionsView.Offset() != 0 {
			t.Fatalf("precondition: offset = %d, want 0 before any key", a.reactionsView.Offset())
		}
	}

	runKeyCases(t, ModeReactionsView, []keyCase{
		{
			name:     "esc closes the overlay and returns to Normal",
			setup:    openView,
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.reactionsView.IsVisible() {
					t.Error("overlay still visible after esc")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name:     "q closes the overlay and returns to Normal",
			setup:    openView,
			key:      keyPress('q'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.reactionsView.IsVisible() {
					t.Error("overlay still visible after q")
				}
			},
		},
		{
			// L is the key that OPENS the overlay in normal mode
			// (mode_normal.go), and reactionsview.HandleKey accepts it
			// as a close too, so it toggles.
			name:     "L closes the overlay and returns to Normal",
			setup:    openView,
			key:      keyPress('L'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.reactionsView.IsVisible() {
					t.Error("overlay still visible after L")
				}
			},
		},
		{
			name:     "down scrolls one line once a render has computed maxOff",
			setup:    scrolled,
			key:      keyCode(tea.KeyDown),
			wantMode: ModeReactionsView,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.reactionsView.Offset(); got != 1 {
					t.Errorf("offset = %d, want 1", got)
				}
				if !a.reactionsView.IsVisible() {
					t.Error("down should not close the overlay")
				}
			},
		},
		{
			// This is what the `switch msg.Key().Code` at the top of
			// the handler is FOR, and the only row that shows it.
			//
			// Key.String() (ultraviolet key.go:391) returns Key.Text
			// when non-empty, else Keystroke(), which writes every
			// active modifier as a prefix (:413-431) BEFORE consulting
			// keyTypeString (:459-467). So an unmodified KeyDown
			// already stringifies to "down" and the arm looks
			// redundant — but shift+down stringifies to "shift+down",
			// which reactionsview.HandleKey does not match. The arm
			// rewrites it back to "down", so a shift-held scroll still
			// scrolls. Delete `case tea.KeyDown` and this row fails
			// while every unmodified row keeps passing.
			name:     "shift+down scrolls: the Code switch strips the modifier",
			setup:    scrolled,
			key:      keyMod(tea.KeyDown, tea.ModShift),
			wantMode: ModeReactionsView,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.reactionsView.Offset(); got != 1 {
					t.Errorf("offset = %d, want 1: shift+down should normalise to down", got)
				}
			},
		},
		{
			name:     "j scrolls like down",
			setup:    scrolled,
			key:      keyPress('j'),
			wantMode: ModeReactionsView,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.reactionsView.Offset(); got != 1 {
					t.Errorf("offset = %d, want 1", got)
				}
			},
		},
		{
			// Documented and pinned, not a suspected defect: maxOff is
			// assigned only inside renderBox, and HandleKey's own doc
			// comment (reactionsview/model.go:69-71) already says so —
			// "Scroll is clamped to [0, maxOff], where maxOff is
			// recomputed on each render; before the first render maxOff
			// is 0 so scrolling is inert." This row is the executable
			// form of that sentence. In production the App renders on
			// the same Update tick that opened the modal, so a user
			// never reaches it; a synthesized wheel event arriving
			// before any render would.
			name:     "down before any render is inert (maxOff still 0)",
			setup:    openReactionsViewFor(40),
			key:      keyCode(tea.KeyDown),
			wantMode: ModeReactionsView,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.reactionsView.Offset(); got != 0 {
					t.Errorf("offset = %d, want 0: maxOff should still be unset", got)
				}
			},
		},
		{
			name: "up scrolls back and clamps at zero",
			setup: func(t *testing.T, a *App) {
				scrolled(t, a)
				_ = dispatchModeKey(a, keyCode(tea.KeyDown))
				if a.reactionsView.Offset() != 1 {
					t.Fatalf("precondition: offset = %d, want 1", a.reactionsView.Offset())
				}
			},
			key:      keyCode(tea.KeyUp),
			wantMode: ModeReactionsView,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.reactionsView.Offset(); got != 0 {
					t.Errorf("offset = %d, want 0", got)
				}
			},
		},
		{
			// The setup scrolls DOWN to 2 and then back UP to 0 with
			// the same 'k' under test, so this row separates "clamped
			// at the top" from "ignored entirely". The previous shape
			// — `scrolled` plus one 'k', asserting offset 0 — asserted
			// exactly the value `scrolled` had just established, so it
			// passed against a handler that did nothing.
			name: "k scrolls up and clamps at zero",
			setup: func(t *testing.T, a *App) {
				scrolled(t, a)
				for range 2 {
					_ = dispatchModeKey(a, keyCode(tea.KeyDown))
				}
				if got := a.reactionsView.Offset(); got != 2 {
					t.Fatalf("precondition: offset = %d, want 2 after two downs", got)
				}
				for range 2 {
					_ = dispatchModeKey(a, keyPress('k'))
				}
				if got := a.reactionsView.Offset(); got != 0 {
					t.Fatalf("precondition: offset = %d, want 0: 'k' did not scroll back to the top", got)
				}
			},
			key:      keyPress('k'),
			wantMode: ModeReactionsView,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.reactionsView.Offset(); got != 0 {
					t.Errorf("offset = %d, want 0: a further 'k' at the top must clamp", got)
				}
			},
		},
		{
			name:     "unhandled key is swallowed: overlay unchanged, nil cmd",
			setup:    scrolled,
			key:      keyPress('x'),
			wantMode: ModeReactionsView,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if !a.reactionsView.IsVisible() {
					t.Error("x should not close the overlay")
				}
				if got := a.reactionsView.Offset(); got != 0 {
					t.Errorf("offset = %d, want 0", got)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// The handler has no "overlay is closed" guard: it forwards
			// to a hidden model and then re-asserts ModeNormal. Reached
			// in production only if a key arrives after the overlay
			// closed but before the mode changed.
			name: "key with the overlay already closed still lands in Normal",
			setup: func(t *testing.T, a *App) {
				if a.reactionsView.IsVisible() {
					t.Fatal("precondition: overlay should start hidden")
				}
			},
			key:      keyPress('x'),
			wantMode: ModeNormal,
		},
	})
}
