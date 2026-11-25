// Package draftlist is the :drafts dialog: the active workspace's Slack
// drafts, newest first. Enter jumps to a draft's conversation, d
// deletes it. Drawn through internal/bubbles/chrome like the marks list.
package draftlist

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/muesli/reflow/truncate"

	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
)

// Row is one draft as the dialog draws it.
type Row struct {
	Destination string // "#general", "#general › thread", "alice"
	Age         string // "5m", "3h", "Jan 2"
	Marker      string // hands-off kind: "scheduled …", "2 files", "saved"
	Preview     string // one line of the draft's text
}

// Action is what a key asked of the highlighted row.
type Action int

const (
	None Action = iota
	Jump
	Delete
)

// Model is the dialog.
type Model struct {
	rows     []Row
	selected int
	visible  bool
}

var _ interface {
	chrome.Dialog
	chrome.Boxed
} = (*Model)(nil)

func New() Model { return Model{} }

// SetRows replaces the rows and clamps the selection.
func (m *Model) SetRows(rows []Row) {
	m.rows = rows
	m.selected = max(min(m.selected, len(rows)-1), 0)
}

// Open shows the dialog with the first row selected.
func (m *Model) Open() {
	m.visible = true
	m.selected = 0
}

func (m *Model) Close() { m.visible = false }

func (m Model) IsVisible() bool { return m.visible }

// HandleKey moves the selection or reports an action on the highlighted
// row with its index. Esc closes.
func (m *Model) HandleKey(keyStr string) (Action, int) {
	switch keyStr {
	case "esc":
		m.Close()
	case "down", "j", "ctrl+n":
		if m.selected < len(m.rows)-1 {
			m.selected++
		}
	case "up", "k", "ctrl+p":
		if m.selected > 0 {
			m.selected--
		}
	case "enter":
		if len(m.rows) > 0 {
			return Jump, m.selected
		}
	case "d", "delete", "backspace":
		if len(m.rows) > 0 {
			return Delete, m.selected
		}
	}
	return None, 0
}

const (
	maxVisibleRows = 10
	boxChromeRows  = 6
)

func boxWidth(termWidth int) int { return max(min(termWidth*2/3, 100), 30) }

func listRows(termHeight int) int { return max(min(maxVisibleRows, termHeight-boxChromeRows), 1) }

// BoxSize returns the outer size of the rendered box.
func (m *Model) BoxSize(termWidth, termHeight int) (int, int) {
	start, end := chrome.Window(m.selected, len(m.rows), listRows(termHeight))
	return boxWidth(termWidth), max(end-start, 1) + boxChromeRows
}

// View renders the box alone (tests).
func (m Model) View(termWidth, termHeight int) string { return m.renderBox(termWidth, termHeight) }

// ViewOverlay renders the box centred over a dimmed background.
func (m Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}
	return overlay.DimmedOverlay(termWidth, termHeight, background, m.renderBox(termWidth, termHeight), 0.5)
}

func (m Model) renderBox(termWidth, termHeight int) string {
	s := messages.ChromeStyles()
	width := boxWidth(termWidth)
	start, end := chrome.Window(m.selected, len(m.rows), listRows(termHeight))
	list := chrome.List{Start: start, End: end, Total: len(m.rows)}
	contentWidth := list.ContentWidth(width - 4)
	dest := s.Row.Foreground(styles.Accent).Bold(true)
	muted := s.Row.Foreground(styles.TextMuted)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		r := m.rows[i]
		d, content, mu := dest, s.Row, muted
		if i == m.selected {
			d, content, mu = dest.Foreground(styles.Primary), s.RowSelected, s.RowSelected
		}
		line := d.Render(r.Destination) + content.Render("  ") + mu.Render(r.Age)
		if r.Marker != "" {
			line += content.Render("  ") + mu.Render("["+r.Marker+"]")
		}
		line += content.Render("  " + r.Preview)
		if lipgloss.Width(line) > contentWidth {
			line = truncate.StringWithTail(line, uint(contentWidth), "…")
		}
		if pad := contentWidth - lipgloss.Width(line); pad > 0 {
			line += content.Render(strings.Repeat(" ", pad))
		}
		lines = append(lines, line)
	}
	return chrome.Frame{
		Title: "Drafts",
		Body:  list.Rows(s, m.selected, lines),
		Empty: "No drafts",
	}.Render(s, width)
}
