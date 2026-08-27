// internal/ui/mode.go
package ui

type Mode int

const (
	ModeNormal Mode = iota
	ModeInsert
	ModeCommand
	ModeSearch
	ModeChannelFinder
	ModeReactionPicker
	ModeWorkspaceFinder
	ModeThemeSwitcher
	ModePresenceMenu
	ModePresenceCustomSnooze
	ModeConfirm
	ModeHelp
	ModeNewMessage
	ModeReactionsView
	ModeLinkPicker
	ModeWorkspaceSearch
	ModeVisual
)

// IsModalOverlay reports whether the mode is a full-screen modal
// overlay that captures input (channel finder, pickers, help,
// confirm, etc.), as opposed to the inline modes (normal, insert,
// command, search) where the main tab stays interactive.
//
// Used by the mouse-wheel router to decide whether a wheel notch
// scrolls the items inside the modal rather than the panel under
// the cursor on the main tab behind it.
func (m Mode) IsModalOverlay() bool {
	switch m {
	case ModeChannelFinder,
		ModeReactionPicker,
		ModeWorkspaceFinder,
		ModeThemeSwitcher,
		ModePresenceMenu,
		ModePresenceCustomSnooze,
		ModeConfirm,
		ModeHelp,
		ModeNewMessage,
		ModeReactionsView,
		ModeLinkPicker,
		ModeWorkspaceSearch:
		return true
	default:
		return false
	}
}

func (m Mode) String() string {
	switch m {
	case ModeNormal:
		return "NORMAL"
	case ModeInsert:
		return "INSERT"
	case ModeCommand:
		return "COMMAND"
	case ModeSearch:
		return "SEARCH"
	case ModeChannelFinder:
		return "FIND"
	case ModeReactionPicker:
		return "REACT"
	case ModeWorkspaceFinder:
		return "WORKSPACE"
	case ModeThemeSwitcher:
		return "THEME"
	case ModePresenceMenu:
		return "STATUS"
	case ModePresenceCustomSnooze:
		return "STATUS-INPUT"
	case ModeConfirm:
		return "CONFIRM"
	case ModeHelp:
		return "HELP"
	case ModeNewMessage:
		return "NEW MSG"
	case ModeReactionsView:
		return "REACTIONS"
	case ModeLinkPicker:
		return "LINKS"
	case ModeWorkspaceSearch:
		return "WS-SEARCH"
	case ModeVisual:
		return "VISUAL"
	default:
		return "UNKNOWN"
	}
}
