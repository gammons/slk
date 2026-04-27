package channelfinder

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// ChannelResult is returned when the user selects a channel.
type ChannelResult struct {
	ID   string
	Name string
}

// Item represents a searchable channel/DM entry.
type Item struct {
	ID       string
	Name     string
	Type     string // channel, dm, group_dm, private
	Presence string // for DMs: active, away
}

// Model is the fuzzy channel finder overlay.
type Model struct {
	items    []Item
	filtered []int // indices into items matching query
	query    string
	selected int // index into filtered
	visible  bool
}

// New creates a new channel finder.
func New() Model {
	return Model{}
}

// SetItems updates the searchable channel list.
func (m *Model) SetItems(items []Item) {
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

// HandleKey processes a key event and returns a ChannelResult if the user
// selected a channel, or nil otherwise.
func (m *Model) HandleKey(keyStr string) *ChannelResult {
	switch keyStr {
	case "enter":
		if len(m.filtered) > 0 {
			idx := m.filtered[m.selected]
			return &ChannelResult{
				ID:   m.items[idx].ID,
				Name: m.items[idx].Name,
			}
		}
		return nil

	case "esc":
		m.Close()
		return nil

	case "down", "ctrl+n":
		if m.selected < len(m.filtered)-1 {
			m.selected++
		}
		return nil

	case "up", "ctrl+p":
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

	// If it's a single printable rune, add to query
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

	// Prefix matches first, then substring matches
	var prefixMatches, substringMatches []int
	for i, item := range m.items {
		name := strings.ToLower(item.Name)
		if strings.HasPrefix(name, q) {
			prefixMatches = append(prefixMatches, i)
		} else if strings.Contains(name, q) {
			substringMatches = append(substringMatches, i)
		}
	}
	m.filtered = append(prefixMatches, substringMatches...)
}

// View renders just the overlay box.
func (m Model) View(termWidth int) string {
	return m.renderBox(termWidth)
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

	// Overlay dimensions
	overlayWidth := termWidth / 2
	if overlayWidth < 30 {
		overlayWidth = 30
	}
	if overlayWidth > 80 {
		overlayWidth = 80
	}
	innerWidth := overlayWidth - 4 // border + padding

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Primary).
		Render("Switch Channel")

	// Query input with blue left border
	var inputText string
	if m.query == "" {
		placeholder := lipgloss.NewStyle().Foreground(styles.TextMuted).Render("Type to filter...")
		inputText = "█ " + placeholder
	} else {
		inputText = m.query + "█"
	}
	input := lipgloss.NewStyle().
		BorderStyle(lipgloss.Border{Left: "▌"}).
		BorderLeft(true).
		BorderForeground(styles.Primary).
		PaddingLeft(1).
		Foreground(styles.TextPrimary).
		Render(inputText)

	// Results (max 10)
	maxVisible := 10
	if maxVisible > len(m.filtered) {
		maxVisible = len(m.filtered)
	}

	// Adjust scroll window for results
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
		item := m.items[idx]

		prefix := channelPrefix(item)
		line := prefix + " " + item.Name

		if lipgloss.Width(line) > innerWidth {
			line = truncate.StringWithTail(line, uint(innerWidth), "…")
		}

		if i == m.selected {
			indicator := lipgloss.NewStyle().Foreground(styles.Accent).Render("▌")
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
			Render("No matching channels")
		resultRows = append(resultRows, noResults)
	}

	// Compose the overlay content
	content := title + "\n" + input + "\n\n" + strings.Join(resultRows, "\n")

	// Wrap in a bordered box
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)
}

// channelPrefix returns the display prefix for a channel type.
func channelPrefix(item Item) string {
	switch item.Type {
	case "private":
		return lipgloss.NewStyle().Foreground(styles.Warning).Render("◆")
	case "dm":
		if item.Presence == "active" {
			return lipgloss.NewStyle().Foreground(styles.Accent).Render("●")
		}
		return lipgloss.NewStyle().Foreground(styles.TextMuted).Render("○")
	case "group_dm":
		return lipgloss.NewStyle().Foreground(styles.TextMuted).Render("●")
	default:
		return lipgloss.NewStyle().Foreground(styles.TextMuted).Render("#")
	}
}
