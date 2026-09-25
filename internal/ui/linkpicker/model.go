// Package linkpicker provides the modal overlay that lets the user
// pick one item from a message: which link to open (the `o`
// keybinding) or which file attachment to download (the `d`
// keybinding). The chosen item is dispatched as ui.OpenLinkMsg or
// ui.DownloadFileMsg by the mode handler, depending on the kind the
// App recorded when opening the picker.
package linkpicker

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

// Model is the picker overlay state.
type Model struct {
	title    string
	items    []Item
	selected int
	visible  bool
}

// New creates a hidden picker.
func New() *Model { return &Model{} }

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
