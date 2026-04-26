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
	default:
		return "UNKNOWN"
	}
}
