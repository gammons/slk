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
	UnreadCount int
	IsStarred   bool
	Presence    string // for DMs: active, away, dnd
}

type Model struct {
	items    []ChannelItem
	selected int
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

func (m Model) View(height, width int) string {
	if len(m.items) == 0 {
		return lipgloss.NewStyle().Width(width).Height(height).Render("No channels")
	}

	// Group channels and DMs
	var channelRows []string
	var dmRows []string

	for fi, idx := range m.filtered {
		item := m.items[idx]
		isSelected := fi == m.selected

		var prefix string
		switch item.Type {
		case "dm":
			if item.Presence == "active" {
				prefix = styles.PresenceOnline.Render("* ")
			} else {
				prefix = styles.PresenceAway.Render("o ")
			}
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

		if item.Type == "dm" || item.Type == "group_dm" {
			dmRows = append(dmRows, row)
		} else {
			channelRows = append(channelRows, row)
		}
	}

	var sections []string

	if len(channelRows) > 0 {
		header := styles.SectionHeader.Render("Channels")
		sections = append(sections, header)
		sections = append(sections, channelRows...)
	}

	if len(dmRows) > 0 {
		header := styles.SectionHeader.Render("Direct Messages")
		sections = append(sections, header)
		sections = append(sections, dmRows...)
	}

	content := strings.Join(sections, "\n")

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
