package workspacefinder

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/text"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// WorkspaceResult is returned when the user selects a workspace.
type WorkspaceResult struct {
	ID   string
	Name string
}

// Item represents a searchable workspace entry.
type Item struct {
	ID       string
	Name     string
	Initials string
	// HasUnread draws an unread dot left of the initials.
	HasUnread bool
}

// Model is the fuzzy workspace finder overlay.
type Model struct {
	items    []Item
	filtered []int // indices into items matching query
	query    string
	selected int // index into filtered
	visible  bool
	// vimKeys opens the finder navigating: j/k move the highlight and
	// / starts the filter (SetVimKeys). Off, typing filters at once.
	vimKeys bool
	// searching is true while keys type into the filter; with vimKeys
	// off it is always true.
	searching bool
}

var _ interface {
	chrome.Dialog
	chrome.Clickable
} = (*Model)(nil)

// New creates a new workspace finder.
func New() Model {
	return Model{}
}

// SetVimKeys sets whether the finder opens navigating with j/k, with /
// to start filtering, instead of filtering on every key.
func (m *Model) SetVimKeys(on bool) { m.vimKeys = on }

// SetItems updates the searchable workspace list.
func (m *Model) SetItems(items []Item) {
	m.items = items
}

// listTopOffset is the box-local row of the first list row.
func listTopOffset() int { return chrome.BodyTop(messages.ChromeStyles(), true) }

// maxVisibleRows is the height of the results scroll window.
const maxVisibleRows = 10

// boxWidth returns the modal's outer width for a given terminal width.
func boxWidth(termWidth int) int {
	w := termWidth / 2
	if w < 30 {
		w = 30
	}
	if w > 80 {
		w = 80
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

// Open shows the overlay and resets state.
func (m *Model) Open() {
	m.visible = true
	m.searching = !m.vimKeys
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

// HandleKey processes a key event and returns a WorkspaceResult if the user
// selected a workspace, or nil otherwise.
//
// With vimKeys the finder opens navigating: j/k and the arrows move the
// highlight, / starts the filter, esc closes. While filtering (always,
// without vimKeys), printable runes and backspace edit the query and the
// arrows still move; with vimKeys, esc clears the filter and goes back to
// navigating. Enter switches to the highlighted workspace either way.
func (m *Model) HandleKey(keyStr string) *WorkspaceResult {
	switch keyStr {
	case "enter":
		if len(m.filtered) > 0 {
			idx := m.filtered[m.selected]
			return &WorkspaceResult{
				ID:   m.items[idx].ID,
				Name: m.items[idx].Name,
			}
		}
		return nil

	case "esc":
		if m.vimKeys && m.searching {
			m.searching = false
			m.query = ""
			m.selected = 0
			m.filter()
			return nil
		}
		m.Close()
		return nil

	case "down", "ctrl+n":
		m.move(1)
		return nil

	case "up", "ctrl+p":
		m.move(-1)
		return nil
	}

	if !m.searching {
		switch keyStr {
		case "j":
			m.move(1)
		case "k":
			m.move(-1)
		case "/":
			m.searching = true
		}
		return nil
	}

	if keyStr == "backspace" {
		if len(m.query) > 0 {
			_, n := utf8.DecodeLastRuneInString(m.query)
			m.query = m.query[:len(m.query)-n]
			m.selected = 0
			m.filter()
		}
		return nil
	}

	// If it's a single printable rune, add to query
	if r := []rune(keyStr); len(r) == 1 && unicode.IsPrint(r[0]) {
		m.query += keyStr
		m.selected = 0
		m.filter()
	}

	return nil
}

// move shifts the highlight by delta, clamped to the filtered list.
func (m *Model) move(delta int) {
	if next := m.selected + delta; next >= 0 && next < len(m.filtered) {
		m.selected = next
	}
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

	// Prefix matches first, then substring matches
	var prefixMatches, substringMatches []int
	for i, item := range m.items {
		name := text.Fold(item.Name)
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
	s := messages.ChromeStyles()
	overlayWidth := boxWidth(termWidth)
	innerWidth := overlayWidth - 4 // border + padding

	start, end := chrome.Window(m.selected, len(m.filtered), maxVisibleRows)
	list := chrome.List{Start: start, End: end, Total: len(m.filtered)}
	width := list.ContentWidth(innerWidth)
	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		item := m.items[m.filtered[i]]
		line := workspacePrefix(item) + " " + item.Name
		// Truncated one column wider than the row is drawn: preserved
		// as found (Phase 4 spec §8.1).
		if lipgloss.Width(line) > width+1 {
			line = truncate.StringWithTail(line, uint(width+1), "\u2026")
		}
		style := s.Row
		if i == m.selected {
			style = s.RowSelected
		}
		rows = append(rows, style.Width(width).Render(line))
	}

	f := chrome.Frame{
		Title: "Switch Workspace",
		Input: m.inputLine(s),
		Body:  list.Rows(s, m.selected, rows),
	}
	if m.query != "" {
		f.Empty = "No matching workspaces"
	}
	return f.Render(s, overlayWidth)
}

// inputLine is the filter prompt while searching, otherwise a hint.
func (m Model) inputLine(s chrome.Styles) string {
	if m.searching {
		return chrome.FilterInput(s, m.query, "Type to filter...")
	}
	return s.Footer.Render("j/k to move, / to search")
}

// workspacePrefix returns the display prefix for a workspace: a one-column
// unread slot (the rail's dot, or a space so the initials stay aligned)
// followed by the initials in a styled block.
func workspacePrefix(item Item) string {
	dot := " "
	if item.HasUnread {
		dot = styles.PresenceOnline.Render("●")
	}
	return dot + " " + styles.WorkspaceActive.Render(item.Initials)
}
