// internal/ui/command.go
//
// The command registries: vi-style ":" commands (window-management
// design §5) and the shared dispatcher slash built-ins use
// (slash_command.go).
//
// executeCommand parses a command line (without the leading ':')
// and dispatches through the commands map — the designated
// extension point for :commands (ws, marks, sp, vsp, q, only, activity,
// huddle, help, version).
package ui

import (
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/slackurl"
	"github.com/gammons/slk/internal/ui/wintree"
)

// commandArgs is a command line after its name. Fields is the remainder
// split on whitespace, which most handlers want; Rest is the remainder
// as typed, inner whitespace and newlines intact, for a handler that
// passes text on (/msg). Origin is where a slash command was typed, for a
// built-in that may refuse and so give the text back; zero for ":" commands.
type commandArgs struct {
	Fields []string
	Rest   string
	Origin commandOrigin
}

// commandFunc executes a named command. Both registries use it: the
// vi-style ":" commands below and the slash built-ins (slashCommands).
type commandFunc func(a *App, args commandArgs) tea.Cmd

// commands maps a command name to its handler. Names are matched
// exactly (no prefix matching); aliases get their own entries.
var commands = map[string]commandFunc{
	"ws":       cmdWorkspaceFinder,
	"marks":    cmdMarks,
	"delmarks": cmdDelMarks,
	"sp":       cmdSplit,
	"vsp":      cmdVSplit,
	"q":        cmdCloseWindow,
	"only":     cmdOnlyWindow,
	"on":       cmdOnlyWindow,
	"activity": cmdActivity,
	"huddle":   cmdHuddle,
	"help":     cmdHelp,
	"version":  cmdVersion,
}

// cmdVersion toasts the running build's version and short commit (see
// version.Short), so a dev build can be told from a release without
// leaving the TUI.
func cmdVersion(a *App, _ commandArgs) tea.Cmd {
	info := a.buildInfo
	if info == "" {
		info = "slk (unknown build)"
	}
	return toastWithClear(a, info, 5*time.Second)
}

// cmdHelp opens the keybindings overlay, as ? does. Its entries list
// the ":" commands too, so this is where to look for what ":" takes.
func cmdHelp(a *App, _ commandArgs) tea.Cmd {
	a.openHelpOverlay()
	return nil
}

// cmdActivity opens the Activity view (mentions, thread replies,
// reactions to your messages, DMs). The sidebar ◉ Activity row is the
// primary entry point; this command mirrors it for discoverability.
func cmdActivity(a *App, _ commandArgs) tea.Cmd {
	a.sidebar.SelectActivityRow()
	return func() tea.Msg { return ActivityViewActivatedMsg{} }
}

// cmdHuddle copies the huddle deep link for the conversation on
// screen — https://app.slack.com/huddle/<TEAM>/<CHANNEL>, the same
// link Slack's own "Copy huddle link" produces. The link is always
// copied: opening it with no huddle active is what Slack's web client
// itself does (it shows the conversation's huddle panel), so knowing
// whether a huddle is live is not a precondition.
//
// Which conversation "on screen" means, in priority order: a focused
// thread pane names its own parent channel (the Threads view opens
// threads without moving a.activeChannelID); in the Threads or
// Activity view, the highlighted row's channel (there PanelMessages
// focus is the list's convention, and the active channel is stale);
// otherwise the active channel. An empty answer — no pane, no
// highlighted row, no active channel — refuses. The H key calls this
// same handler.
func cmdHuddle(a *App, _ commandArgs) tea.Cmd {
	var channelID string
	switch {
	case a.focusedPanel == PanelThread:
		channelID = a.threadPanel.ChannelID()
	case a.view == ViewThreads:
		if sum, ok := a.threadsView.SelectedSummary(); ok {
			channelID = sum.ChannelID
		}
	case a.view == ViewActivity:
		if it, ok := a.activityView.SelectedItem(); ok {
			channelID = it.ChannelID
		}
	default:
		channelID = a.activeChannelID
	}
	if channelID == "" {
		return toastWithClear(a, "No conversation open", 2*time.Second)
	}
	url := slackurl.HuddleURL(a.activeTeamID, channelID)
	return tea.Batch(a.clipboardWrite(url), toastWithClear(a, "Copied huddle link", 2*time.Second))
}

// cmdSplit / cmdVSplit create a stacked / side-by-side split of the
// focused window (window-management design §5).
func cmdSplit(a *App, _ commandArgs) tea.Cmd  { return a.splitWindow(wintree.SplitStacked) }
func cmdVSplit(a *App, _ commandArgs) tea.Cmd { return a.splitWindow(wintree.SplitSideBySide) }

// cmdCloseWindow closes the focused window (never quits the app).
func cmdCloseWindow(a *App, _ commandArgs) tea.Cmd { return a.closeWindow() }

// cmdOnlyWindow closes all other windows.
func cmdOnlyWindow(a *App, _ commandArgs) tea.Cmd {
	a.onlyWindow()
	return nil
}

// cmdWorkspaceFinder opens the workspace finder overlay —
// the :command replacement for the finder's old ctrl+w binding.
func cmdWorkspaceFinder(a *App, _ commandArgs) tea.Cmd {
	a.workspaceFinder.Open()
	a.SetMode(ModeWorkspaceFinder)
	return nil
}

// cmdMarks opens the marks overlay. Unlike the ' chord it ALWAYS shows
// the overlay — the show_jump_overlay option suppresses the overlay on
// the jump key only, never the list-marks command.
func cmdMarks(a *App, _ commandArgs) tea.Cmd {
	cmd := a.openMarksOverlay()
	a.SetMode(ModeMarks)
	return cmd
}

// cmdDelMarks deletes marks by letter. Each whitespace-separated token
// is a run of mark letters, so both `:delmarks abc` and `:delmarks a
// b c` name the same marks (consistent with the layer's
// strings.Fields tokenization). Non-letters are ignored; deleting an
// unset letter is a harmless no-op. Uses the same marksStore.Delete the
// overlay's row delete action calls, so the two surfaces cannot
// diverge.
func cmdDelMarks(a *App, args commandArgs) tea.Cmd {
	var failed string
	for _, arg := range args.Fields {
		for i := 0; i < len(arg); i++ {
			if isMarkLetter(arg[i]) {
				if err := a.marks.Delete(a.activeTeamID, string(arg[i])); err != nil {
					failed += string(arg[i])
				}
			}
		}
	}
	if failed != "" {
		return toastWithClear(a, "Failed to delete saved mark "+failed, 3*time.Second)
	}
	return nil
}

// dispatchCommand runs line's command from table: its first
// whitespace-separated token names the command, the rest are its args.
// unknown runs when table has no such name. Empty input is a no-op.
func dispatchCommand(a *App, table map[string]commandFunc, line string,
	unknown func(a *App, name string, args commandArgs) tea.Cmd) tea.Cmd {
	name, args, ok := splitCommandLine(line)
	if !ok {
		return nil
	}
	return runCommand(a, table, name, args, unknown)
}

// splitCommandLine splits line into its command name and args. ok is
// false for an empty line.
func splitCommandLine(line string) (name string, args commandArgs, ok bool) {
	trimmed := strings.TrimLeftFunc(line, unicode.IsSpace)
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", commandArgs{}, false
	}
	name = fields[0]
	return name, commandArgs{
		Fields: fields[1:],
		Rest:   strings.TrimLeftFunc(trimmed[len(name):], unicode.IsSpace),
	}, true
}

// runCommand calls name's entry in table, or unknown when it has none.
func runCommand(a *App, table map[string]commandFunc, name string, args commandArgs,
	unknown func(a *App, name string, args commandArgs) tea.Cmd) tea.Cmd {
	if fn, ok := table[name]; ok {
		return fn(a, args)
	}
	return unknown(a, name, args)
}

// executeCommand parses and runs one ":" command line (without the
// leading ':'). Empty input is a no-op; unknown commands show a
// transient status-bar toast.
func executeCommand(a *App, line string) tea.Cmd {
	return dispatchCommand(a, commands, line, func(a *App, name string, _ commandArgs) tea.Cmd {
		return toastWithClear(a, "Unknown command: "+name, 2*time.Second)
	})
}
