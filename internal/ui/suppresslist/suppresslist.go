// Package suppresslist is the :suppressed dialog: the active
// workspace's suppression rules, one row each (who, and where), with
// backspace/Delete removing the highlighted rule. It follows the
// overlay convention of internal/ui/marks — HandleKey(keyStr) *Result,
// ViewOverlay(termWidth, termHeight, background) — and draws through
// internal/bubbles/chrome.
package suppresslist

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/muesli/reflow/truncate"
)

// Row is one rule as the dialog draws it. Key is handed back on
// removal; the App chooses what it holds.
type Row struct {
	Key   int
	Who   string // the author's name, or ID when unknown
	Where string // "#channel", "this conversation" or "everywhere"
}

// Result is what HandleKey returns when the user acts: Remove names the
// row to remove. nil means nothing happened.
type Result struct {
	Remove int
}

// Model is the dialog.
type Model struct {
	rows     []Row
	selected int
	visible  bool
	// note is a line shown under the list (a failed removal).
	note string
}

var _ interface {
	chrome.Dialog
	chrome.Clickable
} = (*Model)(nil)

// New returns the dialog closed.
func New() Model { return Model{} }

// SetRows replaces the rows, keeping the selection in range.
func (m *Model) SetRows(rows []Row) {
	m.rows = rows
	m.selected = min(m.selected, len(rows)-1)
	m.selected = max(m.selected, 0)
}

// SetNote sets the line shown under the list ("" for none).
func (m *Model) SetNote(note string) { m.note = note }

// Open shows the dialog with the first row selected.
func (m *Model) Open() {
	m.visible = true
	m.selected = 0
	m.note = ""
}

// Close hides the dialog.
func (m *Model) Close() { m.visible = false }

// IsVisible reports whether the dialog is showing.
func (m Model) IsVisible() bool { return m.visible }

// HandleKey moves the selection, closes on Esc, and returns a Result
// for backspace/Delete (or Enter: the dialog's one action) on a row.
func (m *Model) HandleKey(keyStr string) *Result {
	switch keyStr {
	case "esc", "q":
		m.Close()
	case "down", "j", "ctrl+n":
		if m.selected < len(m.rows)-1 {
			m.selected++
		}
	case "up", "k", "ctrl+p":
		if m.selected > 0 {
			m.selected--
		}
	case "backspace", "delete", "enter", "d":
		if len(m.rows) > 0 {
			return &Result{Remove: m.rows[m.selected].Key}
		}
	}
	return nil
}

const (
	maxVisibleRows = 10
	// boxChromeRows: border and padding above, title and the blank
	// line under it, the footer and its blank line, padding and border
	// below.
	boxChromeRows = 8
)

func boxWidth(termWidth int) int { return min(max(termWidth/2, 36), 80) }

func listRows(termHeight int) int {
	return max(min(maxVisibleRows, termHeight-boxChromeRows), 1)
}

func listTopOffset() int { return chrome.BodyTop(messages.ChromeStyles(), false) }

// BoxSize returns the outer size of the rendered box.
func (m *Model) BoxSize(termWidth, termHeight int) (int, int) {
	start, end := chrome.Window(m.selected, len(m.rows), listRows(termHeight))
	return boxWidth(termWidth), max(end-start, 1) + boxChromeRows
}

// ClickRow selects the row at box-local localY and reports whether one
// was there.
func (m *Model) ClickRow(termWidth, termHeight, localY int) bool {
	row := localY - listTopOffset()
	start, end := chrome.Window(m.selected, len(m.rows), listRows(termHeight))
	if row < 0 || row >= end-start {
		return false
	}
	m.selected = start + row
	return true
}

// View renders the box alone (tests).
func (m Model) View(termWidth, termHeight int) string { return m.renderBox(termWidth, termHeight) }

// ViewOverlay renders the dialog centred over a dimmed background.
func (m Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}
	return overlay.DimmedOverlay(termWidth, termHeight, background, m.renderBox(termWidth, termHeight), 0.5)
}

func (m Model) renderBox(termWidth, termHeight int) string {
	if !m.visible {
		return ""
	}
	s := messages.ChromeStyles()
	width := boxWidth(termWidth)
	start, end := chrome.Window(m.selected, len(m.rows), listRows(termHeight))
	list := chrome.List{Start: start, End: end, Total: len(m.rows)}
	contentWidth := list.ContentWidth(width - 4)

	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		st := s.Row
		if i == m.selected {
			st = s.RowSelected
		}
		line := st.Render(m.rows[i].Who) + st.Render("  ·  "+m.rows[i].Where)
		if lipgloss.Width(line) > contentWidth {
			line = truncate.StringWithTail(line, uint(contentWidth), "…")
		}
		if pad := contentWidth - lipgloss.Width(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		rows = append(rows, line)
	}
	footer := "⌫ stop hiding · esc close"
	if m.note != "" {
		footer = m.note
	}
	return chrome.Frame{
		Title:  "Hidden authors",
		Body:   list.Rows(s, m.selected, rows),
		Empty:  "Nobody is hidden in this workspace",
		Footer: footer,
	}.Render(s, width)
}
