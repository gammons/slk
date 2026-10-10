package confirmprompt

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/muesli/reflow/truncate"

	"github.com/gammons/slk/internal/bubbles/ansi"
)

// Box width as a share of the terminal, and the bounds it is clamped to.
const (
	widthPercent = 35
	minWidth     = 40
	maxWidth     = 60
	// chrome is the border and horizontal padding the content sits inside.
	chrome = 4
)

// View renders the prompt box, or "" when hidden.
func (m Model) View() string {
	if !m.visible {
		return ""
	}

	width := boxWidth(m.width)
	content := strings.Join([]string{
		m.styles.Title.Render(m.title),
		m.styles.Body.Render("> " + preview(m.body, width-chrome)),
		m.styles.Footer.Render(m.footer()),
	}, "\n\n")

	return m.styles.Box.Width(width).Render(ansi.ReapplyAfterResets(content, m.styles.BaseANSI))
}

func boxWidth(termWidth int) int {
	return min(max(termWidth*widthPercent/100, minWidth), maxWidth)
}

// preview flattens body to one line and truncates it to limit columns.
func preview(body string, limit int) string {
	body = strings.ReplaceAll(body, "\n", " ")
	body = strings.ReplaceAll(body, "\t", " ")
	if lipgloss.Width(body) > limit {
		return truncate.StringWithTail(body, uint(limit), "…")
	}
	return body
}

func (m Model) footer() string {
	confirm, cancel := m.KeyMap.Confirm.Help(), m.KeyMap.Cancel.Help()
	return "[" + confirm.Key + "] " + confirm.Desc + "   [" + cancel.Key + "] " + cancel.Desc
}
