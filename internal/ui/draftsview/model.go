// Package draftsview is the Drafts panel: the active workspace's Slack
// drafts, one row each, in the order the :drafts dialog shows them.
//
// It is presentation only. The App builds the rows (the same ones the
// dialog draws) and reads Selected when the user opens or deletes one.
// Navigation matches the Threads view: j/k move the selection, paging
// and the wheel scroll, and the selected row carries the ▌ bar.
package draftsview

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/muesli/reflow/truncate"

	"github.com/gammons/slk/internal/ui/draftlist"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(styles.TextMuted)
}

// thickLeftBorder is the messages/threadsview selection bar.
var thickLeftBorder = lipgloss.Border{Left: "▌"}

func borderInvisStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(styles.InvisibleLeftBorder()).BorderLeft(true).
		BorderForeground(styles.Background).
		BorderBackground(styles.Background)
}

func borderSelectStyle(focused bool) lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).
		BorderForeground(styles.SelectionBorderColor(focused)).
		BorderBackground(styles.SelectionTintColor(focused)).
		Background(styles.SelectionTintColor(focused))
}

func borderFillStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(styles.Background)
}

// Model is the drafts list.
type Model struct {
	rows     []draftlist.Row
	selected int
	yOffset  int
	focused  bool

	snappedSelection int
	hasSnapped       bool

	version int64
}

// New returns an empty list.
func New() Model { return Model{} }

// Version increments when View could change.
func (m *Model) Version() int64 { return m.version }

func (m *Model) dirty() { m.version++ }

// SetFocused records whether the panel has keyboard focus, so the
// selection bar dims when it does not.
func (m *Model) SetFocused(f bool) {
	if m.focused != f {
		m.focused = f
		m.dirty()
	}
}

// SetRows replaces the rows. The selection stays on the same index,
// clamped into the new list.
func (m *Model) SetRows(rows []draftlist.Row) {
	if slices.Equal(m.rows, rows) {
		return
	}
	m.rows = rows
	if m.selected > len(rows)-1 {
		m.selected = max(len(rows)-1, 0)
	}
	m.hasSnapped = false
	m.dirty()
}

// Selected is the highlighted row's index. ok is false when the list
// is empty.
func (m *Model) Selected() (int, bool) {
	if len(m.rows) == 0 || m.selected < 0 || m.selected >= len(m.rows) {
		return 0, false
	}
	return m.selected, true
}

// MoveDown moves the selection down one row.
func (m *Model) MoveDown() {
	if m.selected < len(m.rows)-1 {
		m.selected++
		m.dirty()
	}
}

// MoveUp moves the selection up one row.
func (m *Model) MoveUp() {
	if m.selected > 0 {
		m.selected--
		m.dirty()
	}
}

// GoToTop selects the first row.
func (m *Model) GoToTop() {
	if m.selected != 0 {
		m.selected = 0
		m.dirty()
	}
}

// GoToBottom selects the last row.
func (m *Model) GoToBottom() {
	if n := len(m.rows); n > 0 && m.selected != n-1 {
		m.selected = n - 1
		m.dirty()
	}
}

// ScrollUp moves the viewport up without moving the selection.
func (m *Model) ScrollUp(n int) {
	if n <= 0 {
		return
	}
	m.yOffset -= n
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	m.snappedSelection = m.selected
	m.hasSnapped = true
	m.dirty()
}

// ScrollDown moves the viewport down without moving the selection.
func (m *Model) ScrollDown(n int) {
	if n <= 0 {
		return
	}
	m.yOffset += n
	m.snappedSelection = m.selected
	m.hasSnapped = true
	m.dirty()
}

// ClickAt selects the row at panel-local rowY. False when the click
// is above the list or past the last row.
func (m *Model) ClickAt(rowY int) bool {
	if rowY < 0 || len(m.rows) == 0 {
		return false
	}
	idx := m.yOffset + rowY
	if idx < 0 || idx >= len(m.rows) {
		return false
	}
	if m.selected != idx {
		m.selected = idx
		m.dirty()
	}
	return true
}

// View renders height lines of width columns. An empty list is the
// centred "no drafts" line, the same shape as the Threads view's
// "no threads".
func (m *Model) View(height, width int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	if len(m.rows) == 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, mutedStyle().Render("no drafts"))
	}
	total := len(m.rows)
	if !m.hasSnapped || m.snappedSelection != m.selected {
		if m.selected < m.yOffset {
			m.yOffset = m.selected
		}
		if m.selected >= m.yOffset+height {
			m.yOffset = m.selected - height + 1
		}
		m.snappedSelection = m.selected
		m.hasSnapped = true
	}
	if maxOff := total - height; m.yOffset > maxOff {
		m.yOffset = max(maxOff, 0)
	}
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	end := min(m.yOffset+height, total)
	lines := make([]string, 0, height)
	for i := m.yOffset; i < end; i++ {
		lines = append(lines, m.renderRow(m.rows[i], width, i == m.selected))
	}
	filler := lipgloss.NewStyle().Width(width).Background(styles.Background).Render("")
	for len(lines) < height {
		lines = append(lines, filler)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderRow(r draftlist.Row, width int, selected bool) string {
	contentWidth := max(width-1, 1)
	dest := lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
	muted := lipgloss.NewStyle().Foreground(styles.TextMuted)
	plain := lipgloss.NewStyle()
	if selected {
		dest = dest.Foreground(styles.Accent)
	}
	line := dest.Render(r.Destination) + plain.Render("  ") + muted.Render(r.Age)
	if r.Marker != "" {
		line += plain.Render("  ") + muted.Render("["+r.Marker+"]")
	}
	if r.Preview != "" {
		line += plain.Render("  " + r.Preview)
	}
	line = clipToWidth(line, contentWidth)
	border := borderInvisStyle()
	fill := borderFillStyle().Width(contentWidth)
	if selected {
		border = borderSelectStyle(m.focused)
		fill = lipgloss.NewStyle().Background(styles.SelectionTintColor(m.focused)).Width(contentWidth)
		if styles.TransparentBackground() {
			line = messages.RepaintBgToSelectionTint(line, m.focused)
		}
	}
	return border.Render(fill.Render(line))
}

func clipToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return truncate.StringWithTail(s, uint(width), "…")
}
