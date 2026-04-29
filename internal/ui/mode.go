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
)

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
	default:
		return "UNKNOWN"
	}
}
