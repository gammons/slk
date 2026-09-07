package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/help"
)

// openHelp mirrors the production open path (mode_normal.go:211-213):
// entries derived from the live keymap, then Open. The entry list must
// be non-trivial or every navigation row below would pass vacuously,
// so the precondition is checked.
func openHelp(t *testing.T, a *App) {
	a.help.SetEntries(help.FromKeyMap(a.keys))
	a.help.Open()
	if !a.help.IsVisible() {
		t.Fatal("precondition: help overlay did not open")
	}
	if n := len(a.help.VisibleEntries()); n < 3 {
		t.Fatalf("precondition: %d help entries, need at least 3 to move a selection", n)
	}
}

// openHelpSearching opens the overlay and enters /-search mode, which
// is a second, disjoint key regime inside help.HandleKey.
func openHelpSearching(t *testing.T, a *App) {
	openHelp(t, a)
	_ = dispatchModeKey(a, keyPress('/'))
	if !a.help.IsSearching() {
		t.Fatal("precondition: / did not enter search mode")
	}
}

// TestHelpModeKeys characterizes handleHelpMode (mode_help.go:14).
// The handler normalises enter/esc/up/down/backspace, forwards to
// help.HandleKey, and drops to Normal when the overlay hides itself.
func TestHelpModeKeys(t *testing.T) {
	runKeyCases(t, ModeHelp, []keyCase{
		{
			name:     "q closes the overlay and returns to Normal",
			setup:    openHelp,
			key:      keyPress('q'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.help.IsVisible() {
					t.Error("help still visible after q")
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name:     "esc closes the overlay and returns to Normal",
			setup:    openHelp,
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.help.IsVisible() {
					t.Error("help still visible after esc")
				}
			},
		},
		{
			name:     "? toggles the overlay closed",
			setup:    openHelp,
			key:      keyPress('?'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.help.IsVisible() {
					t.Error("help still visible after ?")
				}
			},
		},
		{
			name:     "down moves the selection and stays in Help",
			setup:    openHelp,
			key:      keyCode(tea.KeyDown),
			wantMode: ModeHelp,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.help.Selected(); got != 1 {
					t.Errorf("selected = %d, want 1", got)
				}
			},
		},
		{
			name:     "j moves the selection like down",
			setup:    openHelp,
			key:      keyPress('j'),
			wantMode: ModeHelp,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.help.Selected(); got != 1 {
					t.Errorf("selected = %d, want 1", got)
				}
			},
		},
		{
			name: "up moves the selection back",
			setup: func(t *testing.T, a *App) {
				openHelp(t, a)
				_ = dispatchModeKey(a, keyCode(tea.KeyDown))
				_ = dispatchModeKey(a, keyCode(tea.KeyDown))
				if a.help.Selected() != 2 {
					t.Fatalf("precondition: selected = %d, want 2", a.help.Selected())
				}
			},
			key:      keyCode(tea.KeyUp),
			wantMode: ModeHelp,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.help.Selected(); got != 1 {
					t.Errorf("selected = %d, want 1", got)
				}
			},
		},
		{
			name:     "k at the top clamps rather than wrapping",
			setup:    openHelp,
			key:      keyPress('k'),
			wantMode: ModeHelp,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.help.Selected(); got != 0 {
					t.Errorf("selected = %d, want 0", got)
				}
			},
		},
		{
			name:     "/ enters search mode without leaving Help",
			setup:    openHelp,
			key:      keyPress('/'),
			wantMode: ModeHelp,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.help.IsSearching() {
					t.Error("IsSearching = false, want true")
				}
				if got := a.help.Query(); got != "" {
					t.Errorf("query = %q, want empty", got)
				}
			},
		},
		{
			name:     "printable key while searching appends to the query and filters",
			setup:    openHelpSearching,
			key:      keyPress('q'),
			wantMode: ModeHelp,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.help.Query(); got != "q" {
					t.Errorf("query = %q, want %q", got, "q")
				}
				all := len(help.FromKeyMap(a.keys))
				if got := len(a.help.VisibleEntries()); got >= all {
					t.Errorf("visible entries = %d, want fewer than the unfiltered %d", got, all)
				}
			},
		},
		{
			// j/k are navigation OUTSIDE search but plain query text
			// INSIDE it: help.handleSearchKey's default arm takes any
			// printable byte. Pins that the two regimes really are
			// disjoint.
			name:     "j while searching types instead of navigating",
			setup:    openHelpSearching,
			key:      keyPress('j'),
			wantMode: ModeHelp,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.help.Query(); got != "j" {
					t.Errorf("query = %q, want %q", got, "j")
				}
				if got := a.help.Selected(); got != 0 {
					t.Errorf("selected = %d, want 0: j should not have navigated", got)
				}
			},
		},
		{
			name: "backspace while searching deletes the last query rune",
			setup: func(t *testing.T, a *App) {
				openHelpSearching(t, a)
				_ = dispatchModeKey(a, keyPress('u'))
				_ = dispatchModeKey(a, keyPress('p'))
				if a.help.Query() != "up" {
					t.Fatalf("precondition: query = %q, want %q", a.help.Query(), "up")
				}
			},
			key:      keyCode(tea.KeyBackspace),
			wantMode: ModeHelp,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.help.Query(); got != "u" {
					t.Errorf("query = %q, want %q", got, "u")
				}
			},
		},
		{
			name: "enter while searching keeps the filter and leaves search mode",
			setup: func(t *testing.T, a *App) {
				openHelpSearching(t, a)
				_ = dispatchModeKey(a, keyPress('q'))
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeHelp,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.help.IsSearching() {
					t.Error("IsSearching = true, want false after enter")
				}
				if got := a.help.Query(); got != "q" {
					t.Errorf("query = %q, want %q: enter must keep the filter", got, "q")
				}
			},
		},
		{
			// The one esc that does NOT reach ModeNormal: inside
			// search it only leaves search mode, so the overlay stays
			// visible and handleHelpMode's IsVisible guard is false.
			name: "esc while searching clears the query but keeps Help open",
			setup: func(t *testing.T, a *App) {
				openHelpSearching(t, a)
				_ = dispatchModeKey(a, keyPress('q'))
			},
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeHelp,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if !a.help.IsVisible() {
					t.Error("help closed; esc in search mode should keep it open")
				}
				if a.help.IsSearching() {
					t.Error("IsSearching = true, want false")
				}
				if got := a.help.Query(); got != "" {
					t.Errorf("query = %q, want empty", got)
				}
			},
		},
		{
			name:     "unhandled key is swallowed and Help stays open",
			setup:    openHelp,
			key:      keyPress('z'),
			wantMode: ModeHelp,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if !a.help.IsVisible() {
					t.Error("z should not close the overlay")
				}
				if got := a.help.Selected(); got != 0 {
					t.Errorf("selected = %d, want 0", got)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			// Pinned by TestRunKeyCases_EstablishesMode's third row
			// too: with no overlay open, any key exits to Normal.
			name: "key with no overlay open falls straight through to Normal",
			setup: func(t *testing.T, a *App) {
				if a.help.IsVisible() {
					t.Fatal("precondition: help should start hidden")
				}
			},
			key:      keyPress('x'),
			wantMode: ModeNormal,
		},
	})
}
