package emojipicker

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/styles"
)

// MaxVisible matches mentionpicker.MaxVisible for UX symmetry.
const MaxVisible = 5

type Model struct {
	entries  []emoji.EmojiEntry
	filtered []emoji.EmojiEntry
	query    string
	selected int
	visible  bool
}

func New() Model { return Model{} }

// SetEntries replaces the full entry list. If the picker is visible, the
// filtered list and selection are recomputed against the current query.
func (m *Model) SetEntries(entries []emoji.EmojiEntry) {
	m.entries = entries
	if m.visible {
		m.filter()
	}
}

func (m *Model) Open(query string) {
	m.visible = true
	m.query = query
	m.selected = 0
	m.filter()
}

func (m *Model) Close() {
	m.visible = false
	m.query = ""
	m.selected = 0
	m.filtered = nil
}

func (m *Model) IsVisible() bool { return m.visible }

func (m *Model) SetQuery(q string) {
	m.query = q
	m.selected = 0
	m.filter()
}

func (m *Model) Query() string { return m.query }

func (m *Model) Filtered() []emoji.EmojiEntry { return m.filtered }

func (m *Model) Selected() int { return m.selected }

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

// SelectedEntry returns the currently highlighted entry. ok=false if the
// filtered list is empty.
func (m *Model) SelectedEntry() (emoji.EmojiEntry, bool) {
	if len(m.filtered) == 0 {
		return emoji.EmojiEntry{}, false
	}
	if m.selected < 0 || m.selected >= len(m.filtered) {
		return emoji.EmojiEntry{}, false
	}
	return m.filtered[m.selected], true
}

// filter walks entries in input order and keeps the first MaxVisible
// matches. Callers must pass alphabetically-sorted entries
// (emoji.BuildEntries already does); the picker preserves that order.
func (m *Model) filter() {
	q := strings.ToLower(m.query)
	var results []emoji.EmojiEntry
	for _, e := range m.entries {
		if q == "" || strings.HasPrefix(strings.ToLower(e.Name), q) {
			results = append(results, e)
			if len(results) >= MaxVisible {
				break
			}
		}
	}
	m.filtered = results
	if m.selected >= len(m.filtered) {
		m.selected = 0
		if len(m.filtered) > 0 {
			m.selected = len(m.filtered) - 1
		}
	}
}

// View renders the bordered dropdown. Returns "" when not visible OR when
// there are no matches (caller already shows the textarea below).
func (m Model) View(width int) string {
	if !m.visible || len(m.filtered) == 0 {
		return ""
	}

	// Compute the widest display preview so name columns line up.
	previewWidth := 1
	for _, e := range m.filtered {
		w := lipgloss.Width(e.Display)
		if w > previewWidth {
			previewWidth = w
		}
	}

	var rows []string
	for i, e := range m.filtered {
		indicator := "  "
		nameStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary)
		if i == m.selected {
			indicator = lipgloss.NewStyle().Foreground(styles.Accent).Render("▌ ")
			nameStyle = nameStyle.Bold(true)
		}
		// Pad preview cell so all names start at the same column.
		pad := previewWidth - lipgloss.Width(e.Display)
		if pad < 0 {
			pad = 0
		}
		preview := e.Display + strings.Repeat(" ", pad)
		row := fmt.Sprintf("%s%s  %s", indicator, preview, nameStyle.Render(":"+e.Name+":"))
		rows = append(rows, row)
	}

	content := strings.Join(rows, "\n")
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Background(styles.SurfaceDark).
		Width(width - 2).
		Render(content)
	return box
}
