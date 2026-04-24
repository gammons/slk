package sidebar

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slack-tui/internal/ui/styles"
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
	offset   int // scroll offset (index into rendered rows)
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
	m.offset = 0
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
	m.offset = 0
}

func (m *Model) GoToBottom() {
	if len(m.filtered) > 0 {
		m.selected = len(m.filtered) - 1
	}
}

func (m *Model) SetFilter(filter string) {
	m.filter = filter
	m.selected = 0
	m.offset = 0
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

		var prefix string
		switch item.Type {
		case "dm":
			if item.Presence == "active" {
				prefix = styles.PresenceOnline.Render("● ")
			} else {
				prefix = styles.PresenceAway.Render("○ ")
			}
		case "private":
			prefix = "🔒"
		default:
			prefix = "# "
		}

		label := prefix + item.Name

		if item.UnreadCount > 0 {
			badge := styles.UnreadBadge.Render(fmt.Sprintf(" %d ", item.UnreadCount))
			label += " " + badge
		}

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

	// Flatten into a single row list with section headers
	var allRows []renderRow
	for _, name := range sectionOrder {
		group := sectionMap[name]
		header := styles.SectionHeader.Render(group.name)
		allRows = append(allRows, renderRow{content: header, filterIdx: -1})
		allRows = append(allRows, group.rows...)
	}

	// Find the row index of the selected item
	selectedRow := 0
	for i, r := range allRows {
		if r.filterIdx == m.selected {
			selectedRow = i
			break
		}
	}

	// Adjust offset to keep selected row visible
	if m.offset > selectedRow {
		m.offset = selectedRow
	}
	if selectedRow >= m.offset+height {
		m.offset = selectedRow - height + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}

	// Render visible window
	end := m.offset + height
	if end > len(allRows) {
		end = len(allRows)
	}

	var visibleLines []string
	for i := m.offset; i < end; i++ {
		visibleLines = append(visibleLines, allRows[i].content)
	}

	content := strings.Join(visibleLines, "\n")

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxHeight(height).
		Background(styles.Surface).
		Render(content)
}

func (m Model) Width() int {
	return 25
}
