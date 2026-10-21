package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
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
// mode_command.go's printable check. Every one of these stringifies to
// more than one byte, so none of them may reach the buffer: a naive
// `a.cmdline += msg.String()` would put ":ctrl+x" or ":up" in the
// status bar.
//
// Tab is deliberately absent: it has its own arm in handleCommandMode
// (name completion, mode_command.go) and is covered by
// TestCommandMode_TabCompletesName and its neighbours.
func TestCommandMode_NonPrintableKeysAreDropped(t *testing.T) {
	keys := []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}},
		{"down", tea.KeyPressMsg{Code: tea.KeyDown}},
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
// changed. Tracked as https://github.com/gammons/slk/issues/187,
// together with the identical filter in channelfinder.
//
// WHEN THAT BUG IS FIXED: the rune is accepted, so this test must be
// re-pinned to assert a.cmdline == "é" and renamed accordingly.
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

// --------------------------------------------------------------------
// Tab completion of the command name (mode_command.go).
// --------------------------------------------------------------------

// cmdCompletionZero reports whether no Tab cycle is in progress. The
// struct holds a slice, so it cannot be compared with ==.
func cmdCompletionZero(c cmdCompletion) bool {
	return c.matches == nil && c.idx == 0
}

// pressTab sends Tab to the command-mode handler; shift reverses the
// cycle. handleCommandMode dispatches on Key().Code alone, so Shift+Tab
// reaches the same arm and the modifier is read inside it.
func pressTab(a *App, shift bool) {
	mod := tea.KeyMod(0)
	if shift {
		mod = tea.ModShift
	}
	_ = handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyTab, Mod: mod})
}

// assertPrompt checks both halves of the completion contract: the buffer
// and the status bar's mirror of it.
func assertPrompt(t *testing.T, a *App, want string) {
	t.Helper()
	if a.cmdline != want {
		t.Errorf("cmdline = %q, want %q", a.cmdline, want)
	}
	if out := a.statusbar.View(120); !strings.Contains(out, ":"+want) {
		t.Errorf("status bar does not show :%s:\n%s", want, out)
	}
}

// afterPrompt returns the stripped status-bar text from just after the prompt
// for line (the ":line▌" segment) to the end, so a test can assert on what the
// bar puts beside it.
func afterPrompt(plain, line string) (string, bool) {
	i := strings.Index(plain, ":"+line+"▌")
	if i < 0 {
		return "", false
	}
	return plain[i+len(":"+line+"▌"):], true
}

// tabStep is one Tab press in a completion walk: shift selects
// Shift+Tab, want is the command line the prompt must hold afterwards.
type tabStep struct {
	shift bool
	want  string
}

// TestCommandMode_TabCompletesName walks the completion cycles the task
// specifies. Each step is one Tab; want is the buffer afterwards.
func TestCommandMode_TabCompletesName(t *testing.T) {
	cases := []struct {
		name  string
		typed string
		steps []tabStep
	}{
		{
			// One match: Tab completes it and further Tabs keep it.
			name:  "single match completes and stays",
			typed: "act",
			steps: []tabStep{{false, "activity"}, {false, "activity"}},
		},
		{
			// Two matches: forward cycle, wrapping last to first.
			name:  "tab cycles forward and wraps",
			typed: "o",
			steps: []tabStep{{false, "on"}, {false, "only"}, {false, "on"}},
		},
		{
			// Shift+Tab on a fresh line goes straight to the last match.
			name:  "shift+tab starts at the last match",
			typed: "o",
			steps: []tabStep{{true, "only"}},
		},
		{
			// Empty line matches every name; sorted order decides.
			name:  "empty buffer starts at the first name forward",
			typed: "",
			steps: []tabStep{{false, "activity"}},
		},
		{
			name:  "empty buffer starts at the last name backwards",
			typed: "",
			steps: []tabStep{{true, "ws"}},
		},
		{
			// Wrapping needs a list longer than two to tell apart.
			name:  "backwards wraps first to last",
			typed: "",
			steps: []tabStep{{true, "ws"}, {false, "activity"}, {true, "ws"}},
		},
		{
			name:  "no match leaves the line alone",
			typed: "xyz",
			steps: []tabStep{{false, "xyz"}, {true, "xyz"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := NewApp()
			a.enterCommandMode()
			typeCommand(a, tc.typed)
			for i, s := range tc.steps {
				pressTab(a, s.shift)
				if a.cmdline != s.want {
					t.Fatalf("step %d: cmdline = %q, want %q", i, a.cmdline, s.want)
				}
				assertPrompt(t, a, s.want)
			}
		})
	}
}

// TestCommandMode_TabThroughAppUpdate drives completion the way a user
// does, every key through App.Update from normal mode, so a key-routing
// change that never lets Tab reach the prompt fails here too. After each
// step it checks the list beside the prompt and which name is highlighted.
func TestCommandMode_TabThroughAppUpdate(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
	a := NewApp()
	for _, r := range ":o" {
		a.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if a.mode != ModeCommand {
		t.Fatalf("mode = %v, want ModeCommand", a.mode)
	}
	for _, s := range []tabStep{{false, "on"}, {false, "only"}, {true, "on"}} {
		mod := tea.KeyMod(0)
		if s.shift {
			mod = tea.ModShift
		}
		a.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: mod})
		assertPrompt(t, a, s.want)

		out := a.statusbar.View(120)
		if rest, ok := afterPrompt(ansi.Strip(out), s.want); !ok || !strings.HasPrefix(rest, "  on  only") {
			t.Errorf("after Tab to %s, the candidates should follow the prompt:\n%s", s.want, ansi.Strip(out))
		}
		other := map[string]string{"on": "only", "only": "on"}[s.want]
		if !strings.Contains(out, styles.SelectionStyle().Render(s.want)) ||
			strings.Contains(out, styles.SelectionStyle().Render(other)) {
			t.Errorf("after Tab to %s, only %s should be highlighted:\n%q", s.want, s.want, out)
		}
	}
}

// TestCommandMode_TabCycleRestartsAfterAnEdit pins that an edit drops
// the remembered cycle. Tab completes ":o" to "on"; Backspace and
// retyping return the line to "on" exactly, so a retained cycle would
// advance to "only" -- only dropping it at edit time yields the fresh
// first match, "on".
func TestCommandMode_TabCycleRestartsAfterAnEdit(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "o")
	pressTab(a, false)
	assertPrompt(t, a, "on")
	_ = handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyBackspace})
	typeCommand(a, "n")
	if a.cmdline != "on" {
		t.Fatalf("cmdline = %q, want %q", a.cmdline, "on")
	}
	pressTab(a, false)
	assertPrompt(t, a, "on")
	// The cycle restarted here, so the next Tab advances from "on".
	pressTab(a, false)
	assertPrompt(t, a, "only")
}

// TestCommandMode_TabCompletionThenTypingNarrowsTheMatch is the task's
// "type into the completed line" case: the completed buffer keeps
// matching as more is typed, and Tab completes the newer prefix.
func TestCommandMode_TabCompletionThenTypingNarrowsTheMatch(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "o")
	pressTab(a, false)
	assertPrompt(t, a, "on")
	typeCommand(a, "l")
	if a.cmdline != "onl" {
		t.Fatalf("cmdline = %q, want %q", a.cmdline, "onl")
	}
	pressTab(a, false)
	assertPrompt(t, a, "only")
}

// TestCommandMode_TabIgnoresArguments records the scope boundary: a
// space means the line names arguments, which this task does not
// complete, so Tab is a no-op and the prompt keeps the typed text.
func TestCommandMode_TabIgnoresArguments(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "sp #gen")
	pressTab(a, false)
	assertPrompt(t, a, "sp #gen")
	pressTab(a, true)
	assertPrompt(t, a, "sp #gen")
}

// TestCommandMode_TabStateClearedOnModeChange keeps the completion
// field's "zero outside a Tab cycle" invariant honest: leaving command
// mode by any path, including a global intercept that calls SetMode
// directly, must not leave a match list behind for the next prompt.
func TestCommandMode_TabStateClearedOnModeChange(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "o")
	pressTab(a, false)
	if a.cmdComplete.matches == nil {
		t.Fatal("precondition: a Tab cycle should be in progress")
	}
	a.SetMode(ModeNormal)
	if a.cmdline != "" {
		t.Errorf("cmdline = %q, want empty", a.cmdline)
	}
	if !cmdCompletionZero(a.cmdComplete) {
		t.Errorf("cmdComplete = %+v, want the zero value", a.cmdComplete)
	}
	// And entering the prompt again starts clean.
	a.enterCommandMode()
	if !cmdCompletionZero(a.cmdComplete) {
		t.Errorf("cmdComplete = %+v after re-entering, want the zero value", a.cmdComplete)
	}
}

// TestCommandMode_TabShowsCandidatesInTheStatusBar is the task's wildmenu
// case: Tab at ":o" completes to "on" and puts the whole match list beside
// the prompt, in match order, so the user can see what the cycle will visit.
func TestCommandMode_TabShowsCandidatesInTheStatusBar(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "o")
	pressTab(a, false)
	assertPrompt(t, a, "on")

	plain := ansi.Strip(a.statusbar.View(120))
	rest, ok := afterPrompt(plain, "on")
	if !ok {
		t.Fatalf("prompt :on▌ missing from the bar:\n%s", plain)
	}
	if !strings.Contains(rest, "on  only") {
		t.Fatalf("candidates not listed in match order with the two-space gap after the prompt:\n%s", plain)
	}
}

// TestCommandMode_TypingDropsCandidates pins that the list belongs to the
// line the Tab was pressed on: any edit drops it with the cycle, so the bar
// cannot keep advertising matches for a prefix that is gone.
func TestCommandMode_TypingDropsCandidates(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "o")
	pressTab(a, false)
	if out := ansi.Strip(a.statusbar.View(120)); !strings.Contains(out, "only") {
		t.Fatalf("precondition: candidates should be listed:\n%s", out)
	}

	typeCommand(a, "l")
	assertPrompt(t, a, "onl")
	if out := ansi.Strip(a.statusbar.View(120)); strings.Contains(out, "only") {
		t.Fatalf("candidates survived an edit to the prompt:\n%s", out)
	}
}

// TestCommandMode_SingleMatchShowsNoCandidateList pins the "more than one
// match" rule: ":act" completes to the only name it could be, and repeating
// it inside the prompt would add nothing.
func TestCommandMode_SingleMatchShowsNoCandidateList(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "act")
	pressTab(a, false)
	assertPrompt(t, a, "activity")

	if n := strings.Count(ansi.Strip(a.statusbar.View(120)), "activity"); n != 1 {
		t.Fatalf("single-match completion shows the name %d times, want once:\n%s",
			n, ansi.Strip(a.statusbar.View(120)))
	}
}

// TestCommandMode_CandidatesClearedOnExit keeps the bar's wildmenu honest
// across prompts: leaving command mode must drop the list with the prompt,
// so re-entering does not show the previous command's matches.
func TestCommandMode_CandidatesClearedOnExit(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "o")
	pressTab(a, false)
	if out := ansi.Strip(a.statusbar.View(120)); !strings.Contains(out, "only") {
		t.Fatalf("precondition: candidates should be listed:\n%s", out)
	}

	_ = handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyEscape})
	a.enterCommandMode()
	if out := ansi.Strip(a.statusbar.View(120)); strings.Contains(out, "only") {
		t.Fatalf("candidates survived exiting and re-entering command mode:\n%s", out)
	}
}

// TestCommandMode_TabThenEnterRunsCompletedCommand proves the completed
// line is what executes: `:act` Tab Enter must run :activity exactly as
// typing the name out would.
func TestCommandMode_TabThenEnterRunsCompletedCommand(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "act")
	pressTab(a, false)
	assertPrompt(t, a, "activity")
	cmd := handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if a.mode != ModeNormal {
		t.Fatalf("mode = %v, want ModeNormal after Enter", a.mode)
	}
	if msgs := deliveredMsgs[ActivityViewActivatedMsg](t, cmd); len(msgs) != 1 {
		t.Fatalf("completed :activity delivered %d messages, want 1", len(msgs))
	}
}
