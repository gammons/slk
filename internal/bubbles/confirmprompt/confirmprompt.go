// Package confirmprompt provides a yes/no confirmation overlay.
//
// The model owns its width and renders only its own box; compositing is the
// caller's job.
package confirmprompt

import tea "charm.land/bubbletea/v2"

// ConfirmFunc is the action a confirmation authorises. Update returns it as a
// tea.Cmd rather than calling it.
type ConfirmFunc func() tea.Msg

// Model is the confirmation overlay.
type Model struct {
	KeyMap KeyMap

	styles    Styles
	width     int
	visible   bool
	title     string
	body      string
	onConfirm ConfirmFunc
}

// Option configures a Model at construction.
type Option func(*Model)

// WithStyles sets the prompt's styles.
func WithStyles(s Styles) Option { return func(m *Model) { m.styles = s } }

// WithKeyMap sets the prompt's key bindings.
func WithKeyMap(k KeyMap) Option { return func(m *Model) { m.KeyMap = k } }

// WithWidth sets the terminal width the box is sized against.
func WithWidth(width int) Option { return func(m *Model) { m.width = width } }

// New returns a hidden prompt with default keys and styles.
func New(opts ...Option) Model {
	m := Model{
		KeyMap: DefaultKeyMap(),
		styles: DefaultStyles(true),
	}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

// Open shows the prompt. body is shown as a single-line preview, and onConfirm
// may be nil for a confirmation with no follow-up action.
func (m *Model) Open(title, body string, onConfirm ConfirmFunc) {
	m.title = title
	m.body = body
	m.onConfirm = onConfirm
	m.visible = true
}

// Close hides the prompt and clears its state.
func (m *Model) Close() {
	m.visible = false
	m.title = ""
	m.body = ""
	m.onConfirm = nil
}

// IsVisible reports whether the prompt is showing.
func (m Model) IsVisible() bool { return m.visible }

// SetWidth sets the terminal width the box is sized against. Height follows
// the content, so there is no SetHeight.
func (m *Model) SetWidth(width int) { m.width = width }

// Styles returns the prompt's current styles.
func (m Model) Styles() Styles { return m.styles }

// SetStyles replaces the prompt's styles.
func (m *Model) SetStyles(s Styles) { m.styles = s }

// Init does nothing; the prompt has no startup work.
func (m Model) Init() tea.Cmd { return nil }
