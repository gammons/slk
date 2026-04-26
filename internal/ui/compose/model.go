// internal/ui/compose/model.go
package compose

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/gammons/slk/internal/ui/mentionpicker"
	"github.com/gammons/slk/internal/ui/styles"
)

type Model struct {
	input       textarea.Model
	channelName string
	width       int // display width, set by SetWidth

	// Mention picker state
	mentionPicker   mentionpicker.Model
	mentionActive   bool
	mentionStartCol int // cursor column where @ was typed
	users           []mentionpicker.User
	reverseNames    map[string]string // displayName -> userID
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

// visualLineCount returns the number of visual lines the text occupies,
// accounting for soft wraps when a line exceeds the textarea width.
func (m Model) visualLineCount() int {
	val := m.input.Value()
	if val == "" {
		return 1
	}
	w := m.input.Width()
	if w <= 0 {
		return m.input.LineCount()
	}
	total := 0
	for _, line := range strings.Split(val, "\n") {
		lineWidth := runewidth.StringWidth(line)
		if lineWidth == 0 {
			total++
		} else {
			total += (lineWidth + w - 1) / w // ceiling division
		}
	}
	if total < 1 {
		total = 1
	}
	return total
}

// SetWidth updates the textarea's internal width so text wraps correctly.
func (m *Model) SetWidth(width int) {
	innerWidth := width - 5 // account for left border + left/right padding
	if innerWidth < 10 {
		innerWidth = 10
	}
	if m.width != width {
		m.width = width
		m.input.SetWidth(innerWidth)
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	// Auto-grow: adjust height to match visual line count (including wraps).
	// LineCount() only counts logical lines (\n). We also need to account
	// for soft wraps when a line exceeds the textarea width.
	lines := m.visualLineCount()
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

// SetUsers provides the list of workspace users for mention autocomplete.
func (m *Model) SetUsers(users []mentionpicker.User) {
	m.users = users
	m.mentionPicker.SetUsers(users)
	m.reverseNames = make(map[string]string)
	for _, u := range users {
		if u.DisplayName != "" {
			m.reverseNames[u.DisplayName] = u.ID
		}
	}
}

// IsMentionActive returns whether the mention picker is currently showing.
func (m Model) IsMentionActive() bool {
	return m.mentionActive
}

// CloseMention dismisses the mention picker without selecting.
func (m *Model) CloseMention() {
	m.mentionActive = false
	m.mentionPicker.Close()
}

// TranslateMentionsForSend replaces @DisplayName with <@UserID> in the text.
func (m Model) TranslateMentionsForSend(text string) string {
	// Handle special mentions first
	specials := map[string]string{
		"@here":     "<!here>",
		"@channel":  "<!channel>",
		"@everyone": "<!everyone>",
	}
	for name, replacement := range specials {
		text = strings.ReplaceAll(text, name, replacement)
	}
	if len(m.reverseNames) == 0 {
		return text
	}
	// Sort display names by length (longest first) to avoid partial matches
	names := make([]string, 0, len(m.reverseNames))
	for name := range m.reverseNames {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return len(names[i]) > len(names[j])
	})
	for _, name := range names {
		userID := m.reverseNames[name]
		text = strings.ReplaceAll(text, "@"+name, "<@"+userID+">")
	}
	return text
}

// MentionPickerView returns the rendered mention picker dropdown, or "" if not active.
func (m Model) MentionPickerView(width int) string {
	if !m.mentionActive {
		return ""
	}
	return m.mentionPicker.View(width)
}

// isAtWordBoundary checks if the character at the given column in the text
// is at a word boundary (preceded by space, newline, or at position 0).
func isAtWordBoundary(text string, col int) bool {
	if col == 0 {
		return true
	}
	lines := strings.Split(text, "\n")
	lastLine := lines[len(lines)-1]
	if col > len(lastLine) {
		return false
	}
	if col == 0 {
		return true
	}
	prev := lastLine[col-1]
	return prev == ' ' || prev == '\t'
}

func (m Model) View(width int, focused bool) string {
	innerWidth := width - 5 // account for left border + left/right padding

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
