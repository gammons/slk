package workspace

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
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
	version  int64
}

// Version returns a counter that increments any time the View() output could
// change.
func (m *Model) Version() int64 { return m.version }

func (m *Model) dirty() { m.version++ }

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
	if idx >= 0 && idx < len(m.items) && m.selected != idx {
		m.selected = idx
		m.dirty()
	}
}

func (m *Model) SetItems(items []WorkspaceItem) {
	m.items = items
	if m.selected >= len(items) {
		m.selected = 0
	}
	m.dirty()
}

func (m *Model) SelectByID(teamID string) {
	for i, item := range m.items {
		if item.ID == teamID {
			if m.selected != i {
				m.selected = i
				m.dirty()
			}
			return
		}
	}
}

func (m *Model) SetUnread(teamID string, hasUnread bool) {
	for i := range m.items {
		if m.items[i].ID == teamID {
			if m.items[i].HasUnread != hasUnread {
				m.items[i].HasUnread = hasUnread
				m.dirty()
			}
			return
		}
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

		initials := item.Initials
		if item.HasUnread && i != m.selected {
			initials = initials + styles.PresenceOnline.Render("●")
		}
		label := style.Render(initials)
		rows = append(rows, label)
	}

	content := strings.Join(rows, "\n\n")

	// Height/MaxHeight in lipgloss include padding in the total,
	// so use the full height directly. Padding(1,0) adds 1 row
	// top + 1 row bottom inside that total, matching the visual
	// offset of RoundedBorder() on adjacent panels.
	rail := lipgloss.NewStyle().
		Width(6).
		Height(height).
		MaxHeight(height).
		Background(styles.RailBackground).
		Padding(1, 0).
		Align(lipgloss.Center).
		Render(content)

	return rail
}

func (m Model) Width() int {
	return 6 // 6 content, no border
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
