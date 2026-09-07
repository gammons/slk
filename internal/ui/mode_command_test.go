package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func typeCommand(a *App, s string) {
	for _, r := range s {
		_ = handleCommandMode(a, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestCommandMode_TypingBuildsBufferAndPrompt(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	if a.mode != ModeCommand {
		t.Fatalf("mode = %v, want ModeCommand", a.mode)
	}
	typeCommand(a, "vsp")
	if a.cmdline != "vsp" {
		t.Fatalf("cmdline = %q, want %q", a.cmdline, "vsp")
	}
	if out := a.statusbar.View(120); !strings.Contains(out, ":vsp") {
		t.Fatalf("status bar missing prompt :vsp:\n%s", out)
	}
}

func TestCommandMode_EscapeCancels(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "ws")
	_ = handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.mode != ModeNormal {
		t.Fatalf("mode = %v, want ModeNormal", a.mode)
	}
	if a.cmdline != "" {
		t.Fatalf("cmdline = %q, want empty after cancel", a.cmdline)
	}
	if out := a.statusbar.View(120); strings.Contains(out, ":ws") {
		t.Fatalf("prompt should be cleared from status bar:\n%s", out)
	}
}

func TestCommandMode_BackspaceEditsAndCancelsAtEmpty(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "ab")
	_ = handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if a.cmdline != "a" {
		t.Fatalf("cmdline = %q, want %q", a.cmdline, "a")
	}
	_ = handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if a.cmdline != "" {
		t.Fatalf("cmdline = %q, want empty", a.cmdline)
	}
	// Backspace past the ':' cancels, like vim.
	_ = handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if a.mode != ModeNormal {
		t.Fatalf("mode = %v, want ModeNormal after backspace on empty buffer", a.mode)
	}
}

func TestCommandMode_EnterExecutesUnknownCommandToast(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "bogus")
	cmd := handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if a.mode != ModeNormal {
		t.Fatalf("mode = %v, want ModeNormal after Enter", a.mode)
	}
	if cmd == nil {
		t.Fatal("expected toast-clear cmd for unknown command")
	}
	if out := a.statusbar.View(120); !strings.Contains(out, "Unknown command: bogus") {
		t.Fatalf("expected unknown-command toast:\n%s", out)
	}
}

func TestCommandMode_EnterExecutesWS(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "ws")
	_ = handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if a.mode != ModeWorkspaceFinder {
		t.Fatalf("mode = %v, want ModeWorkspaceFinder", a.mode)
	}
}

func TestCommandMode_EnterOnEmptyJustExits(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	cmd := handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if a.mode != ModeNormal {
		t.Fatalf("mode = %v, want ModeNormal", a.mode)
	}
	if cmd != nil {
		t.Fatal("empty Enter should produce no cmd")
	}
}

func TestCommandMode_ExternalModeChangeClearsPrompt(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "ws")
	// Simulate command mode being yanked away by a global intercept
	// (e.g. ctrl+c quit-confirm) or an async reducer.
	a.SetMode(ModeConfirm)
	if a.cmdline != "" {
		t.Fatalf("cmdline = %q, want empty after external mode change", a.cmdline)
	}
	if out := a.statusbar.View(120); strings.Contains(out, ":ws") {
		t.Fatalf("stale prompt still rendered after external mode change:\n%s", out)
	}
}

func TestCommandMode_SpaceAppends(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "sp")
	_ = handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	typeCommand(a, "x")
	if a.cmdline != "sp x" {
		t.Fatalf("cmdline = %q, want %q", a.cmdline, "sp x")
	}
}

// --------------------------------------------------------------------
// Gap-filling additions (Task 17). handleCommandMode was already at
// 100% statement coverage; what was missing was the BEHAVIOUR of the
// arms whose false branches produce no statements of their own -- the
// printable filter at mode_command.go:57 and the Code switch at :36.
// Written in this file's existing style (NewApp + enterCommandMode +
// direct handler calls) rather than through runKeyCases, so the tests
// above are left untouched.
// --------------------------------------------------------------------

// TestCommandMode_NonPrintableKeysAreDropped pins the filter at
// mode_command.go:57. Every one of these stringifies to more than one
// byte, so none of them may reach the buffer: a naive `a.cmdline +=
// msg.String()` would put ":ctrl+x" or ":up" in the status bar.
func TestCommandMode_NonPrintableKeysAreDropped(t *testing.T) {
	keys := []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}},
		{"down", tea.KeyPressMsg{Code: tea.KeyDown}},
		{"tab", tea.KeyPressMsg{Code: tea.KeyTab}},
		{"ctrl+x", tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}},
		{"shift+up", tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}},
	}
	for _, k := range keys {
		t.Run(k.name, func(t *testing.T) {
			a := NewApp()
			a.enterCommandMode()
			typeCommand(a, "vs")
			if a.cmdline != "vs" {
				t.Fatalf("precondition: cmdline = %q, want %q", a.cmdline, "vs")
			}
			cmd := handleCommandMode(a, k.msg)
			if a.cmdline != "vs" {
				t.Errorf("cmdline = %q, want %q unchanged", a.cmdline, "vs")
			}
			if a.mode != ModeCommand {
				t.Errorf("mode = %v, want ModeCommand", a.mode)
			}
			if cmd != nil {
				t.Errorf("cmd = %T, want nil", cmd)
			}
		})
	}
}

// TestCommandMode_NonASCIIRuneIsDropped records that the printable
// filter is byte-based (`len(s) == 1 && s[0] >= 32 && s[0] <= 126`), so
// a multi-byte rune cannot be typed into the command line at all.
//
// BUG?: this is a real limitation rather than a deliberate reject --
// a command name with an accented character would be untypeable. It is
// also what keeps the byte-wise backspace at mode_command.go:49 from
// ever producing invalid UTF-8, so the two are coupled. Recorded, not
// changed.
func TestCommandMode_NonASCIIRuneIsDropped(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	_ = handleCommandMode(a, tea.KeyPressMsg{Code: 'é', Text: "é"})
	if a.cmdline != "" {
		t.Errorf("cmdline = %q, want empty: the filter is byte-based", a.cmdline)
	}
	if out := a.statusbar.View(120); !strings.Contains(out, ":") {
		t.Errorf("status bar lost the bare prompt:\n%s", out)
	}
}

// TestCommandMode_ModifiedSpecialKeysStillDispatch pins the difference
// between this handler and the finder-style ones: handleCommandMode
// switches on Key().Code, which ignores modifiers entirely, whereas
// mode_search.go reaches for key.Matches (i.e. msg.String()) and so
// misses a modified Enter. shift+enter therefore EXECUTES here and does
// nothing there.
func TestCommandMode_ModifiedSpecialKeysStillDispatch(t *testing.T) {
	t.Run("shift+enter executes", func(t *testing.T) {
		a := NewApp()
		a.enterCommandMode()
		typeCommand(a, "ws")
		_ = handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
		if a.mode != ModeWorkspaceFinder {
			t.Errorf("mode = %v, want ModeWorkspaceFinder", a.mode)
		}
	})
	t.Run("shift+esc cancels", func(t *testing.T) {
		a := NewApp()
		a.enterCommandMode()
		typeCommand(a, "ws")
		_ = handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyEscape, Mod: tea.ModShift})
		if a.mode != ModeNormal {
			t.Errorf("mode = %v, want ModeNormal", a.mode)
		}
		if a.cmdline != "" {
			t.Errorf("cmdline = %q, want empty", a.cmdline)
		}
	})
	t.Run("shift+backspace edits", func(t *testing.T) {
		a := NewApp()
		a.enterCommandMode()
		typeCommand(a, "ws")
		_ = handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModShift})
		if a.cmdline != "w" {
			t.Errorf("cmdline = %q, want %q", a.cmdline, "w")
		}
	})
}
