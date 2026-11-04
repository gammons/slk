// internal/ui/mode_command.go
//
// Command-mode key handler: the vi-style ":" prompt
// (window-management design §5).
//
// Entry: enterCommandMode (bound to ':' in normal mode). Printable
// keys append to App.cmdline, mirrored into the status bar's
// command-line slot. Backspace edits — and cancels when the buffer
// is already empty, matching vim's backspace-past-':' behavior.
// Tab completes the command name from the registry
// (internal/ui/command.go), cycling through the matches as it is
// pressed again; Shift+Tab cycles backwards. Esc cancels. Enter
// executes the line through the same registry after returning to
// Normal, so commands that set their own mode (e.g. :ws) win.
package ui

import (
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// cmdCompletion is the state of an in-progress Tab completion at the
// ':' prompt: matches is the sorted list of command names the buffer
// matched when the cycle started, and idx is the candidate currently
// sitting in cmdline. Only a Tab press that found the buffer still on
// its candidate continues the cycle; every other key that touches the
// buffer, and every Tab that cannot cycle, resets this to its zero
// value, so a stale list can never be matched against a newer line.
type cmdCompletion struct {
	matches []string
	idx     int
}

// beginInsertMode enters insert mode with a compose box focused - the
// shared body of `i` and of a click on a compose region. toThread picks
// which compose: the thread panel's when true, else the channel's.
// composeDisabled reflects the active CHANNEL's send capability, not
// the thread panel's -- a thread reply targets whatever thread is
// open, which need not be in that channel.
func (a *App) beginInsertMode(toThread bool) tea.Cmd {
	if a.composeDisabled && !toThread {
		return toastWithClear(a, a.composeBlockedText(), 2*time.Second)
	}
	a.SetMode(ModeInsert)
	if toThread {
		a.focusedPanel = PanelThread
		return a.threadCompose.Focus()
	}
	a.focusedPanel = PanelMessages
	return a.compose.Focus()
}

// enterCommandMode switches to ModeCommand with an empty buffer
// and shows the bare ':' prompt in the status bar.
func (a *App) enterCommandMode() {
	a.setCmdline("")
	a.SetMode(ModeCommand)
}

// exitCommandMode clears the buffer and prompt and returns to
// Normal. Shared by the cancel and execute paths.
func (a *App) exitCommandMode() {
	a.clearCmdline()
	a.SetMode(ModeNormal)
}

// setCmdline replaces the command line and mirrors it into the status
// bar, so the prompt and the buffer cannot disagree. It also drops any
// Tab cycle, and with it the completion list the cycle was showing: the
// line they were matched against is gone.
func (a *App) setCmdline(line string) {
	a.cmdline = line
	a.cmdComplete = cmdCompletion{}
	a.statusbar.SetCommandLine(":" + line)
	a.statusbar.SetCommandCandidates(nil, 0)
}

// clearCmdline drops the command line and the prompt with it; leaving
// ModeCommand by any path goes through here, so the status bar cannot
// keep rendering a stale ':' line or a stale completion list.
func (a *App) clearCmdline() {
	a.cmdline = ""
	a.cmdComplete = cmdCompletion{}
	a.statusbar.SetCommandLine("")
	a.statusbar.SetCommandCandidates(nil, 0)
}

// matchingCommands returns the sorted names in the commands registry
// that start with prefix; every name for an empty prefix.
func matchingCommands(prefix string) []string {
	var matches []string
	for name := range commands {
		if strings.HasPrefix(name, prefix) {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	return matches
}

// completeCommandName runs Tab (backwards for Shift+Tab) at the prompt:
// it puts the next command name matching the buffer into cmdline,
// wrapping around the match list, and remembers the list so the next
// Tab advances instead of restarting. A buffer holding a space names a
// command's arguments, which are out of scope, and a Tab with no match
// completes nothing; both only reset the cycle.
func completeCommandName(a *App, backwards bool) {
	if strings.Contains(a.cmdline, " ") {
		a.cmdComplete = cmdCompletion{}
		a.statusbar.SetCommandCandidates(nil, 0)
		return
	}
	if c := a.cmdComplete; c.idx < len(c.matches) && c.matches[c.idx] == a.cmdline {
		next := c.idx + 1
		if backwards {
			next = c.idx - 1 + len(c.matches)
		}
		next %= len(c.matches)
		a.setCmdline(c.matches[next])
		a.cmdComplete = cmdCompletion{matches: c.matches, idx: next}
		a.showCommandCandidates(c.matches, next)
		return
	}
	matches := matchingCommands(a.cmdline)
	if len(matches) == 0 {
		a.cmdComplete = cmdCompletion{}
		a.statusbar.SetCommandCandidates(nil, 0)
		return
	}
	idx := 0
	if backwards {
		idx = len(matches) - 1
	}
	a.setCmdline(matches[idx])
	a.cmdComplete = cmdCompletion{matches: matches, idx: idx}
	a.showCommandCandidates(matches, idx)
}

// showCommandCandidates mirrors a fresh match list into the status bar's
// wildmenu. A single match adds nothing to the prompt -- the name it
// completed is already on screen -- so only a real choice is shown.
func (a *App) showCommandCandidates(matches []string, idx int) {
	if len(matches) < 2 {
		a.statusbar.SetCommandCandidates(nil, 0)
		return
	}
	a.statusbar.SetCommandCandidates(matches, idx)
}

func handleCommandMode(a *App, msg tea.KeyMsg) tea.Cmd {
	switch msg.Key().Code {
	case tea.KeyEscape:
		a.exitCommandMode()
		return nil
	case tea.KeyEnter:
		line := a.cmdline
		a.exitCommandMode()
		return executeCommand(a, line)
	case tea.KeyTab:
		// Completion ignores the shift state in key dispatch
		// everywhere else in this file, so Shift+Tab reaches this
		// arm too; the modifier only reverses the cycle.
		completeCommandName(a, msg.Key().Mod.Contains(tea.ModShift))
		return nil
	case tea.KeyBackspace:
		if a.cmdline == "" {
			a.exitCommandMode()
			return nil
		}
		_, n := utf8.DecodeLastRuneInString(a.cmdline)
		a.setCmdline(a.cmdline[:len(a.cmdline)-n])
		return nil
	}
	s := msg.String()
	if s == "space" {
		s = " "
	}
	if r := []rune(s); len(r) == 1 && unicode.IsPrint(r[0]) {
		a.setCmdline(a.cmdline + s)
	}
	return nil
}
