// internal/ui/reducer_modal_click.go
//
// Mouse-click routing for active modal overlays.
//
// When a modal overlay owns the screen (a.mode.IsModalOverlay()), a
// left click is interpreted relative to the modal's centered box rather
// than the main-tab panels behind it:
//
//   - Click OUTSIDE the box        -> dismiss the modal (synthesised Esc,
//     reusing each mode handler's esc path:
//     close + restore mode + any cleanup).
//   - Click ON a list row          -> move the modal's selection there and
//     synthesise the modal's activation key
//     (Enter to choose, Space to toggle for
//     the multi-select new-message picker;
//     help has no activation).
//   - Click INSIDE but not a row   -> consumed, no-op (never leaks to the
//     main tab).
//
// Geometry comes from each modal's BoxSize, mirroring the centering done
// by overlay.DimmedOverlay so the hit-test lines up with what was drawn.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

// boxedOverlay is implemented by every modal overlay: it reports the
// outer dimensions of its centered box so the router can detect clicks
// that fall outside it.
type boxedOverlay interface {
	BoxSize(termWidth, termHeight int) (int, int)
}

// clickableOverlay is a boxedOverlay whose contents include a selectable
// list. ClickRow moves the selection to the row at box-local localY and
// reports whether a row was actually hit.
type clickableOverlay interface {
	boxedOverlay
	ClickRow(termWidth, termHeight, localY int) bool
}

// modalClickTarget bundles the active modal's geometry source, its
// optional list-hit-testing, and the key to synthesise when a row is
// clicked.
type modalClickTarget struct {
	box        boxedOverlay
	click      clickableOverlay // nil for non-list modals (e.g. confirm)
	activation tea.KeyMsg       // nil when a row click has no activation (help)
}

// activeModalClickTarget resolves the modal addressed by the current
// mode. ok is false for modal modes that have no resolvable box (e.g.
// the custom-snooze numeric input), in which case any click dismisses.
func (a *App) activeModalClickTarget() (modalClickTarget, bool) {
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	space := tea.KeyPressMsg{Code: tea.KeySpace}
	switch a.mode {
	case ModeChannelFinder:
		return modalClickTarget{&a.channelFinder, &a.channelFinder, enter}, true
	case ModeWorkspaceSearch:
		return modalClickTarget{&a.searchResults, &a.searchResults, enter}, true
	case ModeWorkspaceFinder:
		return modalClickTarget{&a.workspaceFinder, &a.workspaceFinder, enter}, true
	case ModeThemeSwitcher:
		return modalClickTarget{&a.themeSwitcher, &a.themeSwitcher, enter}, true
	case ModePresenceMenu:
		return modalClickTarget{&a.presenceMenu, &a.presenceMenu, enter}, true
	case ModeReactionPicker:
		return modalClickTarget{a.reactionPicker, a.reactionPicker, enter}, true
	case ModeNewMessage:
		return modalClickTarget{&a.newMessagePicker, &a.newMessagePicker, space}, true
	case ModeHelp:
		// Help has no activation: clicking a row only moves the highlight.
		return modalClickTarget{&a.help, &a.help, nil}, true
	case ModeConfirm:
		// Confirm has no list: outside dismisses, inside is a no-op.
		return modalClickTarget{a.confirmPrompt, nil, nil}, true
	}
	return modalClickTarget{}, false
}

// reduceModalClick handles a left click while a modal overlay is active.
// See the package-level comment for the routing rules.
func reduceModalClick(a *App, m tea.MouseClickMsg) tea.Cmd {
	esc := func() tea.Cmd { return dispatchModeKey(a, tea.KeyPressMsg{Code: tea.KeyEscape}) }

	target, ok := a.activeModalClickTarget()
	if !ok {
		// Modal mode with no resolvable box: any click dismisses.
		return esc()
	}

	w, h := target.box.BoxSize(a.width, a.height)
	startX := (a.width - w) / 2
	startY := (a.height - h) / 2
	if startX < 0 {
		startX = 0
	}
	if startY < 0 {
		startY = 0
	}

	// Outside the box -> dismiss.
	if m.X < startX || m.X >= startX+w || m.Y < startY || m.Y >= startY+h {
		return esc()
	}

	// Inside the box. Non-list modal: consume, no-op.
	if target.click == nil {
		return nil
	}

	localY := m.Y - startY
	if !target.click.ClickRow(a.width, a.height, localY) {
		// Inside the box but not on a row (title/input/footer/scrollbar).
		return nil
	}

	// A row was selected. Synthesise the activation key, if any.
	if target.activation == nil {
		return nil
	}
	return dispatchModeKey(a, target.activation)
}
