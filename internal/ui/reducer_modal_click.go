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
//   - Click ON a point target      -> for modals whose hot spot is a
//     single glyph rather than a row (the
//     profile dialog's 📋), ClickAt tests
//     the exact cell and a hit synthesises
//     the modal's activation key.
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

// pointClickable is a boxedOverlay whose hot spot is a single cell range
// rather than a list row. ClickAt reports whether the box-local cell
// (localX, localY) is on it. A modal implements this or
// clickableOverlay, not both.
type pointClickable interface {
	boxedOverlay
	ClickAt(termWidth, termHeight, localX, localY int) bool
}

// modalClickTarget bundles the active modal's geometry source, its
// optional list- or point-hit-testing, and the key to synthesise on a
// hit.
type modalClickTarget struct {
	box        boxedOverlay
	click      clickableOverlay // nil for non-list modals (e.g. confirm)
	activation tea.KeyMsg       // nil when a row click has no activation (help)
	point      pointClickable   // nil unless the modal has a point hot spot
}

// activeModalClickTarget resolves the modal addressed by the current
// mode. ok is false for modal modes that have no resolvable box (e.g.
// the custom-snooze numeric input), in which case any click dismisses.
func (a *App) activeModalClickTarget() (modalClickTarget, bool) {
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	space := tea.KeyPressMsg{Code: tea.KeySpace}
	switch a.mode {
	case ModeChannelFinder:
		return modalClickTarget{box: &a.channelFinder, click: &a.channelFinder, activation: enter}, true
	case ModeWorkspaceSearch:
		return modalClickTarget{box: &a.searchResults, click: &a.searchResults, activation: enter}, true
	case ModeWorkspaceFinder:
		return modalClickTarget{box: &a.workspaceFinder, click: &a.workspaceFinder, activation: enter}, true
	case ModeThemeSwitcher:
		return modalClickTarget{box: &a.themeSwitcher, click: &a.themeSwitcher, activation: enter}, true
	case ModePresenceMenu:
		return modalClickTarget{box: &a.presenceMenu, click: &a.presenceMenu, activation: enter}, true
	case ModeReactionPicker:
		return modalClickTarget{box: a.reactionPicker, click: a.reactionPicker, activation: enter}, true
	case ModeNewMessage:
		return modalClickTarget{box: &a.newMessagePicker, click: &a.newMessagePicker, activation: space}, true
	case ModeHelp:
		// Help has no activation: clicking a row only moves the highlight.
		return modalClickTarget{box: &a.help, click: &a.help, activation: nil}, true
	case ModeConfirm:
		// Confirm has no list: outside dismisses, inside is a no-op.
		return modalClickTarget{box: confirmPromptBox{&a.confirmPrompt}}, true
	case ModeUserProfile:
		// Read-only: the only hot spot is the email's 📋, which does
		// what e does. Inside elsewhere is a no-op.
		e := tea.KeyPressMsg{Code: 'e', Text: "e"}
		return modalClickTarget{box: a.userProfile, activation: e, point: a.userProfile}, true
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

	// Inside the box on a point hot spot -> its activation.
	if target.point != nil {
		if target.activation != nil && target.point.ClickAt(a.width, a.height, m.X-startX, m.Y-startY) {
			return dispatchModeKey(a, target.activation)
		}
		return nil
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
