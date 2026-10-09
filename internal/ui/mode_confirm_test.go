package ui

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------
// handleConfirmMode (mode_confirm.go:16)
//
// No sub-branching of its own: it normalises Enter to the string
// confirmprompt.Model.HandleKey matches, forwards, and drops to
// ModeNormal whenever
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

// realKey builds base+mod the way bubbletea's decoder does: a shifted
// letter carries its upper-case Text and ShiftedCode, while any other
// modifier on a letter, and every modifier on a special key, leaves
// Text empty.
func realKey(base rune, mod tea.KeyMod) tea.KeyPressMsg {
	k := tea.KeyPressMsg{Code: base, Mod: mod}
	if unicode.IsLetter(base) {
		switch mod {
		case 0:
			k.Text = string(base)
		case tea.ModShift:
			k.ShiftedCode = unicode.ToUpper(base)
			k.Text = string(k.ShiftedCode)
		}
	}
	return k
}

// TestConfirmPromptModifierGrid drives every {key} x {modifier} pair
// through the real Update chain against the real quit prompt, which
// confirms by returning tea.Quit. want is the grid recorded by running
// this test on main before the prompt moved to internal/bubbles: only
// y, Y and Enter (with any modifier) confirm; ctrl+y and alt+y cancel.
func TestConfirmPromptModifierGrid(t *testing.T) {
	mods := []struct {
		name string
		mod  tea.KeyMod
	}{
		{"none", 0},
		{"shift", tea.ModShift},
		{"ctrl", tea.ModCtrl},
		{"alt", tea.ModAlt},
		{"ctrl+alt", tea.ModCtrl | tea.ModAlt},
	}
	keys := []struct {
		name string
		base rune
		want [5]bool // confirms, per mods
	}{
		{"y", 'y', [5]bool{true, true, false, false, false}},
		{"n", 'n', [5]bool{false, false, false, false, false}},
		{"enter", tea.KeyEnter, [5]bool{true, true, true, true, true}},
		{"esc", tea.KeyEscape, [5]bool{false, false, false, false, false}},
	}

	var grid strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&grid, "\n%-6s", k.name)
		for i, m := range mods {
			press := realKey(k.base, m.mod)
			a := newTestApp(t, withSize(120, 40))
			a.openQuitConfirm()
			if !a.confirmPrompt.IsVisible() || a.mode != ModeConfirm {
				t.Fatal("precondition: quit prompt is not open")
			}
			_, cmd := a.Update(press)
			got := cmd != nil && cmd() == tea.QuitMsg{}
			fmt.Fprintf(&grid, " %-9s", fmt.Sprintf("%s=%s", m.name, map[bool]string{true: "C", false: "x"}[got]))
			if got != k.want[i] {
				t.Errorf("%s (%q): confirmed = %v, want %v", press.String(), press.Text, got, k.want[i])
			}
			if a.confirmPrompt.IsVisible() || a.mode != ModeNormal {
				t.Errorf("%s: prompt should close and drop to ModeNormal", press.String())
			}
		}
	}
	t.Logf("C = confirmed, x = cancelled%s", grid.String())
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
			// The KeyEnter normalisation in handleConfirmMode looks
			// redundant for a bare Enter, whose String() is already
			// "enter". Holding shift is the only input that separates
			// live normalisation from dead code: Keystroke() prefixes the
			// modifier ("shift+enter"), which confirmprompt's switch does
			// not match, so without it this row would cancel.
			name:     "shift+enter confirms: the KeyEnter normalisation strips the modifier",
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
			// handleConfirmMode does not normalise Escape: confirmprompt
			// has no "esc" case at all -- everything that is not
			// y/Y/enter falls into its cancel default. So "shift+esc"
			// and "esc" both cancel without help from this layer.
			//
			// An Escape arm that rewrote the key to "esc" used to sit
			// beside the Enter one; it had no effect and was removed
			// (https://github.com/gammons/slk/issues/188, item 1). This
			// row is the witness that the removal was safe, and fails if
			// a modified Escape ever stops cancelling.
			name:     "shift+esc cancels: confirmprompt's default cancels without an esc arm",
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
