// internal/ui/mode_command.go
//
// Command-mode key handler: the vi-style ":" prompt
// (window-management design §5).
//
// Entry: enterCommandMode (bound to ':' in normal mode). Printable
// keys append to App.cmdline, mirrored into the status bar's
// command-line slot. Backspace edits — and cancels when the buffer
// is already empty, matching vim's backspace-past-':' behavior.
// Esc cancels. Enter executes the line through the command
// registry (internal/ui/command.go) after returning to Normal, so
// commands that set their own mode (e.g. :ws) win.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

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
	a.cmdline = ""
	a.statusbar.SetCommandLine(":")
	a.SetMode(ModeCommand)
}

// exitCommandMode clears the buffer and prompt and returns to
// Normal. Shared by the cancel and execute paths.
func (a *App) exitCommandMode() {
	a.cmdline = ""
	a.statusbar.SetCommandLine("")
	a.SetMode(ModeNormal)
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
	case tea.KeyBackspace:
		if a.cmdline == "" {
			a.exitCommandMode()
			return nil
		}
		a.cmdline = a.cmdline[:len(a.cmdline)-1]
		a.statusbar.SetCommandLine(":" + a.cmdline)
		return nil
	}
	s := msg.String()
	if s == "space" {
		s = " "
	}
	if len(s) == 1 && s[0] >= 32 && s[0] <= 126 {
		a.cmdline += s
		a.statusbar.SetCommandLine(":" + a.cmdline)
	}
	return nil
}
