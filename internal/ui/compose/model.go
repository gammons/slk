// internal/ui/compose/model.go
package compose

import (
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slack-tui/internal/ui/styles"
)

type Model struct {
	input       textarea.Model
	channelName string
}

func New(channelName string) Model {
	ta := textarea.New()
	ta.Placeholder = "Message #" + channelName + "... (i to insert)"
	ta.CharLimit = 40000
	ta.MaxHeight = 5
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.Prompt = "> "
	ta.SetWidth(40)

	// Override default textarea styles to remove background colors
	// that clash with our dark theme
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.EndOfBuffer = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.EndOfBuffer = lipgloss.NewStyle()

	return Model{
		input:       ta,
		channelName: channelName,
	}
}

func (m *Model) SetChannel(name string) {
	m.channelName = name
	m.input.Placeholder = "Message #" + name + "... (i to insert)"
}

func (m *Model) Focus() tea.Cmd {
	return m.input.Focus()
}

func (m *Model) Blur() {
	m.input.Blur()
}

func (m *Model) Value() string {
	return m.input.Value()
}

func (m *Model) SetValue(s string) {
	m.input.SetValue(s)
}

func (m *Model) Reset() {
	m.input.Reset()
	m.input.SetHeight(1)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	// Auto-grow height based on the textarea's actual line count.
	// We must set height and then re-run Update with a nil msg so the
	// textarea's repositionView() recalculates scroll for the new height.
	lines := m.input.LineCount()
	if lines < 1 {
		lines = 1
	}
	if lines > m.input.MaxHeight {
		lines = m.input.MaxHeight
	}
	if m.input.Height() != lines {
		m.input.SetHeight(lines)
		// Trigger viewport reposition with the new height
		m.input, _ = m.input.Update(nil)
	}
	return m, cmd
}

func (m Model) View(width int, focused bool) string {
	m.input.SetWidth(width - 4) // account for padding/border

	var style = styles.ComposeBox.Width(width - 2)
	if focused {
		style = styles.ComposeInsert.Width(width - 2)
	}

	return style.Render(m.input.View())
}
