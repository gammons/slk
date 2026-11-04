package newmessagepicker

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// View renders just the modal box. Use ViewOverlay for the
// dimmed-backdrop composite that the App actually paints.
func (m Model) View(termWidth int) string {
	return m.renderBox(termWidth)
}

// BoxSize returns the rendered modal box's outer dimensions. This modal's
// footer can wrap at narrow widths, so we measure the actual render rather
// than computing height analytically. renderBox has no side effects.
// termHeight is accepted for interface symmetry.
func (m Model) BoxSize(termWidth, termHeight int) (int, int) {
	box := m.renderBox(termWidth)
	return lipgloss.Width(box), lipgloss.Height(box)
}

// ViewOverlay renders the modal centered on a dimmed copy of the
// current screen. background is the already-rendered base screen.
func (m Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}
	box := m.renderBox(termWidth)
	if box == "" {
		return background
	}
	return overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
}

func (m Model) renderBox(termWidth int) string {
	if !m.visible {
		return ""
	}
	s := messages.ChromeStyles()
	overlayWidth := boxWidth(termWidth)
	innerWidth := overlayWidth - 4
	bg := styles.Background

	f := chrome.Frame{
		Title:  "New message",
		Input:  m.renderInputRow(innerWidth, bg),
		Body:   m.renderResultRows(s, innerWidth, bg),
		Footer: m.renderFooter(innerWidth, bg),
	}
	if len(m.filtered) == 0 {
		f.Empty = "No users available"
		if m.query != "" {
			f.Empty = fmt.Sprintf("No users match %q", m.query)
		}
	}
	return f.Render(s, overlayWidth)
}

// renderInputRow renders the "To: [pill] [pill] |query█" row.
func (m Model) renderInputRow(innerWidth int, bg color.Color) string {
	prefix := lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted).Render("To: ")

	var b strings.Builder
	b.WriteString(prefix)

	pillStyle := lipgloss.NewStyle().
		Background(styles.Primary).
		Foreground(styles.Background).
		Padding(0, 1)

	for _, u := range m.users {
		if _, ok := m.selected[u.ID]; !ok {
			continue
		}
		b.WriteString(pillStyle.Render(u.DisplayName))
		b.WriteString(" ")
	}

	queryStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.TextPrimary)
	if m.query == "" && len(m.selected) == 0 {
		placeholder := lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted).Render("type a name…")
		b.WriteString(placeholder)
	} else {
		b.WriteString(queryStyle.Render(m.query))
		b.WriteString(queryStyle.Render("█"))
	}

	row := b.String()
	if lipgloss.Width(row) > innerWidth {
		row = truncate.StringWithTail(row, uint(innerWidth), "…")
	}
	return row
}

// renderResultRows produces up to 10 visible result rows with a
// scrollbar when the list overflows. Highlighted row gets a left bar
// and the Primary foreground.
func (m Model) renderResultRows(s chrome.Styles, innerWidth int, bg color.Color) []string {
	start, end := chrome.Window(m.highlight, len(m.filtered), maxVisibleRows)
	list := chrome.List{Start: start, End: end, Total: len(m.filtered)}
	width := list.ContentWidth(innerWidth)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, m.renderRow(m.users[m.filtered[i]], width, i == m.highlight, bg))
	}
	return list.Rows(s, m.highlight, lines)
}

// renderRow renders a single user row's content, width wide: display
// name, @handle, [ext] tag if external, and a trailing "✓" if the user
// is in the pill bar. The marker column is the list's.
func (m Model) renderRow(u User, width int, highlight bool, bg color.Color) string {
	name := u.DisplayName
	if u.IsExternal {
		name += " [ext]"
	}

	handle := ""
	if u.Username != "" && u.Username != u.DisplayName {
		handle = lipgloss.NewStyle().
			Background(bg).
			Foreground(styles.TextMuted).
			Render(" @" + u.Username)
	}

	var nameStyle lipgloss.Style
	if highlight {
		nameStyle = lipgloss.NewStyle().Background(bg).Foreground(styles.Primary).Bold(true)
	} else {
		nameStyle = lipgloss.NewStyle().Background(bg).Foreground(styles.TextPrimary)
	}
	nameRendered := nameStyle.Render(name)

	check := ""
	if _, sel := m.selected[u.ID]; sel {
		check = lipgloss.NewStyle().Background(bg).Foreground(styles.Accent).Render(" ✓")
	}

	line := nameRendered + handle + check
	if lipgloss.Width(line) > width {
		line = truncate.StringWithTail(line, uint(width), "…")
	}
	if pad := width - lipgloss.Width(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}

	return line
}

// renderFooter is the key-hints + N/8 counter row.
func (m Model) renderFooter(innerWidth int, bg color.Color) string {
	left := lipgloss.NewStyle().
		Background(bg).
		Foreground(styles.TextMuted).
		Render("space toggle  enter open  esc cancel")

	counterText := fmt.Sprintf("%d / %d", len(m.selected), MaxRecipients)
	var counterStyle lipgloss.Style
	if len(m.selected) >= MaxRecipients {
		counterStyle = lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted).Italic(true)
		counterText += "  MPIM limit reached"
	} else {
		counterStyle = lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted)
	}
	right := counterStyle.Render(counterText)

	gap := innerWidth - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}
