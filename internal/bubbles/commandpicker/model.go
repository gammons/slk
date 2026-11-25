// Package commandpicker is the composer's slash-command autocomplete.
// It pulls the command list when it opens (a SourceFunc), draws through
// bubbles/chrome, and completes on Tab or Enter.
package commandpicker

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/bubbles/entrylist"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/text"
)

var _ chrome.Dialog = (*Model)(nil)

// maxVisible is how many rows the popup shows. The highlight moves
// through every match; chrome.Window scrolls the rest into view.
const maxVisible = 5

// SourceFunc returns the commands to complete. The picker calls it when
// it opens and does not keep the caller's slice: Open copies what it
// shows. Nil, or a func that returns nothing visible, leaves the picker
// closed.
type SourceFunc func() []core.SlashCommand

// Styles is everything the picker draws with. The composer maps the
// theme onto it and pushes it at construction and on every theme change.
type Styles struct {
	// Chrome is the shared popup styling (box, marker, scrollbar).
	Chrome chrome.Styles
	// Name styles the command. The selected row is also bold.
	Name lipgloss.Style
	// Detail styles the usage and description after the name.
	Detail lipgloss.Style
}

// KeyMap is the picker's bindings. Select completes the highlighted
// command; Close dismisses without completing.
type KeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Close  key.Binding
}

// DefaultKeyMap returns the bindings the composer uses: arrows and the
// readline pair move, Tab or Enter completes, Esc closes.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:     key.NewBinding(key.WithKeys("up", "ctrl+p")),
		Down:   key.NewBinding(key.WithKeys("down", "ctrl+n")),
		Select: key.NewBinding(key.WithKeys("tab", "enter")),
		Close:  key.NewBinding(key.WithKeys("esc")),
	}
}

// KeyResult is what HandleKey did with one key. Handled means the
// composer should not also apply it. Completed means Name replaces the
// token being typed; the picker is then closed.
type KeyResult struct {
	Handled   bool
	Completed bool
	Name      string
}

// Option configures a Model at construction.
type Option func(*Model)

// WithStyles sets the initial styles.
func WithStyles(s Styles) Option { return func(m *Model) { m.styles = s } }

// WithWidth sets the popup width.
func WithWidth(w int) Option { return func(m *Model) { m.width = w } }

// WithKeyMap replaces the default bindings.
func WithKeyMap(k KeyMap) Option { return func(m *Model) { m.keys = k } }

// Model is the slash-command popup.
type Model struct {
	source   SourceFunc
	commands []core.SlashCommand
	filtered []core.SlashCommand
	query    string
	selected int
	visible  bool
	styles   Styles
	keys     KeyMap
	width    int
}

// New returns a closed picker. source may be nil; Open then stays closed.
func New(source SourceFunc, opts ...Option) Model {
	m := Model{source: source, keys: DefaultKeyMap()}
	for _, o := range opts {
		o(&m)
	}
	if m.keys.Select.Keys() == nil && m.keys.Close.Keys() == nil {
		m.keys = DefaultKeyMap()
	}
	return m
}

// SetSource replaces the list Open pulls.
func (m *Model) SetSource(source SourceFunc) { m.source = source }

// SetStyles replaces the picker's styles.
func (m *Model) SetStyles(s Styles) { m.styles = s }

// SetWidth sets the popup width the composer allots.
func (m *Model) SetWidth(w int) { m.width = w }

// SetKeyMap replaces the bindings.
func (m *Model) SetKeyMap(k KeyMap) { m.keys = k }

// Open pulls the source and shows the picker when at least one command
// is visible. Entries with AliasOf set are dropped. An empty source
// leaves the picker closed and reports false.
func (m *Model) Open() bool {
	var cmds []core.SlashCommand
	if m.source != nil {
		cmds = m.source()
	}
	m.commands = visibleCommands(cmds)
	m.query = ""
	m.selected = 0
	m.filter()
	m.visible = len(m.filtered) > 0
	if !m.visible {
		m.commands = nil
	}
	return m.visible
}

// Close hides the picker and drops its query.
func (m *Model) Close() {
	m.visible = false
	m.query = ""
	m.selected = 0
	m.filtered = nil
}

// IsVisible reports whether the picker is showing.
func (m *Model) IsVisible() bool { return m.visible }

// SetQuery filters by a prefix of the command name. q is the text after
// the slash, without a leading slash.
func (m *Model) SetQuery(q string) {
	m.query = q
	m.selected = 0
	m.filter()
}

// Query returns the current filter.
func (m *Model) Query() string { return m.query }

// Filtered returns the matches in source order, aliases already removed.
func (m *Model) Filtered() []core.SlashCommand { return m.filtered }

// Selected returns the highlighted match index.
func (m *Model) Selected() int { return m.selected }

// HandleKey applies one key. Unrelated keys are not handled, so the
// composer can insert them.
func (m *Model) HandleKey(msg tea.KeyMsg) KeyResult {
	if !m.visible {
		return KeyResult{}
	}
	press, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return KeyResult{}
	}
	// Lock-state bits ride along on Kitty-protocol terminals and are not
	// part of the binding. Strip them the way the composer does.
	press.Mod &^= tea.ModCapsLock | tea.ModNumLock | tea.ModScrollLock
	switch {
	case key.Matches(press, m.keys.Up):
		m.move(-1)
		return KeyResult{Handled: true}
	case key.Matches(press, m.keys.Down):
		m.move(1)
		return KeyResult{Handled: true}
	case key.Matches(press, m.keys.Select):
		return m.complete()
	case key.Matches(press, m.keys.Close):
		m.Close()
		return KeyResult{Handled: true}
	default:
		return KeyResult{}
	}
}

func (m *Model) move(delta int) {
	if len(m.filtered) == 0 {
		return
	}
	next := m.selected + delta
	if next < 0 || next >= len(m.filtered) {
		return
	}
	m.selected = next
}

func (m *Model) complete() KeyResult {
	name := ""
	if m.selected >= 0 && m.selected < len(m.filtered) {
		name = m.filtered[m.selected].Name
	}
	m.Close()
	if name == "" {
		return KeyResult{Handled: true}
	}
	return KeyResult{Handled: true, Completed: true, Name: name}
}

func (m *Model) filter() {
	q := text.Fold(m.query)
	var results []core.SlashCommand
	for _, c := range m.commands {
		name := strings.TrimPrefix(c.Name, "/")
		if q == "" || strings.HasPrefix(text.Fold(name), q) {
			results = append(results, c)
		}
	}
	m.filtered = results
	if m.selected >= len(m.filtered) {
		m.selected = 0
	}
}

// View renders the popup, or "" when it is hidden or nothing matches.
// Each row is "/name  usage — desc", truncated to the width.
func (m *Model) View() string {
	if !m.visible || len(m.filtered) == 0 || m.width <= 2 {
		return ""
	}
	start, end := chrome.Window(m.selected, len(m.filtered), maxVisible)
	list := chrome.List{Start: start, End: end, Total: len(m.filtered)}
	// Popup draws a one-cell border on each side and sets the content
	// width to width-2, so a row of that many cells fills the box.
	inner := m.width - 2
	gutterCols := 0
	if list.Scrolls() {
		gutterCols = 1
	}
	contentW := inner - 2 - gutterCols // PopupMarker is two columns
	if contentW < 1 {
		contentW = 1
	}

	s := m.styles.Chrome
	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		name, rest := splitLabel(m.filtered[i])
		name, rest = fitLabel(name, rest, contentW)
		nameStyle := m.styles.Name
		if i == m.selected {
			nameStyle = nameStyle.Bold(true)
		}
		row := chrome.PopupMarker(s, i == m.selected) + nameStyle.Render(name) + m.styles.Detail.Render(rest)
		if g := list.Gutter(s, i); g != "" {
			row += g
		}
		rows = append(rows, row)
	}
	return chrome.Popup(s, rows, m.width)
}

func visibleCommands(cmds []core.SlashCommand) []core.SlashCommand {
	out := make([]core.SlashCommand, 0, len(cmds))
	for _, c := range cmds {
		if c.AliasOf != "" || c.Name == "" {
			continue
		}
		if !strings.HasPrefix(c.Name, "/") {
			c.Name = "/" + c.Name
		}
		out = append(out, c)
	}
	return out
}

// splitLabel is the row text: the name, then "  usage — desc" (either
// half omitted when empty).
func splitLabel(c core.SlashCommand) (name, rest string) {
	name = c.Name
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	usage := strings.TrimSpace(c.Usage)
	desc := strings.TrimSpace(c.Desc)
	switch {
	case usage != "" && desc != "":
		rest = "  " + usage + " — " + desc
	case usage != "":
		rest = "  " + usage
	case desc != "":
		rest = " — " + desc
	}
	return name, rest
}

func fitLabel(name, rest string, width int) (string, string) {
	if cells(name) >= width {
		return truncateCells(name, width), ""
	}
	remain := width - cells(name)
	if cells(rest) > remain {
		rest = truncateCells(rest, remain)
	}
	return name, rest
}

func cells(s string) int {
	lines := entrylist.PlainLines(s)
	if len(lines) == 0 {
		return 0
	}
	return entrylist.DisplayWidth(lines[0])
}

func truncateCells(s string, width int) string {
	if s == "" || width <= 0 {
		return ""
	}
	if cells(s) <= width {
		return s
	}
	const ell = "…"
	budget := width - cells(ell)
	if budget < 0 {
		return ""
	}
	p := entrylist.PlainLines(s)[0]
	cut := entrylist.SliceColumns(p, 0, budget)
	for cells(cut) > budget && cut != "" {
		rs := []rune(cut)
		cut = string(rs[:len(rs)-1])
	}
	if budget == 0 {
		return ell
	}
	return cut + ell
}
