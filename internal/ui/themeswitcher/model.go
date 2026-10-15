package themeswitcher

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/text"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/muesli/reflow/truncate"
)

const (
	ScopeGlobal    = core.ThemeScopeGlobal
	ScopeWorkspace = core.ThemeScopeWorkspace
)

// ThemeResult is returned when the user selects a theme.
type ThemeResult struct {
	Name  string
	Scope core.ThemeScope
}

// Model is the theme switcher overlay.
type Model struct {
	items      []string // theme display names
	filtered   []int    // indices into items matching query
	query      string
	selected   int // index into filtered
	visible    bool
	scope      core.ThemeScope
	headerText string
}

var _ interface {
	chrome.Dialog
	chrome.Clickable
} = (*Model)(nil)

// New creates a new theme switcher.
func New() Model {
	return Model{}
}

// SetItems updates the list of available theme names.
func (m *Model) SetItems(items []string) {
	m.items = items
}

// Neighbor returns the theme step places from current in the picker's
// item order, wrapping at either end, so repeated calls with the result
// cycle through every theme. current matches case-insensitively. When it
// is not in the list, a forward step starts at the first item and a
// backward step at the last. Returns "" when there are no items.
func (m Model) Neighbor(current string, step int) string {
	n := len(m.items)
	if n == 0 {
		return ""
	}
	idx := slices.IndexFunc(m.items, func(item string) bool {
		return strings.EqualFold(item, current)
	})
	if idx < 0 && step < 0 {
		idx = n
	}
	return m.items[((idx+step)%n+n)%n]
}

// Open shows the overlay and resets state. Defaults to ScopeGlobal with no
// custom header text. Use OpenWithScope to set a scope and header.
func (m *Model) Open() {
	m.OpenWithScope(ScopeGlobal, "")
}

// OpenWithScope shows the overlay scoped to either the active workspace or
// the global default. headerText, if non-empty, replaces the default
// "Switch Theme" title in the rendered overlay.
func (m *Model) OpenWithScope(scope core.ThemeScope, headerText string) {
	m.visible = true
	m.query = ""
	m.selected = 0
	m.scope = scope
	m.headerText = headerText
	m.filter()
}

// Scope returns the scope the picker was last opened with.
func (m Model) Scope() core.ThemeScope { return m.scope }

// HeaderText returns the header text the picker was last opened with.
func (m Model) HeaderText() string { return m.headerText }

// Close hides the overlay.
func (m *Model) Close() {
	m.visible = false
}

// listTopOffset is the box-local row of the first list row.
func listTopOffset() int { return chrome.BodyTop(messages.ChromeStyles(), true) }

// maxVisibleRows is the height of the results scroll window.
const maxVisibleRows = 12

// boxWidth returns the modal's outer width for a given terminal width.
func boxWidth(termWidth int) int {
	w := termWidth / 2
	if w < 30 {
		w = 30
	}
	if w > 60 {
		w = 60
	}
	return w
}

// BoxSize returns the rendered modal box's outer dimensions. termHeight is
// unused (height depends only on row count) but kept for symmetry.
func (m *Model) BoxSize(termWidth, termHeight int) (int, int) {
	start, end := chrome.Window(m.selected, len(m.filtered), maxVisibleRows)
	nRows := end - start
	if nRows < 1 {
		nRows = 1
	}
	return boxWidth(termWidth), nRows + 7
}

// ClickRow moves the selection to the list row at box-local localY and
// returns true when the click lands on a visible row.
func (m *Model) ClickRow(termWidth, termHeight, localY int) bool {
	row := localY - listTopOffset()
	if row < 0 {
		return false
	}
	start, end := chrome.Window(m.selected, len(m.filtered), maxVisibleRows)
	if row >= end-start {
		return false
	}
	m.selected = start + row
	return true
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
			return &ThemeResult{Name: m.items[idx], Scope: m.scope}
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
	q := text.Fold(m.query)

	if q == "" {
		for i := range m.items {
			m.filtered = append(m.filtered, i)
		}
		return
	}

	var prefixMatches, substringMatches []int
	for i, item := range m.items {
		name := text.Fold(item)
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
	s := messages.ChromeStyles()
	overlayWidth := boxWidth(termWidth)
	innerWidth := overlayWidth - 4

	titleText := "Switch Theme"
	if m.headerText != "" {
		titleText = m.headerText
	}

	start, end := chrome.Window(m.selected, len(m.filtered), maxVisibleRows)
	list := chrome.List{Start: start, End: end, Total: len(m.filtered)}
	width := list.ContentWidth(innerWidth)
	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		line := m.items[m.filtered[i]]
		if lipgloss.Width(line) > width {
			line = truncate.StringWithTail(line, uint(width), "\u2026")
		}
		style := s.Row
		if i == m.selected {
			style = s.RowSelected
		}
		rows = append(rows, style.Width(width).Render(line))
	}

	f := chrome.Frame{
		Title: titleText,
		Input: chrome.FilterInput(s, m.query, "Type to filter..."),
		Body:  list.Rows(s, m.selected, rows),
	}
	if m.query != "" {
		f.Empty = "No matching themes"
	}
	return f.Render(s, overlayWidth)
}
