package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/presencemenu"
)

// presenceMenuRows is the number of rows the menu is currently
// showing, derived from the modal's own height math (nRows + 7).
// presencemenu.Model exposes neither View nor Selected, so BoxSize is
// the only window onto its filter state.
func presenceMenuRows(a *App) int {
	_, h := a.presenceMenu.BoxSize(120, 30)
	return h - 7
}

// presenceMenuAllRows is the unfiltered row count for a menu opened
// without DND active: Active, Away, six fixed snoozes, "until tomorrow
// morning", and "Snooze custom...".
const presenceMenuAllRows = 10

// openPresenceMenu mirrors the production open path
// (mode_normal.go:228-231) for a workspace with no DND, and records
// every setStatusFn call.
func openPresenceMenu(calls *[]statusCall) func(*testing.T, *App) {
	return func(t *testing.T, a *App) {
		*calls = nil
		a.SetStatusSetter(func(action presencemenu.Action, mins int) {
			*calls = append(*calls, statusCall{action: action, mins: mins})
		})
		pres, dndEnabled, dndEnd, _ := a.presence.Status(a.activeTeamID)
		a.presenceMenu.OpenWith(a.workspaceNameForActive(), pres, dndEnabled, dndEnd)
		if !a.presenceMenu.IsVisible() {
			t.Fatal("precondition: presence menu did not open")
		}
		if got := presenceMenuRows(a); got != presenceMenuAllRows {
			t.Fatalf("precondition: %d rows, want %d", got, presenceMenuAllRows)
		}
	}
}

// typeQuery drives the given runes through the handler and asserts the
// filter narrowed to wantRows, so a case that meant to isolate one row
// cannot silently act on a different one.
func typePresenceQuery(t *testing.T, a *App, q string, wantRows int) {
	t.Helper()
	for _, r := range q {
		_ = dispatchModeKey(a, keyPress(r))
	}
	if got := presenceMenuRows(a); got != wantRows {
		t.Fatalf("precondition: query %q left %d rows, want %d", q, got, wantRows)
	}
}

// TestPresenceMenuModeKeys characterizes handlePresenceMenuMode
// (mode_presence_menu.go:21).
func TestPresenceMenuModeKeys(t *testing.T) {
	var calls []statusCall
	open := openPresenceMenu(&calls)
	opts := []testOpt{withActiveTeam("T1")}

	runKeyCases(t, ModePresenceMenu, []keyCase{
		{
			name:     "esc closes the menu and returns to Normal",
			opts:     opts,
			setup:    open,
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.presenceMenu.IsVisible() {
					t.Error("menu still visible after esc")
				}
				if _, _, _, ok := a.presence.Status("T1"); ok {
					t.Error("esc applied a presence status; it must not")
				}
				if len(calls) != 0 {
					t.Errorf("setStatusFn called %d times, want 0", len(calls))
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name:     "enter on the first row sets presence active",
			opts:     opts,
			setup:    open,
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.presenceMenu.IsVisible() {
					t.Error("menu still visible after enter")
				}
				pres, _, _, ok := a.presence.Status("T1")
				if !ok || pres != "active" {
					t.Errorf("cached presence = %q (ok=%v), want \"active\"", pres, ok)
				}
				if len(calls) != 1 || calls[0].action != presencemenu.ActionSetActive {
					t.Errorf("setStatusFn calls = %+v, want one ActionSetActive", calls)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name: "enter on Away sets presence away",
			opts: opts,
			setup: func(t *testing.T, a *App) {
				open(t, a)
				typePresenceQuery(t, a, "away", 1)
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				pres, _, _, _ := a.presence.Status("T1")
				if pres != "away" {
					t.Errorf("cached presence = %q, want \"away\"", pres)
				}
				if len(calls) != 1 || calls[0].action != presencemenu.ActionSetAway {
					t.Errorf("setStatusFn calls = %+v, want one ActionSetAway", calls)
				}
			},
		},
		{
			name: "enter on a fixed snooze row applies its minute count",
			opts: opts,
			setup: func(t *testing.T, a *App) {
				open(t, a)
				// "20" matches only "Snooze for 20 minutes";
				// "Snooze for 24 hours" contains 24, not 20.
				typePresenceQuery(t, a, "20", 1)
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				_, dnd, end, ok := a.presence.Status("T1")
				if !ok || !dnd {
					t.Fatalf("DNDEnabled = %v (ok=%v), want true", dnd, ok)
				}
				if d := time.Until(end); d < 19*time.Minute || d > 21*time.Minute {
					t.Errorf("DND ends in %v, want ~20m", d)
				}
				if len(calls) != 1 || calls[0].action != presencemenu.ActionSnooze || calls[0].mins != 20 {
					t.Errorf("setStatusFn calls = %+v, want one (ActionSnooze, 20)", calls)
				}
			},
		},
		{
			// The only result arm that does NOT go to ModeNormal: it
			// hands off to handlePresenceCustomSnoozeMode, and clears
			// the snooze buffer on the way so a stale value from a
			// previous visit cannot be committed by a lone enter.
			name: "enter on Snooze custom hands off to the snooze sub-mode",
			opts: opts,
			setup: func(t *testing.T, a *App) {
				open(t, a)
				a.presence.AppendSnoozeDigit("9")
				typePresenceQuery(t, a, "custom", 1)
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModePresenceCustomSnooze,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.presenceMenu.IsVisible() {
					t.Error("menu still visible after enter")
				}
				if got := a.presence.SnoozeBuf(); got != "" {
					t.Errorf("snooze buffer = %q, want it cleared on hand-off", got)
				}
				if _, _, _, ok := a.presence.Status("T1"); ok {
					t.Error("custom snooze applied a status immediately; it must defer")
				}
				if len(calls) != 0 {
					t.Errorf("setStatusFn called %d times, want 0", len(calls))
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// The End-DND row only exists when the menu was opened
			// with DND active, so this row also pins buildItems'
			// conditional eleventh entry.
			name: "enter on End snooze clears DND",
			opts: opts,
			setup: func(t *testing.T, a *App) {
				open(t, a)
				// Re-open with DND active so buildItems appends the
				// conditional End-DND row.
				a.presence.Set("T1", "away", true, time.Now().Add(time.Hour))
				pres, dndEnabled, dndEnd, _ := a.presence.Status("T1")
				a.presenceMenu.OpenWith("ws", pres, dndEnabled, dndEnd)
				if got := presenceMenuRows(a); got != presenceMenuAllRows+1 {
					t.Fatalf("precondition: %d rows, want %d (End-DND row missing)", got, presenceMenuAllRows+1)
				}
				typePresenceQuery(t, a, "end", 1)
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				pres, dnd, end, _ := a.presence.Status("T1")
				if dnd {
					t.Error("DNDEnabled = true, want false after End snooze")
				}
				if !end.IsZero() {
					t.Errorf("DNDEndTS = %v, want zero", end)
				}
				if pres != "away" {
					t.Errorf("presence = %q, want it preserved as \"away\"", pres)
				}
				if len(calls) != 1 || calls[0].action != presencemenu.ActionEndDND {
					t.Errorf("setStatusFn calls = %+v, want one ActionEndDND", calls)
				}
			},
		},
		{
			name: "enter with no matching row is a no-op",
			opts: opts,
			setup: func(t *testing.T, a *App) {
				open(t, a)
				// No menu label contains "x"; the row count falls to
				// the modal's 1-row floor.
				typePresenceQuery(t, a, "x", 1)
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModePresenceMenu,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if !a.presenceMenu.IsVisible() {
					t.Error("menu closed on a no-match enter")
				}
				if _, _, _, ok := a.presence.Status("T1"); ok {
					t.Error("a no-match enter applied a status")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// presencemenu exposes neither View nor Selected, so the
			// only way to observe where the cursor landed is to commit
			// it. The follow-up enter is part of the probe, not part
			// of the key under test — wantMode has already been
			// checked against ModePresenceMenu by the time this runs.
			name:     "down moves the cursor to the second row",
			opts:     opts,
			setup:    open,
			key:      keyCode(tea.KeyDown),
			wantMode: ModePresenceMenu,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				assertCommitsTo(t, a, presencemenu.ActionSetAway)
			},
		},
		{
			name:     "j moves the cursor like down",
			opts:     opts,
			setup:    open,
			key:      keyPress('j'),
			wantMode: ModePresenceMenu,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				// BUG?-adjacent, and deliberate: j/k are navigation
				// here, so the query can never contain them. Filtering
				// for "Snooze" by typing "j"-free prefixes is the only
				// option; the workspace finder, by contrast, treats j
				// as text.
				assertCommitsTo(t, a, presencemenu.ActionSetAway)
			},
		},
		{
			name:     "ctrl+n moves the cursor like down",
			opts:     opts,
			setup:    open,
			key:      keyMod('n', tea.ModCtrl),
			wantMode: ModePresenceMenu,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				assertCommitsTo(t, a, presencemenu.ActionSetAway)
			},
		},
		{
			name: "up moves the cursor back",
			opts: opts,
			setup: func(t *testing.T, a *App) {
				open(t, a)
				_ = dispatchModeKey(a, keyCode(tea.KeyDown))
			},
			key:      keyCode(tea.KeyUp),
			wantMode: ModePresenceMenu,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				assertCommitsTo(t, a, presencemenu.ActionSetActive)
			},
		},
		{
			name:     "k at the top clamps rather than wrapping",
			opts:     opts,
			setup:    open,
			key:      keyPress('k'),
			wantMode: ModePresenceMenu,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				assertCommitsTo(t, a, presencemenu.ActionSetActive)
			},
		},
		{
			name:     "ctrl+p at the top clamps rather than wrapping",
			opts:     opts,
			setup:    open,
			key:      keyMod('p', tea.ModCtrl),
			wantMode: ModePresenceMenu,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				assertCommitsTo(t, a, presencemenu.ActionSetActive)
			},
		},
		{
			name:     "a printable key filters the menu",
			opts:     opts,
			setup:    open,
			key:      keyPress('a'),
			wantMode: ModePresenceMenu,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				// "Active" and "Away" prefix-match; no snooze label
				// contains an "a".
				if got := presenceMenuRows(a); got != 2 {
					t.Errorf("rows = %d, want 2 after filtering on \"a\"", got)
				}
			},
		},
		{
			name: "backspace removes the last query rune and restores the menu",
			opts: opts,
			setup: func(t *testing.T, a *App) {
				open(t, a)
				typePresenceQuery(t, a, "a", 2)
			},
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModePresenceMenu,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := presenceMenuRows(a); got != presenceMenuAllRows {
					t.Errorf("rows = %d, want %d", got, presenceMenuAllRows)
				}
			},
		},
		{
			name:     "backspace on an empty query is inert",
			opts:     opts,
			setup:    open,
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModePresenceMenu,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := presenceMenuRows(a); got != presenceMenuAllRows {
					t.Errorf("rows = %d, want %d", got, presenceMenuAllRows)
				}
			},
		},
		{
			name:     "an unhandled modified key changes nothing",
			opts:     opts,
			setup:    open,
			key:      keyMod('x', tea.ModCtrl),
			wantMode: ModePresenceMenu,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := presenceMenuRows(a); got != presenceMenuAllRows {
					t.Errorf("rows = %d, want %d: ctrl+x should not have filtered", got, presenceMenuAllRows)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
				assertCommitsTo(t, a, presencemenu.ActionSetActive)
			},
		},
		{
			name: "key with the menu closed falls through to Normal",
			opts: opts,
			setup: func(t *testing.T, a *App) {
				if a.presenceMenu.IsVisible() {
					t.Fatal("precondition: menu should start hidden")
				}
			},
			key:      keyPress('x'),
			wantMode: ModeNormal,
		},
	})
}

// assertCommitsTo presses enter and asserts the menu committed the
// given action, which is how this table observes cursor position.
func assertCommitsTo(t *testing.T, a *App, want presencemenu.Action) {
	t.Helper()
	var got presencemenu.Action
	seen := false
	a.SetStatusSetter(func(action presencemenu.Action, _ int) {
		got, seen = action, true
	})
	_ = dispatchModeKey(a, keyCode(tea.KeyEnter))
	if !seen {
		t.Fatal("enter did not commit any action; the cursor probe proves nothing")
	}
	if got != want {
		t.Errorf("cursor committed %v, want %v", got, want)
	}
}
