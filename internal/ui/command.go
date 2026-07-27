// internal/ui/command.go
//
// The vi-style ":" command registry (window-management design §5).
//
// executeCommand parses a command line (without the leading ':')
// and dispatches through the commands map — the designated
// extension point for future :commands. Later phases of the
// window-management plan register sp / vsp / q / only here.
package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/wintree"
)

// commandFunc executes a named :command. args holds the
// whitespace-separated tokens after the command name (unused by
// v1 commands, reserved for e.g. ":sp #channel").
type commandFunc func(a *App, args []string) tea.Cmd

// commands maps a command name to its handler. Names are matched
// exactly (no prefix matching); aliases get their own entries.
var commands = map[string]commandFunc{
	"ws":       cmdWorkspaceFinder,
	"sp":       cmdSplit,
	"vsp":      cmdVSplit,
	"q":        cmdCloseWindow,
	"only":     cmdOnlyWindow,
	"on":       cmdOnlyWindow,
	"activity": cmdActivity,
}

// cmdActivity opens the Activity view (mentions, thread replies,
// reactions to your messages, DMs). The sidebar 🔔 Activity row is the
// primary entry point; this command mirrors it for discoverability.
func cmdActivity(a *App, _ []string) tea.Cmd {
	a.sidebar.SelectActivityRow()
	return func() tea.Msg { return ActivityViewActivatedMsg{} }
}

// cmdSplit / cmdVSplit create a stacked / side-by-side split of the
// focused window (window-management design §5).
func cmdSplit(a *App, _ []string) tea.Cmd  { return a.splitWindow(wintree.SplitStacked) }
func cmdVSplit(a *App, _ []string) tea.Cmd { return a.splitWindow(wintree.SplitSideBySide) }

// cmdCloseWindow closes the focused window (never quits the app).
func cmdCloseWindow(a *App, _ []string) tea.Cmd { return a.closeWindow() }

// cmdOnlyWindow closes all other windows.
func cmdOnlyWindow(a *App, _ []string) tea.Cmd {
	a.onlyWindow()
	return nil
}

// cmdWorkspaceFinder opens the workspace finder overlay —
// the :command replacement for the finder's old ctrl+w binding.
func cmdWorkspaceFinder(a *App, _ []string) tea.Cmd {
	a.workspaceFinder.Open()
	a.SetMode(ModeWorkspaceFinder)
	return nil
}

// executeCommand parses and runs one command line (without the
// leading ':'). Empty input is a no-op; unknown commands show a
// transient status-bar toast.
func executeCommand(a *App, line string) tea.Cmd {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}
	fn, ok := commands[fields[0]]
	if !ok {
		return toastWithClear(a, "Unknown command: "+fields[0], 2*time.Second)
	}
	return fn(a, fields[1:])
}
