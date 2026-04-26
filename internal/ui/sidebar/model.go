package sidebar

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slack-tui/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

type ChannelItem struct {
	ID          string
	Name        string
	Type        string // channel, dm, group_dm, private
	Section     string // section name for grouping (e.g. "Engineering", "Starred")
	UnreadCount int
	IsStarred   bool
	Presence    string // for DMs: active, away, dnd
}

type Model struct {
	items    []ChannelItem
	selected int
	vp       viewport.Model
	filter   string
	filtered []int // indices into items that match filter
}

func New(items []ChannelItem) Model {
	m := Model{items: items}
	m.rebuildFilter()
	return m
}

func (m *Model) SetItems(items []ChannelItem) {
	m.items = items
	m.rebuildFilter()
	if m.selected >= len(m.filtered) {
		m.selected = 0
	}
}

func (m *Model) SelectedID() string {
	if len(m.filtered) == 0 {
		return ""
	}
	idx := m.filtered[m.selected]
	return m.items[idx].ID
}

func (m *Model) SelectedItem() (ChannelItem, bool) {
	if len(m.filtered) == 0 {
		return ChannelItem{}, false
	}
	idx := m.filtered[m.selected]
	return m.items[idx], true
}

func (m *Model) MoveDown() {
	if m.selected < len(m.filtered)-1 {
		m.selected++
	}
}

func (m *Model) MoveUp() {
	if m.selected > 0 {
		m.selected--
	}
}

func (m *Model) GoToTop() {
	m.selected = 0
}

func (m *Model) GoToBottom() {
	if len(m.filtered) > 0 {
		m.selected = len(m.filtered) - 1
	}
}

// Items returns all channel items.
func (m *Model) Items() []ChannelItem {
	return m.items
}

func (m *Model) SetFilter(filter string) {
	m.filter = filter
	m.selected = 0
	m.rebuildFilter()
}

func (m *Model) VisibleItems() []ChannelItem {
	var result []ChannelItem
	for _, idx := range m.filtered {
		result = append(result, m.items[idx])
	}
	return result
}

func (m *Model) SelectByID(id string) {
	for i, idx := range m.filtered {
		if m.items[idx].ID == id {
			m.selected = i
			return
		}
	}
}

func (m *Model) rebuildFilter() {
	m.filtered = nil
	lower := strings.ToLower(m.filter)
	for i, item := range m.items {
		if m.filter == "" || strings.Contains(strings.ToLower(item.Name), lower) {
			m.filtered = append(m.filtered, i)
		}
	}
}

// renderRow describes a single rendered row in the sidebar.
type renderRow struct {
	content   string
	filterIdx int // index into m.filtered, or -1 for section headers
}

func (m *Model) View(height, width int) string {
	if len(m.items) == 0 {
		return lipgloss.NewStyle().Width(width).Height(height).Render("No channels")
	}

	// Build all rows: section headers + channel items
	// Track which row corresponds to which filtered index for selection tracking.
	type sectionGroup struct {
		name string
		rows []renderRow
	}
	sectionOrder := []string{}
	sectionMap := map[string]*sectionGroup{}

	for fi, idx := range m.filtered {
		item := m.items[idx]
		isSelected := fi == m.selected

		// Selection indicator -- green left border for selected, space for others
		cursor := " "
		if isSelected {
			cursor = lipgloss.NewStyle().Foreground(styles.Accent).Render("▌")
		}

		// Unread dot indicator
		unreadDot := " "
		if item.UnreadCount > 0 {
			unreadDot = lipgloss.NewStyle().Foreground(styles.Primary).Render("●")
		}

		var prefix string
		switch item.Type {
		case "dm":
			if item.Presence == "active" {
				prefix = styles.PresenceOnline.Render("● ")
			} else {
				prefix = styles.PresenceAway.Render("○ ")
			}
		case "group_dm":
			prefix = styles.PresenceAway.Render("● ")
		case "private":
			prefix = lipgloss.NewStyle().Foreground(styles.Warning).Render("◆ ")
		default:
			prefix = "# "
		}

		// Truncate name to fit sidebar width (account for cursor + prefix + padding)
		name := item.Name
		maxNameLen := width - 8 // account for cursor, prefix, padding, border
		if maxNameLen < 5 {
			maxNameLen = 5
		}
		if len(name) > maxNameLen {
			name = truncate.StringWithTail(name, uint(maxNameLen), "…")
		}

		label := cursor + prefix + name + " " + unreadDot

		var style lipgloss.Style
		if isSelected {
			style = styles.ChannelSelected
		} else if item.UnreadCount > 0 {
			style = styles.ChannelUnread
		} else {
			style = styles.ChannelNormal
		}

		row := style.Width(width - 2).Render(label)

		sectionName := item.Section
		if sectionName == "" {
			if item.Type == "dm" || item.Type == "group_dm" {
				sectionName = "Direct Messages"
			} else {
				sectionName = "Channels"
			}
		}

		if _, ok := sectionMap[sectionName]; !ok {
			sectionMap[sectionName] = &sectionGroup{name: sectionName}
			sectionOrder = append(sectionOrder, sectionName)
		}
		sectionMap[sectionName].rows = append(sectionMap[sectionName].rows, renderRow{
			content:   row,
			filterIdx: fi,
		})
	}

	// Flatten into a single row list with section headers.
	// Add a blank line between sections for visual separation.
	var allRows []renderRow
	for i, name := range sectionOrder {
		if i > 0 {
			// Blank line between sections
			allRows = append(allRows, renderRow{content: "", filterIdx: -1})
		}
		group := sectionMap[name]
		header := styles.SectionHeader.Render(group.name)
		allRows = append(allRows, renderRow{content: header, filterIdx: -1})
		allRows = append(allRows, group.rows...)
	}

	// Build full content from all rows
	var allLines []string
	selectedStartLine := 0
	selectedEndLine := 0
	currentLine := 0

	for _, r := range allRows {
		if r.filterIdx == m.selected {
			selectedStartLine = currentLine
		}
		h := lipgloss.Height(r.content)
		if r.filterIdx == m.selected {
			selectedEndLine = currentLine + h
		}
		allLines = append(allLines, r.content)
		currentLine += h
	}

	fullContent := strings.Join(allLines, "\n")

	// Configure viewport
	m.vp.Width = width
	m.vp.Height = height
	m.vp.KeyMap = viewport.KeyMap{}
	m.vp.SetContent(fullContent)

	// Scroll to keep selected row visible
	if selectedEndLine > m.vp.YOffset+m.vp.Height {
		m.vp.SetYOffset(selectedEndLine - m.vp.Height)
	}
	if selectedStartLine < m.vp.YOffset {
		m.vp.SetYOffset(selectedStartLine)
	}

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxHeight(height).
		Render(m.vp.View())
}

func (m Model) Width() int {
	return 25
}
