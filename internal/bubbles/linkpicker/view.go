package linkpicker

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/muesli/reflow/truncate"
)

// maxVisibleRows is how many rows the picker shows at once; a longer list
// scrolls with the selection (chrome.Window) and gets a scrollbar.
const maxVisibleRows = 10

// View renders the picker's box, or "" when it is hidden. The caller
// composites it over its background.
func (m *Model) View() string {
	if !m.visible {
		return ""
	}
	s := m.styles.Chrome
	overlayWidth := m.width * 6 / 10
	if overlayWidth < 40 {
		overlayWidth = 40
	}
	if overlayWidth > 80 {
		overlayWidth = 80
	}
	if overlayWidth > m.width-2 {
		overlayWidth = m.width - 2
	}
	innerWidth := overlayWidth - 4 // border + padding

	start, end := chrome.Window(m.selected, len(m.items), maxVisibleRows)
	list := chrome.List{Start: start, End: end, Total: len(m.items)}

	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		it := m.items[i]
		var parts []string
		if it.Label != "" {
			parts = append(parts, it.Label)
		}
		if it.URL != "" && it.URL != it.Label {
			parts = append(parts, it.URL)
		}
		if it.Detail != "" {
			parts = append(parts, it.Detail)
		}
		text := strings.Join(parts, "  ")
		badge := ""
		if it.InApp {
			badge = " [slk]"
		}
		budget := list.ContentWidth(innerWidth) - lipgloss.Width(badge)
		if budget < 1 {
			budget = 1
		}
		if lipgloss.Width(text) > budget {
			text = truncate.StringWithTail(text, uint(budget), "…")
		}
		style := s.Row
		if i == m.selected {
			style = s.RowSelected
		}
		rows = append(rows, style.Width(budget).Render(text)+m.styles.Badge.Render(badge))
	}

	return chrome.Frame{
		Title:  m.title,
		Body:   list.Rows(s, m.selected, rows),
		Footer: s.Footer.Render("j/k move   enter select   esc/q close"),
	}.Render(s, overlayWidth)
}
