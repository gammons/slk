package mentionpicker

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/styles"
)

const MaxVisible = 5

type User struct {
	ID          string
	DisplayName string
	Username    string
}

type MentionResult struct {
	UserID      string
	DisplayName string
}

var specialMentions = []User{
	{ID: "special:here", DisplayName: "here", Username: "here"},
	{ID: "special:channel", DisplayName: "channel", Username: "channel"},
	{ID: "special:everyone", DisplayName: "everyone", Username: "everyone"},
}

type Model struct {
	users    []User
	filtered []User
	query    string
	selected int
	visible  bool
}

func New() Model {
	return Model{}
}

func (m *Model) SetUsers(users []User) {
	m.users = users
}

func (m *Model) Open() {
	m.visible = true
	m.query = ""
	m.selected = 0
	m.filter()
}

func (m *Model) Close() {
	m.visible = false
	m.query = ""
	m.selected = 0
	m.filtered = nil
}

func (m *Model) IsVisible() bool {
	return m.visible
}

func (m *Model) SetQuery(q string) {
	m.query = q
	m.selected = 0
	m.filter()
}

func (m *Model) Query() string {
	return m.query
}

func (m *Model) Filtered() []User {
	return m.filtered
}

func (m *Model) Selected() int {
	return m.selected
}

func (m *Model) MoveUp() {
	if m.selected > 0 {
		m.selected--
	}
}

func (m *Model) MoveDown() {
	if m.selected < len(m.filtered)-1 {
		m.selected++
	}
}

func (m *Model) Select() *MentionResult {
	if len(m.filtered) == 0 {
		return nil
	}
	if m.selected < 0 || m.selected >= len(m.filtered) {
		return nil
	}
	u := m.filtered[m.selected]
	return &MentionResult{
		UserID:      u.ID,
		DisplayName: u.DisplayName,
	}
}

func (m *Model) filter() {
	q := strings.ToLower(m.query)
	var results []User

	// Special mentions first
	for _, u := range specialMentions {
		if q == "" || strings.HasPrefix(strings.ToLower(u.DisplayName), q) || strings.HasPrefix(strings.ToLower(u.Username), q) {
			results = append(results, u)
		}
	}

	// Then regular users
	for _, u := range m.users {
		if q == "" || strings.HasPrefix(strings.ToLower(u.DisplayName), q) || strings.HasPrefix(strings.ToLower(u.Username), q) {
			results = append(results, u)
		}
	}

	if len(results) > MaxVisible {
		results = results[:MaxVisible]
	}

	m.filtered = results
}

func (m *Model) View(width int) string {
	if !m.visible || len(m.filtered) == 0 {
		return ""
	}

	var rows []string
	for i, u := range m.filtered {
		indicator := "  "
		nameStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary)
		if i == m.selected {
			indicator = lipgloss.NewStyle().Foreground(styles.Accent).Render("▌ ")
			nameStyle = nameStyle.Bold(true)
		}

		label := fmt.Sprintf("%s (%s)", u.DisplayName, u.Username)
		row := indicator + nameStyle.Render(label)
		rows = append(rows, row)
	}

	content := strings.Join(rows, "\n")

	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Background(styles.SurfaceDark).
		Width(width - 2). // account for border
		Render(content)

	return box
}
