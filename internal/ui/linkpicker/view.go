package linkpicker

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// ViewOverlay renders the picker centered on a dimmed copy of
// background. Returns background unchanged when not visible.
func (m *Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}
	box := m.renderBox(termWidth)
	if box == "" {
		return background
	}
	return overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
}

func (m *Model) renderBox(termWidth int) string {
	overlayWidth := termWidth * 6 / 10
	if overlayWidth < 40 {
		overlayWidth = 40
	}
	if overlayWidth > 80 {
		overlayWidth = 80
	}
	if overlayWidth > termWidth-2 {
		overlayWidth = termWidth - 2
	}
	innerWidth := overlayWidth - 4 // border + padding

	bg := styles.Background
	title := lipgloss.NewStyle().
		Bold(true).
		Background(bg).
		Foreground(styles.Primary).
		Render(m.title)

	badgeStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.Accent)

	var rows []string
	for i, it := range m.items {
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
		budget := innerWidth - 1 - lipgloss.Width(badge) // 1 = indicator column
		if budget < 1 {
			budget = 1
		}
		if lipgloss.Width(text) > budget {
			text = truncate.StringWithTail(text, uint(budget), "\u2026")
		}
		var row string
		if i == m.selected {
			indicator := lipgloss.NewStyle().Background(bg).Foreground(styles.Accent).Render("\u258c")
			body := lipgloss.NewStyle().
				Background(bg).
				Foreground(styles.Primary).
				Bold(true).
				Width(budget).
				Render(text)
			row = indicator + body + badgeStyle.Render(badge)
		} else {
			body := lipgloss.NewStyle().
				Background(bg).
				Foreground(styles.TextPrimary).
				Width(budget).
				Render(text)
			row = " " + body + badgeStyle.Render(badge)
		}
		rows = append(rows, row)
	}

	footer := lipgloss.NewStyle().
		Background(bg).
		Foreground(styles.TextMuted).
		Render("j/k move   enter select   esc/q close")

	content := title + "\n\n" + strings.Join(rows, "\n") + "\n\n" + footer
	content = messages.ReapplyBgAfterResets(content, messages.BgANSI()+messages.FgANSI())

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		Background(bg).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)
}
