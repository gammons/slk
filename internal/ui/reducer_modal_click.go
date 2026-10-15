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
//     the modal's activation key. The
//     confirm prompt's buttons are point
//     targets too, but a hit calls onPoint
//     to press the clicked button directly:
//     a synthesised Enter would confirm.
//
// Geometry comes from each modal's BoxSize, mirroring the centering done
// by overlay.DimmedOverlay so the hit-test lines up with what was drawn.
// The contract (chrome.Boxed, chrome.Clickable) is declared in
// internal/bubbles/chrome and asserted by each modal package.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/ui/userprofile"
)

// pointClickable is a chrome.Boxed modal whose hot spot is a single cell
// range rather than a list row. ClickAt reports whether the box-local
// cell (localX, localY) is on it. A modal implements this or
// chrome.Clickable, not both.
type pointClickable interface {
	chrome.Boxed
	ClickAt(termWidth, termHeight, localX, localY int) bool
}

// modalClickTarget bundles the active modal's geometry source, its
// optional list- or point-hit-testing, and the key to synthesise on a
// hit.
type modalClickTarget struct {
	box        chrome.Boxed
	click      chrome.Clickable // nil for non-list modals (e.g. confirm)
	activation tea.KeyMsg       // nil when a row click has no activation (help)
	point      pointClickable   // nil unless the modal has a point hot spot
	onPoint    func() tea.Cmd   // set: a point hit calls this instead of synthesising activation
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
	case ModeMarks:
		return modalClickTarget{box: &a.marksOverlay, click: &a.marksOverlay, activation: enter}, true
	case ModeHelp:
		// Help has no activation: clicking a row only moves the highlight.
		return modalClickTarget{box: &a.help, click: &a.help, activation: nil}, true
	case ModeConfirm:
		// A click on a button presses it directly; elsewhere inside is a
		// no-op, outside dismisses.
		box := confirmPromptBox{&a.confirmPrompt}
		return modalClickTarget{box: box, point: box, onPoint: a.pressConfirmPromptHighlighted}, true
	case ModeUserProfile:
		// The hot spots are the email's 📋 and, when the huddle row
		// carries one, its 📋: a click does what e / c does. Inside
		// elsewhere is a no-op. ClickAt records which hot spot it hit
		// in the model, so the onPoint can dispatch the right copy.
		return modalClickTarget{
			box:   a.userProfile,
			point: a.userProfile,
			onPoint: func() tea.Cmd {
				if a.userProfile.LastClickTarget() == userprofile.ClickTargetHuddle {
					return copyProfileHuddle(a)
				}
				return copyProfileEmail(a)
			},
		}, true
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

	// Inside the box on a point hot spot -> its press, or its activation.
	if target.point != nil {
		if target.point.ClickAt(a.width, a.height, m.X-startX, m.Y-startY) {
			if target.onPoint != nil {
				return target.onPoint()
			}
			if target.activation != nil {
				return dispatchModeKey(a, target.activation)
			}
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
