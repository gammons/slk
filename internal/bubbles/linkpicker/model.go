// Package linkpicker provides the modal overlay that lets the user
// pick one item from a message: which link to open (the `o`
// keybinding) or which file attachment to download (the `d`
// keybinding). The chosen item is dispatched as ui.OpenLinkMsg or
// ui.DownloadFileMsg by the mode handler, depending on the kind the
// App recorded when opening the picker.
//
// The model owns its width and styles: the App maps the theme onto Styles
// (linkPickerStyles), pushes it with SetStyles, pushes the terminal width
// with SetWidth, and composites View's box over a dimmed background.
package linkpicker

import (
	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/chrome"
)

// Item is one selectable row.
type Item struct {
	URL   string
	Label string // filename for file rows; link label (may be empty) for links
	// Detail is trailing muted info shown after the label (e.g. file
	// size). Empty for link rows.
	Detail string
	// InApp marks links that the router will navigate inside slk
	// (active-workspace archive permalinks); rendered with a badge.
	InApp bool
	// Index is the item's position in the slice passed to Open,
	// assigned by Open so the dispatcher can map the chosen row back
	// to its source data.
	Index int
}

// Styles is everything the picker draws with. The App maps the theme onto
// it and pushes it on open and on every theme change.
type Styles struct {
	// Chrome is the shared dialog styling (box, rows, footer).
	Chrome chrome.Styles
	// Badge styles the trailing " [slk]" mark on in-app links.
	Badge lipgloss.Style
}

// Option configures a Model at construction.
type Option func(*Model)

// WithStyles sets the initial styles.
func WithStyles(s Styles) Option { return func(m *Model) { m.styles = s } }

// WithWidth sets the terminal width the box is sized from.
func WithWidth(w int) Option { return func(m *Model) { m.width = w } }

// Model is the picker overlay state.
type Model struct {
	title    string
	items    []Item
	selected int
	visible  bool
	styles   Styles
	width    int
}

var _ chrome.Dialog = (*Model)(nil)

// New creates a hidden picker.
func New(opts ...Option) *Model {
	m := &Model{}
	for _, o := range opts {
		o(m)
	}
	return m
}

// SetStyles replaces the picker's styles.
func (m *Model) SetStyles(s Styles) { m.styles = s }

// SetWidth sets the terminal width the box is sized from: 60% of it,
// between 40 and 80 columns, and at most width-2.
func (m *Model) SetWidth(w int) { m.width = w }

// Open shows the picker over items with the given dialog title, first
// row selected.
func (m *Model) Open(title string, items []Item) {
	m.title = title
	m.items = items
	for i := range m.items {
		m.items[i].Index = i
	}
	m.selected = 0
	m.visible = true
}

// Close hides the picker and drops its items.
func (m *Model) Close() {
	m.visible = false
	m.items = nil
	m.selected = 0
}

// IsVisible reports whether the picker is showing.
func (m *Model) IsVisible() bool { return m.visible }

// Title returns the dialog title set by Open.
func (m *Model) Title() string { return m.title }

// Items returns the current rows (for rendering and tests).
func (m *Model) Items() []Item { return m.items }

// Selected returns the highlighted row index.
func (m *Model) Selected() int { return m.selected }

// HandleKey processes one key. Returns (item, true) when the user
// chose a row with enter (the picker closes itself); (Item{}, false)
// otherwise. esc/q close without choosing.
func (m *Model) HandleKey(key string) (Item, bool) {
	switch key {
	case "esc", "q":
		m.Close()
	case "j", "down":
		if m.selected < len(m.items)-1 {
			m.selected++
		}
	case "k", "up":
		if m.selected > 0 {
			m.selected--
		}
	case "enter":
		if len(m.items) == 0 {
			return Item{}, false
		}
		item := m.items[m.selected]
		m.Close()
		return item, true
	}
	return Item{}, false
}
