package workspace

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slk/internal/ui/styles"
)

type WorkspaceItem struct {
	ID        string
	Name      string
	Initials  string
	HasUnread bool
}

type Model struct {
	items    []WorkspaceItem
	selected int
}

func New(items []WorkspaceItem, selected int) Model {
	return Model{items: items, selected: selected}
}

func (m *Model) SelectedID() string {
	if len(m.items) == 0 {
		return ""
	}
	return m.items[m.selected].ID
}

func (m *Model) SelectedIndex() int {
	return m.selected
}

func (m *Model) Select(idx int) {
	if idx >= 0 && idx < len(m.items) {
		m.selected = idx
	}
}

func (m *Model) SetItems(items []WorkspaceItem) {
	m.items = items
	if m.selected >= len(items) {
		m.selected = 0
	}
}

func (m Model) View(height int) string {
	if len(m.items) == 0 {
		return ""
	}

	var rows []string
	for i, item := range m.items {
		var style lipgloss.Style
		if i == m.selected {
			style = styles.WorkspaceActive
		} else {
			style = styles.WorkspaceInactive
		}

		label := style.Render(item.Initials)
		if item.HasUnread && i != m.selected {
			label += "\n" + styles.PresenceOnline.Render("*")
		}
		rows = append(rows, label)
	}

	content := strings.Join(rows, "\n\n")

	// Height/MaxHeight in lipgloss include padding in the total,
	// so use the full height directly. Padding(1,0) adds 1 row
	// top + 1 row bottom inside that total, matching the visual
	// offset of RoundedBorder() on adjacent panels.
	rail := lipgloss.NewStyle().
		Width(5).
		Height(height).
		MaxHeight(height).
		Background(styles.SurfaceDark).
		Padding(1, 0).
		Align(lipgloss.Center).
		Render(content)

	return rail
}

func (m Model) Width() int {
	return 5 // 5 content, no border
}

func WorkspaceInitials(name string) string {
	words := strings.Fields(name)
	switch len(words) {
	case 0:
		return "?"
	case 1:
		if len(words[0]) >= 2 {
			return strings.ToUpper(words[0][:2])
		}
		return strings.ToUpper(words[0])
	default:
		return strings.ToUpper(fmt.Sprintf("%c%c", words[0][0], words[1][0]))
	}
}
