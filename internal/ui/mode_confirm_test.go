package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------
// handleConfirmMode (mode_confirm.go:16)
//
// Eight statements, no sub-branching of its own: it normalises Escape
// and Enter to the strings confirmprompt.Model.HandleKey matches
// (mode_confirm.go:18-23), forwards, and drops to ModeNormal whenever
// the prompt closed itself. confirmprompt.HandleKey always closes
// (confirmprompt/model.go:70-88), so in practice every key exits the
// mode -- including keys the prompt treats as a cancel.
//
// The only observable difference between confirm and cancel at this
// layer is the returned Cmd, so every confirm row registers a callback
// and asserts the cmd both exists AND yields that callback's sentinel.
// "cmd != nil" alone would pass against a handler that returned some
// other command.
// ---------------------------------------------------------------------

// confirmSentinelMsg is what the registered onConfirm callback emits.
// A distinct type (rather than, say, ToastMsg) so a row cannot be
// satisfied by any command the App produces on its own.
type confirmSentinelMsg struct{}

// openConfirm opens the confirm prompt with a callback that records its
// invocation into fired and emits confirmSentinelMsg.
//
// It asserts visibility, because a hidden prompt makes every row here
// vacuous in the worst way: HandleKey returns a zero Result without
// looking at the key (confirmprompt/model.go:71-73), so a cancel row
// would still see cmd == nil and mode == ModeNormal and pass.
func openConfirm(fired *bool) func(*testing.T, *App) {
	return func(t *testing.T, a *App) {
		t.Helper()
		a.confirmPrompt.Open("Delete message?", "> hello", func() tea.Msg {
			*fired = true
			return confirmSentinelMsg{}
		})
		if !a.confirmPrompt.IsVisible() {
			t.Fatal("precondition: confirm prompt is not visible")
		}
	}
}

// wantConfirmed asserts the confirm path: prompt closed, a cmd came
// back, and running it reaches the registered callback.
func wantConfirmed(fired *bool) func(*testing.T, *App, tea.Cmd) {
	return func(t *testing.T, a *App, cmd tea.Cmd) {
		t.Helper()
		if a.confirmPrompt.IsVisible() {
			t.Error("confirm prompt still visible after a decision key")
		}
		if cmd == nil {
			t.Fatal("cmd = nil, want the registered onConfirm cmd")
		}
		if got := cmd(); got != (confirmSentinelMsg{}) {
			t.Errorf("cmd() = %T(%v), want confirmSentinelMsg", got, got)
		}
		if !*fired {
			t.Error("onConfirm callback never ran")
		}
	}
}

// wantCancelled asserts the cancel path: prompt closed, no cmd, and the
// callback demonstrably not run. The callback check is what stops this
// from passing against a prompt that confirmed but happened to return a
// nil cmd.
func wantCancelled(fired *bool) func(*testing.T, *App, tea.Cmd) {
	return func(t *testing.T, a *App, cmd tea.Cmd) {
		t.Helper()
		if a.confirmPrompt.IsVisible() {
			t.Error("confirm prompt still visible after a decision key")
		}
		if cmd != nil {
			t.Errorf("cmd = %v, want nil on cancel", cmd)
		}
		if *fired {
			t.Error("onConfirm callback ran on a cancel key")
		}
	}
}

func TestConfirmModeKeys(t *testing.T) {
	// Each row gets its own flag; runKeyCases runs subtests
	// sequentially (no t.Parallel anywhere in this package), so the
	// closures below cannot interleave.
	var yFired, upperYFired, enterFired, shiftEnterFired bool
	var escFired, shiftEscFired, nFired, upperNFired, otherFired bool
	var nilCbFired bool

	runKeyCases(t, ModeConfirm, []keyCase{
		{
			name:     "y confirms and returns the registered callback's cmd",
			setup:    openConfirm(&yFired),
			key:      keyPress('y'),
			wantMode: ModeNormal,
			assert:   wantConfirmed(&yFired),
		},
		{
			name:     "Y confirms",
			setup:    openConfirm(&upperYFired),
			key:      keyPress('Y'),
			wantMode: ModeNormal,
			assert:   wantConfirmed(&upperYFired),
		},
		{
			name:     "enter confirms",
			setup:    openConfirm(&enterFired),
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert:   wantConfirmed(&enterFired),
		},
		{
			// The Code switch (mode_confirm.go:21) looks redundant for
			// a bare Enter, whose String() is already "enter". Holding
			// shift is the only input that separates live normalisation
			// from dead code: Keystroke() prefixes the modifier
			// ("shift+enter"), which confirmprompt's switch does not
			// match, so without the arm this row would cancel.
			name:     "shift+enter confirms: the Code switch strips the modifier",
			setup:    openConfirm(&shiftEnterFired),
			key:      keyMod(tea.KeyEnter, tea.ModShift),
			wantMode: ModeNormal,
			assert:   wantConfirmed(&shiftEnterFired),
		},
		{
			name:     "esc cancels",
			setup:    openConfirm(&escFired),
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert:   wantCancelled(&escFired),
		},
		{
			// Escape's arm cannot be proven the same way: confirmprompt
			// has no "esc" case at all -- everything that is not
			// y/Y/enter falls into the cancel default
			// (confirmprompt/model.go:83). So "shift+esc" and "esc"
			// both cancel, and this row pins that the normalisation is
			// harmless rather than that it is load-bearing.
			//
			// BUG?: mode_confirm.go:19-20 therefore has no effect on
			// behaviour today. Recorded, not changed.
			name:     "shift+esc cancels: the esc arm is inert, the default cancels either way",
			setup:    openConfirm(&shiftEscFired),
			key:      keyMod(tea.KeyEscape, tea.ModShift),
			wantMode: ModeNormal,
			assert:   wantCancelled(&shiftEscFired),
		},
		{
			name:     "n cancels",
			setup:    openConfirm(&nFired),
			key:      keyPress('n'),
			wantMode: ModeNormal,
			assert:   wantCancelled(&nFired),
		},
		{
			name:     "N cancels",
			setup:    openConfirm(&upperNFired),
			key:      keyPress('N'),
			wantMode: ModeNormal,
			assert:   wantCancelled(&upperNFired),
		},
		{
			name:     "an unrelated printable key cancels",
			setup:    openConfirm(&otherFired),
			key:      keyPress('z'),
			wantMode: ModeNormal,
			assert:   wantCancelled(&otherFired),
		},
		{
			// A prompt opened with no follow-up action (Open's third
			// argument may be nil) confirms without a cmd. Separated
			// from the cancel rows by the prompt state, not the cmd.
			name: "y on a prompt with no callback confirms but returns no cmd",
			setup: func(t *testing.T, a *App) {
				a.confirmPrompt.Open("Really?", "", nil)
				if !a.confirmPrompt.IsVisible() {
					t.Fatal("precondition: confirm prompt is not visible")
				}
			},
			key:      keyPress('y'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.confirmPrompt.IsVisible() {
					t.Error("confirm prompt still visible after y")
				}
				if cmd != nil {
					t.Errorf("cmd = %v, want nil when onConfirm is nil", cmd)
				}
				if nilCbFired {
					t.Error("a callback ran for a prompt opened with nil")
				}
			},
		},
		{
			// ModeConfirm with a closed prompt is not a state the App
			// reaches on purpose, but the handler has no guard for it:
			// HandleKey short-circuits, IsVisible stays false, and the
			// mode is forced back to Normal. Pinning it documents that
			// the mode cannot get stuck.
			name: "a key with the prompt already hidden still drops to Normal",
			setup: func(t *testing.T, a *App) {
				if a.confirmPrompt.IsVisible() {
					t.Fatal("precondition: confirm prompt should start hidden")
				}
			},
			key:      keyPress('y'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, _ *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %v, want nil from a hidden prompt", cmd)
				}
			},
		},
	})
}
