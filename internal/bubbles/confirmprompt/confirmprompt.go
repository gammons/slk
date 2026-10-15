// Package confirmprompt provides a prompt overlay: a title, a body and a row of buttons. Open is the yes/no preset; OpenChoice shows any buttons.
//
// The model owns its width and renders only its own box; compositing is the
// caller's job.
package confirmprompt

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/bubbles/chrome"
)

// ConfirmFunc is the action a confirmation authorises. Update returns it as a
// tea.Cmd rather than calling it.
type ConfirmFunc func() tea.Msg

// Model is the confirmation overlay. Construct it with New; the zero value is
// not usable: its KeyMap is empty, so nothing matches Confirm and every key
// cancels.
type Model struct {
	KeyMap KeyMap

	styles    Styles
	width     int
	height    int // terminal height; 0 = unknown
	visible   bool
	choice    Choice
	preset    bool // opened with Open: keys and labels follow KeyMap
	highlight int  // highlighted button; -1 = none
	cache     renderCache
}

var _ chrome.Dialog = (*Model)(nil)

// Option configures a Model at construction.
type Option func(*Model)

// WithStyles sets the prompt's styles.
func WithStyles(s Styles) Option { return func(m *Model) { m.styles = s } }

// WithKeyMap sets the prompt's key bindings.
func WithKeyMap(k KeyMap) Option { return func(m *Model) { m.KeyMap = k } }

// WithWidth sets the terminal width the box is sized against.
func WithWidth(width int) Option { return func(m *Model) { m.width = width } }

// WithHeight sets the terminal height the body is fitted to; 0 means unknown.
func WithHeight(height int) Option { return func(m *Model) { m.height = height } }

// New returns a hidden prompt with default keys and styles, then applies opts.
// It is the only usable constructor.
func New(opts ...Option) Model {
	m := Model{
		KeyMap:    DefaultKeyMap(),
		styles:    DefaultStyles(true),
		highlight: -1,
	}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

// Open shows the yes/no preset: Confirm (KeyMap.Confirm) runs onConfirm,
// Cancel (KeyMap.Cancel) and any other key close. onConfirm may be nil.
func (m *Model) Open(title, body string, onConfirm ConfirmFunc) {
	m.open(Choice{
		Title:             title,
		Body:              body,
		Buttons:           []Button{presetConfirm: {Action: onConfirm}, presetCancel: {}},
		Default:           presetConfirm,
		CancelOnOtherKeys: true,
	})
	m.preset = true
}

// OpenChoice shows c.
func (m *Model) OpenChoice(c Choice) {
	m.open(c)
	m.preset = false
}

func (m *Model) open(c Choice) {
	m.choice = c
	m.highlight = -1
	if c.Default >= 0 && c.Default < len(c.Buttons) {
		m.highlight = c.Default
	}
	m.visible = true
}

// Close hides the prompt and clears its state.
func (m *Model) Close() {
	m.visible = false
	m.choice = Choice{}
	m.preset = false
	m.highlight = -1
}

// Highlight highlights button i; out of range is ignored.
func (m *Model) Highlight(i int) {
	if i >= 0 && i < len(m.choice.Buttons) {
		m.highlight = i
	}
}

// Highlighted returns the highlighted button, or -1.
func (m Model) Highlighted() int { return m.highlight }

// IsVisible reports whether the prompt is showing.
func (m Model) IsVisible() bool { return m.visible }

// SetWidth sets the terminal width the box is sized against.
func (m *Model) SetWidth(width int) { m.width = width }

// SetHeight sets the terminal height. A body that would push the buttons
// past it is trimmed (or dropped); 0 means unknown, which limits the body
// to maxBodyLines only.
func (m *Model) SetHeight(height int) { m.height = height }

// Styles returns the prompt's current styles.
func (m Model) Styles() Styles { return m.styles }

// SetStyles replaces the prompt's styles.
func (m *Model) SetStyles(s Styles) {
	m.styles = s
	m.cache = renderCache{}
}

// Init does nothing; the prompt has no startup work.
func (m Model) Init() tea.Cmd { return nil }
