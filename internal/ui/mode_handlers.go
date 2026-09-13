// internal/ui/mode_handlers.go
//
// Phase 5 of the SOLID refactor of internal/ui/app.go: opens the
// per-mode key-dispatch in App.handleKey to extension via a
// registration table.
//
// Each mode owns one handler function with the signature:
//
//	func(a *App, msg tea.KeyMsg) tea.Cmd
//
// modeHandlers maps each Mode to its handler. handleKey looks up
// a.mode in the table; if there's no entry, the Normal-mode
// handler runs (mirrors the pre-Phase-5 switch's `default:` arm).
//
// Why a map rather than a switch:
//   - Adding/removing a mode is a one-line edit to the map plus
//     a new mode_*.go file, not a switch arm in handleKey.
//   - Mode handlers can be referenced as values (e.g. for tests
//     that want to invoke a handler directly without going
//     through Mode lookup).
//   - Mirrors the Phase 4 dispatchReducers / reducerFunc pattern.
//
// Why method values for the initial population:
//   - The existing handle*Mode methods on App have the right
//     receiver-and-arg shape; Go's method-value syntax
//     (*App).handleXxxMode produces a func(*App, tea.KeyMsg) tea.Cmd
//     without any per-call indirection beyond a method dispatch.
//   - Phases 5b-5l migrate each method body into a per-mode
//     mode_*.go file as a free function and swap the map entry;
//     the dispatcher contract is unchanged.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

// modeHandler is the signature shared by every per-mode key
// handler. The receiver-style first argument keeps the handlers
// readable inside per-mode files (the `a *App` parameter reads as
// "this mode operates on App `a`").
type modeHandler func(a *App, msg tea.KeyMsg) tea.Cmd

// modeHandlers is the per-Mode dispatch table consulted by
// App.handleKey. A missing entry falls back to the Normal handler
// (mirrors the pre-Phase-5 `default:` arm).
//
// Initial population uses method values for handlers that still
// live on App; Phases 5b-5l replace each entry with a free
// function as the body moves to its own file.
var modeHandlers = map[Mode]modeHandler{
	ModeNormal:               handleNormalMode,
	ModeInsert:               handleInsertMode,
	ModeCommand:              handleCommandMode,
	ModeSearch:               handleSearchMode,
	ModeChannelFinder:        handleChannelFinderMode,
	ModeNewMessage:           handleNewMessageMode,
	ModeReactionPicker:       handleReactionPickerMode,
	ModeConfirm:              handleConfirmMode,
	ModeWorkspaceFinder:      handleWorkspaceFinderMode,
	ModeThemeSwitcher:        handleThemeSwitcherMode,
	ModePresenceMenu:         handlePresenceMenuMode,
	ModePresenceCustomSnooze: handlePresenceCustomSnoozeMode,
	ModeHelp:                 handleHelpMode,
	ModeReactionsView:        handleReactionsViewMode,
	ModeLinkPicker:           handleLinkPickerMode,
	ModeWorkspaceSearch:      handleWorkspaceSearchMode,
	ModeVisual:               handleVisualMode,
}

// normalizeFinderKey maps a tea.KeyMsg to the plain-string form the
// finder-style prompt widgets consume ("enter", "esc", "up", "down",
// "backspace", or the key's String() for everything else). Shared by
// the channel-finder, in-channel search, and workspace-search modes.
func normalizeFinderKey(msg tea.KeyMsg) string {
	switch msg.Key().Code {
	case tea.KeyEnter:
		return "enter"
	case tea.KeyEscape:
		return "esc"
	case tea.KeyUp:
		return "up"
	case tea.KeyDown:
		return "down"
	case tea.KeyBackspace:
		return "backspace"
	}
	return msg.String()
}

// dispatchModeKey looks up the handler for the App's current
// mode and invokes it; falls back to the Normal handler when no
// entry exists. Returns the handler's tea.Cmd unchanged.
func dispatchModeKey(a *App, msg tea.KeyMsg) tea.Cmd {
	if h, ok := modeHandlers[a.mode]; ok {
		return h(a, msg)
	}
	return handleNormalMode(a, msg)
}

// Compile-time assertion: the free-function form of every
// per-mode handler satisfies the modeHandler signature. If a
// future change to a handler's signature drifts, the map literal
// would still compile but this single anchor catches it. (One
// assertion is enough; the map values themselves are already
// type-checked against modeHandler.)
var _ modeHandler = handleNormalMode
