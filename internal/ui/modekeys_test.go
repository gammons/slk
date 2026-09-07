package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestEveryModeHasAHandler pins the registration invariant.
// dispatchModeKey falls back to handleNormalMode for unregistered modes
// (mode_handlers.go:96); that fallback should be unreachable because all
// 16 modes are registered, and a mode that reached it would silently
// serve normal-mode behaviour to a modal.
//
// What this test detects:
//
//   - a modeHandlers entry deleted — len(modeHandlers) drops below
//     len(all), and the loop names the orphaned mode;
//   - a 17th Mode added AND registered — len(modeHandlers) exceeds
//     len(all), forcing the pinned list below to be updated;
//   - a 17th Mode added, NOT registered, but given a String() arm — the
//     Mode(len(all)) probe below stops answering "UNKNOWN".
//
// What it cannot detect: a 17th Mode with neither a handler nor a
// String() arm. `all` is a hand-maintained literal, and Mode is a bare
// int iota with no count sentinel (mode.go:4-23), so nothing available
// to test code ties the list to the constant block. Closing that
// residual gap would take a production change (a ModeCount sentinel, or
// a generated stringer), which this PR's budget does not allow. The
// String() probe covers the overwhelmingly likely shape of the mistake,
// since a new mode needs a String() arm for the statusbar.
func TestEveryModeHasAHandler(t *testing.T) {
	all := []Mode{
		ModeNormal, ModeInsert, ModeCommand, ModeSearch,
		ModeChannelFinder, ModeReactionPicker, ModeWorkspaceFinder,
		ModeThemeSwitcher, ModePresenceMenu, ModePresenceCustomSnooze,
		ModeConfirm, ModeHelp, ModeNewMessage, ModeReactionsView,
		ModeLinkPicker, ModeWorkspaceSearch,
	}
	if len(modeHandlers) != len(all) {
		t.Errorf("modeHandlers has %d entries, want %d", len(modeHandlers), len(all))
	}
	for _, m := range all {
		if _, ok := modeHandlers[m]; !ok {
			t.Errorf("mode %v (%s) has no handler; keys would fall back to Normal", m, m)
		}
	}
	// A 17th Mode would take the value len(all). Mode.String() answers
	// "UNKNOWN" only for values outside the constant block
	// (mode.go:87-88), so anything else here means a Mode exists that
	// the pinned list does not know about — including the case where it
	// was added without a handler, which the two checks above cannot
	// see because both counts stay at 16.
	if got := Mode(len(all)).String(); got != "UNKNOWN" {
		t.Errorf("Mode(%d).String() = %q, want UNKNOWN: a Mode exists beyond the pinned list", len(all), got)
	}
}

func TestRunKeyCases_Harness(t *testing.T) {
	runKeyCases(t, ModeNormal, []keyCase{
		{
			name:     "unbound key leaves mode unchanged",
			key:      keyPress('\''),
			wantMode: ModeNormal,
		},
		{
			name:     "i enters insert mode and returns the compose focus cmd",
			key:      keyPress('i'),
			wantMode: ModeInsert,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Error("cmd = nil, want the compose.Focus() cmd")
				}
			},
		},
		{
			// Proves setup runs, and runs BEFORE dispatch. It also
			// shows how a multi-key sequence is expressed: setup
			// dispatches the leading key, the case's key is the last
			// one. Here ctrl+w arms the window-command chord
			// (mode_normal.go:92), which handleNormalMode's first
			// branch consumes and disarms (mode_normal.go:42) before
			// delegating 'v' to handleWindowChord -> splitWindow.
			//
			// The assertion is deliberately positive (two windows),
			// not merely "the chord is no longer armed": the negative
			// form passes vacuously when setup never runs, since the
			// flag starts false. Without setup, 'v' is OpenPreview and
			// the window count stays 1.
			name: "setup runs, and runs before dispatch",
			// 200x50 because splitWindow refuses ("Not enough room")
			// at the harness default 120x30, same threshold
			// TestNewTestApp_WithWindowSplit sizes around.
			opts: []testOpt{withSize(200, 50)},
			setup: func(t *testing.T, a *App) {
				_ = dispatchModeKey(a, keyMod('w', tea.ModCtrl))
				if !a.pendingWinCmd {
					t.Fatal("precondition: ctrl+w did not arm the window chord")
				}
			},
			key:      keyPress('v'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.wins.Len(); got != 2 {
					t.Errorf("window count = %d, want 2 (ctrl+w v should have split)", got)
				}
				if a.pendingWinCmd {
					t.Error("pendingWinCmd still armed after dispatch")
				}
			},
		},
		{
			name:     "per-case opts reach newTestApp",
			opts:     []testOpt{withSize(80, 24)},
			key:      keyPress('\''),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.width != 80 || a.height != 24 {
					t.Errorf("size = %dx%d, want 80x24", a.width, a.height)
				}
			},
		},
	})
}

func TestRunKeyCases_EstablishesMode(t *testing.T) {
	runKeyCases(t, ModeCommand, []keyCase{
		{
			name:     "printable key is consumed by command mode, not normal mode",
			key:      keyPress('x'),
			wantMode: ModeCommand,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.cmdline != "x" {
					t.Errorf("cmdline = %q, want %q", a.cmdline, "x")
				}
			},
		},
		{
			name:     "esc leaves command mode",
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
		},
		{
			// Pins the append order in runKeyCases: withMode(mode) is
			// appended AFTER tc.opts, so the mode argument wins. If a
			// future edit swaps those two statements, this App is built
			// in ModeHelp, handleHelpMode consumes 'x' (exiting to
			// ModeNormal, since no help overlay is open —
			// mode_help.go:29-31), and both assertions below fail.
			name:     "mode argument beats a withMode in opts",
			opts:     []testOpt{withMode(ModeHelp)},
			key:      keyPress('x'),
			wantMode: ModeCommand,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.cmdline != "x" {
					t.Errorf("cmdline = %q, want %q: the table's mode argument did not win over opts", a.cmdline, "x")
				}
			},
		},
	})
}

// TestKeyPress_CarriesText pins that keyPress populates Key.Text and
// not just Key.Code. The two are not interchangeable: the compose
// textarea inserts Key.Text, so a Code-only message navigates but types
// nothing (verified — the same case with Text stripped leaves the
// buffer empty). Tables that type characters depend on this.
//
// It also records that withMode(ModeInsert) does NOT focus the compose:
// in production 'i' focuses it as a side effect of handleNormalMode
// (mode_normal.go:68), which the harness bypasses by construction.
// Hence the explicit Focus in setup.
func TestKeyPress_CarriesText(t *testing.T) {
	runKeyCases(t, ModeInsert, []keyCase{
		{
			name:     "printable key reaches the compose textarea",
			setup:    func(_ *testing.T, a *App) { _ = a.compose.Focus() },
			key:      keyPress('x'),
			wantMode: ModeInsert,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.compose.Value(); got != "x" {
					t.Errorf("compose value = %q, want %q", got, "x")
				}
			},
		},
	})
}

// TestRunKeyCases_InvokesHooks closes the harness's last vacuity gap.
// Every other piece of evidence in this file lives inside a setup or
// assert func, so a runner that silently skipped those hooks would make
// the whole harness — and Tasks 15-17's tables with it — pass without
// checking anything. Counting the calls from outside is the only way to
// see that from within the harness's own tests.
//
// Safe because subtests are sequential here: nothing in this repo calls
// t.Parallel.
func TestRunKeyCases_InvokesHooks(t *testing.T) {
	setups, asserts := 0, 0
	runKeyCases(t, ModeNormal, []keyCase{
		{
			name:     "first",
			setup:    func(*testing.T, *App) { setups++ },
			key:      keyPress('\''),
			wantMode: ModeNormal,
			assert:   func(*testing.T, *App, tea.Cmd) { asserts++ },
		},
		{
			name:     "second",
			setup:    func(*testing.T, *App) { setups++ },
			key:      keyPress('\''),
			wantMode: ModeNormal,
			assert:   func(*testing.T, *App, tea.Cmd) { asserts++ },
		},
	})
	if setups != 2 {
		t.Errorf("setup invoked %d times, want 2", setups)
	}
	if asserts != 2 {
		t.Errorf("assert invoked %d times, want 2", asserts)
	}
}

// TestRunKeyCases_IsolatesCases pins that each row gets its own App.
// Tables in Tasks 15-17 mutate the App freely in setup; if the harness
// reused one instance, row N's mutations would silently become row
// N+1's precondition.
func TestRunKeyCases_IsolatesCases(t *testing.T) {
	var seen []*App
	runKeyCases(t, ModeNormal, []keyCase{
		{
			name:     "first mutates",
			setup:    func(_ *testing.T, a *App) { seen = append(seen, a); a.cmdline = "dirty" },
			key:      keyPress('\''),
			wantMode: ModeNormal,
		},
		{
			name:  "second sees a clean App",
			setup: func(_ *testing.T, a *App) { seen = append(seen, a) },
			key:   keyPress('\''),
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.cmdline != "" {
					t.Errorf("cmdline = %q, want empty: App leaked from the previous case", a.cmdline)
				}
			},
			wantMode: ModeNormal,
		},
	})
	if len(seen) != 2 {
		t.Fatalf("setup ran %d times, want 2", len(seen))
	}
	if seen[0] == seen[1] {
		t.Error("both cases got the same *App")
	}
}

// keyCase is one characterization row: given this precondition and
// this key, the handler leaves the App in this state.
type keyCase struct {
	name string
	// opts are extra construction options, appended after the
	// harness defaults, so they win on any last-wins option
	// (withSize, withView, ...). The dispatch mode is the one
	// exception: runKeyCases appends withMode(mode) after opts, so a
	// withMode here does NOT win.
	opts []testOpt
	// setup runs after construction, before dispatch. nil means the
	// harness default is the precondition. Use it for preconditions
	// newTestApp's options cannot express (unexported flags,
	// sub-model state), and for anything that must happen after the
	// mode is established.
	//
	// It takes the subtest's *testing.T so it can fail the case
	// directly when its own precondition does not hold. Without that,
	// a setup that silently no-ops turns the row into a vacuous pass.
	setup func(t *testing.T, a *App)
	key   tea.KeyMsg
	// wantMode is the mode AFTER dispatch.
	wantMode Mode
	// assert checks anything beyond mode. nil means mode is the whole
	// assertion. The tea.Cmd is dispatchModeKey's return value,
	// unexecuted.
	assert func(t *testing.T, a *App, cmd tea.Cmd)
}

// runKeyCases drives each case through dispatchModeKey directly rather
// than App.Update, so a failure localises to the mode handler instead
// of to the reducer chain that runs ahead of it. Each case gets its own
// freshly built App.
//
// CONSEQUENCE, and it is not just a coverage gap: a key that never
// reaches the handler in production still reaches it here, so a row for
// such a key records behaviour that does not happen. Everything ahead
// of dispatchModeKey is bypassed — the reducer chain (app.go:609), the
// image-preview key swallow (app.go:643), and inside App.handleKey the
// Quit/ctrl+c intercept (app.go:708), the bootstrap-loading gate
// (app.go:715) and the scroll-coalesce flush (app.go:724). ctrl+c is
// the concrete trap: app.go:708 returns before dispatch, so
// handleNormalMode never sees it, and a row here would characterise a
// dead branch as live. Rows that must be production-reachable belong on
// a.handleKey or a.Update, outside this runner.
//
// The dispatch mode is established through newTestApp's withMode
// option, i.e. through App.SetMode, rather than by assigning a.mode
// directly. The plan's brief prescribed the direct assignment, on the
// theory that SetMode's side effects would contaminate the
// precondition. On a freshly built App they do not: of SetMode's three
// guarded branches (app.go:1640) the chord disarm needs pendingWinCmd
// set, the cmdline clear needs the App to already be in ModeCommand,
// and the ModeInsert branch calls clearSelections, which is a no-op
// with no selection active. What SetMode does unconditionally is
// a.statusbar.SetMode(mode) -- and skipping that is the contaminating
// choice, not the safe one: it would leave the statusbar reading
// "NORMAL" while a.mode says otherwise, a state production never
// reaches, since every production mode change goes through SetMode.
//
// withMode is appended last so the mode argument is authoritative: a
// withMode in a case's opts cannot silently redirect the table to
// another handler. A case that genuinely needs a different mode at
// dispatch time should set it in setup.
//
// Note that withMode(ModeNormal) is deliberately a no-op inside
// buildTestApp (NewApp already starts there), so a ModeNormal table
// gets NewApp's untouched statusbar rather than a redundant SetMode.
func runKeyCases(t *testing.T, mode Mode, cases []keyCase) {
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// withSize(120, 30) restates buildTestApp's own default
			// so the geometry these tables assert against is written
			// down here and overridable per case, rather than being
			// whatever newTestApp happens to default to later.
			opts := append([]testOpt{withSize(120, 30)}, tc.opts...)
			opts = append(opts, withMode(mode))
			a := newTestApp(t, opts...)
			if tc.setup != nil {
				tc.setup(t, a)
			}
			cmd := dispatchModeKey(a, tc.key)
			if a.mode != tc.wantMode {
				t.Errorf("mode after %v = %v (%s), want %v (%s)",
					tc.key, a.mode, a.mode, tc.wantMode, tc.wantMode)
			}
			if tc.assert != nil {
				tc.assert(t, a, cmd)
			}
		})
	}
}

// keyPress builds a printable-rune key message. Key.Code is a rune and
// Key.Text carries the printable character, matching the shape already
// used across internal/ui tests (e.g. mode_command_test.go:12).
func keyPress(r rune) tea.KeyMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// keyCode builds a special-key message (tea.KeyEnter, tea.KeyEscape,
// tea.KeyUp, ...). Those constants are runes in this bubbletea version,
// and Key.Text stays empty for them, which is what msg.String() and
// normalizeFinderKey rely on to tell "enter" from a literal "\r".
func keyCode(c rune) tea.KeyMsg {
	return tea.KeyPressMsg{Code: c}
}

// keyMod builds a modified key press (ctrl+w, shift+enter, ...).
func keyMod(c rune, mod tea.KeyMod) tea.KeyMsg {
	return tea.KeyPressMsg{Code: c, Mod: mod}
}
