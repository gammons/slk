// Package appview is the UI model for a modal an app opened (a Slack
// "view"). Slack renders the dialog from Block Kit blocks; the App
// draws them with the shared block renderer inside the dialog chrome,
// and this model adds the scroll window and the size a click is routed
// against.
package appview

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// frameRows is the vertical chrome around the body: the box's top and
// bottom border and padding (4), the title line and the blank line
// after it (2), and the blank line and footer (2).
const frameRows = 8

// Model is a modal's presentation state: the view and its scroll
// offset. It holds no rendering cache; the App reparses the view's
// blocks at every draw, as dialogs do.
type Model struct {
	view    *core.AppView
	yOffset int

	// actionAt maps a rendered button's (block_id, action_id) to its
	// index in view.Actions. Identity, not position, so a hit presses
	// the button it was drawn from even if the renderer and the action
	// list ever disagree on an ordinal.
	actionAt map[actionKey]int
	// hits are the visible buttons' footprints in box-local
	// coordinates, recorded by the last View.
	hits []actionHit
	// clicked is the action a hit-test last landed on, which the click
	// router then presses.
	clicked int

	// lastWidth and lastHeight are the box's outer size from the last
	// View, which BoxSize reports for the modal click router. lastBody
	// is the body window a page key moves.
	lastWidth  int
	lastHeight int
	lastBody   int
	lastTotal  int
}

// actionKey identifies a rendered button the way a press echoes it.
type actionKey struct{ blockID, actionID string }

// actionHit is one visible button's footprint in box-local coordinates.
type actionHit struct {
	action           int
	rowStart, rowEnd int
	colStart, colEnd int
}

// New returns an empty model, which renders nothing.
func New() Model { return Model{} }

// SetView installs a modal, replacing any already shown (a pushed view
// included). A nil view clears it.
func (m *Model) SetView(v *core.AppView) {
	m.view = v
	m.actionAt = actionIndexMap(v)
	m.hits = nil
	m.yOffset = 0
}

// actionIndexMap maps each action's (block_id, action_id) to its index
// in v.Actions. A duplicate key keeps the first.
func actionIndexMap(v *core.AppView) map[actionKey]int {
	if v == nil || len(v.Actions) == 0 {
		return nil
	}
	out := make(map[actionKey]int, len(v.Actions))
	for i, act := range v.Actions {
		k := actionKey{blockID: act.BlockID, actionID: act.ActionID}
		if _, dup := out[k]; !dup {
			out[k] = i
		}
	}
	return out
}

// HitAction returns the index in Actions of the button drawn at (row,
// col) in the last View's box-local coordinates. ok is false when that
// cell is not on a button.
func (m *Model) HitAction(row, col int) (int, bool) {
	for _, h := range m.hits {
		if row >= h.rowStart && row < h.rowEnd && col >= h.colStart && col < h.colEnd {
			return h.action, true
		}
	}
	return 0, false
}

// ClickAt records the button under the box-local cell and reports
// whether there was one (the pointClickable contract).
func (m *Model) ClickAt(_, _, localX, localY int) bool {
	action, ok := m.HitAction(localY, localX)
	if ok {
		m.clicked = action
	}
	return ok
}

// LastClickAction is the action the last successful ClickAt landed on.
func (m *Model) LastClickAction() int { return m.clicked }

// Clear forgets the modal.
func (m *Model) Clear() {
	if m.view == nil {
		return
	}
	m.view = nil
	m.actionAt = nil
	m.hits = nil
	m.yOffset = 0
	m.lastWidth, m.lastHeight = 0, 0
}

// Current returns the installed view, nil when there is none.
func (m *Model) Current() *core.AppView { return m.view }

// ScrollUp moves the content up n lines.
func (m *Model) ScrollUp(n int) {
	if n <= 0 {
		return
	}
	m.setOffset(m.yOffset - n)
}

// ScrollDown moves the content down n lines, clamped in View.
func (m *Model) ScrollDown(n int) {
	if n <= 0 {
		return
	}
	m.setOffset(m.yOffset + n)
}

// GoToTop scrolls to the first line.
func (m *Model) GoToTop() { m.setOffset(0) }

// GoToBottom scrolls to the last page.
func (m *Model) GoToBottom() { m.setOffset(m.maxOffset()) }

// PageSize is the number of body rows the last View drew.
func (m *Model) PageSize() int {
	if m.lastBody < 1 {
		return 1
	}
	return m.lastBody
}

func (m *Model) maxOffset() int {
	max := m.lastTotal - m.PageSize()
	if max < 0 {
		return 0
	}
	return max
}

func (m *Model) setOffset(off int) {
	if max := m.maxOffset(); off > max {
		off = max
	}
	if off < 0 {
		off = 0
	}
	if off == m.yOffset {
		return
	}
	m.yOffset = off
}

// modalWidth is the box's outer width: two thirds of the terminal,
// clamped so a dialog stays readable on a wide screen and usable on a
// narrow one.
func modalWidth(termWidth int) int {
	w := termWidth * 2 / 3
	if w < 40 {
		w = 40
	}
	if w > 90 {
		w = 90
	}
	if w > termWidth {
		w = termWidth
	}
	if w < 1 {
		w = 1
	}
	return w
}

// View renders the modal as a box termWidth columns wide and at most
// termHeight rows tall. ctx is the Block Kit host context the App
// builds; the model does not own it because the render needs the
// pane's names and emoji settings.
func (m *Model) View(termWidth, termHeight int, ctx blockkit.Context) string {
	if m.view == nil {
		return ""
	}
	s := messages.ChromeStyles()
	boxW := modalWidth(termWidth)
	innerW := boxW - 4 // border and padding, both sides
	if innerW < 1 {
		innerW = 1
	}

	res := blockkit.Render(m.view.Blocks, ctx, innerW)
	lines := res.Lines
	m.lastTotal = len(lines)

	bodyH := termHeight - frameRows
	if bodyH < 1 {
		bodyH = 1
	}
	if bodyH > len(lines) {
		bodyH = len(lines)
	}
	if bodyH < 1 {
		bodyH = 1
	}
	m.lastBody = bodyH

	if max := len(lines) - bodyH; max < 0 {
		m.yOffset = 0
	} else if m.yOffset > max {
		m.yOffset = max
	}
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	end := m.yOffset + bodyH
	if end > len(lines) {
		end = len(lines)
	}
	body := make([]string, 0, bodyH)
	for i := m.yOffset; i < end; i++ {
		body = append(body, lines[i])
	}
	if len(lines) == 0 {
		body = nil
	}

	// Record the visible buttons' footprints for the click router. The
	// body starts under the border, padding, title and blank line; its
	// columns start after the border and padding.
	bodyTop := chrome.BodyTop(s, false)
	leftPad := s.Box.GetBorderLeftSize() + s.Box.GetPaddingLeft()
	m.hits = m.hits[:0]
	for _, ah := range res.ActionHits {
		action, ok := m.actionAt[actionKey{blockID: ah.BlockID, actionID: ah.ActionID}]
		if !ok {
			continue
		}
		rowStart := bodyTop + ah.RowStart - m.yOffset
		if rowStart < bodyTop || rowStart >= bodyTop+bodyH {
			continue
		}
		m.hits = append(m.hits, actionHit{
			action:   action,
			rowStart: rowStart, rowEnd: rowStart + (ah.RowEnd - ah.RowStart),
			colStart: leftPad + ah.ColStart, colEnd: leftPad + ah.ColEnd,
		})
	}

	title := m.view.Title
	if title == "" {
		title = "App dialog"
	}
	box := chrome.Frame{
		Title:  title,
		Body:   body,
		Empty:  "This dialog has no content.",
		Footer: s.Footer.Render(footer(m.view)),
	}.Render(s, boxW)
	m.lastWidth = lipgloss.Width(box)
	m.lastHeight = lipgloss.Height(box)
	return box
}

// footer is the key hint under the dialog. The view's own button labels
// are not drawn: slk closes the dialog locally, and a submit slk cannot
// perform would only mislead.
func footer(v *core.AppView) string {
	parts := make([]string, 0, 2)
	if len(v.Actions) > 0 {
		parts = append(parts, "b buttons")
	}
	parts = append(parts, "esc close")
	return strings.Join(parts, " · ")
}

// BoxSize reports the box's outer size from the last View, for the
// modal click router. (0, 0) when nothing was drawn yet.
func (m *Model) BoxSize(int, int) (int, int) {
	if m.view == nil || m.lastWidth == 0 || m.lastHeight == 0 {
		return 0, 0
	}
	return m.lastWidth, m.lastHeight
}
