package themeswitcher

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// ThemeResult is returned when the user selects a theme.
type ThemeResult struct {
	Name string
}

// Model is the theme switcher overlay.
type Model struct {
	items    []string // theme display names
	filtered []int    // indices into items matching query
	query    string
	selected int // index into filtered
	visible  bool
}

// New creates a new theme switcher.
func New() Model {
	return Model{}
}

// SetItems updates the list of available theme names.
func (m *Model) SetItems(items []string) {
	m.items = items
}

// Open shows the overlay and resets state.
func (m *Model) Open() {
	m.visible = true
	m.query = ""
	m.selected = 0
	m.filter()
}

// Close hides the overlay.
func (m *Model) Close() {
	m.visible = false
}

// IsVisible returns whether the overlay is showing.
func (m Model) IsVisible() bool {
	return m.visible
}

// HandleKey processes a key event and returns a ThemeResult if the user
// selected a theme, or nil otherwise.
func (m *Model) HandleKey(keyStr string) *ThemeResult {
	switch keyStr {
	case "enter":
		if len(m.filtered) > 0 {
			idx := m.filtered[m.selected]
			return &ThemeResult{Name: m.items[idx]}
		}
		return nil

	case "esc":
		m.Close()
		return nil

	case "down", "ctrl+n", "j":
		if m.selected < len(m.filtered)-1 {
			m.selected++
		}
		return nil

	case "up", "ctrl+p", "k":
		if m.selected > 0 {
			m.selected--
		}
		return nil

	case "backspace":
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
			m.selected = 0
			m.filter()
		}
		return nil
	}

	// Single printable rune — add to query
	if len(keyStr) == 1 && keyStr[0] >= 32 && keyStr[0] <= 126 {
		m.query += keyStr
		m.selected = 0
		m.filter()
	}

	return nil
}

// filter rebuilds the filtered list based on the current query.
func (m *Model) filter() {
	m.filtered = nil
	q := strings.ToLower(m.query)

	if q == "" {
		for i := range m.items {
			m.filtered = append(m.filtered, i)
		}
		return
	}

	var prefixMatches, substringMatches []int
	for i, item := range m.items {
		name := strings.ToLower(item)
		if strings.HasPrefix(name, q) {
			prefixMatches = append(prefixMatches, i)
		} else if strings.Contains(name, q) {
			substringMatches = append(substringMatches, i)
		}
	}
	m.filtered = append(prefixMatches, substringMatches...)
}

// ViewOverlay renders the overlay as a centered modal with a dark backdrop.
func (m Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}

	box := m.renderBox(termWidth)
	if box == "" {
		return background
	}

	return lipgloss.Place(termWidth, termHeight,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(lipgloss.Color("#0F0F1A"))),
	)
}

func (m Model) renderBox(termWidth int) string {
	if !m.visible {
		return ""
	}

	overlayWidth := termWidth / 2
	if overlayWidth < 30 {
		overlayWidth = 30
	}
	if overlayWidth > 60 {
		overlayWidth = 60
	}
	innerWidth := overlayWidth - 4

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Primary).
		Render("Switch Theme")

	var inputText string
	if m.query == "" {
		placeholder := lipgloss.NewStyle().Foreground(styles.TextMuted).Render("Type to filter...")
		inputText = "\u2588 " + placeholder
	} else {
		inputText = m.query + "\u2588"
	}
	input := lipgloss.NewStyle().
		BorderStyle(lipgloss.Border{Left: "\u258c"}).
		BorderLeft(true).
		BorderForeground(styles.Primary).
		PaddingLeft(1).
		Foreground(styles.TextPrimary).
		Render(inputText)

	maxVisible := 12
	if maxVisible > len(m.filtered) {
		maxVisible = len(m.filtered)
	}

	startIdx := 0
	if m.selected >= maxVisible {
		startIdx = m.selected - maxVisible + 1
	}
	endIdx := startIdx + maxVisible
	if endIdx > len(m.filtered) {
		endIdx = len(m.filtered)
		startIdx = endIdx - maxVisible
		if startIdx < 0 {
			startIdx = 0
		}
	}

	var resultRows []string
	for i := startIdx; i < endIdx; i++ {
		idx := m.filtered[i]
		line := m.items[idx]

		if lipgloss.Width(line) > innerWidth {
			line = truncate.StringWithTail(line, uint(innerWidth), "\u2026")
		}

		if i == m.selected {
			indicator := lipgloss.NewStyle().Foreground(styles.Accent).Render("\u258c")
			row := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Bold(true).
				Width(innerWidth - 1).
				Render(line)
			resultRows = append(resultRows, indicator+row)
		} else {
			row := lipgloss.NewStyle().
				Foreground(styles.TextPrimary).
				Width(innerWidth - 1).
				Render(line)
			resultRows = append(resultRows, " "+row)
		}
	}

	if len(m.filtered) == 0 && m.query != "" {
		noResults := lipgloss.NewStyle().
			Foreground(styles.TextMuted).
			Italic(true).
			Render("No matching themes")
		resultRows = append(resultRows, noResults)
	}

	content := title + "\n" + input + "\n\n" + strings.Join(resultRows, "\n")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)
}
