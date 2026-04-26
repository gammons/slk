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
	ta.Prompt = ""
	ta.SetWidth(40)

	// Override textarea styles to use our dark background consistently
	bg := lipgloss.NewStyle().Background(styles.SurfaceDark)
	ta.FocusedStyle.Base = bg
	ta.FocusedStyle.Text = bg
	ta.FocusedStyle.CursorLine = bg
	ta.FocusedStyle.EndOfBuffer = bg
	ta.FocusedStyle.Prompt = bg
	ta.BlurredStyle.Base = bg
	ta.BlurredStyle.Text = bg
	ta.BlurredStyle.CursorLine = bg
	ta.BlurredStyle.EndOfBuffer = bg
	ta.BlurredStyle.Prompt = bg
	ta.FocusedStyle.Placeholder = bg.Foreground(styles.TextMuted)
	ta.BlurredStyle.Placeholder = bg.Foreground(styles.TextMuted)

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
	m.input.Placeholder = "" // hide placeholder when focused
	return m.input.Focus()
}

func (m *Model) Blur() {
	m.input.Placeholder = "Message #" + m.channelName + "... (i to insert)"
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
	// Auto-grow: adjust height to match line count.
	// After SetHeight, we must force a full viewport recalculation because
	// SetHeight doesn't reset the scroll offset. We do this by re-setting
	// the value (which rebuilds the viewport content and repositions).
	lines := m.input.LineCount()
	if lines < 1 {
		lines = 1
	}
	if lines > m.input.MaxHeight {
		lines = m.input.MaxHeight
	}
	if m.input.Height() != lines {
		m.input.SetHeight(lines)
		// Force viewport recalculation by re-setting the value.
		// This resets cursor to end of text, which is fine for a compose box.
		val := m.input.Value()
		m.input.SetValue(val)
	}
	return m, cmd
}

func (m Model) View(width int, focused bool) string {
	innerWidth := width - 5 // account for left border + left/right padding
	m.input.SetWidth(innerWidth)

	var style = styles.ComposeBox.Width(width - 2)
	if focused {
		style = styles.ComposeInsert.Width(width - 2)
	}

	// If empty and unfocused, render placeholder manually with correct background.
	// When focused, show an empty compose box with cursor (no placeholder).
	if m.input.Value() == "" && !focused {
		placeholder := lipgloss.NewStyle().
			Foreground(styles.TextMuted).
			Background(styles.SurfaceDark).
			Width(innerWidth).
			Render(m.input.Placeholder)
		return style.Render(placeholder)
	}

	// Wrap textarea output with full-width dark background.
	// The textarea's internal styles use Inline(true) which only covers text,
	// not the full line width. This wrapper ensures consistent background.
	content := lipgloss.NewStyle().
		Background(styles.SurfaceDark).
		Width(innerWidth).
		Render(m.input.View())
	return style.Render(content)
}
