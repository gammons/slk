package channelfinder

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// nonJoinedColor is a hard-coded dim grey used for channels the user is not a
// member of, so the dim treatment is unmistakable across all themes and does
// not rely on terminal Faint support (which many emulators render weakly or
// not at all).
var nonJoinedColor = lipgloss.Color("#5a5a5a")

// ChannelResult is returned when the user selects a channel.
type ChannelResult struct {
	ID     string
	Name   string
	Joined bool // false => caller should join the channel before opening it
}

// Item represents a searchable channel/DM entry.
type Item struct {
	ID       string
	Name     string
	Type     string // channel, dm, group_dm, private
	Presence string // for DMs: active, away
	Joined   bool   // true if the user is already a member; false for browseable public channels
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

// MarkJoined flips the Joined bit on a channel that the user just joined,
// so it stops rendering as dimmed and the next Enter on it skips the join
// step.
func (m *Model) MarkJoined(channelID string) {
	for i := range m.items {
		if m.items[i].ID == channelID {
			m.items[i].Joined = true
			return
		}
	}
}

// SetBrowseable replaces the non-joined channel entries in the finder.
// Joined items (added via SetItems) are preserved; previous non-joined items
// are dropped and replaced with the new set. Items whose IDs already appear
// among the joined entries are skipped to avoid duplicates.
func (m *Model) SetBrowseable(browseable []Item) {
	// Drop existing non-joined items and build an ID set of joined items.
	joined := m.items[:0]
	have := make(map[string]struct{}, len(m.items))
	for _, it := range m.items {
		if it.Joined {
			joined = append(joined, it)
			have[it.ID] = struct{}{}
		}
	}
	m.items = joined
	for _, it := range browseable {
		if _, dup := have[it.ID]; dup {
			continue
		}
		it.Joined = false
		m.items = append(m.items, it)
	}
	// Re-filter against current query so the new items appear immediately if
	// the overlay is open.
	if m.visible {
		m.filter()
	}
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
				ID:     m.items[idx].ID,
				Name:   m.items[idx].Name,
				Joined: m.items[idx].Joined,
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

		isSelected := i == m.selected

		// Render prefix and name as SEPARATE styled fragments. If we built a
		// single string and ran one outer style over it, the prefix's own
		// ANSI reset (\x1b[0m) would drop the outer foreground / faint
		// attributes for everything after it, defeating the dim treatment.
		var prefix, name string
		if item.Joined {
			prefix = channelPrefix(item)
			nameStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary)
			if isSelected {
				nameStyle = nameStyle.Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
			}
			name = nameStyle.Render(item.Name)
		} else {
			// Non-joined: dim grey for everything, including the prefix.
			dim := lipgloss.NewStyle().Foreground(nonJoinedColor)
			prefix = dim.Render("#")
			name = dim.Render(item.Name)
		}

		line := prefix + " " + name
		// Truncate to fit (truncate.StringWithTail is ANSI-aware).
		if lipgloss.Width(line) > innerWidth-1 {
			line = truncate.StringWithTail(line, uint(innerWidth-1), "…")
		}
		// Right-pad with spaces to fill the row.
		if pad := innerWidth - 1 - lipgloss.Width(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}

		if isSelected {
			indicator := lipgloss.NewStyle().Foreground(styles.Accent).Render("▌")
			resultRows = append(resultRows, indicator+line)
		} else {
			resultRows = append(resultRows, " "+line)
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
