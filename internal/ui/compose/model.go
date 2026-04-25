// internal/ui/compose/model.go
package compose

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slack-tui/internal/ui/styles"
)

type Model struct {
	input       textinput.Model
	channelName string
}

func New(channelName string) Model {
	ti := textinput.New()
	ti.Placeholder = "Message #" + channelName + "... (i to insert)"
	ti.CharLimit = 40000 // Slack's message limit
	ti.Width = 40

	return Model{
		input:       ti,
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
	m.input.SetValue("")
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) View(width int, focused bool) string {
	m.input.Width = width - 4 // account for padding/border

	var style lipgloss.Style
	if focused {
		style = styles.ComposeInsert.Width(width - 2)
	} else {
		style = styles.ComposeBox.Width(width - 2)
	}

	return style.Render(m.input.View())
}
