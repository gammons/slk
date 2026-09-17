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
	Styles Styles

	width     int
	height    int
	visible   bool
	title     string
	body      string
	onConfirm ConfirmFunc
}

// New returns a hidden prompt with default keys and styles.
func New() Model {
	return Model{
		KeyMap: DefaultKeyMap(),
		Styles: DefaultStyles(true),
	}
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

// SetSize records the terminal dimensions the box is sized against.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Init does nothing; the prompt has no startup work.
func (m Model) Init() tea.Cmd { return nil }
