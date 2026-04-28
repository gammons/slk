package themeswitcher

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
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

	return overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
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

	// All inner elements share the modal background so the dimmed app
	// behind the overlay doesn't bleed through individual styled spans.
	bg := styles.Background

	title := lipgloss.NewStyle().
		Bold(true).
		Background(bg).
		Foreground(styles.Primary).
		Render("Switch Theme")

	var inputText string
	if m.query == "" {
		placeholder := lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted).Render("Type to filter...")
		inputText = "\u2588 " + placeholder
	} else {
		inputText = m.query + "\u2588"
	}
	input := lipgloss.NewStyle().
		BorderStyle(lipgloss.Border{Left: "\u258c"}).
		BorderLeft(true).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		PaddingLeft(1).
		Background(bg).
		Foreground(styles.TextPrimary).
		Render(inputText)

	maxVisible := 12
	total := len(m.filtered)
	if maxVisible > total {
		maxVisible = total
	}

	startIdx := 0
	if m.selected >= maxVisible {
		startIdx = m.selected - maxVisible + 1
	}
	endIdx := startIdx + maxVisible
	if endIdx > total {
		endIdx = total
		startIdx = endIdx - maxVisible
		if startIdx < 0 {
			startIdx = 0
		}
	}

	// Scrollbar: only shown when the list is taller than the visible window.
	// Reserve one column on the right; rows shrink by 1 to make room. The
	// thumb size and position are proportional to the visible-window/total
	// ratio so users see at a glance how much more content exists.
	showScrollbar := total > maxVisible
	rowWidth := innerWidth - 1 // leave room for the scrollbar gutter (1 col)
	if !showScrollbar {
		rowWidth = innerWidth
	}

	var thumbStart, thumbEnd int
	if showScrollbar {
		// Thumb height: at least 1 row, proportional otherwise.
		thumbHeight := maxVisible * maxVisible / total
		if thumbHeight < 1 {
			thumbHeight = 1
		}
		// Thumb top: proportional to scroll position. Clamp so the thumb
		// reaches the bottom exactly when endIdx == total.
		denom := total - maxVisible
		if denom < 1 {
			denom = 1
		}
		thumbStart = startIdx * (maxVisible - thumbHeight) / denom
		if thumbStart < 0 {
			thumbStart = 0
		}
		if thumbStart > maxVisible-thumbHeight {
			thumbStart = maxVisible - thumbHeight
		}
		thumbEnd = thumbStart + thumbHeight
	}

	thumbStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.Primary)
	trackStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.Border)

	var resultRows []string
	for i := startIdx; i < endIdx; i++ {
		idx := m.filtered[i]
		line := m.items[idx]

		if lipgloss.Width(line) > rowWidth-1 {
			line = truncate.StringWithTail(line, uint(rowWidth-1), "\u2026")
		}

		var row string
		if i == m.selected {
			indicator := lipgloss.NewStyle().Background(bg).Foreground(styles.Accent).Render("\u258c")
			label := lipgloss.NewStyle().
				Background(bg).
				Foreground(styles.Primary).
				Bold(true).
				Width(rowWidth - 1).
				Render(line)
			row = indicator + label
		} else {
			label := lipgloss.NewStyle().
				Background(bg).
				Foreground(styles.TextPrimary).
				Width(rowWidth - 1).
				Render(line)
			row = " " + label
		}

		if showScrollbar {
			rel := i - startIdx
			var sb string
			if rel >= thumbStart && rel < thumbEnd {
				sb = thumbStyle.Render("\u2588") // █ thumb
			} else {
				sb = trackStyle.Render("\u2502") // │ track
			}
			row += sb
		}
		resultRows = append(resultRows, row)
	}

	if total == 0 && m.query != "" {
		noResults := lipgloss.NewStyle().
			Background(bg).
			Foreground(styles.TextMuted).
			Italic(true).
			Render("No matching themes")
		resultRows = append(resultRows, noResults)
	}

	content := title + "\n" + input + "\n\n" + strings.Join(resultRows, "\n")

	// Re-paint modal bg+fg after every ANSI reset so trailing/unstyled
	// cells don't leak the dimmed app behind the overlay.
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
